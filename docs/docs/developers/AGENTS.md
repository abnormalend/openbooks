<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# developers

## Purpose
Developer-facing documentation pages: high-level architecture, dev-container quickstart, experimental features (notably the webview build), and an open-todo list.

## Key Files
| File | Description |
|------|-------------|
| `index.md` | Developers tab landing page (matches the `developers/` folder via `navigation.indexes`). |
| `architecture.md` | Mermaid diagrams of the current per-user IRC connection model and the proposed shared-connection future. |
| `dev-container.md` | Notes on using the `.devcontainer/` definition. |
| `experimental.md` | Notes on optional/experimental builds (webview tag). |
| `todo.md` | Tracker for outstanding work. |

## For AI Agents

### Working In This Directory
- `architecture.md` uses ` ```mermaid ` fences enabled by `pymdownx.superfences` in `../../mkdocs.yml`. Don't switch to inline SVG.
- The "Future" section in `architecture.md` is aspirational — refactoring `server/` to a shared IRC connection means revisiting the single-client guard in `server/routes.go:serveWs`.

<!-- MANUAL: -->
