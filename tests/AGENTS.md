<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# tests

## Purpose
Build-tagged Go integration tests that drive the real OpenBooks pipeline end-to-end against in-process fakes. Only compiled when the `integration` build tag is set so unit-test runs stay fast.

## Key Files
| File | Description |
|------|-------------|
| `integration_test.go` | `TestSearchToBookDetails` — boots inline IRC + DCC TCP servers on free ports, connects via `irc.New`+`Connect`, calls `core.SearchBook`, lets `core.StartReader` classify the response, then exercises `core.DownloadExtractDCCString` + `core.ParseSearchFile`. Asserts `BookDetail` records survive the round-trip. |

## For AI Agents

### Working In This Directory
- All files here must carry `//go:build integration` so they don't compile during normal `go test ./...`. CI runs them as a separate step (see `.github/workflows/test.yml`).
- The fake IRC server here is intentionally inline rather than reusing the `mock/` package — `mock.IrcServer` hardcodes DCC ports (6668/6669), which would force this test to bind known ports and risk collisions.
- Listeners use `127.0.0.1:0` to grab a free ephemeral port.
- The IP literal `2130706433` in the DCC SEND payload is the integer form of `127.0.0.1` (DCC's wire format).

### Testing Requirements
- Run with `go test -race -tags=integration ./tests/...` (or `./...`).

## Dependencies

### Internal
- `core/`, `irc/` — exercised end-to-end.

### External
- Standard library only.

<!-- MANUAL: -->
