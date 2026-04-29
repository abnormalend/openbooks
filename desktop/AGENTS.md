<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-04-29 | Updated: 2026-04-29 -->

# desktop

## Purpose
Optional native-webview wrapper around the running web server, gated by build tags so the default binary doesn't carry the heavy webview dependency. Build with `-tags webview` to embed a 1200×800 native window pointed at the local server; without the tag, the implementation simply opens the user's default browser.

## Key Files
| File | Description |
|------|-------------|
| `desktop.go` | Default (`!webview`) implementation — calls `util.OpenBrowser` and blocks forever on a never-closed channel. |
| `desktop_webview.go` | `!windows && webview` build — uses `github.com/webview/webview` (system webview on Linux/macOS). |
| `desktop_webview_windows.go` | `windows && webview` build — uses `github.com/inkeliz/gowebview` (WebView2). |

## For AI Agents

### Working In This Directory
- All three files implement `func StartWebView(url string, debug bool)` — the build-tag set must remain mutually exclusive. If you add a new variant, check that the existing `//go:build` lines still partition the matrix.
- The default (no-tag) variant is what `build.sh` and the Dockerfile produce; treat `webview` as opt-in for developers building locally.
- The non-webview path blocks on `<-make(chan struct{})` so the parent goroutine that started the HTTP server keeps running. Don't replace it with `os.Exit(0)`.

### Testing Requirements
- Webview builds depend on system libraries (gtk-webkit2 / WebView2). They cannot be cross-compiled with CGO disabled, so the release pipeline does not exercise them — verify locally before merging webview changes.

## Dependencies

### Internal
- `util/` — `util.OpenBrowser` in the default implementation.

### External
- `github.com/webview/webview` (Linux/macOS, webview tag).
- `github.com/inkeliz/gowebview` and `github.com/inkeliz/w32` (Windows, webview tag).

<!-- MANUAL: -->
