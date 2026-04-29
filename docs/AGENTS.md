<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# docs

## Purpose
MkDocs (Material theme) documentation site for OpenBooks. Built with `task docs:*` and published to `https://evan-buss.github.io/openbooks`. Houses end-user setup guides, configuration reference, IRC notes, the changelog, and developer docs (architecture, dev container, experimental features, todo list).

## Key Files
| File | Description |
|------|-------------|
| `mkdocs.yml` | Site config — Material theme palette, navigation tree, markdown extensions (admonition, mermaid via `pymdownx.superfences`, tasklist, footnotes, highlight). |
| `requirements.txt` | Python dependencies for building the site (consumed by the documentation Taskfile). |

## Subdirectories
| Directory | Purpose |
|-----------|---------|
| `docs/` | Markdown source files (Home, Configuration, Setup, IRC Notes, Changelog, Developers) (see `docs/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- The navigation tree in `mkdocs.yml` controls page order. Renaming a markdown file requires updating the `nav:` block as well.
- `pymdownx.superfences` is configured to render `mermaid` blocks via the named fence — use ` ```mermaid ` rather than the language-less form.
- Material features enabled: `navigation.instant`, `navigation.tabs`, `navigation.indexes`. Pages with the same name as a folder act as that folder's index (used by `developers/index.md`).

## Dependencies

### External
- MkDocs + `mkdocs-material` (with `pymdownx.*` extensions). Pin via `requirements.txt`.

<!-- MANUAL: -->
