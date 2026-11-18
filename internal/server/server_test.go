package server_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/milvasoft/pidief/internal/chromium"
	"github.com/milvasoft/pidief/internal/imgpdf"
	"github.com/milvasoft/pidief/internal/ops"
	"github.com/milvasoft/pidief/internal/raster"
	"github.com/milvasoft/pidief/internal/server"
	"github.com/milvasoft/pidief/internal/testutil"
)

var (
	env     *ops.Env
	browser bool
)

func TestMain(m *testing.M) {
	env = &ops.Env{
		Browser:       chromium.New(chromium.Config{ExecPath: chromium.ExecPathFromEnv(), NoSandbox: os.Getenv("PIDIEF_CHROMIUM_NO_SANDBOX") == "true"}),
		Raster:        raster.New(2),
		AllowExternal: false,
		MaxTimeout:    time.Minute,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	browser = env.Browser.Ready(ctx) == nil
	cancel()
	code := m.Run()
	env.Browser.Close()
	env.Raster.Close()
	os.Exit(code)
}

func newServer(t *testing.T, mut func(*server.Config)) *httptest.Server {
	t.Helper()
	cfg := server.Config{
		MaxRequestBytes: 8 << 20, Concurrency: 2, QueueSize: 10, QueueTimeout: 10 * time.Second,
		MaxTimeout: time.Minute, JobTTL: time.Minute, MaxJobs: 10, MetricsPublic: true,
	}
	if mut != nil {
		mut(&cfg)
	}
	s := server.New(cfg, env, slog.New(slog.NewTextHandler(io.Discard, nil)), prometheus.NewRegistry())
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); s.Close() })
	return ts
}

func samplePDF(t *testing.T, pages int) []byte {
	t.Helper()
	var in []imgpdf.Input
	for i := 0; i < pages; i++ {
		in = append(in, imgpdf.Input{Name: "p.png", Data: testutil.PNG(t, testutil.Gradient(200, 280))})
	}
	pdf, err := imgpdf.Convert(context.Background(), in, imgpdf.Options{PageSize: "A4"})
	if err != nil {
		t.Fatal(err)
	}
	return pdf
}

type part struct {
	field, filename string
	data            []byte
}

func multipartBody(t *testing.T, parts []part) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.filename != "" {
			fw, _ := w.CreateFormFile(p.field, p.filename)
			fw.Write(p.data)
		} else {
			_ = w.WriteField(p.field, string(p.data))
		}
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func do(t *testing.T, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func post(t *testing.T, url, ct string, body io.Reader, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, body)
	req.Header.Set("Content-Type", ct)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return do(t, req)
}

func TestHTMLMultipartAndJSONTemplate(t *testing.T) {
	if !browser {
		t.Skip("chromium not available")
	}
	ts := newServer(t, nil)
	body, ct := multipartBody(t, []part{
		{"files", "index.html", []byte(`<html><head><link rel="stylesheet" href="css/a.css"></head><body><h1>Şantiye günlüğü</h1></body></html>`)},
		{"files", "css/a.css", []byte(`h1{color:#3D45AA;font-family:Onest}`)},
		{"pageNumbers", "", []byte("true")},
		{"failOnResourceError", "", []byte("true")},
		{"paper", "", []byte("A5")},
	})
	res, b := post(t, ts.URL+"/v1/html", ct, body, nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/pdf" || !ops.IsPDF(b) {
		t.Fatalf("html: %d %s %.200s", res.StatusCode, res.Header.Get("Content-Type"), b)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "document.pdf") {
		t.Errorf("content-disposition = %q", cd)
	}

	js, _ := json.Marshal(map[string]any{
		"template": `<p>{{firma}} – {{para tutar}}</p>`,
		"data":     map[string]any{"firma": "Örnek Yapı", "tutar": 1500.5},
		"title":    "Makbuz",
	})
	res, b = post(t, ts.URL+"/v1/template", "application/json", bytes.NewReader(js), nil)
	if res.StatusCode != 200 || !ops.IsPDF(b) {
		t.Fatalf("template: %d %.300s", res.StatusCode, b)
	}
	res, b = post(t, ts.URL+"/v1/text", "application/pdf", bytes.NewReader(b), nil)
	if res.StatusCode != 200 || !strings.Contains(string(b), "Örnek Yapı – ₺1.500,50") {
		t.Fatalf("text: %d %s", res.StatusCode, b)
	}

	// External resources are refused by this server.
	body, ct = multipartBody(t, []part{{"html", "", []byte("<p>x</p>")}, {"allowExternal", "", []byte("true")}})
	res, b = post(t, ts.URL+"/v1/html", ct, body, nil)
	if res.StatusCode != 400 || !strings.Contains(string(b), "allowExternal") {
		t.Fatalf("allowExternal: %d %s", res.StatusCode, b)
	}
}

