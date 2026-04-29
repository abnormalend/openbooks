<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# tables

## Purpose
Virtualized data tables built on `@tanstack/react-table` + `@tanstack/react-virtual`. Renders search results (with per-column server/format faceted filters and free-text author/title filters) and parse-error rows for lines the backend couldn't parse.

## Key Files
| File | Description |
|------|-------------|
| `BookTable.tsx` | Search-results table: server (with online indicator), author, title, format, size, and a Download button per row. Custom `stringInArray` filter for faceted columns; row virtualization with 50px estimated row height and 10-row overscan. |
| `ErrorTable.tsx` | Parse-error table — listens to text selection (`useTextSelection`) and pushes the selection into the parent's manual-download input so users can copy the un-parseable line and download it directly. |
| `styles.ts` | `useTableStyles` — shared scroll container, sticky head, column resizer styles. |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `Filters/` | Column filter UIs — facet popover and text input (see `Filters/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Column widths are computed as fractions of the rendered viewport width (`(width / 12) * cols`) and re-memoized on resize. Adding a column means re-balancing the column counts so they sum to 12.
- The "server online" badge is driven by `useGetServersQuery` from `../../state/api` — server list comes from the Go REST endpoint, which itself is populated by IRC NAMES events.
- Virtualization padding is implemented as full-width spacer `<tr><td height>` before/after the rendered window — preserve this so scrollbars stay accurate.
- The Download button locks itself after click via `clicked` local state and shows a `Loader` while `inFlightDownloads` (from `stateSlice`) contains the book identifier. Don't dispatch `sendDownload` outside this button without coordinating with that gate.

## Dependencies

### Internal
- `../../state/api` (`useGetServersQuery`).
- `../../state/messages` (`BookDetail`, `ParseError`).
- `../../state/stateSlice` (`sendDownload`, `inFlightDownloads`).

### External
- `@tanstack/react-table`, `@tanstack/react-virtual`, `@mantine/core`, `@mantine/hooks`, `phosphor-react`.

<!-- MANUAL: -->
