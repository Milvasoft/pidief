// Package server exposes the ops registry as a REST API.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/milvasoft/pidief/internal/ops"
)

// Version is set at build time.
var Version = "dev"

// Server is the HTTP API.
type Server struct {
	cfg     Config
	env     *ops.Env
	log     *slog.Logger
	limiter *limiter
	jobs    *jobStore
	metrics *metrics
	mux     *http.ServeMux

	readyMu  sync.Mutex
	readyAt  time.Time
	readyErr error
	shutdown atomic.Bool
}

// New builds the server.
func New(cfg Config, env *ops.Env, log *slog.Logger, reg prometheus.Registerer) *Server {
	s := &Server{
		cfg: cfg, env: env, log: log,
		limiter: newLimiter(cfg.Concurrency, cfg.QueueSize),
		jobs:    newJobStore(cfg.JobTTL, cfg.MaxJobs),
		metrics: newMetrics(reg),
		mux:     http.NewServeMux(),
	}
	s.metrics.inflightFn(func() float64 { return float64(s.limiter.running()) }, func() float64 { return float64(s.limiter.waiting()) })
	for _, op := range ops.All() {
		s.mux.Handle("POST "+op.Path, s.auth(s.opHandler(op)))
	}
	s.mux.Handle("GET /v1/jobs/{id}", s.auth(http.HandlerFunc(s.jobStatus)))
	s.mux.Handle("GET /v1/jobs/{id}/result", s.auth(http.HandlerFunc(s.jobResult)))
	s.mux.Handle("DELETE /v1/jobs/{id}", s.auth(http.HandlerFunc(s.jobDelete)))
	s.mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /health/ready", s.ready)
	metricsH := promhttp.Handler()
	if g, ok := reg.(prometheus.Gatherer); ok {
		metricsH = promhttp.HandlerFor(g, promhttp.HandlerOpts{})
	}
	if !cfg.MetricsPublic {
		metricsH = s.auth(metricsH)
	}
	s.mux.Handle("GET /metrics", metricsH)
	s.mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(OpenAPI())
	})
	s.mux.HandleFunc("GET /docs", s.docs)
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"name": "pidief", "version": Version, "openapi": "/openapi.json", "docs": "/docs"})
	})
	return s
}

// Handler returns the root handler with logging, request ids and panic recovery.
func (s *Server) Handler() http.Handler { return s.middleware(s.mux) }

// StartShutdown makes /health/ready fail so load balancers drain the instance.
func (s *Server) StartShutdown() { s.shutdown.Store(true) }

// Close stops background work.
func (s *Server) Close() { s.jobs.close() }

// ---------- middleware ----------

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

type ctxKey int

const reqIDKey ctxKey = 1

func requestID(r *http.Request) string {
	if v, ok := r.Context().Value(reqIDKey).(string); ok {
		return v
	}
	return ""
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = newID()
		}
		w.Header().Set("X-Request-Id", id)
		r = r.WithContext(context.WithValue(r.Context(), reqIDKey, id))
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "requestId", id, "panic", fmt.Sprint(rec))
				if sw.status == 0 {
					problem(sw, r, http.StatusInternalServerError, "internal error")
				}
			}
			if strings.HasPrefix(r.URL.Path, "/health") || r.URL.Path == "/metrics" {
				return
			}
			s.log.Info("request", "requestId", id, "method", r.Method, "path", r.URL.Path,
				"status", sw.status, "bytesOut", sw.bytes, "bytesIn", r.ContentLength,
				"durationMs", time.Since(start).Milliseconds(), "remote", r.RemoteAddr)
		}()
		next.ServeHTTP(sw, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.cfg.APIKeys) > 0 {
			key := r.Header.Get("X-Api-Key")
			ok := false
			for _, k := range s.cfg.APIKeys {
				if subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
					ok = true
				}
			}
			if !ok {
				problem(w, r, http.StatusUnauthorized, "missing or invalid X-Api-Key")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- responses ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// Problem is the error body (RFC 9457).
type Problem struct {
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	RequestID string `json:"requestId,omitempty"`
}

func problem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Title: http.StatusText(status), Status: status, Detail: detail, RequestID: requestID(r)})
}

