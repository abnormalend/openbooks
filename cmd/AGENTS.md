<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# cmd

## Purpose
Holds Cobra-based `main` packages — the buildable binaries for OpenBooks. The `openbooks` binary is the user-facing executable (CLI / server / desktop modes); `mock_server` is a developer-only mock IRC + DCC stand-in.

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `openbooks/` | Production binary with `cli`, `server`, and root `desktop` commands (see `openbooks/AGENTS.md`) |
| `mock_server/` | Local mock IRC server + two DCC senders for offline development (see `mock_server/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- New commands belong here as a separate `main` package — keep library code out of `cmd/` so it stays importable.
- `build.sh` and the Dockerfile both `cd cmd/openbooks` before `go build`. Do not move the openbooks binary's `main` package without updating both.

## Dependencies

### Internal
- `cli/`, `server/`, `desktop/`, `mock/`, `util/` — the `cmd` packages thread Cobra flags into these libraries.

### External
- `github.com/spf13/cobra` for command definition and flag parsing.

<!-- MANUAL: -->
