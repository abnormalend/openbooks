// server/api/api.go
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

//go:embed openapi.yaml
var openapiSpec []byte

// Config is everything the API needs from the server config.
type Config struct {
	Token       string
	Version     string
	BasePath    string
	DownloadDir string
	// LibrarySubdir is the subdirectory under DownloadDir where downloaded
	// books are stored. Empty is a valid, intended value meaning "the
	// DownloadDir root" — it must NOT be defaulted here; the server sets
	// it explicitly (the CLI flag's default is "books").
	LibrarySubdir string

	IdleTimeout     time.Duration // disconnect IRC after this long with no jobs (default 5m)
	SearchTimeout   time.Duration // default 120s
	DownloadTimeout time.Duration // default 10m
	JobTTL          time.Duration // default 1h
	QueueDepth      int           // default 3

	Session SessionConfig
}

// Deps are hooks back into the hosting server.
type Deps struct {
	Limiter          *SearchLimiter
	BrowserConnected func() bool
	Servers          func() core.IrcServers
	OnServerList     func(core.IrcServers)
	Log              *log.Logger
}

type API struct {
	cfg     Config
	deps    Deps
	reg     *Registry
	sess    *Session
	worker  *Worker
	stats   *ServerStats
	started time.Time
	once    sync.Once
}

func New(cfg Config, deps Deps) *API {
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.SearchTimeout == 0 {
		cfg.SearchTimeout = 120 * time.Second
	}
	if cfg.DownloadTimeout == 0 {
		cfg.DownloadTimeout = 10 * time.Minute
	}
	if cfg.JobTTL == 0 {
		cfg.JobTTL = time.Hour
	}
	if cfg.QueueDepth == 0 {
		cfg.QueueDepth = 3
	}
	if deps.Log == nil {
		deps.Log = log.Default()
	}
	if deps.BrowserConnected == nil {
		deps.BrowserConnected = func() bool { return false }
	}
	if deps.Servers == nil {
		deps.Servers = func() core.IrcServers { return core.IrcServers{} }
	}
	reg := NewRegistry(cfg.QueueDepth, cfg.JobTTL)
	sess := NewSession(cfg.Session, deps.Log, deps.OnServerList)
	stats := NewServerStats()
	worker := NewWorker(reg, sess, deps.Limiter, WorkerConfig{
		DownloadDir:      cfg.DownloadDir,
		LibrarySubdir:    cfg.LibrarySubdir,
		SearchTimeout:    cfg.SearchTimeout,
		DownloadTimeout:  cfg.DownloadTimeout,
		BrowserConnected: deps.BrowserConnected,
		Stats:            stats,
	}, deps.Log)
	return &API{cfg: cfg, deps: deps, reg: reg, sess: sess, worker: worker, stats: stats, started: time.Now()}
}

// Start launches the workers, the TTL sweeper and the idle watcher.
// Nothing connects to IRC until the first job arrives. A second call is
// a no-op: the server only ever calls this once, but guarding it means
// a mistaken extra call can't double up the background loops.
func (a *API) Start(ctx context.Context) {
	a.once.Do(func() {
		go a.worker.RunSearch(ctx)
		go a.worker.RunDownload(ctx)
		go a.loop(ctx, time.Minute, a.reg.Sweep)
		go a.loop(ctx, 15*time.Second, a.idleCheck)
		go func() {
			<-ctx.Done()
			a.sess.Disconnect()
		}()
	})
}

func (a *API) loop(ctx context.Context, every time.Duration, fn func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			fn()
		case <-ctx.Done():
			return
		}
	}
}

func (a *API) idleCheck() {
	// LastFinished's zero value means "connected but never finished a
	// job" — by definition idle, since nothing is running or queued
	// (Busy would be true otherwise) and no job's completion has ever
	// reset the timer. Disconnecting in that case is correct.
	if a.sess.Connected() && !a.reg.Busy() && time.Since(a.reg.LastFinished()) > a.cfg.IdleTimeout {
		a.deps.Log.Println("API session idle, disconnecting from IRC")
		a.sess.Disconnect()
	}
}

// Yield is called by the websocket handler before it lets a browser
// connect. If the API is idle it drops its IRC connection and returns
// true; if a job is running or queued it returns false.
func (a *API) Yield() bool {
	if a.reg.Busy() {
		return false
	}
	a.sess.Disconnect()
	return true
}

