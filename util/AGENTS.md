<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# util

## Purpose
Small, self-contained helpers shared across packages: archive extraction (rar/zip/tar/gz/etc.) when a single book is wrapped, cross-platform browser launch, and per-session log-file creation.

## Key Files
| File | Description |
|------|-------------|
| `archiver.go` | `ExtractArchive` — opens a `.temp`-suffixed archive via `mholt/archiver`, walks it, extracts the *first* file only when the archive contains exactly one entry, and otherwise leaves the original archive in place. `IsArchive` — does a `.temp`-aware extension check. |
| `browser.go` | `OpenBrowser(url)` — `xdg-open` (Linux), `rundll32` (Windows), `open` (macOS); logs an error otherwise. |
| `logger.go` | `CreateLogFile(username, dir)` — creates `<dir>/logs/<username>--<timestamp>.log` and returns a `*log.Logger` plus its closer. |

## For AI Agents

### Working In This Directory
- `ExtractArchive` strips the `.temp` suffix before calling `archiver.ByExtension` because callers (notably `core.DownloadExtractDCCString`) write a temp file first. Preserve this offset math (`archivePath[:len(archivePath)-len(".temp")]`).
- The "single-file" extraction policy is intentional: per the recent fix in commit `ad12382` (`fix: don't decompress an archive that contains multiple files for delivery`), multi-file archives are returned as-is to the user. Don't regress.
- `IsArchive` returns true purely from extension lookup (no magic-byte sniffing) — fine for the OpenBooks flow because the IRC bot reports the filename directly.
- `OpenBrowser` swallows non-fatal errors via `log.Println`; production code paths should not depend on it succeeding.
- `CreateLogFile` always creates a `logs/` subdirectory — callers pass the *root* download dir, not the logs dir.

## Dependencies

### External
- `github.com/mholt/archiver/v3` (and the various format-specific dependencies it pulls in: `klauspost/compress`, `pierrec/lz4`, `nwaples/rardecode`, `dsnet/compress`, `ulikunitz/xz`, `xi2/xz`, `andybalholm/brotli`, `golang/snappy`).
- Standard library `os/exec`, `runtime`, `log`.

<!-- MANUAL: -->
