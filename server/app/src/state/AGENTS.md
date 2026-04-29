<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# state

## Purpose
Redux store, slices, RTK Query API, websocket middleware, and the wire-format message types. The store is the single source of truth for connection state, search history, in-flight downloads, and notifications. The websocket middleware bridges the browser ↔ Go server in both directions; RTK Query handles the small REST surface (`/library`, `/servers`).

## Key Files
| File | Description |
|------|-------------|
| `store.ts` | `configureStore` wiring the four reducers + RTK Query reducer + middleware (websocket + RTK Query); `useAppDispatch` / `useAppSelector` typed hooks; throttled persistence of `history.items` and `state.activeItem` to `localStorage`. |
| `messages.ts` | TypeScript mirrors of the Go server message contract — `MessageType` and `NotificationType` enums (must match `server/messages.go` integer ordering), plus `Notification`, `Response`, `ConnectionResponse`, `SearchResponse`, `DownloadResponse`, `BookDetail`, `ParseError`. |
| `socketMiddleware.ts` | The `websocketConn(url)` Redux middleware — opens the websocket on store creation, dispatches inbound messages to the right slice action, and forwards `socket/send_message` actions to the wire. |
| `stateSlice.ts` | App-state slice (`isConnected`, `isSidebarOpen`, `activeItem`, `username`, `inFlightDownloads`) plus thunks/actions: `sendMessage`, `sendDownload`, `sendSearch`, `setSearchResults`. |
| `historySlice.ts` | Search history (max 16 items, persisted), `addHistoryItem`/`updateHistoryItem`/`deleteByTimetamp`, and the `deleteHistoryItem` thunk. |
| `notificationSlice.ts` | Drawer + notification list state. |
| `api.ts` | RTK Query API — `getServers`, `getBooks`, `deleteBook`. `tagTypes: ["books", "servers"]`; mutations invalidate `books`. |
| `util.ts` | `getWebsocketURL`, `getApiURL` (rewrites port to `5228` in dev), `displayNotification` (dispatches Mantine toasts), `downloadFile` (programmatic anchor click). |

## For AI Agents

### Working In This Directory
- **Sync `MessageType` and `NotificationType` numerically with `server/messages.go`** — they are sent as integers over the wire. Adding/reordering values requires updating both ends *and* regenerating `server/messagetype_string.go` (`go generate ./server/...`).
- The websocket is opened the moment the store is constructed (`socketMiddleware`) — there's no reconnect loop. If the socket closes, the user is told to reload the page (`displayNotification` warning).
- `sendMessage` is the single outbound action; the middleware matches it via `sendMessage.match(action)` and serializes the payload. New outbound message types should be dispatched via `sendMessage({ type, payload })`.
- `enableMapSet()` is called at module load so reducers can use `Map`/`Set` if needed — leave it in even if not currently used.
- `localStorage` persistence is throttled to 1s via `lodash/throttle`. Don't switch to per-action persistence without measuring.
- RTK Query baseUrl uses `getApiURL().href` and `credentials: "include"` so the `OpenBooks` cookie is sent (required for `requireUser` middleware on the Go side).

### Common Patterns
- Inbound websocket messages route through `route()` in `socketMiddleware.ts` based on `MessageType`. Each branch may dispatch slice actions, invalidate RTK Query caches (`openbooksApi.util.invalidateTags`), and always returns a `Notification` for the drawer.

## Dependencies

### External
- `@reduxjs/toolkit`, `@reduxjs/toolkit/query/react`, `react-redux`, `immer` (re-exported via `@reduxjs/toolkit`), `lodash/throttle`, `@mantine/notifications`.

<!-- MANUAL: -->
