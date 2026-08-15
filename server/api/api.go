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
	started time.Time
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
	worker := NewWorker(reg, sess, deps.Limiter, WorkerConfig{
		DownloadDir:     cfg.DownloadDir,
		SearchTimeout:   cfg.SearchTimeout,
		DownloadTimeout: cfg.DownloadTimeout,
	}, deps.Log)
	return &API{cfg: cfg, deps: deps, reg: reg, sess: sess, worker: worker, started: time.Now()}
}

// Start launches the workers, the TTL sweeper and the idle watcher.
// Nothing connects to IRC until the first job arrives.
func (a *API) Start(ctx context.Context) {
	go a.worker.RunSearch(ctx)
	go a.worker.RunDownload(ctx)
	go a.loop(ctx, time.Minute, a.reg.Sweep)
	go a.loop(ctx, 15*time.Second, a.idleCheck)
	go func() {
		<-ctx.Done()
		a.sess.Disconnect()
	}()
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
	r.Get("/health", a.health)
	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openapiSpec)
	})
	r.Group(func(p chi.Router) {
		p.Use(RequireToken(a.cfg.Token))
		p.Post("/search", a.postSearch)
		p.Get("/search/{id}", a.getJob(JobSearch))
		// GET /search has no meaning (POST-only), but chi's default
		// 405 handling bypasses group middleware entirely, which would
		// let an unauthenticated client learn the route exists. Register
		// it explicitly so RequireToken still runs first.
		p.Get("/search", methodNotAllowed)
		p.Post("/download", a.postDownload)
		p.Get("/download/{id}", a.getJob(JobDownload))
		p.Get("/download", methodNotAllowed)
		p.Get("/jobs", a.listJobs)
		p.Get("/servers", a.servers)
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

func decode(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
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
	if err := decode(r, &body); err != nil {
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
	if err := decode(r, &body); err != nil {
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

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
