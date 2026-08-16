# API Cancel + Configurable Library Dir — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: subagent-driven-development or executing-plans. Checkbox steps.

**Goal:** Add `DELETE /api/{search,download}/{id}` cancel (queued or running, aborting in-flight DCC), a configurable `--library-subdir` so downloads can land in the watched root, MCP cancel tools, and book-format docs.

**Spec:** `docs/superpowers/specs/2026-08-16-api-cancel-and-library-dir.md` (read first).

**Conventions:** repo root `/home/brent/openbooks`; `gofmt -l ./...` empty + `go vet ./...` clean before each commit; commit trailers:
```
Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01PHyjTkfDAfEjj8Jaoeqdd4
```
Branch `feature/api-cancel-and-library-dir` (checked out). Do not push.

---

### Task 1: Cancellable DCC download

**Files:** `dcc/dcc.go`, `dcc/dcc_test.go`, `core/file.go`

- [ ] **Step 1 — failing test** in `dcc/dcc_test.go`:

```go
func TestDownloadContextAbortsOnCancel(t *testing.T) {
	// A DCC server that announces a big size but dribbles bytes forever.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		for {
			if _, err := conn.Write(buf); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	ip, portStr, _ := net.SplitHostPort(l.Addr().String())
	var ipInt uint32
	for _, b := range net.ParseIP(ip).To4() {
		ipInt = ipInt<<8 | uint32(b)
	}
	port, _ := strconv.Atoi(portStr)
	d := Download{Filename: "x", IP: ip, Port: fmt.Sprintf("%d", port), Size: 1 << 30}
	_ = ipInt

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(80 * time.Millisecond); cancel() }()
	done := make(chan error, 1)
	go func() { done <- d.DownloadContext(ctx, io.Discard) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a cancelled download")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DownloadContext did not return promptly after cancel")
	}
}
```

Add imports as needed (`context`, `io`, `net`, `strconv`, `fmt`, `time`).

- [ ] **Step 2 — run:** `go test -race -run TestDownloadContext ./dcc/ 2>&1 | head -3` → `undefined: DownloadContext`.

- [ ] **Step 3 — implement** in `dcc/dcc.go`. Add `import "context"`. Replace the existing `Download` method:

```go
// Download writes the DCC data to writer (uncancellable).
func (download Download) Download(writer io.Writer) error {
	return download.DownloadContext(context.Background(), writer)
}

// DownloadContext is Download with cancellation: when ctx is cancelled the
// underlying connection is closed, unblocking the read loop with an error.
func (download Download) DownloadContext(ctx context.Context, writer io.Writer) error {
	conn, err := net.Dial("tcp", download.IP+":"+download.Port)
	if err != nil {
		return err
	}
	defer conn.Close()

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()

	received := 0
	bytes := make([]byte, 4096)
	for int64(received) < download.Size {
		n, err := conn.Read(bytes)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}
		if _, err = writer.Write(bytes[:n]); err != nil {
			return err
		}
		received += n
	}
	if int64(received) != download.Size {
		return ErrMissingBytes
	}
	return nil
}
```

