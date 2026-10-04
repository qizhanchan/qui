// Package webview embeds an HTML/CSS/JS web engine into the qui widget
// tree. Web content renders into either an OpenGL texture or a Go-owned
// image, and whichever the backend produces is composited onto qui's
// framebuffer, so a WebView participates in the qui dirty-region
// pipeline, can be obscured by other widgets, and can live inside
// ScrollView / Tabs with proper clipping. The CEF backend currently
// delivers CPU frames via canvas.DrawImage; GL/IOSurface compositing is
// not implemented yet.
//
// # Architecture
//
// All platform-specific work (CEF lifecycle, OnAcceleratedPaint, IOSurface
// binding, JS↔Go IPC) hides behind the unexported Backend / Page
// interfaces. The cross-platform WebView widget depends only on those
// interfaces, so its event handling, focus, and IME plumbing stays
// platform-agnostic.
//
// Currently macOS via CEF (Chromium Embedded Framework) multi-process
// OSR. Other platforms return ErrNotSupported until a platform-specific
// backend is added.
//
// # macOS Build / Deploy
//
// CEF requires a macOS .app bundle (multi-process needs helper
// executables under Contents/Frameworks/). The webview/scripts/
// directory ships tooling for this:
//
//	webview/scripts/fetch-cef.sh        # one-time: download CEF binary distribution
//	webview/scripts/build-helpers.sh    # compile the helper executable and 5 helper .apps
//	webview/scripts/package-app.sh      # bundle a Go binary + helpers + CEF framework into .app
//
// CEF integration is gated behind the build tag webview_cef so the default
// `go build ./...` works without the CEF SDK on hand:
//
//	go build ./...                      # default — no CEF, NewWebView returns ErrNotSupported
//	go build -tags webview_cef ./...    # enable darwin CEF backend
//
// # Threading
//
// The cross-platform widget API is safe to call from any goroutine —
// Page methods serialize calls onto the CEF UI thread (= qui's main
// goroutine) via an internal mpsc channel drained inside Backend.Tick.
// Synchronous methods (EvaluateJS) block on a per-call reply channel.
//
// # Limitations
//
//   - macOS only.
//   - JS↔Go bridge transports JSON strings only (no automatic struct
//     reflection); JS side uses `window.qui.<name>(JSON.stringify(...))`
//     and awaits a Promise resolving to the reply string.
//   - Popups (select dropdowns, datepickers) collapse to non-popup
//     fallbacks; richer popup support is not implemented.
//   - No printing, no extensions, no DevTools.
//
// # Subpackage Import Direction
//
// Allowed: this package imports `github.com/qizhanchan/qui`. It does
// NOT import widgets/, scene3d/, anim/, graphs/, svg/, or media/ — and
// root never imports webview/. Reuse of rect-shader machinery from
// media/ is via copy-paste; a shared rect-texture helper could be
// promoted to root later for cleanliness.
package webview
