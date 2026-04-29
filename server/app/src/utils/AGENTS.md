<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# utils

## Purpose
Small UI utility constants used across components. Not to be confused with `state/util.ts`, which holds Redux/network helpers.

## Key Files
| File | Description |
|------|-------------|
| `animation.ts` | `defaultAnimation` — shared framer-motion `HTMLMotionProps<"div">` preset (`layout: true`, scale 0.8 ↔ 1, opacity 0 ↔ 1, tween transition). Used by sidebar lists and the notification drawer for consistent enter/exit motion. |

## For AI Agents

### Working In This Directory
- Use `defaultAnimation` (spread it into `motion.div`) for any list-item animation so timing/feel stays consistent across the UI.

## Dependencies

### External
- `framer-motion`.

<!-- MANUAL: -->
