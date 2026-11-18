package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/milvasoft/pidief/internal/ops"
)

// Job states.
const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

type job struct {
	ID       string
	Op       string
	State    string
	Created  time.Time
	Started  time.Time
	Finished time.Time
	Err      error
	Out      *ops.Output
	cancel   context.CancelFunc
}

// JobView is the JSON form of a job.
type JobView struct {
	ID         string     `json:"id"`
	Operation  string     `json:"operation"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
	ErrorKind  string     `json:"errorKind,omitempty"`
	ResultURL  string     `json:"resultUrl,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

func (j *job) view() JobView {
	v := JobView{ID: j.ID, Operation: j.Op, State: j.State, CreatedAt: j.Created}
	if !j.Started.IsZero() {
		t := j.Started
		v.StartedAt = &t
	}
	if !j.Finished.IsZero() {
		t := j.Finished
		v.FinishedAt = &t
	}
	if j.Err != nil {
		v.Error = j.Err.Error()
		v.ErrorKind = "server"
		if ops.IsClientError(j.Err) {
			v.ErrorKind = "invalid"
		} else if errors.Is(j.Err, context.DeadlineExceeded) {
			v.ErrorKind = "timeout"
		}
	}
	if j.State == JobSucceeded {
		v.ResultURL = "/v1/jobs/" + j.ID + "/result"
	}
	return v
}

type jobStore struct {
	mu   sync.Mutex
	jobs map[string]*job
	ttl  time.Duration
	max  int
	stop chan struct{}
}

func newJobStore(ttl time.Duration, max int) *jobStore {
	s := &jobStore{jobs: map[string]*job{}, ttl: ttl, max: max, stop: make(chan struct{})}
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.expire()
			case <-s.stop:
				return
			}
		}
	}()
	return s
}

func (s *jobStore) close() { close(s.stop) }

func (s *jobStore) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.jobs {
		if !j.Finished.IsZero() && time.Since(j.Finished) > s.ttl {
			delete(s.jobs, id)
		}
	}
}

var errTooManyJobs = errors.New("too many jobs in memory, fetch or delete finished jobs first")

func (s *jobStore) add(op *ops.Op) (*job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) >= s.max {
		// Drop the oldest finished job to make room.
		var oldest *job
		for _, j := range s.jobs {
			if !j.Finished.IsZero() && (oldest == nil || j.Finished.Before(oldest.Finished)) {
				oldest = j
			}
		}
		if oldest == nil {
			return nil, errTooManyJobs
		}
		delete(s.jobs, oldest.ID)
	}
	j := &job{ID: newID(), Op: op.Path, State: JobQueued, Created: time.Now().UTC()}
	s.jobs[j.ID] = j
	return j, nil
}

func (s *jobStore) setCancel(j *job, c context.CancelFunc) {
	s.mu.Lock()
	j.cancel = c
	s.mu.Unlock()
}

func (s *jobStore) start(j *job) {
	s.mu.Lock()
	j.State, j.Started = JobRunning, time.Now().UTC()
	s.mu.Unlock()
}

func (s *jobStore) finish(j *job, out *ops.Output, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j.Finished = time.Now().UTC()
	if err != nil {
		j.State, j.Err = JobFailed, err
		return
	}
	j.State, j.Out = JobSucceeded, out
}

func (s *jobStore) get(id string) (*job, JobView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, JobView{}, false
	}
	v := j.view()
	if !j.Finished.IsZero() {
		e := j.Finished.Add(s.ttl)
		v.ExpiresAt = &e
	}
	return j, v, true
}

func (s *jobStore) remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if ok {
		if j.cancel != nil {
			j.cancel()
		}
		delete(s.jobs, id)
	}
	return ok
}

func (s *Server) jobStatus(w http.ResponseWriter, r *http.Request) {
	_, v, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		problem(w, r, http.StatusNotFound, "job not found (unknown or expired)")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) jobResult(w http.ResponseWriter, r *http.Request) {
	j, v, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		problem(w, r, http.StatusNotFound, "job not found (unknown or expired)")
		return
	}
	switch v.State {
	case JobSucceeded:
		op := ops.ByPath(j.Op)
		writeOutput(w, r, op, j.Out)
	case JobFailed:
		status := http.StatusInternalServerError
		if v.ErrorKind == "invalid" {
			status = http.StatusBadRequest
		} else if v.ErrorKind == "timeout" {
			status = http.StatusGatewayTimeout
		}
		problem(w, r, status, v.Error)
	default:
		w.Header().Set("Retry-After", "1")
		problem(w, r, http.StatusConflict, "job is "+v.State)
	}
}

func (s *Server) jobDelete(w http.ResponseWriter, r *http.Request) {
	if !s.jobs.remove(r.PathValue("id")) {
		problem(w, r, http.StatusNotFound, "job not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
