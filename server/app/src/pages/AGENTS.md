<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# pages

## Purpose
Top-level "page" components. OpenBooks is single-page and currently has only one — `SearchPage` — but the directory is preserved for future routes (e.g. an admin/stats view).

## Key Files
| File | Description |
|------|-------------|
| `SearchPage.tsx` | Main view — search/download text input, error-mode toggle (when parse errors exist), and the `BookTable` / `ErrorTable` switch. |

## For AI Agents

### Working In This Directory
- Search vs. manual-download mode is selected by a local `showErrors` toggle plus `searchQuery.startsWith("!")` validation: in error mode, only lines starting with `!` count as a valid download identifier (matching the IRC bot's expected syntax).
- The empty state shows the `reading.svg` illustration sized differently for `MediaQuery` mobile vs. desktop. Don't merge into a single `<Image>` — Mantine renders both and toggles `display`.
- `useMemo` on `bookTable` / `errorTable` is keyed on `activeItem.results` / `activeItem.errors` — new memoized children must follow the same pattern to avoid re-rendering the virtualized table on every keystroke.

## Dependencies

### Internal
- `../components/tables/BookTable`, `../components/tables/ErrorTable`.
- `../state/stateSlice` (`sendSearch`, `sendMessage`, `toggleSidebar`).
- `../state/messages` (`MessageType`).
- `../assets/reading.svg`.

### External
- `@mantine/core`, `phosphor-react`.

<!-- MANUAL: -->
