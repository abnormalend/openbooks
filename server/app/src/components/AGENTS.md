<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# components

## Purpose
Reusable React components grouped by their location/role in the UI. The sidebar drives navigation between search history and the persisted library, the drawer houses notifications, and the tables render search results / parse errors with virtualization and column filtering.

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `drawer/` | Right-hand notifications drawer (see `drawer/AGENTS.md`) |
| `log/` | IRC log panel pinned below the search results (see `log/AGENTS.md`) |
| `sidebar/` | Left-hand sidebar with `History` and `Library` tabs (see `sidebar/AGENTS.md`) |
| `tables/` | Virtualized result tables (`BookTable`, `ErrorTable`) and their filters (see `tables/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Components are colocated with their `styles.ts` — keep `createStyles` hooks next to the component that consumes them (see `tables/styles.ts`, `sidebar/styles.ts`).
- Animations come from `framer-motion` paired with the shared `defaultAnimation` preset in `../utils/animation.ts`.
- Components dispatch through `useAppDispatch` / `useAppSelector` (from `../state/store.ts`) — no prop-drilling for global state.

## Dependencies

### Internal
- `../state/` for slices, RTK Query hooks, and message types.
- `../utils/animation.ts` for shared motion presets.

### External
- `@mantine/core`, `@mantine/hooks`, `@tanstack/react-table`, `@tanstack/react-virtual`, `framer-motion`, `phosphor-react`.

<!-- MANUAL: -->
