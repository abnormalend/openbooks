// server/api/worker_test.go
package api

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSession satisfies sessionAPI without touching the network.
type fakeSession struct {
	connectErr error
	connected  bool
	searched   []string
	downloaded []string
	// searchReply / downloadReply are pushed onto the matching channel at
	// the moment SearchBook/DownloadBook is called, mirroring how a real
	// bot's DCC reply arrives only after the request goes out.
	searchReply   []Event
	downloadReply []Event
	searchEv      chan Event
	downloadEv    chan Event
}

func newFakeSession() *fakeSession {
	return &fakeSession{searchEv: make(chan Event, 8), downloadEv: make(chan Event, 8)}
}
func (f *fakeSession) Connect() error {
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connected = true
	return nil
}
func (f *fakeSession) Connected() bool { return f.connected }
func (f *fakeSession) SearchBook(q string) {
	f.searched = append(f.searched, q)
	for _, ev := range f.searchReply {
		f.searchEv <- ev
	}
}
func (f *fakeSession) DownloadBook(b string) {
	f.downloaded = append(f.downloaded, b)
	for _, ev := range f.downloadReply {
		f.downloadEv <- ev
	}
}
func (f *fakeSession) SearchEvents() <-chan Event   { return f.searchEv }
func (f *fakeSession) DownloadEvents() <-chan Event { return f.downloadEv }

const sampleResults = `Search results
!DV8 F. Scott Fitzgerald - The Great Gatsby (Epub).rar  ::INFO:: 394.7KB
!Horla F Scott Fitzgerald - The Great Gatsby (retail) (epub).epub  ::INFO:: 1.2MB
!Bot J. R. R. Tolkien - The Hobbit.epub  ::INFO:: 800KB
`

// writeResultsTxt writes the extracted results.txt a fake fetch would
// hand back (core.DownloadExtractDCCString normally does the zip fetch
// *and* extraction; tests stub that whole step and just produce the file
// it would have left behind).
func writeResultsTxt(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "results.txt")
	if err := os.WriteFile(p, []byte(sampleResults), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestWorker(t *testing.T, sess sessionAPI) (*Worker, *Registry) {
	t.Helper()
	reg := NewRegistry(3, time.Hour)
	w := NewWorker(reg, sess, NewSearchLimiter(0), WorkerConfig{
		DownloadDir:     t.TempDir(),
		SearchTimeout:   200 * time.Millisecond,
		DownloadTimeout: 200 * time.Millisecond,
	}, log.New(io.Discard, "", 0))
	w.tempDir = t.TempDir()
	return w, reg
}

func TestSearchJobCompletesWithParsedResults(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	var fetchedDir, fetchedText string
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		fetchedDir, fetchedText = baseDir, dccStr
		return writeResultsTxt(t, baseDir), nil
	}

	job := NewSearchJob("gatsby", 2)
	sess.searchReply = []Event{
		{Kind: EvSearchAccepted},
		{Kind: EvSearchResult, Text: "DCC SEND results.zip 1 2 3"},
	}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { w.RunSearch(ctx) }()
	waitStatus(t, reg, job, StatusComplete)
	cancel()

	snap, _ := reg.Get(job.ID)
	if len(sess.searched) != 1 || sess.searched[0] != "gatsby" {
		t.Errorf("searched = %v", sess.searched)
	}
	if fetchedDir != w.tempDir || fetchedText != "DCC SEND results.zip 1 2 3" {
		t.Errorf("fetch called with (%q,%q)", fetchedDir, fetchedText)
	}
	if len(snap.Results) != 2 { // limit applied
		t.Errorf("results = %d, want 2 (limit)", len(snap.Results))
	}
	if snap.ParseErrors != 0 || snap.Error != nil {
		t.Errorf("snap = %+v", snap)
	}
	if _, err := os.Stat(filepath.Join(w.tempDir, "results.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("results file should be removed after parsing, stat err = %v", err)
	}
}

func TestSearchJobNoResultsIsCompleteEmpty(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewSearchJob("nothing", 0)
	sess.searchReply = []Event{{Kind: EvNoResults, Text: "Sorry"}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusComplete)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Results == nil || len(snap.Results) != 0 {
		t.Errorf("results = %#v, want empty non-nil", snap.Results)
	}
}

func TestSearchJobTimesOut(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewSearchJob("slow", 0)
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "timeout" {
		t.Errorf("error = %+v, want timeout", snap.Error)
	}
}

func TestSearchJobConnectFailure(t *testing.T) {
	sess := newFakeSession()
	sess.connectErr = errors.New("dial tcp: refused")
	w, reg := newTestWorker(t, sess)
	job := NewSearchJob("q", 0)
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "irc_connect_failed" {
		t.Errorf("error = %+v", snap.Error)
	}
	if len(sess.searched) != 0 {
		t.Error("must not send a search when connect failed")
	}
}

func TestSearchJobFailsFastWhenBrowserConnected(t *testing.T) {
	sess := newFakeSession()
	reg := NewRegistry(3, time.Hour)
	w := NewWorker(reg, sess, NewSearchLimiter(0), WorkerConfig{
		DownloadDir:      t.TempDir(),
		SearchTimeout:    200 * time.Millisecond,
		DownloadTimeout:  200 * time.Millisecond,
		BrowserConnected: func() bool { return true },
	}, log.New(io.Discard, "", 0))
	w.tempDir = t.TempDir()

	job := NewSearchJob("q", 0)
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "browser_session_active" {
		t.Errorf("error = %+v", snap.Error)
	}
	if len(sess.searched) != 0 {
		t.Error("must not send a search when a browser client is connected")
	}
}

func TestSearchJobDisconnectedMidFlight(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewSearchJob("q", 0)
	sess.searchReply = []Event{{Kind: EvDisconnected}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "irc_disconnected" {
		t.Errorf("error = %+v", snap.Error)
	}
}

func TestSearchDrainsStaleEventsBeforeSending(t *testing.T) {
	// A stale SearchResult left over from a previous (timed-out) job must
	// not be attributed to the next job.
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	sess.searchEv <- Event{Kind: EvSearchResult, Text: "stale"}
	fetched := 0
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		fetched++
		return writeResultsTxt(t, baseDir), nil
	}
	job := NewSearchJob("q", 0)
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError) // times out: stale event was drained
	cancel()
	if fetched != 0 {
		t.Errorf("stale event was consumed as a result (fetch called %d times)", fetched)
	}
}

