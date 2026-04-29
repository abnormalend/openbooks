<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# drawer

## Purpose
Right-hand notification drawer. Shows the chronological list of notifications stored in `notificationSlice` with framer-motion enter/exit animations, color-coded by severity (NOTIFY/SUCCESS/WARNING/DANGER), and a "clear all" affordance.

## Key Files
| File | Description |
|------|-------------|
| `NotificationDrawer.tsx` | The drawer itself — opens/closes via `notifications.isOpen`, dispatches `dismissNotification` / `clearNotifications` / `toggleDrawer`. |

## For AI Agents

### Working In This Directory
- The intent-to-color mapping (`getIntent`) mirrors the Mantine palette and toggles `brand` ↔ `brand.2` between light/dark. Keep it aligned with `Sidebar`'s connection indicator if you change the palette.
- Notifications are displayed with their `timestamp` formatted via `Intl.DateTimeFormat("en-US")` — preserve locale handling if you internationalize.

## Dependencies

### Internal
- `../../state/notificationSlice` for state and reducers.
- `../../utils/animation` for the entry/exit transition.

### External
- `@mantine/core` (Drawer/Notification), `framer-motion`, `phosphor-react`.

<!-- MANUAL: -->
