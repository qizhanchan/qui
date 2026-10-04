//go:build !darwin || !cgo || !webview_cef

package webview

import "github.com/qizhanchan/qui"

// drawWebViewTexture is the platform-specific texture composite hook.
// On builds without a real backend it's a no-op — WebView.Draw still
// queues the closure for consistency with the production path, but
// nothing draws because there's no texture handle to bind.
func drawWebViewTexture(_ qui.GLState, _ uint32, _, _ int, _ qui.Rect, _ qui.Rect) {
}
