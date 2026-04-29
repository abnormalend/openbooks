<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# cli

## Purpose
Implements the terminal/CLI mode of OpenBooks. Handles interactive menus, one-shot search, and one-shot download flows by wiring `core.EventHandler` callbacks to a `core.StartReader` goroutine and an `irc.Conn`. Surfaces a progress bar via `schollz/progressbar` during downloads.

## Key Files
| File | Description |
|------|-------------|
| `cli.go` | Public entry points: `StartInteractive`, `StartDownload`, `StartSearch`. Owns `Config` (username, server, dir, TLS, search bot, version). |
| `handlers.go` | Per-event handler methods on `Config` — search/download extraction, no-results, bad-server, search-accepted, ping/version replies. |
| `interactive.go` | Read-eval terminal menu loop (`s`/`g`/`se`/`d`) and full-handler builder for interactive mode. |
| `util.go` | Connection bootstrap, signal-based graceful shutdown, log-file setup, search rate-limiting via a temp-file mtime, server-online warning. |

## For AI Agents

### Working In This Directory
- Search rate limiting is enforced by the mtime of `os.TempDir()/.openbooks` — searches sleep until 15 seconds after the last attempt. Do not bypass this without coordinating with the same logic in `server/`.
- `clearLine = "\r\033[2K"` is used to overwrite progress messages — keep it in mind when adding new prints between status lines.
- `addEssentialHandlers` registers `Ping`/`Version`/`ServerList`. Every CLI mode must call it so the connection stays alive and CTCP VERSION queries are answered.
- The download/search file is downloaded via `core.DownloadExtractDCCString`, which handles archive extraction transparently — `handlers.go` should not re-implement extraction.

### Testing Requirements
- No package tests today. Validate manually against the mock server in `cmd/mock_server/`: run the mock, then `go run ./cmd/openbooks cli --server localhost --name test`.

### Common Patterns
- Each `Start*` function: `instantiate(&config)` → register signal-driven shutdown → assemble `core.EventHandler` → `go core.StartReader(...)` → block on `<-ctx.Done()`.
- Handlers that should return the user to the menu re-invoke `terminalMenu(config)` at the end; one-shot handlers call `cancel()` instead.

## Dependencies

### Internal
- `core/` — `Join`, `SearchBook`, `DownloadBook`, `StartReader`, `EventHandler`, `DownloadExtractDCCString`, `ParseServers`, `SendVersionInfo`.
- `irc/` — `irc.New`, `irc.Conn`.
- `dcc/` — `dcc.ParseString` (used for size/filename in the progress bar).
- `util/` — `CreateLogFile`.

### External
- `github.com/spf13/cobra` (used by callers in `cmd/openbooks` to wire CLI flags into `Config`).
- `github.com/schollz/progressbar/v3` for the download bar.

<!-- MANUAL: -->
