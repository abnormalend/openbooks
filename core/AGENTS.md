<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# core

## Purpose
IRC-Highway–specific business logic that sits between the low-level IRC connection (`irc/`) and the consumer (`cli/` or `server/`). Contains the streaming message reader/event dispatcher, the `@search` and download protocol commands, the search-results file parser (with v1 and v2 implementations), the user-list parser, and the DCC download/extract pipeline.

## Key Files
| File | Description |
|------|-------------|
| `irchighway.go` | High-level commands: `Join` (connect + join `#ebooks`), `SearchBook`, `DownloadBook`, `SendVersionInfo` (CTCP VERSION reply). |
| `reader.go` | `StartReader(ctx, conn, EventHandler)` — scans IRC lines, classifies into `event` constants (Message, SearchResult, BookResult, NoResults, BadServer, SearchAccepted, MatchesFound, ServerList, Ping, Version), and dispatches handlers in goroutines. |
| `search_parser.go` | `ParseSearchFile`, `ParseSearch`, and v2 (`ParseSearchV2`) — parse `!server author - title.ext ::INFO:: size` lines into `BookDetail`. Returns both successful results and per-line `ParseError`s. |
| `search_parser_test.go` | Table-driven parser tests covering edge cases (escaped author prefixes, archive-wrapped formats, missing fields). |
| `server_parser.go` | `ParseServers` — splits a NAMES (353/366) reply into `ElevatedUsers` (download bots) and `RegularUsers` based on prefix chars (`~&@%+`). |
| `server_parser_test.go` | Tests for the user-list parser. |
| `file.go` | `DownloadExtractDCCString` — orchestrates the full pipeline: parse DCC SEND → write `<file>.temp` → optionally extract single-file archive via `util.ExtractArchive` → rename. |

## For AI Agents

### Working In This Directory
- `event` constants are unexported as a custom int type but the values are exported (`SearchResult`, etc.). Adding a new event requires a new constant *and* matching detection logic in `StartReader`.
- `StartReader` invokes handlers via `go invoke(text)` — handlers run concurrently and must be safe under concurrent calls.
- Unique-substring matching in `reader.go` is order-sensitive: `DCC SEND` is checked before any NOTICE branch. Don't reorder without revisiting tests.
- The search parser has two versions; v2 is the active path used by `server/irc_events.go` via `ParseSearchFile`. Prefer fixing/extending v2.
- `fileTypes` is order-sensitive — compressed extensions (`rar`, `zip`) must remain the last two so `parseLineV2` can detect "actual format inside an archive" by looking at extensions before them.
- Filenames produced by `DownloadExtractDCCString` always pass through a `.temp` suffix during write to make partial downloads easy to recognize. Don't bypass `renameTempFile`.

### Testing Requirements
- `go test ./core/...` — both parser tests are table-driven; add new cases there when adjusting parsing logic.

### Common Patterns
- `EventHandler = map[event]HandlerFunc` — consumers register only the events they care about; missing events are ignored. The `Message` event is always invoked first (raw line) for logging.

## Dependencies

### Internal
- `irc/` — reads from `*irc.Conn` (which is an `io.Reader`) in `StartReader`; sends commands via its methods.
- `dcc/` — `dcc.ParseString` and `Download.Download` for DCC SEND payloads.
- `util/` — `util.IsArchive`, `util.ExtractArchive`.

### External
- Standard library only.

<!-- MANUAL: -->
