<!-- Parent: ../AGENTS.md -->

# server/api

## Purpose
Token-protected, job-based REST API (search / download / poll / health) that
drives one headless IRC session so an external agent can use OpenBooks
without the browser. Spec: `docs/superpowers/specs/2026-08-15-rest-api-design.md`.
Contract: `openapi.yaml` (embedded, served at `/api/openapi.yaml`).

## Key Files
| File | Description |
|---|---|
| `api.go` | `Config`, `Deps`, `API` façade (`New`, `Start`, `Router`, `Yield`), HTTP handlers |
| `auth.go` | `RequireToken` bearer middleware; empty token → 503 fail-closed |
| `jobs.go` | `Job`, `Registry` (map + bounded per-type queues + TTL sweep) |
| `session.go` | `Session`: lazy-connect IRC conn, routes `core` events onto search/download channels |
| `worker.go` | `Worker`: one goroutine per job type; single-flight; talks to `Session`, fetches DCC |
| `limiter.go` | `SearchLimiter` shared with the websocket path |
| `errors.go` | `APIError`, `writeJSON`, `writeError` |
| `openapi.yaml` | OpenAPI 3 document |

## For AI Agents
- Never import `server` from here (cycle). Everything the API needs from the host arrives via `Deps`.
- Single-flight per type is a correctness requirement, not a perf choice: IRC bots' DCC SEND replies carry no request id.
- Tests: `go test -race ./server/api/` (unit, fake session) and `go test -race -tags=integration ./tests/` (real pipeline against fake IRC/DCC).
- Do not connect to real irchighway in tests.