(Keep the existing benchmark comment block if you like; it's not required.)

- [ ] **Step 4 — core wrapper** in `core/file.go`. Add `import "context"`. Rename the body of `DownloadExtractDCCString` into a ctx variant and delegate:

```go
func DownloadExtractDCCString(baseDir, dccStr string, progress io.Writer) (string, error) {
	return DownloadExtractDCCStringContext(context.Background(), baseDir, dccStr, progress)
}

func DownloadExtractDCCStringContext(ctx context.Context, baseDir, dccStr string, progress io.Writer) (string, error) {
	download, err := dcc.ParseString(dccStr)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	dccPath := filepath.Join(baseDir, download.Filename+".temp")
	file, err := os.Create(dccPath)
	if err != nil {
		return "", err
	}
	writer := io.Writer(file)
	if progress != nil {
		writer = io.MultiWriter(file, progress)
	}
	if err = download.DownloadContext(ctx, writer); err != nil {
		file.Close()
		return "", err
	}
	file.Close()
	if !util.IsArchive(dccPath) {
		return renameTempFile(dccPath), nil
	}
	extractedPath, err := util.ExtractArchive(dccPath)
	if err != nil {
		return "", err
	}
	return renameTempFile(extractedPath), nil
}
```

- [ ] **Step 5 — run:** `go test -race ./dcc/ ./core/` → `ok`. `go build ./...`.
- [ ] **Step 6 — commit:** `git add dcc/dcc.go dcc/dcc_test.go core/file.go && git commit -m "feat(dcc): context-cancellable DCC download"`

---

### Task 2: Registry cancel machinery + `cancelled` status

**Files:** `server/api/jobs.go`, `server/api/jobs_test.go`

- [ ] **Step 1 — failing tests** in `server/api/jobs_test.go`:

```go
func TestCancelQueuedMarksCancelledAndNextSkips(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	job := NewDownloadJob("!x")
	r.Enqueue(job)
	got, apiErr := r.Cancel(job.ID)
	if apiErr != nil {
		t.Fatalf("cancel: %+v", apiErr)
	}
	if got.Status != StatusCancelled {
		t.Errorf("status = %s, want cancelled", got.Status)
	}
	// Next must skip the cancelled job (and, with nothing else queued, block
	// until ctx cancel → return nil).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if j := r.Next(ctx, JobDownload); j != nil {
		t.Errorf("Next returned a cancelled job: %+v", j)
	}
}

func TestCancelRunningInvokesCancelFunc(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	job := NewDownloadJob("!x")
	r.Enqueue(job)
	r.Next(context.Background(), JobDownload) // marks running
	called := false
	r.SetCancel(job.ID, func() { called = true })

	got, apiErr := r.Cancel(job.ID)
	if apiErr != nil {
		t.Fatalf("cancel: %+v", apiErr)
	}
	if !called {
		t.Error("cancel func not invoked for running job")
	}
	if !r.WasCancelled(job.ID) {
		t.Error("WasCancelled should be true")
	}
	// Worker finalizes:
	r.FinishCancelled(job)
	snap, _ := r.Get(job.ID)
	if snap.Status != StatusCancelled || snap.FinishedAt == nil {
		t.Errorf("after FinishCancelled: %+v", snap)
	}
	_ = got
}

func TestCancelUnknownAndTerminal(t *testing.T) {
	r := NewRegistry(3, time.Hour)
	if _, e := r.Cancel(uuid.New()); e == nil || e.Code != "job_not_found" {
		t.Errorf("unknown: %+v", e)
	}
	job := NewSearchJob("q", 0)
	r.Enqueue(job)
	r.Next(context.Background(), JobSearch)
	r.Finish(job, nil) // complete
	if _, e := r.Cancel(job.ID); e == nil || e.Code != "not_cancellable" {
		t.Errorf("terminal: %+v", e)
	}
}
```

- [ ] **Step 2 — run:** `go test -race -run TestCancel ./server/api/ 2>&1 | head -3` → undefined symbols.

- [ ] **Step 3 — implement** in `server/api/jobs.go`:

Add the status constant:
```go
	StatusError    Status = "error"
	StatusCancelled Status = "cancelled"
```

Add `"context"` import if missing. Add maps to `Registry` (in `NewRegistry` initialize both):
```go
	cancels   map[uuid.UUID]context.CancelFunc
	cancelled map[uuid.UUID]bool
```
```go
		cancels:   make(map[uuid.UUID]context.CancelFunc),
		cancelled: make(map[uuid.UUID]bool),
```

Add methods:
```go
// SetCancel registers a running job's cancel func. ClearCancel removes it.
func (r *Registry) SetCancel(id uuid.UUID, fn context.CancelFunc) {
	r.mu.Lock(); defer r.mu.Unlock()
	r.cancels[id] = fn
}
func (r *Registry) ClearCancel(id uuid.UUID) {
	r.mu.Lock(); defer r.mu.Unlock()
	delete(r.cancels, id)
}
func (r *Registry) WasCancelled(id uuid.UUID) bool {
	r.mu.Lock(); defer r.mu.Unlock()
	return r.cancelled[id]
}

// Cancel cancels a queued or running job. Queued jobs are marked cancelled
// in place (Next skips them, freeing the slot on receive); running jobs get
// their cancel func invoked and are finalized by the worker.
func (r *Registry) Cancel(id uuid.UUID) (Job, *APIError) {
	r.mu.Lock()
	job, ok := r.jobs[id]
	if !ok {
		r.mu.Unlock()
		return Job{}, &APIError{Code: "job_not_found", Message: "no such job"}
	}
	switch job.Status {
	case StatusComplete, StatusError, StatusCancelled:
		snap := *job
		r.mu.Unlock()
		return snap, &APIError{Code: "not_cancellable", Message: "job already finished"}
	case StatusQueued:
		now := r.now()
		job.Status = StatusCancelled
		job.FinishedAt = &now
		r.lastFinished = now
		snap := *job
		r.mu.Unlock()
		return snap, nil
	default: // running
		r.cancelled[id] = true
		fn := r.cancels[id]
		snap := *job
		r.mu.Unlock()
		if fn != nil {
			fn()
		}
		return snap, nil
	}
}

// FinishCancelled finalizes a running job the worker observed as cancelled.
func (r *Registry) FinishCancelled(job *Job) {
	r.mu.Lock(); defer r.mu.Unlock()
	now := r.now()
	job.Status = StatusCancelled
	job.FinishedAt = &now
	r.lastFinished = now
	delete(r.cancels, job.ID)
}
```

Modify `Next` to skip cancelled jobs — replace its receive with a loop:
```go
func (r *Registry) Next(ctx context.Context, t JobType) *Job {
	for {
		select {
		case job := <-r.queues[t]:
			r.mu.Lock()
			if job.Status == StatusCancelled {
				r.mu.Unlock()
				continue // slot already freed by the receive; skip
			}
			now := r.now()
			job.Status = StatusRunning
			job.StartedAt = &now
			r.mu.Unlock()
			return job
		case <-ctx.Done():
			return nil
		}
	}
}
```

- [ ] **Step 4 — run:** `go test -race -count=3 ./server/api/` → `ok`.
- [ ] **Step 5 — commit:** `git add server/api/jobs.go server/api/jobs_test.go && git commit -m "feat(api): registry cancel machinery and cancelled status"`

---

### Task 3: Worker per-job cancellation

**Files:** `server/api/worker.go`, `server/api/worker_test.go`

- [ ] **Step 1 — failing tests** append to `server/api/worker_test.go`:

```go
func TestDownloadCancelledMidTransfer(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	fetchStarted := make(chan struct{})
	// fetch blocks until its ctx is cancelled, simulating an in-flight transfer.
	w.fetchCtx = func(ctx context.Context, baseDir, dccStr string, progress io.Writer) (string, error) {
		close(fetchStarted)
		<-ctx.Done()
		return "", ctx.Err()
	}
	sess.downloadReply = []Event{{Kind: EvBookResult, Text: "DCC SEND x 2130706433 6669 10"}}
	job := NewDownloadJob("!x")
	reg.Enqueue(job)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.RunDownload(ctx)

	<-fetchStarted
	if _, e := reg.Cancel(job.ID); e != nil {
		t.Fatalf("cancel: %+v", e)
	}
	waitStatus(t, reg, job, StatusCancelled)
}

func TestQueuedDownloadCancelledIsSkipped(t *testing.T) {
	sess := newFakeSession()
	w, reg := newTestWorker(t, sess)
	job := NewDownloadJob("!x")
	reg.Enqueue(job)
	reg.Cancel(job.ID) // cancel while queued
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.RunDownload(ctx)
	// job stays cancelled, never runs (fetch never called)
	time.Sleep(50 * time.Millisecond)
	snap, _ := reg.Get(job.ID)
	if snap.Status != StatusCancelled {
		t.Errorf("status = %s, want cancelled", snap.Status)
	}
	if len(sess.downloaded) != 0 {
		t.Error("cancelled queued job should not be sent to IRC")
	}
}
```

Note: this introduces `w.fetchCtx` (a ctx-aware fetch hook). The worker keeps `w.fetch` for search (unchanged) and adds `w.fetchCtx` for downloads.

- [ ] **Step 2 — run:** fails (`w.fetchCtx` undefined; download not cancellable).

- [ ] **Step 3 — implement** in `server/api/worker.go`:

Add the ctx-aware fetch field and default in `NewWorker`:
```go
	fetch    func(baseDir, dccStr string, progress io.Writer) (string, error)          // search
	fetchCtx func(ctx context.Context, baseDir, dccStr string, progress io.Writer) (string, error) // download
```
```go
	w.fetch = core.DownloadExtractDCCString
	w.fetchCtx = core.DownloadExtractDCCStringContext
```

In `doDownload`, wrap a per-job context and register the cancel func. After the browser check and before `Connect` is fine, but the cancel must cover the whole job:
```go
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
				// ... unchanged position-record + bounded-reset block ...
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
```
(Keep the existing `EvQueuePosition` body from PR #15 verbatim in the marked spot.)

`completeDownload` takes and uses the ctx:
```go
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
	w.reg.Update(job, func(j *Job) { j.Path = &path; j.FileName = &name })
	w.reg.Finish(job, nil)
}
```
(`w.cfg.LibrarySubdir` is added in Task 5; for now use `filepath.Join(w.cfg.DownloadDir, "books")` and change it in Task 5 — OR add the `LibrarySubdir` field to `WorkerConfig` now defaulting to "books". To avoid churn, add `LibrarySubdir string` to `WorkerConfig` in this task and default it to `"books"` in `newTestWorker`.)

Also thread `jobCtx` through `doSearch` for parity (register SetCancel/ClearCancel and add a `case <-jobCtx.Done()` mirroring the download handling with `FinishCancelled`). Keep it minimal but present so `DELETE /api/search/{id}` works.

- [ ] **Step 4 — run:** `gofmt -l ./server && go vet ./server/api/ && go test -race -count=3 ./server/api/` → clean/ok.
- [ ] **Step 5 — commit:** `git add server/api/worker.go server/api/worker_test.go && git commit -m "feat(api): per-job cancellation in search/download workers"`

---

### Task 4: Cancel endpoints + OpenAPI

**Files:** `server/api/api.go`, `server/api/api_test.go`, `server/api/openapi.yaml`

- [ ] **Step 1 — failing tests** append to `server/api/api_test.go`:

```go
func TestCancelEndpoints(t *testing.T) {
	a := newTestAPI(t, false)
	// queue a download, cancel it
	_, m := call(t, a, "POST", "/download", `{"book":"!x y.epub"}`, "tok")
	id := m["jobId"].(string)
	rec, cm := call(t, a, "DELETE", "/download/"+id, "", "tok")
	if rec.Code != 200 || cm["status"] != "cancelled" {
		t.Fatalf("cancel = %d %v", rec.Code, cm)
	}
	// second cancel → not_cancellable
	rec, cm = call(t, a, "DELETE", "/download/"+id, "", "tok")
	if rec.Code != 409 || cm["code"] != "not_cancellable" {
		t.Errorf("re-cancel = %d %v", rec.Code, cm)
	}
	// unknown → 404
	rec, _ = call(t, a, "DELETE", "/download/00000000-0000-0000-0000-000000000000", "", "tok")
	if rec.Code != 404 {
		t.Errorf("unknown = %d", rec.Code)
	}
	// unauth → 401
	rec, _ = call(t, a, "DELETE", "/download/"+id, "", "")
	if rec.Code != 401 {
		t.Errorf("unauth = %d", rec.Code)
	}
	// type mismatch (search id space) → 404
	rec, _ = call(t, a, "DELETE", "/search/"+id, "", "tok")
	if rec.Code != 404 {
		t.Errorf("type mismatch = %d", rec.Code)
	}
}
```

- [ ] **Step 2 — run:** fails (405/404 for DELETE).

- [ ] **Step 3 — implement** in `server/api/api.go`. In `Router()`'s protected group add:
```go
		p.Delete("/search/{id}", a.cancelJob(JobSearch))
		p.Delete("/download/{id}", a.cancelJob(JobDownload))
```
Add the handler:
```go
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
```

- [ ] **Step 4 — openapi.yaml:** add `delete` operations under `/search/{jobId}` and `/download/{jobId}` (auth required; 200 job, 404 `job_not_found`, 409 `not_cancellable`), add `cancelled` to the job `status` enum and `not_cancellable` to the error responses list.

- [ ] **Step 5 — run:** `gofmt -l ./server && go vet ./server/api/ && go test -race ./server/api/` → clean/ok. Also `python3 -c "import yaml; yaml.safe_load(open('server/api/openapi.yaml'))"` (skip if PyYAML missing).
- [ ] **Step 6 — commit:** `git add server/api/api.go server/api/api_test.go server/api/openapi.yaml && git commit -m "feat(api): DELETE cancel endpoints for search/download jobs"`

---

### Task 5: Configurable `--library-subdir`

**Files:** `server/server.go`, `server/routes.go`, `server/irc_events.go`, `server/api/api.go`, `server/api/worker.go`, `cmd/openbooks/server.go`, `server/routes_test.go`

- [ ] **Step 1 — failing test** append to `server/routes_test.go` (uses `NewHandler` + `silentIRC` already there): write a book file into a temp dir root (empty subdir) and assert `GET /openbooks/library` lists it. Then a default-subdir server lists from `<dir>/books`.

```go
func TestLibraryHonorsSubdir(t *testing.T) {
	dir := t.TempDir()
	// empty subdir → library reads the dir root
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "root-book.epub"), []byte("x"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := NewHandler(ctx, Config{
		Basepath: "/openbooks/", DownloadDir: dir, LibrarySubdir: "", Persist: true,
		SearchTimeout: 10 * time.Second, UserName: "t", Server: silentIRC(t), APIToken: "tok", Version: "t",
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	res, _ := http.Get(ts.URL + "/openbooks/library")
	defer res.Body.Close()
	var books []map[string]any
	json.NewDecoder(res.Body).Decode(&books)
	found := false
	for _, b := range books {
		if b["name"] == "root-book.epub" {
			found = true
		}
	}
	if !found {
		t.Errorf("empty subdir: library did not list root-book.epub: %v", books)
	}
}
```
(Add `os`, `path/filepath` imports to the test file if missing.)

- [ ] **Step 2 — run:** fails (unknown field `LibrarySubdir`).

- [ ] **Step 3 — implement.**
  - `server/server.go` `Config`: add `LibrarySubdir string`. In `New`, pass it into `api.Config` (add field there) and `WorkerConfig` (already added in Task 3). Helper: `func (c Config) libraryDir() string { return filepath.Join(c.DownloadDir, c.LibrarySubdir) }`. Replace `createBooksDirectory` to `os.MkdirAll(config.libraryDir(), 0755)`.
  - `server/routes.go`: replace the three `filepath.Join(server.config.DownloadDir, "books"...)` with `filepath.Join(server.config.libraryDir(), ...)` (getAllBooks uses `server.config.libraryDir()`; getBook/deleteBooks join the fileName onto it).
  - `server/irc_events.go` `bookResultHandler`: it receives `downloadDir` — change to receive the full library dir (thread `server.config.libraryDir()` from `NewIrcEventHandler`), or compute `filepath.Join(downloadDir, subdir)`. Simplest: pass `server.config.libraryDir()` into `bookResultHandler` instead of `downloadDir`.
  - `server/api/api.go` `Config`: add `LibrarySubdir string`; pass into `WorkerConfig{LibrarySubdir: cfg.LibrarySubdir ...}`. Default to `"books"` in `New` if empty *only when the caller didn't explicitly intend empty* — but empty is a valid intended value, so DO NOT default it in api.New; the server sets it explicitly. In `server.New`, set `LibrarySubdir: config.LibrarySubdir` and default the **flag** to "books" (below) so unset CLI keeps today's behavior.
  - `cmd/openbooks/server.go`: `serverCmd.Flags().StringVar(&serverConfig.LibrarySubdir, "library-subdir", "books", "Subdirectory under --dir where downloaded books are stored and served. Empty = --dir root.")`.
  - `server/api/worker.go` `completeDownload`: already uses `filepath.Join(w.cfg.DownloadDir, w.cfg.LibrarySubdir)` from Task 3.

- [ ] **Step 4 — run:** `gofmt -l . && go vet ./... && go test -race ./server/... ./cmd/...` → clean/ok. Confirm existing library tests (default subdir) still pass.
- [ ] **Step 5 — commit:** `git add -A && git commit -m "feat(server): configurable --library-subdir (default books)"`

---

### Task 6: MCP cancel tools

**Files:** `~/openbooks-mcp/openbooks_mcp_server/server.py`, `tests/test_server.py`, `README.md`

- [ ] **Step 1 — failing tests** in `~/openbooks-mcp/tests/test_server.py`: extend `FakeAPI.handler` to answer `DELETE /download/d1` → 200 `{jobId,status:"cancelled",...}` and `DELETE /download/gone` → 409 `{code:"not_cancellable"}`. Add:
```python
def test_cancel_download(api):
    out = s.cancel_download(jobId="d1")
    assert out["status"] == "cancelled"

def test_cancel_download_not_cancellable(api):
    out = s.cancel_download(jobId="gone")
    assert out["error"] == "not_cancellable" and out["hint"]
```
(Add a `DELETE`/`gone` branch to the fake.)

- [ ] **Step 2 — implement** in `server.py`: add `HINTS["not_cancellable"] = "That job already finished (complete/error/cancelled); nothing to cancel."`; add client methods `cancel_search`/`cancel_download` doing `self._call("DELETE", f"/{kind}/{jid}")`; add `@mcp.tool()` `cancel_download(job_id="", jobId="")` and `cancel_search(...)` that resolve the id and call through, returning the job or error dict.
- [ ] **Step 3 — run:** `~/.local/share/pipx/venvs/stash-mcp-server/bin/python -m pytest -q tests/` → all pass. README: add the two tools to the tool list.
- [ ] **Step 4 — commit** in `~/openbooks-mcp`: `git add -A && git commit -m "mcp: cancel_download / cancel_search tools"`.

---

### Task 7: Docs (book formats) + integration test + PR

**Files:** `docs/docs/api.md`, `tests/api_integration_test.go`

- [ ] **Step 1 — docs:** in `docs/docs/api.md` add a "Cancelling jobs" note (`DELETE /api/{search,download}/{jobId}` → `cancelled`; `409 not_cancellable` if already finished) and a "book string formats" section:
  - Bare: `!Server Author - Title (format).ext` (most servers)
  - Firebound: `%HASH% Author - Title...` (leading `%…%` token)
  - TrainFiles: `hash | Author - Title...` (leading `hash |`)
  - Note: treat `book` as opaque; always pass the search result's `book` verbatim to download; it always starts with `!`.
  Update the endpoints table with the DELETE rows.

- [ ] **Step 2 — integration test** in `tests/api_integration_test.go`: add `TestAPICancelDownload` — start a download whose fake IRC server sends the DCC offer but the fake DCC server dribbles bytes slowly (announce a large size), `DELETE /api/download/{id}`, poll, assert `status == "cancelled"` and a subsequent search job (or the same queue) proceeds. Keep it bounded (<5s). Reuse `startApiIrcServer`/`startDccServer` patterns; add a slow/large DCC variant.

- [ ] **Step 3 — full suite:** `gofmt -l .; go vet ./...; go test -race ./...; go test -race -tags=integration ./...` all green.
- [ ] **Step 4 — commit + PR** against `master`, title `feat(api): cancel endpoint + configurable library dir + book-format docs`, body summarizing items 5/8/9, spec+plan links, and a test-plan checkbox for the deploy step (roll with `--library-subdir ""`).
