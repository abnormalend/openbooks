<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# docs (markdown)

## Purpose
Markdown source for the OpenBooks documentation site. Each file is rendered by MkDocs Material per the `nav:` block in `../mkdocs.yml`. Pages here cover the user-facing flow (home, getting-started, configuration, setup, IRC notes, changelog).

## Key Files
| File | Description |
|------|-------------|
| `index.md` | Site landing page with light/dark hero screenshots and a WIP banner. |
| `getting-started.md` | Onboarding walkthrough using `task dev:*` targets. |
| `configuration.md` | CLI flag and environment variable reference. |
| `irc-notes.md` | Behavioral notes about the real IRC Highway server. |
| `changelog.md` | Release-by-release changelog. |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `developers/` | Developer-focused documentation — architecture, dev container, experimental webview, todos (see `developers/AGENTS.md`) |
| `setup/` | Install guides for binary and Docker deployments (see `setup/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Internal links use relative paths (e.g. `./dev-container.md`, `../irc-notes.md`). MkDocs resolves them; keep them relative so the site builds locally and on GitHub Pages.
- `index.md` uses Material's instant-rendered hero (figure/figcaption + light/dark image swap via `#only-light` / `#only-dark`). Mirror that pattern for any new dual-mode imagery.

<!-- MANUAL: -->
