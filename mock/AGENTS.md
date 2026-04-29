<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# mock

## Purpose
In-process IRC and DCC servers used by `cmd/mock_server` (and ad-hoc tests) to drive OpenBooks without contacting the real IRC Highway. Replays canned responses for `@search`, `!download`, NAMES (353/366), and CTCP VERSION inquiries.

## Key Files
| File | Description |
|------|-------------|
| `irc_server.go` | `IrcServer{Port}` — listens on the given port, sends a fake user list and a CTCP `\x01VERSION\x01` ping on connect, handles `@search` (returns a fixed `DCC SEND` pointing at port 6668) and `!download` (returns one pointing at 6669). |
| `dcc_server.go` | `DccServer{Port, Reader}` — accepts a TCP connection and streams `Reader` in 4KB chunks with a 250ms inter-chunk sleep so you can observe the progress bar. Resets the reader between connections. |

## For AI Agents

### Working In This Directory
- The DCC handler intentionally throttles to 4KB / 250ms so the CLI progress bar is visible — comment that out if you need full-speed transfers.
- `IrcServer` writes a hardcoded DCC SEND with IP `2130706433` (loopback). If you change the listen address, also update the IP literal so the client can reach the DCC sender.
- `Start(ready chan<- struct{})` blocks on `Listen` then signals `ready`; callers chain multiple servers (see `cmd/mock_server/main.go`) — preserve the channel signature.
- `dcc.Reader.Seek(0, io.SeekStart)` runs in the deferred close — required because the same `*os.File` is reused across connections.

### Testing Requirements
- No package tests; exercised end-to-end through `cmd/mock_server`.

## Dependencies

### External
- Standard library only.

<!-- MANUAL: -->
