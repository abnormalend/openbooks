# OpenBooks API — cancel endpoint, configurable library dir, book-format docs — Design

**Date:** 2026-08-16 · **Branch:** `feature/api-cancel-and-library-dir` · **Status:** approved

Source: Sage's handoff `~/hermes-handoffs/openbooks-improvements-2026-08-16.md`, items 5, 8, 9 (item 6 already shipped in PR #15; item 7 deferred — needs its own design; items 1–4 shipped in openbooks-mcp).

## Why

- **#5 Cancel (highest value).** A download job can sit `running` while a bot ignores the DCC offer (bounded now by `DownloadTimeout` per PR #15) or, worse, while a mid-transfer read stalls (the DCC transfer in `completeDownload` is unbounded). Today the only recovery is restarting the container, which wipes all in-memory job/queue state. A cancel endpoint frees the slot and aborts the in-flight transfer without a restart.
- **#9 Download destination.** Downloads land in `<DownloadDir>/books/`, but Grimmory watches the `<DownloadDir>` root, forcing a manual `mv` — the last manual step in the find→download→Kindle loop. Make the subdir configurable (default `books`, unchanged) so the deploy can set it empty and files land in the watched root. **Chosen approach: server-side, write to the watched root** (the `books` subdir moves for the browser UI's library list/serve/delete paths too).
- **#8 `book` formats.** The `book` string varies by server (bare, Firebound `%HASH%`, TrainFiles `hash |`); undocumented variance makes clients treat it as opaque. Document it.

## Scope

In:
- `DELETE /api/download/{jobId}` and `DELETE /api/search/{jobId}` → cancel a queued or running job. New terminal status `cancelled`.
- Context-cancellable DCC download so a running-job cancel aborts the in-flight transfer (new `core.DownloadExtractDCCStringContext` + `dcc.Download.DownloadContext`; the existing non-ctx funcs delegate with `context.Background()` — non-breaking for browser/CLI/search paths).
- `--library-subdir` config (default `books`), threaded through the download path and the browser library routes.
- `cancel_download` / `cancel_search` tools in `openbooks-mcp`.
- A "book string formats" section in `docs/docs/api.md` + the openapi.yaml DELETE routes.

Out: item 7 (per-server queue depth — separate design); any change to search/download job semantics beyond cancel; SPA changes (the library routes are REST, the SPA already calls them).

## Design

### Cancel (`server/api/jobs.go`, `worker.go`, `api.go`)

- New `StatusCancelled Status = "cancelled"`.
- Registry gains a `cancels map[uuid.UUID]context.CancelFunc` (guarded by the existing `mu`) plus a `cancelled map[uuid.UUID]bool` marker:
  - `SetCancel(id, fn)` / `ClearCancel(id)` — the worker registers its per-job cancel func on start, clears on finish.
  - `Cancel(id) (Job, *APIError)` — endpoint logic under lock:
    - unknown id → `job_not_found` (404).
    - terminal (`complete`/`error`/`cancelled`) → `not_cancellable` (409).
    - `queued` → set `Status=cancelled`, `FinishedAt=now`; leave it in the channel (the worker skips it on dequeue — the channel receive frees the slot).
    - `running` → set the `cancelled[id]` marker, call the stored cancel func; the worker observes its job-ctx cancellation and finalizes the job as `cancelled`.
- Worker: `doSearch`/`doDownload` create `jobCtx, jobCancel := context.WithCancel(ctx)`, call `reg.SetCancel(job.ID, jobCancel)`, `defer jobCancel()` + `reg.ClearCancel(job.ID)`. The select loop adds `case <-jobCtx.Done():` — if `reg.WasCancelled(job.ID)` finalize via `reg.FinishCancelled(job)` (sets `StatusCancelled`), else it's parent shutdown → existing `cancelled` **error** path. `completeDownload` takes `jobCtx` and uses the ctx-aware fetch, so a cancel during transfer aborts it (job then finalizes `cancelled`).
- `Next` must not flip an already-cancelled queued job to `running`: after the channel receive, under lock, if `Status==cancelled` skip it and receive again.
- Endpoints: `DELETE /api/{search,download}/{id}` → `Cancel`; 200 with the job JSON on success.

### Cancellable DCC (`dcc/dcc.go`, `core/file.go`)

- `func (d Download) DownloadContext(ctx context.Context, w io.Writer) error`: same loop, but after `net.Dial` spawn `go func(){ <-ctx.Done(); conn.Close() }()` so a cancelled ctx closes the conn and the blocking `conn.Read` returns an error. `Download(w)` becomes `DownloadContext(context.Background(), w)`.
- `core.DownloadExtractDCCStringContext(ctx, baseDir, dccStr, progress)` mirrors `DownloadExtractDCCString` (which delegates with `context.Background()`), passing ctx into `DownloadContext`.

### Configurable library dir (`server/*`)

- `Config.LibrarySubdir string` (default `"books"`), flag `--library-subdir`. Empty ⇒ `filepath.Join(dir, "")` = the root.
- Thread into `api.Config`/`WorkerConfig`; replace the hardcoded `"books"` in `createBooksDirectory`, `routes.go` (getAllBooks/getBook/deleteBooks), `worker.go completeDownload`, and the browser `bookResultHandler`. Search-result zips stay in `os.TempDir()` (unchanged).
- Deploy sets `--library-subdir ""` so downloads land in `/mnt/nfs/booklore/bookdrop` directly. Note: with an empty subdir the browser "library" view lists the whole bookdrop root (all sources, not just OpenBooks), and the `logs/` dir (from `--log`) is skipped by the existing dir/dotfile filter. Acceptable.

### MCP (`~/openbooks-mcp`)

- `cancel_download(job_id="", jobId="")` and `cancel_search(...)` → `DELETE`. Return the cancelled job or the structured error. Hints for `not_cancellable`.

### Docs

- `docs/docs/api.md`: a "book string formats" table (bare / Firebound `%HASH%` / TrainFiles `hash |`), a note that they're opaque and passed verbatim, and the DELETE cancel routes + `cancelled` status + `not_cancellable` error. Update `server/api/openapi.yaml` with the DELETE operations and the `cancelled` status enum.

## Tests

- `dcc`: `DownloadContext` returns promptly when ctx is cancelled mid-read (fake slow DCC server).
- `server/api`: cancel a queued job (skipped on dequeue, slot freed, status `cancelled`); cancel a running download (jobCtx fires, transfer aborts via a fake fetch that blocks on ctx, status `cancelled`); cancel terminal → `not_cancellable`; cancel unknown → `job_not_found`; `DELETE` routes behind auth.
- `server`: library routes honor a non-default subdir (and empty) via `NewHandler` + httptest.
- integration: start a download, `DELETE` it, assert `cancelled` and the queue frees.
- openbooks-mcp: `cancel_download` both id spellings; terminal → `not_cancellable` hint.

## Verification / rollout

1. `go test -race ./...` + `-tags=integration`; MCP `pytest`.
2. New image; roll the `books` stack adding `--library-subdir ""` to the command; confirm a real download lands in the bookdrop root (no manual `mv`) and Grimmory imports it, and that `DELETE` on a running job frees the queue.
