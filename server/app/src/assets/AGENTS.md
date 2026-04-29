<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# assets

## Purpose
Static assets imported directly by TSX modules. Vite hashes the filename and inlines the URL at build time, so referencing `import image from "../assets/reading.svg"` gives you a content-addressed path.

## Key Files
| File | Description |
|------|-------------|
| `reading.svg` | Hero illustration shown on `SearchPage` when no search has been issued. |

## For AI Agents

### Working In This Directory
- Anything that should be imported by TS/TSX belongs here (it gets bundled and content-hashed). Anything served by absolute URL belongs in `server/app/public/`.

<!-- MANUAL: -->