func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(name))
}

func writeOutput(w http.ResponseWriter, r *http.Request, op *ops.Op, out *ops.Output) {
	if out.JSON != nil {
		writeJSON(w, http.StatusOK, out.JSON)
		return
	}
	if len(out.Files) == 1 && op.Output != "pdfs" {
		f := out.Files[0]
		w.Header().Set("Content-Type", f.ContentType)
		w.Header().Set("Content-Disposition", contentDisposition(f.Name))
		w.Header().Set("Content-Length", strconv.Itoa(len(f.Data)))
		_, _ = w.Write(f.Data)
		return
	}
	z, err := ops.Zip(out.Files)
	if err != nil {
		problem(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(strings.TrimSuffix(path.Base(op.Path), "/")+".zip"))
	w.Header().Set("X-Pidief-File-Count", strconv.Itoa(len(out.Files)))
	_, _ = w.Write(z)
}

// ---------- request parsing ----------

type jsonFile struct {
	Name    string `json:"name"`
	Content string `json:"content"` // base64
}

// parseRequest returns input files and bound params. Supported bodies:
// multipart/form-data (file parts + fields), application/json ({files:[{name,content}], ...params}),
// or a raw document (application/pdf, image/*, text/html) with params in the query string.
func (s *Server) parseRequest(r *http.Request, op *ops.Op) ([]ops.File, any, error) {
	params := op.Params()
	query := url.Values{}
	for k, v := range r.URL.Query() {
		if k != "async" {
			query[k] = v
		}
	}
	var files []ops.File
	ct, mp, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch {
	case ct == "multipart/form-data":
		mr, err := r.MultipartReader()
		if err != nil {
			return nil, nil, err
		}
		form := url.Values{}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, err
			}
			data, err := io.ReadAll(part)
			if err != nil {
				return nil, nil, err
			}
			name := part.FormName()
			if fn := rawFileName(part); fn != "" {
				files = append(files, ops.File{Name: cleanName(fn, len(files)), ContentType: part.Header.Get("Content-Type"), Data: data})
				continue
			}
			if ti := textInput(op, name); ti != nil {
				files = append(files, ops.File{Name: ti.File, Data: data})
				continue
			}
			form.Add(name, string(data))
		}
		_ = mp
		for k, v := range query {
			form[k] = append(form[k], v...)
		}
		if err := ops.BindForm(params, form); err != nil {
			return nil, nil, err
		}
	case ct == "application/json":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, nil, err
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, nil, fmt.Errorf("body is not a JSON object: %v", err)
		}
		if fr, ok := raw["files"]; ok {
			var jf []jsonFile
			if err := json.Unmarshal(fr, &jf); err != nil {
				return nil, nil, fmt.Errorf("files: %v", err)
			}
			for i, f := range jf {
				data, err := base64.StdEncoding.DecodeString(f.Content)
				if err != nil {
					return nil, nil, fmt.Errorf("files[%d].content is not base64", i)
				}
				files = append(files, ops.File{Name: cleanName(f.Name, i), Data: data})
			}
			delete(raw, "files")
		}
		for _, ti := range op.TextInputs {
			if v, ok := raw[ti.Field]; ok {
				var sv string
				if err := json.Unmarshal(v, &sv); err != nil {
					return nil, nil, fmt.Errorf("%s must be a string", ti.Field)
				}
				files = append([]ops.File{{Name: ti.File, Data: []byte(sv)}}, files...)
				delete(raw, ti.Field)
			}
		}
		// Template data may be an object instead of a JSON string.
		if d, ok := raw["data"]; ok && len(d) > 0 && d[0] != '"' {
			enc, _ := json.Marshal(string(d))
			raw["data"] = enc
		}
		rest, _ := json.Marshal(raw)
		if err := ops.BindJSON(params, rest); err != nil {
			return nil, nil, err
		}
		if err := ops.BindForm(params, query); err != nil {
			return nil, nil, err
		}
	default:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, nil, err
		}
		if len(body) > 0 {
			name := query.Get("filename")
			query.Del("filename")
			if name == "" {
				name = "input" + extFor(ct)
			}
			files = append(files, ops.File{Name: cleanName(name, 0), ContentType: ct, Data: body})
		}
		if err := ops.BindForm(params, query); err != nil {
			return nil, nil, err
		}
	}
	return files, params, nil
}

