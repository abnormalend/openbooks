<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# openbooks

## Purpose
OpenBooks is a Go application that downloads eBooks from `irc.irchighway.net` quickly and easily. It can run as a CLI tool, a self-hosted web server with an embedded React SPA, or an experimental desktop app powered by a native webview. Each user gets their own IRC connection bridged to a websocket so server activity is isolated per client.

## Key Files
| File | Description |
|------|-------------|
| `README.md` | Project overview, install/dev instructions, technology stack |
| `Dockerfile` | Multi-stage build: Node builds React SPA, Go compiles binary, distroless runtime |
| `build.sh` | Cross-platform release script — builds the SPA then compiles Windows/macOS/Linux/ARM binaries |
| `go.mod` / `go.sum` | Go module (`github.com/evan-buss/openbooks`, Go 1.19) and dependency lock |
| `Taskfile.yaml` | Top-level Task runner that includes Development, Documentation, and Release task files |
| `Taskfile.Development.yaml` | `task dev:init`, `dev:mock`, `dev:server`, `dev:client`, `dev:cli` targets |
| `Taskfile.Documentation.yaml` | MkDocs documentation tasks |
| `Taskfile.Release.yaml` | Release-time tasks |
| `docker.md` | Docker usage notes |
| `LICENSE` | Project license |
| `.dockerignore` / `.gitignore` | Ignore lists for Docker and Git |
| `FUNDING.yml` | GitHub sponsor metadata |
| `todo` | Plain-text scratch list |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `cli/` | Interactive terminal mode — menu, search, and download flows (see `cli/AGENTS.md`) |
| `cmd/` | Cobra entry points — `openbooks` binary and `mock_server` (see `cmd/AGENTS.md`) |
| `core/` | IRC Highway-specific logic — message reader, search/server parsers, DCC orchestration (see `core/AGENTS.md`) |
| `dcc/` | DCC SEND string parsing and TCP file-transfer client (see `dcc/AGENTS.md`) |
| `desktop/` | Optional native webview wrappers behind the `webview` build tag (see `desktop/AGENTS.md`) |
| `docs/` | MkDocs documentation site (see `docs/AGENTS.md`) |
| `irc/` | Low-level IRC connection wrapper (PRIVMSG / NOTICE / PING / NAMES) (see `irc/AGENTS.md`) |
| `mock/` | In-process IRC and DCC servers used to develop without hitting real IRC (see `mock/AGENTS.md`) |
| `server/` | Web server, websocket hub, REST routes, and React SPA (see `server/AGENTS.md`) |
| `util/` | Cross-cutting helpers — archive extraction, browser launch, log files (see `util/AGENTS.md`) |
| `.devcontainer/` | VS Code dev container definition |
| `.github/` | GitHub workflows and screenshots |
| `.vscode/` / `.idea/` | Editor configuration |
| `.omc/` | Local oh-my-claudecode state (not part of the project source) |

## For AI Agents

### Working In This Directory
- Module path is `github.com/evan-buss/openbooks` — preserve this when adding new packages.
- The Go entry point is `cmd/openbooks` (not the repo root). `go build` at the root no longer produces the binary; build from `cmd/openbooks/` or use `build.sh`.
- The React SPA in `server/app/dist` is embedded via `//go:embed` in `server/routes.go`. Run `npm run build` inside `server/app/` before building the Go binary if frontend changed.
- Only the latest release is supported (per README). Don't add backward-compat shims for older clients.
- Two version constants live in `cmd/openbooks/main.go`: `version` (binary version, matches GitHub releases) and `ircVersion` (the version string reported via CTCP — change only when IRC admins require it).

### Testing Requirements
- `go test -race -cover ./...` — unit tests across all Go packages.
- `go test -race -tags=integration ./...` — integration suite under `tests/` that spins up in-process IRC + DCC servers.
- `cd server/app && npm run test:ci` — Vitest suite for Redux slices, util helpers, and the cross-language `MessageType` enum drift catcher.
- For manual end-to-end work, prefer the mock server (`task dev:mock`) over connecting to the real `irc.irchighway.net` to avoid spamming the public service.

### Common Patterns
- Cobra commands are wired up in `cmd/openbooks/{cli,server}.go` via `init()` calling `desktopCmd.AddCommand(...)`.
- The `core.EventHandler` map (`map[event]HandlerFunc`) is the dispatch mechanism for IRC events; both CLI and server modes assemble their own handler maps and share the parser via `core.StartReader`.
- Each connected web user gets a dedicated `irc.Conn` — there is no shared IRC connection.

## Dependencies

### External
- Backend: `go-chi/chi/v5`, `gorilla/websocket`, `mholt/archiver/v3`, `spf13/cobra`, `google/uuid`, `rs/cors`, `schollz/progressbar/v3`, `webview/webview` (optional), `inkeliz/gowebview` (Windows webview).
- Frontend: React 18, TypeScript, Redux Toolkit + RTK Query, Mantine UI, framer-motion, `@tanstack/react-table` + virtualizer, phosphor-react, Vite.
- Tooling: [Task](https://taskfile.dev/), MkDocs (Material theme).

<!-- MANUAL: -->