func TestPDFEndpointsRawJSONAndZip(t *testing.T) {
	ts := newServer(t, nil)
	pdf := samplePDF(t, 4)

	res, b := post(t, ts.URL+"/v1/info?filename=belge.pdf", "application/pdf", bytes.NewReader(pdf), nil)
	var info struct {
		PageCount int `json:"pageCount"`
	}
	if res.StatusCode != 200 || json.Unmarshal(b, &info) != nil || info.PageCount != 4 {
		t.Fatalf("info: %d %s", res.StatusCode, b)
	}

	js, _ := json.Marshal(map[string]any{
		"files":   []map[string]string{{"name": "a.pdf", "content": base64.StdEncoding.EncodeToString(pdf)}},
		"pages":   "1-2",
		"degrees": 180,
	})
	res, b = post(t, ts.URL+"/v1/pages/rotate", "application/json", bytes.NewReader(js), nil)
	if res.StatusCode != 200 || !ops.IsPDF(b) {
		t.Fatalf("rotate: %d %.200s", res.StatusCode, b)
	}

	res, b = post(t, ts.URL+"/v1/split?span=3", "application/pdf", bytes.NewReader(pdf), nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("split: %d %s", res.StatusCode, b)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("zip: %v %d", err, len(zr.File))
	}

	body, ct := multipartBody(t, []part{{"files", "a.pdf", pdf}, {"files", "b.pdf", pdf}})
	res, b = post(t, ts.URL+"/v1/merge", ct, body, nil)
	if res.StatusCode != 200 || !ops.IsPDF(b) {
		t.Fatalf("merge: %d %.200s", res.StatusCode, b)
	}

	res, b = post(t, ts.URL+"/v1/render?pages=2&format=jpeg&maxWidth=120", "application/pdf", bytes.NewReader(pdf), nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || len(b) < 100 {
		t.Fatalf("render: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	// Validation errors are 400 with a problem body.
	for _, c := range []struct{ url, ct string }{
		{"/v1/pages/delete?pages=", "application/pdf"},
		{"/v1/pages/rotate?degrees=45", "application/pdf"},
		{"/v1/info?nope=1", "application/pdf"},
	} {
		res, b = post(t, ts.URL+c.url, c.ct, bytes.NewReader(pdf), nil)
		if res.StatusCode != 400 || !strings.Contains(res.Header.Get("Content-Type"), "problem+json") {
			t.Errorf("%s: %d %s", c.url, res.StatusCode, b)
		}
	}
	res, _ = post(t, ts.URL+"/v1/merge", "application/pdf", strings.NewReader("not a pdf"), nil)
	if res.StatusCode != 400 {
		t.Errorf("merge garbage: %d", res.StatusCode)
	}
}

func TestAuthSizeLimitAndHealth(t *testing.T) {
	ts := newServer(t, func(c *server.Config) {
		c.APIKeys = []string{"anahtar-1", "anahtar-2"}
		c.MaxRequestBytes = 64 << 10
		c.MetricsPublic = true
	})
	pdf := samplePDF(t, 1)
	res, _ := post(t, ts.URL+"/v1/info", "application/pdf", bytes.NewReader(pdf), nil)
	if res.StatusCode != 401 {
		t.Fatalf("no key: %d", res.StatusCode)
	}
	res, _ = post(t, ts.URL+"/v1/info", "application/pdf", bytes.NewReader(pdf), map[string]string{"X-Api-Key": "anahtar-2"})
	if res.StatusCode != 200 {
		t.Fatalf("with key: %d", res.StatusCode)
	}
	if res.Header.Get("X-Request-Id") == "" {
		t.Error("missing X-Request-Id")
	}
	big := bytes.Repeat([]byte("x"), 100<<10)
	res, _ = post(t, ts.URL+"/v1/info", "application/pdf", bytes.NewReader(big), map[string]string{"X-Api-Key": "anahtar-1"})
	if res.StatusCode != 413 {
		t.Fatalf("too large: %d", res.StatusCode)
	}
	for _, p := range []string{"/health/live", "/openapi.json", "/metrics"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+p, nil)
		res, b := do(t, req)
		if res.StatusCode != 200 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
		switch p {
		case "/openapi.json":
			var doc map[string]any
			if err := json.Unmarshal(b, &doc); err != nil || doc["openapi"] != "3.1.0" {
				t.Errorf("openapi: %v", err)
			}
			paths := doc["paths"].(map[string]any)
			for _, op := range ops.All() {
				if _, ok := paths[op.Path]; !ok {
					t.Errorf("openapi lacks %s", op.Path)
				}
			}
		case "/metrics":
			if !strings.Contains(string(b), `pidief_operations_total{op="/v1/info",status="ok"} 1`) {
				t.Errorf("metrics missing counter:\n%.500s", b)
			}
		}
	}
}

func TestAsyncJob(t *testing.T) {
	ts := newServer(t, nil)
	pdf := samplePDF(t, 2)
	res, b := post(t, ts.URL+"/v1/pages/select?pages=2,1,2&async=true", "application/pdf", bytes.NewReader(pdf), nil)
	if res.StatusCode != 202 {
		t.Fatalf("async: %d %s", res.StatusCode, b)
	}
	var job server.JobView
	_ = json.Unmarshal(b, &job)
	if job.ID == "" || res.Header.Get("Location") != "/v1/jobs/"+job.ID {
		t.Fatalf("job = %+v", job)
	}
	deadline := time.Now().Add(20 * time.Second)
	for job.State != server.JobSucceeded {
		if job.State == server.JobFailed || time.Now().After(deadline) {
			t.Fatalf("job = %+v", job)
		}
		time.Sleep(50 * time.Millisecond)
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/jobs/"+job.ID, nil)
		_, b = do(t, req)
		_ = json.Unmarshal(b, &job)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+job.ResultURL, nil)
	res, b = do(t, req)
	if res.StatusCode != 200 || !ops.IsPDF(b) {
		t.Fatalf("result: %d", res.StatusCode)
	}
	res, b = post(t, ts.URL+"/v1/text", "application/pdf", bytes.NewReader(b), nil)
	if res.StatusCode != 200 || strings.Count(string(b), `"number"`) != 3 {
		t.Fatalf("result pages: %s", b)
	}
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/v1/jobs/"+job.ID, nil)
	if res, _ = do(t, req); res.StatusCode != 204 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/v1/jobs/"+job.ID, nil)
	if res, _ = do(t, req); res.StatusCode != 404 {
		t.Fatalf("deleted job: %d", res.StatusCode)
	}

	// A failing job reports the error kind.
	_, b = post(t, ts.URL+"/v1/decrypt?async=true", "application/pdf", bytes.NewReader(pdf), nil)
	_ = json.Unmarshal(b, &job)
	for i := 0; i < 200 && job.State != server.JobFailed; i++ {
		time.Sleep(25 * time.Millisecond)
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/jobs/"+job.ID, nil)
		_, b = do(t, req)
		_ = json.Unmarshal(b, &job)
	}
	if job.State != server.JobFailed || job.ErrorKind != "invalid" {
		t.Fatalf("failed job = %+v", job)
	}
}

func TestQueueFull(t *testing.T) {
	if !browser {
		t.Skip("chromium not available")
	}
	ts := newServer(t, func(c *server.Config) { c.Concurrency = 1; c.QueueSize = 0; c.QueueTimeout = time.Second })
	slow := `<p id="x"></p><script>setTimeout(()=>window.ready=true,1500)</script>`
	done := make(chan int, 1)
	go func() {
		body, ct := multipartBody(t, []part{{"html", "", []byte(slow)}, {"waitForExpression", "", []byte("window.ready")}})
		res, _ := post(t, ts.URL+"/v1/html", ct, body, nil)
		done <- res.StatusCode
	}()
	time.Sleep(300 * time.Millisecond)
	res, _ := post(t, ts.URL+"/v1/info", "application/pdf", bytes.NewReader(samplePDF(t, 1)), nil)
	if res.StatusCode != 429 || res.Header.Get("Retry-After") == "" {
		t.Fatalf("want 429, got %d", res.StatusCode)
	}
	if code := <-done; code != 200 {
		t.Fatalf("slow request: %d", code)
	}
}
