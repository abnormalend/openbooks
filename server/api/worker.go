// server/api/worker.go
package api

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/evan-buss/openbooks/dcc"
)

// sessionAPI is what a Worker needs from Session; tests substitute a fake.
type sessionAPI interface {
	Connect() error
	Connected() bool
	SearchBook(query string) error
	DownloadBook(book string) error
	SearchEvents() <-chan Event
	DownloadEvents() <-chan Event
}

type WorkerConfig struct {
	DownloadDir     string
	SearchTimeout   time.Duration
	DownloadTimeout time.Duration

	// LibrarySubdir is the subdirectory under DownloadDir where downloaded
	// books are stored. Empty is a valid, intended value meaning "the
	// DownloadDir root" (not coerced to a default here).
	LibrarySubdir string

	// BrowserConnected, if set, is re-checked immediately before Connect
	// so a browser tab that connects while a job sits in the queue still
	// wins: the job fails fast with browser_session_active instead of
	// stealing the one IRC identity out from under the browser.
	BrowserConnected func() bool
}

// Worker drains the registry queues, one goroutine per job type. Search
// and download are single-flight each because the IRC bots' DCC SEND
// replies carry no request identity: whatever arrives while job X is
// running belongs to X. A corollary accepted as a limitation of the IRC
// protocol: a reply that arrives late for a previous job that already
// timed out — after that job gave up but before the next job's drain —
// cannot be distinguished from a genuine reply to the current job and
// will be attributed to it.
type Worker struct {
	reg     *Registry
	sess    sessionAPI
	limiter *SearchLimiter
	cfg     WorkerConfig
	log     *log.Logger

	// fetch downloads+extracts a DCC payload; core.DownloadExtractDCCString
	// in production, stubbed in tests. Used for search (no per-job cancel).
	fetch func(baseDir, dccStr string, progress io.Writer) (string, error)
	// fetchCtx is the ctx-aware equivalent used for downloads, so a job
	// cancel aborts an in-flight transfer. core.DownloadExtractDCCStringContext
	// in production, stubbed in tests.
	fetchCtx func(ctx context.Context, baseDir, dccStr string, progress io.Writer) (string, error)
	tempDir  string
}

func NewWorker(reg *Registry, sess sessionAPI, limiter *SearchLimiter, cfg WorkerConfig, logger *log.Logger) *Worker {
	// A dedicated subdirectory keeps search-result zip/txt fetches from
	// colliding with the browser client's own use of os.TempDir() for the
	// same well-known file names (e.g. results.txt).
	tempDir := filepath.Join(os.TempDir(), "openbooks-api")
	// Error ignored: a failure here will surface as dcc_failed when the
	// fetch itself tries to create the file inside tempDir.
	_ = os.MkdirAll(tempDir, 0o755)
	return &Worker{
		reg:      reg,
		sess:     sess,
		limiter:  limiter,
		cfg:      cfg,
		log:      logger,
		fetch:    core.DownloadExtractDCCString,
		fetchCtx: core.DownloadExtractDCCStringContext,
		tempDir:  tempDir,
	}
}

// RunSearch processes search jobs until ctx is done.
func (w *Worker) RunSearch(ctx context.Context) {
	for {
		job := w.reg.Next(ctx, JobSearch)
		if job == nil {
			return
		}
		w.doSearch(ctx, job)
	}
}

// RunDownload processes download jobs until ctx is done.
func (w *Worker) RunDownload(ctx context.Context) {
	for {
		job := w.reg.Next(ctx, JobDownload)
		if job == nil {
			return
		}
		w.doDownload(ctx, job)
	}
}

// failureMessages gives human-readable text for codes that fail() reports
// without an underlying error (i.e. the failure is a Worker-observed
// condition, not something returned by a dependency).
var failureMessages = map[string]string{
	"timeout":                "no reply from the IRC bot within the job timeout",
	"irc_disconnected":       "IRC connection lost",
	"server_unavailable":     "the book server rejected the request; try another server",
	"browser_session_active": "a browser websocket client connected before the job started",
}

