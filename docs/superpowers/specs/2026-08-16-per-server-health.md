# OpenBooks per-server health / queue signal — Design

**Date:** 2026-08-16 · **Status:** proposed (Sage handoff item #7) · **Branch:** TBD `feature/per-server-health`

## The honest framing

IRC book bots do **not** expose their queue depth on demand — there is no "how deep is your queue" query, and the `#ebooks` firehose of other users' requests is addressed to other nicks (our `--log`/`ircLogHandler` filter already drops non-us lines) and is noisy/unreliable. So "live per-server queue depth" is **not obtainable**. What *is* obtainable and actionable:

1. **Presence** — the NAMES (353/366) elevated-user list already tells us which servers are online (`/api/servers`).
2. **Our own last-observed queue position at a server** — the `queueposition N` notice during a download (already an `EvQueuePosition` event as of PR #15), attributable to the server we requested from. This is "how deep *we* were last time", not live depth — label it as such.
3. **Per-server outcome history** — the API already sees every download's result (complete / `server_unavailable` / `timeout` / `dcc_failed` / `irc_disconnected` / `cancelled`). Accumulated per server, this turns the current tribal knowledge ("prefer Horla/TrainFiles, avoid Bsk/Dumbledore") into a machine-readable, self-updating reliability signal.

**Chosen approach: passive per-server stats derived from the API's own job outcomes + queue notices.** Rejected: active probing (impossible); channel-firehose scraping (filtered out, noisy, unreliable).

## Attribution

A download job stores its `book` string, which always begins `!<server> …` (bare `!DV8 Author - Title.epub`, Firebound `!Firebound %HASH% …`, TrainFiles `!TrainFiles hash | …` — the leading `!<server>` token is uniform). Server = `book[1:strings.Index(book," ")]`, identical to the search result's `server` field (`core/search_parser.go getServer`). No new parsing risk.

## Data model (`server/api/serverstats.go`, in-memory)

```go
type ServerStat struct {
    Server              string
    Online              bool                 // from the NAMES elevated list at read time
    Attempts            int
    Completed           int
    Failed              int
    FailuresByCode      map[string]int       // server_unavailable, timeout, dcc_failed, irc_disconnected, cancelled
    LastQueuePosition   int                  // our last observed position at this server (0 = unknown)
    LastQueuePositionAt *time.Time
    RecentFailStreak    int                  // consecutive failures since the last completion (reset on complete)
    LastSuccessAt       *time.Time
    LastFailureAt       *time.Time
    AvgCompleteSeconds  float64              // rolling over completed downloads
    Health              string               // healthy | degraded | down | unknown
}
```

- A `ServerStats` struct: `mu sync.Mutex` + `map[string]*ServerStat` + injectable `now func() time.Time`. One entry per server (~6–12 total, bounded, no TTL — stats should outlive the 1h job TTL).
- Held by the `API`; a pointer is passed into `WorkerConfig` so the worker records outcomes. `Online` is merged from `Deps.Servers().ElevatedUsers` at read time (presence is authoritative from NAMES, not from our history).

### Recording points (worker `doDownload`)

- start (after server parse): `Attempts++`.
- `EvQueuePosition`: `LastQueuePosition = pos`, `LastQueuePositionAt = now`.
- complete: `Completed++`, `LastSuccessAt = now`, fold duration into `AvgCompleteSeconds`.
- `fail(code)` / `EvBadServer`: `Failed++`, `FailuresByCode[code]++`, `LastFailureAt = now`. (`server_unavailable` is the strongest avoid signal.)
- Cancellations count as `cancelled` but should be weighted lightly in health (user-initiated, not a server fault).

### Health heuristic — pinned constants (refinement 1)

Thresholds are concrete named constants so the label is deterministic and the
table test is meaningful:

```go
const (
    healthWindow        = time.Hour // "recent" success recency window
    downFailStreak      = 3         // consecutive failures (no recent success) => down
    degradedSuccessRate = 0.60      // success rate below this => degraded
)
```

`RecentFailStreak` increments on every fail and resets to 0 on every complete.
`successRate = Completed / max(1, Completed + (Failed - FailuresByCode["cancelled"]))`
— cancellations are excluded from the denominator (weighted lightly, user-initiated).

Deterministic classification (first match wins):
- `unknown` — `Attempts == 0`.
- `down` — `!Online`, OR (`RecentFailStreak >= downFailStreak` AND (`LastSuccessAt == nil` OR `now - LastSuccessAt > healthWindow`)).
- `healthy` — `LastSuccessAt != nil` AND `now - LastSuccessAt <= healthWindow` AND `successRate >= degradedSuccessRate`.
- `degraded` — has attempts but matches none of the above.

The record carries the raw counts + `RecentFailStreak` alongside the label so a
client can apply its own policy instead of trusting `Health`. `now` is injectable
for the table test.

## Endpoint

**New** `GET /api/server-stats` (auth) → `{"servers": [ServerStat, …]}` sorted by health (healthy→down) then name. **Do not change `/api/servers`** (ops skill + Hermes depend on its shape — hard constraint from the handoff). openapi.yaml + `docs/docs/api.md` document it, and state plainly that `lastQueuePosition` is our last observed position, not live depth.

## MCP (`~/openbooks-mcp`)

- `server_stats()` tool → `GET /api/server-stats`.
- Search-flow guidance: rank candidate results' servers by health with a fixed
  order **`healthy(0) > unknown(1) > degraded(2) > down(3)`** (lower = prefer).
  `unknown` is deliberately NEUTRAL and ranks above `degraded`/`down` — a
  cold-start server is not penalized for having no history, otherwise the
  folklore self-reinforces forever (refinement 2). Tie-break on
  `lastQueuePosition` (lower better), then higher `successRate`. Document in the
  skill/README.

## Non-goals / future

- No active probing; no channel scraping.
- **In-memory only in v1** (stats reset on redeploy). Optional future step: persist the map as JSON under `<DownloadDir>` (or a sibling) so stats survive a container roll — cheap follow-up, not v1.
- No auto-selection server-side (the API stays a mechanism; the client/MCP ranks).

## Tests

- `serverstats`: each outcome transition; the health heuristic as a table; concurrent record under `-race`.
- attribution parse for bare/Firebound/TrainFiles `book` strings.
- `server/api`: `/api/server-stats` shape + auth + sort order; `Online` reflects `Deps.Servers()`.
- integration: two downloads against fake servers (one completes, one `BadServer`) → assert stats/health reflect it.
- MCP: `server_stats` both id-less call + error shape.

## Effort

Medium — one stats module + worker hooks + one endpoint + MCP tool + docs; comparable to the cancel PR. No changes to `core`/`dcc`/`irc`.
