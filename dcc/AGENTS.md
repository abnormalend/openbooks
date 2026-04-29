<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# dcc

## Purpose
Parses DCC SEND strings and performs the actual TCP file transfer. DCC ("Direct Client-to-Client") is the protocol IRC servers use to send files: the bot announces an IP, port, and file size in a PRIVMSG, and the client makes a separate TCP connection to fetch the bytes.

## Key Files
| File | Description |
|------|-------------|
| `dcc.go` | `ParseString` (regex-extracts filename / 32-bit IP / port / size from a DCC SEND line), `Download.Download(io.Writer)` (streams the file over TCP), `stringToIP` (decodes the 32-bit integer IP form). |
| `dcc_test.go` | Tests covering quoted filenames, integer-IP decoding, and parse-error sentinels. |

## For AI Agents

### Working In This Directory
- `Download.Download` deliberately uses a hand-rolled 4096-byte read loop instead of `io.Copy` — comments explain that the DCC server never sends EOF, which is why `io.Copy` measured 4× slower. Don't "simplify" this back to `io.Copy`.
- IP addresses arrive as a 32-bit unsigned integer in network byte order (e.g. `2130706433` → `127.0.0.1`). `stringToIP` performs the conversion via `binary.BigEndian.PutUint32`.
- `ErrInvalidDCCString`, `ErrInvalidIP`, and `ErrMissingBytes` are the package's exported sentinels — propagate them with `errors.Is` rather than introducing new error types for the same conditions.
- The regex (`dccRegex`) tolerates optional double-quoted filenames so it handles paths with spaces.

### Testing Requirements
- `go test ./dcc/...`.

## Dependencies

### External
- Standard library only.

<!-- MANUAL: -->