func (w *Worker) fail(job *Job, code string, err error) {
	msg := code
	if err != nil {
		msg = err.Error()
	} else if human, ok := failureMessages[code]; ok {
		msg = human
	}
	w.log.Printf("%s job %s failed: %s: %s", job.Type, job.ID, code, msg)
	w.reg.Finish(job, &APIError{Code: code, Message: msg})
}

var queuePosRe = regexp.MustCompile(`(?i)queue\s*position[^0-9]*([0-9]+)`)

// parseQueuePosition extracts the integer queue position from a bot notice,
// or 0 if none is present.
func parseQueuePosition(text string) int {
	m := queuePosRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

func drain(ch <-chan Event) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (w *Worker) doSearch(ctx context.Context, job *Job) {
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()
	w.reg.SetCancel(job.ID, jobCancel)
	defer w.reg.ClearCancel(job.ID)

	if w.cfg.BrowserConnected != nil && w.cfg.BrowserConnected() {
		w.fail(job, "browser_session_active", nil)
		return
	}
	if err := w.sess.Connect(); err != nil {
		w.fail(job, "irc_connect_failed", err)
		return
	}
	if err := w.limiter.Wait(jobCtx); err != nil {
		if w.reg.WasCancelled(job.ID) {
			w.log.Printf("search job %s: cancelled", job.ID)
			w.reg.FinishCancelled(job)
		} else {
			w.fail(job, "cancelled", err)
		}
		return
	}
	// Drain right before sending, not right after Connect: a reply
	// belonging to a previous, already-timed-out job can land at any
	// point up to and including while this job is waiting on the rate
	// limiter, and must not be mistaken for this job's response.
	drain(w.sess.SearchEvents())
	w.log.Printf("search job %s: sending %q", job.ID, job.Query)
	if err := w.sess.SearchBook(job.Query); err != nil {
		w.fail(job, "irc_disconnected", err)
		return
	}

	timer := time.NewTimer(w.cfg.SearchTimeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-w.sess.SearchEvents():
			switch ev.Kind {
			case EvSearchResult:
				w.completeSearch(job, ev.Text)
				return
			case EvNoResults:
				w.reg.Update(job, func(j *Job) { j.Results = []core.BookDetail{} })
				w.reg.Finish(job, nil)
				return
			case EvDisconnected:
				w.fail(job, "irc_disconnected", nil)
				return
			}
			// SearchAccepted / MatchesFound: informational, keep waiting.
		case <-timer.C:
			w.fail(job, "timeout", nil)
			return
		case <-jobCtx.Done():
			if w.reg.WasCancelled(job.ID) {
				w.log.Printf("search job %s: cancelled", job.ID)
				w.reg.FinishCancelled(job)
			} else {
				w.fail(job, "cancelled", jobCtx.Err())
			}
			return
		}
	}
}

// completeSearch fetches and parses the results file. The job timeout only
// bounds the wait for the bot's DCC offer (handled in doSearch); the
// transfer itself, like completeDownload's, runs to completion or DCC
// error rather than being bounded by SearchTimeout.
func (w *Worker) completeSearch(job *Job, dccText string) {
	path, err := w.fetch(w.tempDir, dccText, nil)
	if err != nil {
		w.fail(job, "dcc_failed", err)
		return
	}
	defer os.Remove(path)
	books, parseErrs, err := core.ParseSearchFile(path)
	if err != nil {
		w.fail(job, "parse_failed", err)
		return
	}
	if job.Limit > 0 && len(books) > job.Limit {
		books = books[:job.Limit]
	}
	w.log.Printf("search job %s: %d results (%d parse errors)", job.ID, len(books), len(parseErrs))
	w.reg.Update(job, func(j *Job) {
		j.Results = books
		j.ParseErrors = len(parseErrs)
	})
	w.reg.Finish(job, nil)
}

