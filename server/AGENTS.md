<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# server

## Purpose
HTTP + websocket server that powers OpenBooks' web UI. Hosts the embedded React SPA (`app/dist/`), accepts websocket connections (one per browser, identified by the `OpenBooks` cookie UUID), opens a per-client IRC connection, and routes IRC events back as JSON messages. Also exposes REST endpoints for the persisted-library flow.

## Key Files
| File | Description |
|------|-------------|
| `server.go` | `Config` (port, basepath, persist flag, search timeout, user-agent, etc.), `New`, `Start` — wires Chi router, CORS, the client-hub goroutine, and graceful shutdown. |
| `client.go` | Per-connection `Client` struct, `readPump` / `writePump`, websocket ping/pong timing constants. |
| `routes.go` | Chi route table — `/ws`, `/stats`, `/servers`, and `/library/*` REST endpoints; `staticFilesHandler` serves the embedded React build via `//go:embed app/dist`. |
| `messages.go` | Wire formats: `MessageType` enum (STATUS/CONNECT/SEARCH/DOWNLOAD/RATELIMIT), `NotificationType`, request/response structs, response builders. |
| `messagetype_string.go` | Generated via `stringer -type=MessageType` — DO NOT EDIT, regenerate. |
| `websocket_requests.go` | `routeMessage` switch over inbound websocket payload types; per-client handlers for connect/search/download (including rate-limit check). |
| `irc_events.go` | `NewIrcEventHandler` and per-event callbacks that translate `core` events into outbound websocket messages. |
| `middlewares.go` | `requireUser` middleware (validates the `OpenBooks` cookie + UUID) and `getClient`/`getUUID` helpers. |
| `repository.go` | Process-level shared state (currently just the cached IRC server list). |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `app/` | React + Vite frontend; built into `app/dist/` and embedded into the binary (see `app/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- The SPA is embedded with `//go:embed app/dist` in `routes.go`. **Run `npm run build` in `server/app/` before `go build`** or the binary will fail to compile (missing `dist/`).
- `serveWs` rejects new connections when there is already a client (`len(server.clients) > 0`) — OpenBooks is single-user by design. Don't lift this without rethinking the IRC connection model (see `docs/docs/developers/architecture.md` for the future plan).
- Cookies: the `OpenBooks` cookie holds a UUID (HttpOnly, SameSite=Strict, 7d expiry). The `requireUser` middleware reads it for REST routes; `serveWs` issues it on first connect.
- `MessageType` constants are mirrored in `app/src/state/messages.ts` — keep the integer ordering in lockstep, and regenerate `messagetype_string.go` (`go generate ./server/...`) after changes.
- Search rate limiting is enforced server-side via `lastSearchMutex` + `config.SearchTimeout`; the client also has a sense of it via the `RATELIMIT` response.
- CORS allows only `http://127.0.0.1:5173` (Vite dev server) — the production build is same-origin so it doesn't need CORS. If you change the dev port, update both ends.
- "Persist" off means downloaded books are deleted right after `http.ServeFile` streams them to the browser (`getBookHandler`).

### Testing Requirements
- No Go tests today. Exercise via `task dev:server` against `task dev:mock`, then drive the React app at `http://localhost:5228/`.

### Common Patterns
- All outbound websocket messages are constructed via the `new*Response` helpers in `messages.go` so titles/notification types stay consistent.
- Every `Client` owns its own `irc.Conn` — the IRC connection lifecycle is bound to the websocket lifecycle in `readPump`'s deferred cleanup.

## Dependencies

### Internal
- `core/`, `irc/`, `util/` for the IRC pipeline and logging.

### External
- `github.com/go-chi/chi/v5`, `github.com/go-chi/chi/v5/middleware`, `github.com/gorilla/websocket`, `github.com/google/uuid`, `github.com/rs/cors`.

<!-- MANUAL: -->
