// server/api/worker_test.go
package api

import (
	"archive/zip"
	"bytes"
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

// writeZipFile writes a zip containing results.txt into dir and returns
// its path (this is what core.DownloadExtractDCCString would produce
// *before* extraction; our fake fetch returns the extracted txt instead).
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
		SearchTimeout:   500 * time.Millisecond,
		DownloadTimeout: 500 * time.Millisecond,
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

// makeZip is used by the download tests in Task 6.
func makeZip(t *testing.T, name, contents string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte(contents))
	zw.Close()
	return buf.Bytes()
}
