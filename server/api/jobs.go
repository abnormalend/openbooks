// server/api/jobs.go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/google/uuid"
)

type JobType string

const (
	JobSearch   JobType = "search"
	JobDownload JobType = "download"
)

type Status string

const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusError    Status = "error"
)

// ErrQueueFull is returned by Enqueue when the per-type queue is at capacity.
var ErrQueueFull = errors.New("queue full")

// Job is a single search or download request tracked by the Registry.
// Fields are guarded by the Registry mutex; handlers only ever see copies
// obtained via Registry.Get / Registry.List.
type Job struct {
	ID         uuid.UUID
	Type       JobType
	Status     Status
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	Error      *APIError

	// Search fields
	Query       string
	Limit       int
	Results     []core.BookDetail // nil until the job completes
	ParseErrors int

	// Download fields
	Book     string
	Bytes    int64
	Size     int64
	Path     *string
	FileName *string
}

func NewSearchJob(query string, limit int) *Job {
	return &Job{ID: uuid.New(), Type: JobSearch, Status: StatusQueued, Query: query, Limit: limit}
}

func NewDownloadJob(book string) *Job {
	return &Job{ID: uuid.New(), Type: JobDownload, Status: StatusQueued, Book: book}
}

// MarshalJSON emits only the fields relevant to the job's type. Nullable
// fields are always present (as null) so clients get a stable shape.
func (j Job) MarshalJSON() ([]byte, error) {
	type common struct {
		ID         uuid.UUID  `json:"jobId"`
		Type       JobType    `json:"type"`
		Status     Status     `json:"status"`
		CreatedAt  time.Time  `json:"createdAt"`
		StartedAt  *time.Time `json:"startedAt"`
		FinishedAt *time.Time `json:"finishedAt"`
		Error      *APIError  `json:"error"`
	}
	c := common{j.ID, j.Type, j.Status, j.CreatedAt, j.StartedAt, j.FinishedAt, j.Error}

	switch j.Type {
	case JobSearch:
		return json.Marshal(struct {
			common
			Query       string            `json:"query"`
			Limit       int               `json:"limit"`
			Results     []core.BookDetail `json:"results"`
			ParseErrors int               `json:"parseErrors"`
		}{c, j.Query, j.Limit, j.Results, j.ParseErrors})
	default:
		return json.Marshal(struct {
			common
			Book     string  `json:"book"`
			Bytes    int64   `json:"bytes"`
			Size     int64   `json:"size"`
			Path     *string `json:"path"`
			FileName *string `json:"fileName"`
		}{c, j.Book, j.Bytes, j.Size, j.Path, j.FileName})
	}
}

// Registry is the in-memory job store: a map of all live jobs plus one
// bounded FIFO queue per job type. Workers pull from the queues with Next.
type Registry struct {
	mu      sync.Mutex
	jobs    map[uuid.UUID]*Job
	queues  map[JobType]chan *Job
	running map[JobType]*Job
	ttl     time.Duration
	// lastFinished is used by the idle watcher to decide when to drop IRC.
	lastFinished time.Time
	now          func() time.Time
}

func NewRegistry(queueDepth int, ttl time.Duration) *Registry {
	return &Registry{
		jobs: make(map[uuid.UUID]*Job),
		queues: map[JobType]chan *Job{
			JobSearch:   make(chan *Job, queueDepth),
			JobDownload: make(chan *Job, queueDepth),
		},
		running: make(map[JobType]*Job),
		ttl:     ttl,
		now:     time.Now,
	}
}

// Enqueue registers the job and places it on its type's queue. Returns
// the 1-based queue position, or ErrQueueFull.
func (r *Registry) Enqueue(job *Job) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.queues[job.Type]
	select {
	case q <- job:
	default:
		return 0, ErrQueueFull
	}
	job.Status = StatusQueued
	job.CreatedAt = r.now()
	r.jobs[job.ID] = job
	return len(q), nil
}

// Next blocks until a job of the given type is available (marking it
// running) or ctx is done (returning nil).
func (r *Registry) Next(ctx context.Context, t JobType) *Job {
	select {
	case job := <-r.queues[t]:
		r.mu.Lock()
		now := r.now()
		job.Status = StatusRunning
		job.StartedAt = &now
		r.running[t] = job
		r.mu.Unlock()
		return job
	case <-ctx.Done():
		return nil
	}
}

// Update mutates a job under the registry lock.
func (r *Registry) Update(job *Job, fn func(*Job)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(job)
}

// Finish marks the job terminal: complete when err is nil, error otherwise.
func (r *Registry) Finish(job *Job, err *APIError) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	job.FinishedAt = &now
	if err != nil {
		job.Status = StatusError
		job.Error = err
	} else {
		job.Status = StatusComplete
	}
	if r.running[job.Type] == job {
		delete(r.running, job.Type)
	}
	r.lastFinished = now
}

// Get returns a snapshot copy of the job.
func (r *Registry) Get(id uuid.UUID) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *job, true
}

// List returns snapshots of all live jobs (optionally filtered by type),
// newest first.
func (r *Registry) List(t JobType) []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		if t == "" || job.Type == t {
			out = append(out, *job)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Counts reports whether a job of type t is running and how many are queued.
func (r *Registry) Counts(t JobType) (running bool, queued int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[t] != nil, len(r.queues[t])
}

// Busy reports whether any job is running or queued.
func (r *Registry) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.running) > 0 {
		return true
	}
	for _, q := range r.queues {
		if len(q) > 0 {
			return true
		}
	}
	return false
}

// LastFinished is the time the most recent job reached a terminal state.
func (r *Registry) LastFinished() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastFinished
}

// Sweep drops finished jobs older than the TTL.
func (r *Registry) Sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := r.now().Add(-r.ttl)
	for id, job := range r.jobs {
		if job.FinishedAt != nil && job.FinishedAt.Before(cutoff) {
			delete(r.jobs, id)
		}
	}
}
