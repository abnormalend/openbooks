<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# public

## Purpose
Static assets that Vite copies verbatim into `dist/` at build time. These are referenced by absolute paths from `index.html` / `manifest.json`.

## Key Files
| File | Description |
|------|-------------|
| `favicon-16x16.png` / `favicon-32x32.png` | Browser tab icons. |
| `manifest.json` | PWA web app manifest. |
| `robots.txt` | Crawler directives. |

## For AI Agents

### Working In This Directory
- Don't import these files from TS — Vite emits them by URL. Reference them with absolute paths (e.g. `/favicon-32x32.png`).
- Bundled assets (imported by `*.ts`/`*.tsx`) live under `src/assets/` instead, where they get content-hashed.

<!-- MANUAL: -->
