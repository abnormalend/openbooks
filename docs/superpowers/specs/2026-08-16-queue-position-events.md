# OpenBooks queue-position events + download timeout hardening — Design

**Date:** 2026-08-16 · **Branch:** `master` · **Status:** proposed

## Why

The REST API's download worker waits for a `BookResult` event (a DCC SEND
offer) and otherwise fails the job on `DownloadTimeout`. Two gaps bit a real
download on 2026-08-16 (Congo via Dumbledore):

1. **Queue notices are invisible to the API.** IRC book bots routinely answer
   a download request with `NOTICE <nick> :Added <book> to queueposition 5.`
   The reader (`core/reader.go`) classifies NOTICEs by four substrings
   (`Sorry`, `try another server`, `has been accepted`, `matches`); a queue
   notice matches none of them and falls through to `noOp`, which no handler
   is registered for. The API session then waits out the whole download
   timeout with no signal that the job is actually making progress. The
   browser UI sees these lines (the `ircLogHandler` log panel), so the
   asymmetry is API-only.
2. **The download timeout never fires when the send blocks.** In
   `doDownload`, `time.NewTimer(DownloadTimeout)` is created *after*
   `sess.DownloadBook()` returns. `DownloadBook` does a blocking
   `net.Conn.Write` on the IRC socket; if the peer wedges (send buffer full),
   the Write blocks forever and the timer is never created — the job sits
   `running` indefinitely and blocks the whole download queue behind it
   (observed: 20+ minutes past the 10m default, no `failed: timeout` log).

Also: the API session only logs raw IRC lines when `--log` is passed, so a
silent bot reply is currently unrecoverable in post-mortem.

## Scope

In:
- New `QueuePosition` event in `core/reader.go` (NOTICE/PRIVMSG containing
  `queueposition` or `queue position`, targeted at our nick).
- API session routes it to the download worker; the worker records it on the
  job (new `position` field + `queued` status note) instead of ignoring it.
- `doDownload` creates the timeout timer *before* the send, and/or
  `irc.Conn.Write` gets a write deadline, so a wedged socket cannot hang a
  job forever.
- Document/enable `--log` for the API session in the deploy (stack compose).

Out (later passes): SPA/websocket changes, richer status streams, any change
to search-job event handling.

## Architecture

### 1. Reader: classify queue notices (`core/reader.go`)

Add a `QueuePosition` event constant and detect it in the NOTICE branch:

```
queuePosition = "queueposition"  // also match "queue position"
```

Detection order matters: `queueposition` must be checked after the existing
substrings (a queue notice could in principle contain `matches`, but the
specific phrases are disjoint today). Keep the existing `noOp` fallthrough
for genuinely unknown lines — the goal is a *recognized, actionable* event,
not a catch-all.

### 2. API session: route it (`server/api/session.go`)

- `EventKind`: add `EvQueuePosition`.
- `Session.handlers()`: `core.QueuePosition: s.route(s.downloadEv, EvQueuePosition)`.
  Queue notices only ever arrive while a download is in flight, so they go
  to the download channel (mirror of `EvBadServer`).

### 3. Worker: record it, don't block on it (`server/api/worker.go`)

In `doDownload`'s select:

```
case ev := <-w.sess.DownloadEvents():
    switch ev.Kind {
    case EvBookResult:  ... (unchanged)
    case EvBadServer:   ... (unchanged)
    case EvQueuePosition:
        w.reg.Update(job, func(j *Job) { j.QueuePosition = parsePosition(ev.Text) })
        // keep waiting — the DCC offer still comes later
    case EvDisconnected: ...
    }
```

`parsePosition` extracts the integer after `queueposition`/`queue position`
(best-effort; on failure just note the line arrived). Add `QueuePosition int`
+ a `queued` flag to `Job`, surfaced in the download job JSON so agents can
report "queued, position 5" instead of a blind wait.

Optionally: extend the timeout when a queue notice arrives (bots queue for
minutes; the current 10m starts counting at send). Simplest correct version:
reset the timer on `EvQueuePosition` (bounded — cap total wait at e.g. 2×
`DownloadTimeout` so a bot that queued us and then dies still fails).

### 4. Hardening: timer before send + write deadline

`doDownload` currently:

```
sess.DownloadBook(book)      // blocking Write, no timeout
timer := time.NewTimer(...)  // never reached if Write blocks
```

Reorder so the timer (or a `context.WithTimeout`-style bound) is armed before
the send. Additionally add a write deadline on the IRC connection
(`net.Conn.SetWriteDeadline`) in `irc.Conn.SendMessage` — a blocked send then
errors out and the worker fails the job with `irc_disconnected` instead of
hanging the queue.

### 5. Deploy: enable raw IRC logging

Container runs `--persist --name rgrsbookbot --port 5228` without `--log`.
Add `--log` so each API session writes
`<DownloadDir>/logs/rgrsbookbot--<ts>.log` (raw IRC lines) — the post-mortem
evidence that was missing during the Dumbledore hang. This is a stack-compose
change on compute3 (Portainer stack `books`, local compose), plus the git
mirror `~/portainer-stacks/books.yaml` (branch `compute2`).

## Tests

- `core/reader_test.go`: table case for `NOTICE ... to queueposition 5.` →
  `QueuePosition`; negative cases (channel chatter, `matches` lines) still
  classify as before.
- `server/api/worker_test.go`: fake session pushes `EvQueuePosition` then
  `EvBookResult`; assert job completes with `QueuePosition` recorded. Timeout
  test: fake session whose `DownloadBook` blocks; assert the job still times
  out (timer armed before send).
- `server/irc_events_test.go` already covers the browser log panel; extend
  the shared fixtures with the queue-position line.

## Verification

1. `go test -race ./...` and `go test -race -tags=integration ./...`.
2. Deploy `--log`; run a real download against a busy server; confirm the
   job shows a queue position while waiting and completes.
3. Confirm raw IRC log file appears under `/mnt/nfs/booklore/bookdrop/logs/`.
4. Simulate a wedged bot (stop the DCC offer) and confirm the job fails with
   `timeout`/`irc_disconnected` at the bound instead of hanging.
