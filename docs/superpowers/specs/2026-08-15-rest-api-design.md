# OpenBooks REST API — Design

**Date:** 2026-08-15 · **Branch:** `feature/rest-api` · **Status:** approved

## Why

OpenBooks is the acquisition front-end of the home library stack: it searches
IRC book servers and drops ebooks into a shared bookdrop that BookLore picks
up. Today the only way to drive it is the React SPA over a websocket. An agent
(Hermes, via an MCP wrapper) needs to do the same three things a human does —
**search, read the results, download one of them** — over plain HTTP.

This spec supersedes the hand-off in `rgrs_home_network/plans/openbooks-api-enhancement.md`
where the two disagree; that plan was written without knowledge of the
codebase (e.g. it assumed downloads are keyed by hash/ip/port; they are keyed
by the result's `full` string).

## Scope

In:
- Job-based `POST /api/search`, `POST /api/download` + polling `GET`s
- `GET /api/jobs`, `GET /api/servers`, `GET /api/health`, `GET /api/openapi.yaml`
- Bearer-token auth, fail-closed when unconfigured
- One headless IRC session for the API, mutually exclusive with the browser
- Structured JSON errors; committed `openapi.yaml`
- Unit + integration tests against the in-repo mock IRC/DCC servers

Out (later passes): rich library listing/pagination, SSE progress streams,
download dedupe, any change to the websocket protocol or the SPA, any change
to `core/`/`dcc/`/`irc/` beyond what the session needs.

## Architecture

New package **`server/api`**. `server/routes.go` mounts it at `<BASE_PATH>api/`.
Nothing in the existing websocket path changes except one hook in `serveWs`
(see *Mutual exclusion*).

```
HTTP handler ──▶ Registry (jobs, queues) ──▶ Session (one irc.Conn + StartReader)
                     ▲                              │
                     └──── event callbacks ◀────────┘  (core.EventHandler)
```

### Session (`server/api/session.go`)

One process-wide headless IRC session. It owns an `*irc.Conn`, connects with
the configured `--name` nick (same nick as the browser would use), joins
`#ebooks`, and runs `core.StartReader` with an `EventHandler` map that routes
events into jobs (the API-side twin of `server/irc_events.go`).

- **Lazy start:** created and connected on the first job that reaches the
  front of a queue. Connect = `core.Join` (dial + USER/NICK + 2s + JOIN).
- **Idle stop:** a timer disconnects the session after `APIIdleTimeout`
  (default 5m) with no running or queued job. Next job reconnects.
- **Drop recovery:** if the reader loop exits (socket closed), the session is
  marked disconnected; the next job reconnects. A job whose session drops
  mid-flight fails with `error.code = "irc_disconnected"`.
- Handlers wired: `SearchResult`, `NoResults`, `MatchesFound`,
  `SearchAccepted`, `BookResult`, `BadServer`, `Ping`, `Version`,
  `ServerList` (updates `server.repository.servers` — same as the browser
  path), and `Message` for the optional `--log` file.

### Mutual exclusion with the browser

Same nick ⇒ only one may be online.

- **Browser connects while API session is connected:**
  - no running/queued job → API session disconnects immediately (yields);
    browser proceeds.
  - a job is running/queued → `serveWs` returns `409` with body
    `API session active`. (Today the second-client case is a bare `400`;
    that stays for the "another browser" case.)
- **API job submitted while a browser client is connected** →
  `409 {"code":"browser_session_active"}`. No queuing behind a human.

Implementation: `server` exposes `browserConnected() bool` (from `clients`)
and the API package exposes `Session.Yield() bool` (returns false if busy).
`serveWs` calls `Yield` before upgrading.

### Jobs (`server/api/jobs.go`)

```go
type Status string // queued | running | complete | error

type Job struct {
    ID         uuid.UUID
    Type       JobType   // search | download
    Status     Status
    CreatedAt, StartedAt, FinishedAt time.Time
    Error      *APIError // {code,message}
    // search
    Query      string
    Limit      int
    Results    []core.BookDetail
    ParseErrs  int
    // download
    Book       string
    Bytes, Size int64
    Path, FileName string
}
```

`Registry` = mutex-guarded `map[uuid.UUID]*Job` + two bounded FIFO queues
(`searchQ`, `downloadQ`, cap 3 each) + one worker goroutine per queue.

- **Search worker:** pops a job, waits for the shared rate limiter
  (`server.lastSearch` / `SearchTimeout` — same mutex the browser path uses,
  so combined traffic to the search bot never exceeds one query per
  `SearchTimeout`), sends `core.SearchBook`, then waits for one of:
  `SearchResult` (download+parse zip → `complete`, truncate to `Limit` if
  `>0`), `NoResults` (`complete`, empty results), timeout
  `SearchJobTimeout` (default 120s → `error.code="timeout"`).
- **Download worker:** pops a job, sends `core.DownloadBook(full)`, waits for
  `BookResult` (DCC download into `<DownloadDir>/books` with a progress
  writer updating `Bytes`; `Size` from the parsed DCC string → `complete`,
  `Path`/`FileName` set), `BadServer` (`error.code="server_unavailable"`),
  or timeout `DownloadJobTimeout` (default 10m).
- Single-flight per queue is required for correctness: neither the search
  bot's DCC SEND nor the download bot's carries the request identity, so
  "the event that arrives while job X is running belongs to X".
- Search and download workers run concurrently with each other (distinct
  bots, distinguishable events), matching UI behaviour.
- **TTL:** finished jobs are dropped 1h after `FinishedAt` by a sweeper
  goroutine (every minute).
- **Queue full** → `409 {"code":"queue_full"}`.

### Auth (`server/api/auth.go`)

- Config: `--api-token` flag; if flag unset, env `OPENBOOKS_API_TOKEN`
  (same precedence pattern `BASE_PATH` uses today).
- Middleware on all `/api/*` except `/health` and `/openapi.yaml`.
- `Authorization: Bearer <token>`, `crypto/subtle.ConstantTimeCompare`.
- Unset token → `503 {"code":"api_disabled","message":"API disabled: set --api-token or OPENBOOKS_API_TOKEN"}` for every protected route.
- Missing/wrong → `401 {"code":"unauthorized"}`.
- Cookie/websocket path untouched.

## HTTP surface

Base: `<BASE_PATH>api/` (e.g. `/openbooks/api/`). JSON in/out,
`Content-Type: application/json`.

| Method & path | Auth | Body / params | Response |
|---|---|---|---|
| `GET /health` | no | – | `200 {version, uptimeSeconds, downloadDir, basePath, apiEnabled, ircConnected, browserConnected, search:{running:bool,queued:int}, download:{running:bool,queued:int}}` |
| `GET /openapi.yaml` | no | – | the committed spec |
| `POST /search` | yes | `{"query":"fourth wing","limit":25}` — `query` required non-empty; `limit` optional, `0`/omitted = all | `202 {"jobId","status":"queued","position":N}` |
| `GET /search/{jobId}` | yes | – | `200 SearchJob` (below) |
| `POST /download` | yes | `{"book":"!DV8 F. Scott Fitzgerald - The Great Gatsby (Epub).rar"}` — required, must start with `!` | `202 {"jobId","status":"queued","position":N}` |
| `GET /download/{jobId}` | yes | – | `200 DownloadJob` |
| `GET /jobs` | yes | `?type=search\|download` (optional) | `200 {"jobs":[...]}` all non-expired jobs, newest first |
| `GET /servers` | yes | – | `200 {"servers":[...]}` from `repository.servers` (empty until a session has received the NAMES list) |

```jsonc
// SearchJob
{"jobId":"…","type":"search","status":"complete","query":"fourth wing","limit":25,
 "createdAt":"…","startedAt":"…","finishedAt":"…",
 "results":[{"server":"DV8","author":"Rebecca Yarros","title":"Fourth Wing","format":"epub","size":"1.2MB","full":"!DV8 Rebecca Yarros - Fourth Wing.epub"}],
 "parseErrors":0, "error":null}

// DownloadJob
{"jobId":"…","type":"download","status":"running","book":"!DV8 …",
 "createdAt":"…","startedAt":"…","finishedAt":null,
 "bytes":524288,"size":1258291,"path":null,"fileName":null,"error":null}
// on complete: "path":"/books/books/Fourth Wing.epub","fileName":"Fourth Wing.epub"
```

Optional fields are `null` (not omitted) so clients get a stable shape.

### Errors

Every non-2xx from `/api/*` is `{"code":"…","message":"…"}`:

| HTTP | code | when |
|---|---|---|
| 400 | `bad_request` | malformed JSON, missing `query`/`book`, bad `type` filter |
| 401 | `unauthorized` | missing/invalid bearer token |
| 404 | `job_not_found` | unknown or expired jobId, or type mismatch (`/search/{id}` for a download job) |
| 409 | `browser_session_active` | a browser websocket client is connected |
| 409 | `queue_full` | 3 jobs already queued for that type |
| 503 | `api_disabled` | no token configured |

Job-level `error.code` values: `timeout`, `server_unavailable`,
`irc_connect_failed`, `irc_disconnected`, `dcc_failed`, `parse_failed`.
A search that returns nothing is *not* an error: it finishes `complete` with
an empty `results` array.

## Config & CLI

`server.Config` gains: `APIToken string`, `APIIdleTimeout`,
`SearchJobTimeout`, `DownloadJobTimeout time.Duration`, `Version string`.

`openbooks server` flags: `--api-token`, `--api-idle-timeout` (5m),
`--search-job-timeout` (120s), `--download-job-timeout` (10m). `Version` is
set from `cmd/openbooks/main.go`'s `version` var. Desktop mode leaves the
API disabled (no token).

`Start` also widens CORS `AllowedMethods` to include `POST` and
`AllowedHeaders` already covers `Authorization` (`*`); production is
same-origin/curl so this only matters for the Vite dev server.

## Testing

- `server/api` unit tests (`go test -race`): registry queue depth & 409s,
  TTL sweep, worker state transitions with a fake session (interface
  `ircSender` with `SearchBook`/`DownloadBook`), auth middleware matrix
  (unset / missing / wrong / right), handler JSON shapes via `httptest`.
- `tests/` integration (`-tags=integration`): start `server.Start`-equivalent
  in-process against the existing mock IRC + DCC servers; `POST /search` →
  poll to `complete` with parsed results → `POST /download` with a result's
  `full` → poll to `complete` → file exists under the temp books dir. Also:
  unauth'd call → 401; browser ws connect while a job runs → 409.
- Frontend tests unchanged; SPA untouched.

## Ship

PR from `feature/rest-api`; CI publishes `ghcr.io/abnormalend/openbooks:pr-N`;
`books` stack on compute3 gets `OPENBOOKS_API_TOKEN` and the new tag.
`docs/docs/` gains an "API" page with a curl walkthrough for the
`openbooks-mcp` follow-up.