func (w *Worker) doDownload(ctx context.Context, job *Job) {
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()
	w.reg.SetCancel(job.ID, jobCancel)
	defer w.reg.ClearCancel(job.ID)

	if w.cfg.BrowserConnected != nil && w.cfg.BrowserConnected() {
		w.fail(job, "browser_session_active", nil)
		return
	}
	if err := w.sess.Connect(); err != nil {
		w.fail(job, "irc_connect_failed", err)
		return
	}
	drain(w.sess.DownloadEvents())

	// Arm the timer BEFORE the send: DownloadBook does a socket write which,
	// even with the irc write deadline, we never want to precede the timeout
	// bound. deadline is the absolute cap; queue notices reset the timer but
	// can't push total wait past cap.
	timer := time.NewTimer(w.cfg.DownloadTimeout)
	defer timer.Stop()
	deadline := time.Now().Add(2 * w.cfg.DownloadTimeout)

	w.log.Printf("download job %s: requesting %q", job.ID, job.Book)
	if err := w.sess.DownloadBook(job.Book); err != nil {
		w.fail(job, "irc_disconnected", err)
		return
	}

	for {
		select {
		case ev := <-w.sess.DownloadEvents():
			switch ev.Kind {
			case EvBookResult:
				w.completeDownload(jobCtx, job, ev.Text)
				return
			case EvBadServer:
				w.fail(job, "server_unavailable", nil)
				return
			case EvQueuePosition:
				pos := parseQueuePosition(ev.Text)
				w.log.Printf("download job %s: queued at position %d", job.ID, pos)
				w.reg.Update(job, func(j *Job) {
					if pos > 0 {
						j.QueuePosition = pos
					}
				})
				// Liveness proof: reset the timer, bounded by cap.
				remaining := time.Until(deadline)
				if remaining <= 0 {
					w.fail(job, "timeout", nil)
					return
				}
				next := w.cfg.DownloadTimeout
				if remaining < next {
					next = remaining
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(next)
			case EvDisconnected:
				w.fail(job, "irc_disconnected", nil)
				return
			}
		case <-timer.C:
			w.fail(job, "timeout", nil)
			return
		case <-jobCtx.Done():
			if w.reg.WasCancelled(job.ID) {
				w.log.Printf("download job %s: cancelled", job.ID)
				w.reg.FinishCancelled(job)
			} else {
				w.fail(job, "cancelled", jobCtx.Err())
			}
			return
		}
	}
}

// completeDownload runs the DCC transfer into <DownloadDir>/<LibrarySubdir>.
// The job timeout only bounds the wait for the bot's offer; the transfer
// itself runs to completion, DCC error, or ctx cancellation.
func (w *Worker) completeDownload(ctx context.Context, job *Job, dccText string) {
	if d, err := dcc.ParseString(dccText); err == nil {
		w.reg.Update(job, func(j *Job) { j.Size = d.Size })
	}
	progress := &progressWriter{fn: func(n int) {
		w.reg.Update(job, func(j *Job) { j.Bytes += int64(n) })
	}}
	path, err := w.fetchCtx(ctx, filepath.Join(w.cfg.DownloadDir, w.cfg.LibrarySubdir), dccText, progress)
	if err != nil {
		if w.reg.WasCancelled(job.ID) {
			w.reg.FinishCancelled(job)
			return
		}
		w.fail(job, "dcc_failed", err)
		return
	}
	name := filepath.Base(path)
	w.log.Printf("download job %s: saved %s", job.ID, path)
	w.reg.Update(job, func(j *Job) {
		j.Path = &path
		j.FileName = &name
	})
	w.reg.Finish(job, nil)
}

// progressWriter forwards byte counts to fn; used to update Job.Bytes.
type progressWriter struct{ fn func(n int) }

func (p *progressWriter) Write(b []byte) (int, error) {
	p.fn(len(b))
	return len(b), nil
}