// Router returns the chi router to mount under <BASE_PATH>api.
func (a *API) Router() chi.Router {
	r := chi.NewRouter()
	// chi's default 405/404 handling runs outside any route's middleware
	// chain, so without these every non-2xx response from unmatched
	// methods/paths would fall back to chi's plain-text default instead
	// of our JSON error shape — and a bare 405 would let an
	// unauthenticated client learn a route exists before auth ever runs.
	// Wrapping the 405 handler in RequireToken keeps auth first.
	r.MethodNotAllowed(RequireToken(a.cfg.Token)(http.HandlerFunc(methodNotAllowed)).ServeHTTP)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	})
	r.Get("/health", a.health)
	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openapiSpec)
	})
	r.Group(func(p chi.Router) {
		p.Use(RequireToken(a.cfg.Token))
		p.Post("/search", a.postSearch)
		p.Get("/search/{id}", a.getJob(JobSearch))
		p.Delete("/search/{id}", a.cancelJob(JobSearch))
		p.Post("/download", a.postDownload)
		p.Get("/download/{id}", a.getJob(JobDownload))
		p.Delete("/download/{id}", a.cancelJob(JobDownload))
		p.Get("/jobs", a.listJobs)
		p.Get("/servers", a.servers)
		p.Get("/server-stats", a.serverStats)
	})
	return r
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	type counts struct {
		Running bool `json:"running"`
		Queued  int  `json:"queued"`
	}
	sr, sq := a.reg.Counts(JobSearch)
	dr, dq := a.reg.Counts(JobDownload)
	writeJSON(w, 200, map[string]interface{}{
		"version":          a.cfg.Version,
		"uptimeSeconds":    int64(time.Since(a.started).Seconds()),
		"downloadDir":      a.cfg.DownloadDir,
		"basePath":         a.cfg.BasePath,
		"apiEnabled":       a.cfg.Token != "",
		"ircConnected":     a.sess.Connected(),
		"browserConnected": a.deps.BrowserConnected(),
		"search":           counts{sr, sq},
		"download":         counts{dr, dq},
	})
}

func decode(w http.ResponseWriter, r *http.Request, v interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// enqueue applies the shared admission rules and writes the 202/409.
func (a *API) enqueue(w http.ResponseWriter, job *Job) {
	if a.deps.BrowserConnected() {
		writeError(w, http.StatusConflict, "browser_session_active",
			"a browser websocket client is connected; close it to use the API")
		return
	}
	pos, err := a.reg.Enqueue(job)
	if errors.Is(err, ErrQueueFull) {
		writeError(w, http.StatusConflict, "queue_full",
			"too many "+string(job.Type)+" jobs queued; retry later")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":    job.ID,
		"status":   StatusQueued,
		"position": pos,
	})
}

func (a *API) postSearch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	body.Query = strings.TrimSpace(body.Query)
	if body.Query == "" {
		writeError(w, 400, "bad_request", "\"query\" is required")
		return
	}
	if body.Limit < 0 {
		writeError(w, 400, "bad_request", "\"limit\" must be >= 0")
		return
	}
	a.enqueue(w, NewSearchJob(body.Query, body.Limit))
}

func (a *API) postDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Book string `json:"book"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body: "+err.Error())
		return
	}
	body.Book = strings.TrimSpace(body.Book)
	if !strings.HasPrefix(body.Book, "!") {
		writeError(w, 400, "bad_request", "\"book\" must be a search result's \"full\" string (starts with '!')")
		return
	}
	a.enqueue(w, NewDownloadJob(body.Book))
}

func (a *API) getJob(t JobType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, 404, "job_not_found", "no such job")
			return
		}
		job, ok := a.reg.Get(id)
		if !ok || job.Type != t {
			writeError(w, 404, "job_not_found", "no such job")
			return
		}
		writeJSON(w, 200, job)
	}
}

// cancelJob handles DELETE /{search,download}/{id}: cancels a queued or
// running job of type t. A 404 job_not_found type-guard mirrors getJob so
// a search id can't be used to cancel a download job or vice versa.
func (a *API) cancelJob(t JobType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, 404, "job_not_found", "no such job")
			return
		}
		// type guard: only cancel a job of the addressed type
		if job, ok := a.reg.Get(id); !ok || job.Type != t {
			writeError(w, 404, "job_not_found", "no such job")
			return
		}
		job, apiErr := a.reg.Cancel(id)
		if apiErr != nil {
			status := 409
			if apiErr.Code == "job_not_found" {
				status = 404
			}
			writeError(w, status, apiErr.Code, apiErr.Message)
			return
		}
		writeJSON(w, 200, job)
	}
}

func (a *API) listJobs(w http.ResponseWriter, r *http.Request) {
	t := JobType(r.URL.Query().Get("type"))
	switch t {
	case "", JobSearch, JobDownload:
	default:
		writeError(w, 400, "bad_request", "\"type\" must be search or download")
		return
	}
	writeJSON(w, 200, map[string]interface{}{"jobs": a.reg.List(t)})
}

func (a *API) servers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]interface{}{"servers": a.deps.Servers()})
}

// serverStats returns the API's own accumulated per-server reliability
// stats (see ServerStats), with Online merged in from the current NAMES
// presence list. lastQueuePosition is our last-observed position at that
// server, not live queue depth (IRC book bots don't expose that on demand).
func (a *API) serverStats(w http.ResponseWriter, _ *http.Request) {
	online := map[string]bool{}
	for _, name := range a.deps.Servers().ElevatedUsers {
		online[name] = true
	}
	writeJSON(w, 200, map[string]interface{}{"servers": a.stats.Snapshot(online)})
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
