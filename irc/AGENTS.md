<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# irc

## Purpose
Minimal IRC client: a thin wrapper over `net.Conn` (or `tls.Conn`) exposing only the verbs OpenBooks needs — connect, JOIN, PRIVMSG, NOTICE, NAMES, PONG, and graceful QUIT. It deliberately doesn't implement the full RFC; the line-level parser lives in `core/reader.go`.

## Key Files
| File | Description |
|------|-------------|
| `irc.go` | `Conn` struct (embeds `net.Conn` + channel/username/realname), `New`, `Connect`, `Disconnect`, `SendMessage`, `SendNotice`, `JoinChannel`, `GetUsers` (NAMES), `Pong`, `IsConnected`. |

## For AI Agents

### Working In This Directory
- `Conn` embeds `net.Conn` so callers (notably `core.StartReader`) can `bufio.NewScanner(irc)` directly — preserve the embedding when refactoring.
- TLS uses `InsecureSkipVerify: true` because IRC Highway's certificate is not always trusted by stock root stores. Don't switch to verified TLS without coordinating with users.
- All write methods early-return when `IsConnected()` is false to avoid panicking on a disconnected `Conn`. Keep that guard when adding methods.
- `JoinChannel` sets the internal `i.channel` so subsequent `SendMessage` calls target the right channel — joining a second channel without sending elsewhere overwrites the target.
- Lines must end with `\r\n` per RFC 1459. Every write method appends it explicitly.

## Dependencies

### External
- Standard library only (`crypto/tls`, `net`).

<!-- MANUAL: -->
