<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# log

## Purpose
The IRC log panel displayed below the search results. Subscribes to `state.ircLog.entries` (filled by `socketMiddleware` when an `IRC_MESSAGE` arrives) and renders them as a monospace, append-only feed with smart auto-scroll.

## Key Files
| File | Description |
|------|-------------|
| `IrcLogPanel.tsx` | The panel — header with entry count, scroll body with timestamp-prefixed lines, auto-scrolls to bottom only while the user is already pinned there. |

## For AI Agents

### Working In This Directory
- The "stick to bottom only when already at bottom" behavior is the standard live-tail pattern; don't change it to always-scroll without a strong reason — it stomps on users reading older lines.
- The cap on `entries` is enforced by `ircLogSlice.appendEntry` (currently 500), not by this component.
- Entries are append-only chronologically — `entries[0]` is oldest, `entries[length-1]` is newest. Don't reverse on render.
- This panel is hidden when `state.state.isLogOpen` is false; the parent (`SearchPage`) handles the layout collapse.

### Common Patterns
- Subscribe via the typed `useAppSelector` from `../../state/store`.
- Mantine `Text` for typography so dark/light scheme follows automatically.

## Dependencies

### Internal
- `../../state/ircLogSlice` — `IrcLogEntry` shape.
- `../../state/store` — typed hook.

### External
- `@mantine/core`.

<!-- MANUAL: -->
