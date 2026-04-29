<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# src

## Purpose
All TypeScript / TSX source for the OpenBooks SPA. Bootstraps React with `createRoot`, wraps the tree in a Redux `Provider`, and renders `<App />` (Mantine theming + sidebar + search page + notification drawer).

## Key Files
| File | Description |
|------|-------------|
| `main.tsx` | React 18 entry point — `createRoot`, `<StrictMode>`, `<Provider store={store}>`, `<App />`. |
| `App.tsx` | Mantine `ColorSchemeProvider` + `MantineProvider` + `NotificationsProvider`, custom brand palette, `AppShell` with `Sidebar` and `SearchPage` + `NotificationDrawer`. Persists color scheme to `localStorage`. |
| `vite-env.d.ts` | Vite client types reference. |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `assets/` | Bundled images imported by TSX modules (see `assets/AGENTS.md`) |
| `components/` | Reusable UI — sidebar, drawer, tables (see `components/AGENTS.md`) |
| `pages/` | Top-level page components (currently just `SearchPage`) (see `pages/AGENTS.md`) |
| `state/` | Redux store, slices, RTK Query API, websocket middleware, message types (see `state/AGENTS.md`) |
| `utils/` | UI utility constants — animation presets (see `utils/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Brand palette is defined inline in `App.tsx` (`colors.brand`, primary shade differs by scheme). Reference `brand.4` (light) / `brand.2` (dark) in components for the active color.
- The single emotion cache key is `"openbooks"` — keep it stable so SSR-style class hashes don't shift.
- Color scheme is read from / written to `localStorage["color-scheme"]` via `@mantine/hooks` `useLocalStorage` — don't add a separate persistence layer.
- Sidebar visibility is in Redux (`state.state.isSidebarOpen`) so it survives across components; toggle via `toggleSidebar`.

## Dependencies

### Internal
- `state/store.ts` and slices (single source of truth for connection, history, notifications).

### External
- `@mantine/core`, `@mantine/hooks`, `@mantine/notifications`, `@reduxjs/toolkit`, `react-redux`, `react`, `react-dom`.

<!-- MANUAL: -->
