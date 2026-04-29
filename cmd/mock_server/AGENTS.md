<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# mock_server (cmd)

## Purpose
Standalone development binary that boots an in-process mock IRC server on `:6667` plus two mock DCC senders on `:6668` (search-results zip) and `:6669` (sample epub). Lets you exercise OpenBooks end-to-end without touching the real IRC Highway. Sleeps for 24h after startup so the goroutines stay alive.

## Key Files
| File | Description |
|------|-------------|
| `main.go` | Wires `mock.IrcServer` + two `mock.DccServer` instances and waits on a `ready` channel between starts. |
| `great-gatsby.epub` | Sample eBook served by the DCC sender on port 6669 when a download is requested. |
| `SearchBot_results_for__the_great_gatsby.txt.zip` | Sample zipped search-results file served on port 6668 when a search is performed. |

## For AI Agents

### Working In This Directory
- The mock IRC server hardcodes a DCC SEND response with IP `2130706433` (= `127.0.0.1`) and ports `6668` (search) / `6669` (download). Keep these in sync with `main.go` if you change either side.
- The two sample fixtures must remain in this directory — `main.go` opens them by relative path, so run the mock from this folder (`cd cmd/mock_server && go run .`).
- Don't move logic into this `main` — put it in `mock/` so it can be reused.

### Testing Requirements
- Run alongside the real client: `go run . server --server localhost --log` from `cmd/openbooks/`.

## Dependencies

### Internal
- `mock/` — `mock.IrcServer`, `mock.DccServer`.

<!-- MANUAL: -->
