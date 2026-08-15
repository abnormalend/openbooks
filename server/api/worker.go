// server/api/worker.go
package api

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/evan-buss/openbooks/core"
	"github.com/evan-buss/openbooks/dcc"
)

// sessionAPI is what a Worker needs from Session; tests substitute a fake.
type sessionAPI interface {
	Connect() error
	Connected() bool
	SearchBook(query string)
	DownloadBook(book string)
	SearchEvents() <-chan Event
	DownloadEvents() <-chan Event
}

type WorkerConfig struct {
	DownloadDir     string
	SearchTimeout   time.Duration
	DownloadTimeout time.Duration
}

// Worker drains the registry queues, one goroutine per job type. Search
// and download are single-flight each because the IRC bots' DCC SEND
// replies carry no request identity: whatever arrives while job X is
// running belongs to X.
type Worker struct {
	reg     *Registry
	sess    sessionAPI
	limiter *SearchLimiter
	cfg     WorkerConfig
	log     *log.Logger

	// fetch downloads+extracts a DCC payload; core.DownloadExtractDCCString
	// in production, stubbed in tests.
	fetch   func(baseDir, dccStr string, progress io.Writer) (string, error)
	tempDir string
}

func NewWorker(reg *Registry, sess sessionAPI, limiter *SearchLimiter, cfg WorkerConfig, logger *log.Logger) *Worker {
	return &Worker{
		reg:     reg,
		sess:    sess,
		limiter: limiter,
		cfg:     cfg,
		log:     logger,
		fetch:   core.DownloadExtractDCCString,
		tempDir: os.TempDir(),
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

func (w *Worker) fail(job *Job, code string, err error) {
	msg := code
	if err != nil {
		msg = err.Error()
	}
	w.log.Printf("%s job %s failed: %s: %s", job.Type, job.ID, code, msg)
	w.reg.Finish(job, &APIError{Code: code, Message: msg})
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
	if err := w.sess.Connect(); err != nil {
		w.fail(job, "irc_connect_failed", err)
		return
	}
	drain(w.sess.SearchEvents())
	if err := w.limiter.Wait(ctx); err != nil {
		w.fail(job, "cancelled", err)
		return
	}
	w.log.Printf("search job %s: sending %q", job.ID, job.Query)
	w.sess.SearchBook(job.Query)

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
		case <-ctx.Done():
			w.fail(job, "cancelled", ctx.Err())
			return
		}
	}
}

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

// doDownload is implemented in Task 6.
func (w *Worker) doDownload(ctx context.Context, job *Job) {
	w.fail(job, "not_implemented", nil)
}

// progressWriter forwards byte counts to fn; used to update Job.Bytes.
type progressWriter struct{ fn func(n int) }

func (p *progressWriter) Write(b []byte) (int, error) {
	p.fn(len(b))
	return len(b), nil
}

var _ = dcc.ParseString // used in Task 6
var _ = filepath.Join   // used in Task 6