// rawFileName keeps relative paths (css/site.css): multipart.Part.FileName drops directories.
func rawFileName(p *multipart.Part) string {
	_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	if err != nil {
		return p.FileName()
	}
	return params["filename"]
}

func extFor(ct string) string {
	switch {
	case ct == "application/pdf":
		return ".pdf"
	case ct == "text/html":
		return ".html"
	case strings.HasPrefix(ct, "image/"):
		return "." + strings.TrimPrefix(ct, "image/")
	}
	return ""
}

func textInput(op *ops.Op, name string) *ops.TextInput {
	for i := range op.TextInputs {
		if op.TextInputs[i].Field == name {
			return &op.TextInputs[i]
		}
	}
	return nil
}

// cleanName keeps relative asset paths (img/logo.png) but strips traversal and drive letters.
func cleanName(name string, i int) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if j := strings.Index(name, ":"); j >= 0 {
		name = name[j+1:]
	}
	name = strings.TrimLeft(path.Clean("/"+name), "/")
	if name == "" || name == "." {
		name = fmt.Sprintf("file-%d", i+1)
	}
	return name
}

// ---------- operation handler ----------

func (s *Server) opHandler(op *ops.Op) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := "ok"
		defer func() { s.metrics.observe(op.Path, status, time.Since(start)) }()

		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxRequestBytes)
		files, params, err := s.parseRequest(r, op)
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				status = "too_large"
				problem(w, r, http.StatusRequestEntityTooLarge, fmt.Sprintf("request body is larger than %d MB", s.cfg.MaxRequestBytes>>20))
				return
			}
			status = "invalid"
			problem(w, r, http.StatusBadRequest, err.Error())
			return
		}
		if err := op.Validate(files); err != nil {
			status = "invalid"
			problem(w, r, http.StatusBadRequest, err.Error())
			return
		}
		var inBytes int
		for _, f := range files {
			inBytes += len(f.Data)
		}
		s.metrics.inputBytes.WithLabelValues(op.Path).Observe(float64(inBytes))

		if isAsync(r) {
			job, err := s.jobs.add(op)
			if err != nil {
				status = "rejected"
				problem(w, r, http.StatusTooManyRequests, err.Error())
				return
			}
			go s.runJob(job, op, files, params, requestID(r)) //nolint:gosec // G118: the job must outlive the request
			status = "accepted"
			w.Header().Set("Location", "/v1/jobs/"+job.ID)
			writeJSON(w, http.StatusAccepted, job.view())
			return
		}

		release, err := s.limiter.acquire(r.Context(), s.cfg.QueueTimeout)
		if err != nil {
			status = "busy"
			if errors.Is(err, errQueueFull) {
				w.Header().Set("Retry-After", "5")
				problem(w, r, http.StatusTooManyRequests, err.Error())
			} else {
				w.Header().Set("Retry-After", "10")
				problem(w, r, http.StatusServiceUnavailable, err.Error())
			}
			return
		}
		out, err := op.Run(r.Context(), s.env, files, params)
		release()
		if err != nil {
			status = s.writeRunError(w, r, op, err)
			return
		}
		writeOutput(w, r, op, out)
	})
}

func isAsync(r *http.Request) bool {
	if v := r.URL.Query().Get("async"); v != "" {
		b, _ := strconv.ParseBool(v)
		return b
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "respond-async")
}

func (s *Server) writeRunError(w http.ResponseWriter, r *http.Request, op *ops.Op, err error) string {
	switch {
	case ops.IsClientError(err):
		problem(w, r, http.StatusBadRequest, err.Error())
		return "invalid"
	case errors.Is(err, context.DeadlineExceeded):
		problem(w, r, http.StatusGatewayTimeout, "operation timed out: "+err.Error())
		return "timeout"
	case errors.Is(err, context.Canceled):
		problem(w, r, 499, "client closed the request")
		return "canceled"
	}
	s.log.Error("operation failed", "requestId", requestID(r), "op", op.Path, "error", err.Error())
	problem(w, r, http.StatusInternalServerError, err.Error())
	return "error"
}