func TestSearchJobParseFailed(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		return filepath.Join(baseDir, "does-not-exist.txt"), nil
	}
	job := NewSearchJob("q", 0)
	sess.searchReply = []Event{{Kind: EvSearchResult, Text: "DCC SEND results.zip 1 2 3"}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "parse_failed" {
		t.Errorf("error = %+v, want parse_failed", snap.Error)
	}
}

func TestSearchJobDccFailed(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		return "", errors.New("connection reset")
	}
	job := NewSearchJob("q", 0)
	sess.searchReply = []Event{{Kind: EvSearchResult, Text: "DCC SEND results.zip 1 2 3"}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()

	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "dcc_failed" {
		t.Errorf("error = %+v, want dcc_failed", snap.Error)
	}
}

func TestSearchJobLimitZeroReturnsAll(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		return writeResultsTxt(t, baseDir), nil
	}
	job := NewSearchJob("q", 0) // limit 0 means unbounded
	sess.searchReply = []Event{{Kind: EvSearchResult, Text: "DCC SEND results.zip 1 2 3"}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	waitStatus(t, reg, job, StatusComplete)
	cancel()

	snap, _ := reg.Get(job.ID)
	if len(snap.Results) != 3 {
		t.Errorf("results = %d, want 3 (all, unbounded)", len(snap.Results))
	}
}

func TestSearchJobCancelledByContext(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewSearchJob("q", 0)
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunSearch(ctx)
	// No reply is queued, so doSearch blocks in its event-wait loop; give
	// it time to get past Next/Connect/limiter/drain/SearchBook and into
	// that select before cancelling, well inside the 200ms SearchTimeout,
	// so the result is unambiguously "cancelled" rather than "timeout".
	time.Sleep(20 * time.Millisecond)
	cancel()

	waitStatus(t, reg, job, StatusError)
	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "cancelled" {
		t.Errorf("error = %+v, want cancelled", snap.Error)
	}
}

func waitStatus(t *testing.T, reg *Registry, job *Job, want Status) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := reg.Get(job.ID)
		if snap.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap, _ := reg.Get(job.ID)
	t.Fatalf("job never reached %s; now %s (err %+v)", want, snap.Status, snap.Error)
}

func TestDownloadJobCompletesWithFile(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	var fetchedDir string
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		fetchedDir = baseDir
		p := filepath.Join(baseDir, "great-gatsby.epub")
		os.MkdirAll(baseDir, 0o755)
		os.WriteFile(p, []byte("epub-bytes"), 0o644)
		progress.Write(make([]byte, 5))
		progress.Write(make([]byte, 5))
		return p, nil
	}
	job := NewDownloadJob("!DV8 F. Scott Fitzgerald - The Great Gatsby.epub")
	// 2130706433 = 127.0.0.1 ; size 10
	sess.downloadReply = []Event{{Kind: EvBookResult, Text: ":DV8!x@y PRIVMSG me :DCC SEND great-gatsby.epub 2130706433 6669 10"}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusComplete)
	cancel()

	snap, _ := reg.Get(job.ID)
	if len(sess.downloaded) != 1 || sess.downloaded[0] != job.Book {
		t.Errorf("downloaded = %v", sess.downloaded)
	}
	if want := filepath.Join(w.cfg.DownloadDir, "books"); fetchedDir != want {
		t.Errorf("fetch dir = %q, want %q", fetchedDir, want)
	}
	if snap.Size != 10 || snap.Bytes != 10 {
		t.Errorf("size/bytes = %d/%d, want 10/10", snap.Size, snap.Bytes)
	}
	if snap.Path == nil || snap.FileName == nil || *snap.FileName != "great-gatsby.epub" {
		t.Errorf("path/fileName = %v/%v", snap.Path, snap.FileName)
	}
}

func TestDownloadJobBadServer(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewDownloadJob("!Nope book.epub")
	sess.downloadReply = []Event{{Kind: EvBadServer}}
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()
	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "server_unavailable" {
		t.Errorf("error = %+v", snap.Error)
	}
}

func TestDownloadJobTimeout(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewDownloadJob("!Slow book.epub")
	reg.Enqueue(job)
	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()
	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "timeout" {
		t.Errorf("error = %+v", snap.Error)
	}
}

func TestDownloadJobDccFailure(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	w.fetch = func(baseDir, dccStr string, progress io.Writer) (string, error) {
		return "", errors.New("connection reset")
	}
	job := NewDownloadJob("!DV8 book.epub")
	sess.downloadReply = []Event{{Kind: EvBookResult, Text: "DCC SEND book.epub 2130706433 6669 10"}}
	reg.Enqueue(job)
	ctx, cancel := context.WithCancel(context.Background())
	go w.RunDownload(ctx)
	waitStatus(t, reg, job, StatusError)
	cancel()
	snap, _ := reg.Get(job.ID)
	if snap.Error == nil || snap.Error.Code != "dcc_failed" {
		t.Errorf("error = %+v", snap.Error)
	}
}
