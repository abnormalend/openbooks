<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# app

## Purpose
React + TypeScript + Vite single-page app that provides the OpenBooks browser UI. Talks to the Go server over a single websocket (search/download/notify) and a small REST surface (servers list + persisted library). The compiled output (`dist/`) is embedded into the Go binary via `//go:embed` in `server/routes.go`.

## Key Files
| File | Description |
|------|-------------|
| `package.json` | NPM scripts (`dev`, `build`, `serve`, `prettier`) and dependency list — React 18, Mantine UI, Redux Toolkit + RTK Query, framer-motion, `@tanstack/react-table`, phosphor-react. |
| `package-lock.json` | NPM lockfile — committed; do not regenerate gratuitously. |
| `vite.config.ts` | Vite config (React plugin). |
| `tsconfig.json` / `tsconfig.node.json` | TypeScript compiler options. |
| `index.html` | Vite entry document. |
| `.prettierrc` | Prettier config used by `npm run prettier`. |
| `.gitignore` | Ignores `dist/` and friends. |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `public/` | Static assets copied verbatim into `dist/` (favicons, manifest, robots.txt) (see `public/AGENTS.md`) |
| `src/` | All TypeScript / TSX source (see `src/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- `npm run build` runs `tsc && vite build`, emitting to `dist/`. The Go build embeds `dist/` — without it, `go build` fails with a missing-embed error.
- The dev server runs on Vite's default port (5173). The Go server's CORS allowlist is hardcoded to `http://127.0.0.1:5173` in `server/server.go`. The Vite app talks to the API on port `5228` when `import.meta.env.DEV` is true (`src/state/util.ts`).
- Style with Mantine + `createStyles` (Emotion) — there are no separate CSS files except inline.
- Use `useAppDispatch` / `useAppSelector` from `src/state/store.ts`, never raw `useDispatch`/`useSelector`, so types stay correct.
- `npm run prettier` formats `src/**` per `.prettierrc`. Run before committing.

### Testing Requirements
- No JS test runner is configured. Manual verification: `task dev:client` (Vite) + `task dev:server` (Go server pointed at the mock).

## Dependencies

### External
- React 18, TypeScript 4.8, Vite 3, Mantine UI 5, Redux Toolkit 1.8, framer-motion 7, `@tanstack/react-table` 8, `@tanstack/react-virtual` 3 alpha, `phosphor-react`, `lodash`.

<!-- MANUAL: -->