func (s *Server) runJob(job *job, op *ops.Op, files []ops.File, params any, reqID string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.MaxTimeout+s.cfg.QueueTimeout*10)
	defer cancel()
	s.jobs.setCancel(job, cancel)
	release, err := s.limiter.acquire(ctx, s.cfg.QueueTimeout*10)
	if err != nil {
		s.jobs.finish(job, nil, err)
		return
	}
	defer release()
	s.jobs.start(job)
	start := time.Now()
	out, err := op.Run(ctx, s.env, files, params)
	status := "ok"
	if err != nil {
		status = "error"
		if ops.IsClientError(err) {
			status = "invalid"
		}
		s.log.Warn("job failed", "requestId", reqID, "jobId", job.ID, "op", op.Path, "error", err.Error())
	}
	s.metrics.observe(op.Path, "job_"+status, time.Since(start))
	s.jobs.finish(job, out, err)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.shutdown.Load() {
		problem(w, r, http.StatusServiceUnavailable, "shutting down")
		return
	}
	s.readyMu.Lock()
	if time.Since(s.readyAt) > 15*time.Second {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		s.readyErr = s.env.Browser.Ready(ctx)
		cancel()
		s.readyAt = time.Now()
	}
	err := s.readyErr
	s.readyMu.Unlock()
	if err != nil {
		problem(w, r, http.StatusServiceUnavailable, "chromium: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "running": s.limiter.running(), "waiting": s.limiter.waiting(), "concurrency": s.cfg.Concurrency,
	})
}

func (s *Server) docs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><html><head><meta charset="utf-8"><title>pidief API</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head><body>
<script id="api-reference" data-url="/openapi.json"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.25.0"></script>
</body></html>`)
}

// ---------- concurrency limiter ----------

var errQueueFull = errors.New("server is busy: the queue is full, retry later")

type limiter struct {
	slots   chan struct{}
	maxWait int64
	wait    atomic.Int64
}

func newLimiter(n, queue int) *limiter {
	return &limiter{slots: make(chan struct{}, n), maxWait: int64(queue)}
}

func (l *limiter) running() int { return len(l.slots) }
func (l *limiter) waiting() int { return int(l.wait.Load()) }

func (l *limiter) acquire(ctx context.Context, timeout time.Duration) (func(), error) {
	select {
	case l.slots <- struct{}{}:
		return l.release, nil
	default:
	}
	if l.wait.Add(1) > l.maxWait {
		l.wait.Add(-1)
		return nil, errQueueFull
	}
	defer l.wait.Add(-1)
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case l.slots <- struct{}{}:
		return l.release, nil
	case <-t.C:
		return nil, fmt.Errorf("server is busy: no free slot within %s", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *limiter) release() { <-l.slots }

// ---------- metrics ----------

type metrics struct {
	requests   *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	inputBytes *prometheus.HistogramVec
	reg        prometheus.Registerer
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		reg: reg,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pidief_operations_total", Help: "Operations by endpoint and outcome.",
		}, []string{"op", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "pidief_operation_duration_seconds", Help: "Operation duration.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30, 60, 120},
		}, []string{"op"}),
		inputBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "pidief_input_bytes", Help: "Size of the input files per request.",
			Buckets: prometheus.ExponentialBuckets(1024, 4, 10),
		}, []string{"op"}),
	}
	reg.MustRegister(m.requests, m.duration, m.inputBytes)
	return m
}

func (m *metrics) inflightFn(running, waiting func() float64) {
	m.reg.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "pidief_operations_running", Help: "Operations running now."}, running),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "pidief_operations_waiting", Help: "Operations waiting for a slot."}, waiting),
	)
}

func (m *metrics) observe(op, status string, d time.Duration) {
	m.requests.WithLabelValues(op, status).Inc()
	m.duration.WithLabelValues(op).Observe(d.Seconds())
}
