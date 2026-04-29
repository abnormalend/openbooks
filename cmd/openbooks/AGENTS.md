<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# openbooks (cmd)

## Purpose
The `main` package for the user-facing `openbooks` binary. Defines the Cobra command tree (`openbooks` for desktop mode → `cli` and `server` subcommands), parses flags, and dispatches to the corresponding library.

## Key Files
| File | Description |
|------|-------------|
| `main.go` | Root `desktopCmd` (also acts as the entry point), `GlobalFlags` shared across modes, `version` and `ircVersion` constants, `init()` registering global flags and desktop-only flags. |
| `cli.go` | `cli`, `cli download`, and `cli search` subcommands; copies `globalFlags` into `cli.Config` before running. |
| `server.go` | `server` subcommand with port, base path, persist, dir, browser flags; reads `BASE_PATH` env var when `--basepath` is at its default. |
| `util.go` | `bindGlobalServerFlags`, `ensureValidRate` (enforces ≥10s search rate limit), `sanitizePath` (`path.Clean` + trailing slash). |

## For AI Agents

### Working In This Directory
- `version` (release tag) and `ircVersion` (CTCP version string) are intentionally separate. Bump `ircVersion` only when IRC admins block the current value; bump `version` for every release.
- The root command is `desktopCmd` — it both runs the desktop webview when invoked bare and serves as the parent for `cli` and `server`. `cli.go` and `server.go` use `init()` to call `desktopCmd.AddCommand(...)`.
- `desktop` mode forces `DisableBrowserDownloads=true`, `Basepath="/"`, `Persist=true` — do not read these flags from the user in that mode.
- `--rate-limit` below 10 silently clamps to 10 (`ensureValidRate`); preserve this behavior when changing rate-limit handling.
- `cobra.MousetrapHelpText = ""` in `main()` prevents Windows GUI launches from blocking — keep it.

### Testing Requirements
- No automated tests. Validate by running `go run . --help`, `go run . cli --help`, etc. from this directory.

### Common Patterns
- `PreRun` populates the typed config struct from `globalFlags` and applies env-var overrides; `Run` invokes the library entrypoint.
- Build with the optional `webview` tag for the experimental native window (`go build -tags webview`); without the tag, `desktop.StartWebView` falls back to opening the system browser.

## Dependencies

### Internal
- `cli/` — `cli.StartInteractive`, `StartDownload`, `StartSearch`.
- `server/` — `server.Start`, `server.Config`.
- `desktop/` — `desktop.StartWebView`.
- `util/` — `util.OpenBrowser`.

### External
- `github.com/spf13/cobra` (commands), `github.com/davecgh/go-spew` (debug dump).

<!-- MANUAL: -->
