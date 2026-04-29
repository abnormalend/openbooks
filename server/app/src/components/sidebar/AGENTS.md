<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# sidebar

## Purpose
Left-hand navigation panel. Contains the brand title, a connection-state bell that opens the notifications drawer, a `SegmentedControl` that toggles between recent search history and the previously-downloaded library, and a footer with username + theme toggle + sidebar collapse.

## Key Files
| File | Description |
|------|-------------|
| `Sidebar.tsx` | The shell — Mantine `Navbar` with title, header controls, child panel selector, footer. |
| `History.tsx` | Recent searches with per-item Show/Hide/Delete menu. Animated list driven by `selectHistory` and the `activeItem` timestamp. |
| `Library.tsx` | RTK Query–backed list of persisted downloads (`useGetBooksQuery`); per-item Download/Delete actions via `useDeleteBookMutation` and `downloadFile`. Empty/disabled-state copy switches based on RTK status. |
| `styles.ts` | `useSidebarButtonStyle` — shared button styling that reacts to an `isActive` flag. |

## For AI Agents

### Working In This Directory
- The selected tab (`"books"` / `"history"`) is persisted to `localStorage["sidebar-state"]` via `useLocalStorage`. Keep the value union narrow when adding tabs.
- `Library` returns "Book persistence disabled." on `isError` — that's the GET `/library` 404 returned by the Go server when `--persist` is off (see `server/routes.go:getAllBooksHandler`).
- History items are capped at 16 in `historySlice.addHistoryItem`; the sidebar relies on that ordering.
- `Sidebar` early-returns `<></>` when the sidebar is collapsed; the toggle button lives in `pages/SearchPage.tsx`.

## Dependencies

### Internal
- `../../state/historySlice`, `../../state/notificationSlice`, `../../state/stateSlice`, `../../state/api` (RTK Query hooks).
- `../../utils/animation` for list transitions.

### External
- `@mantine/core`, `@mantine/hooks`, `framer-motion`, `phosphor-react`.

<!-- MANUAL: -->
