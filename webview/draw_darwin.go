//go:build darwin && cgo && webview_cef

package webview

import "github.com/qizhanchan/qui"

// drawWebViewTexture is the darwin compositor for an IOSurface-backed
// GL_TEXTURE_RECTANGLE produced by CefRenderHandler::OnAcceleratedPaint.
// It is currently a stub: until the rect-shader and IOSurface binding
// land in iosurface_darwin.{go,m}, the texture handle is always zero
// from the (also-stubbed) CEF backend and this is never called with
// meaningful arguments. The body should eventually set up the
// samplerRect shader, push uniforms, glScissor the physical clip via
// qui.PhysicalScissor, and draw a fullscreen quad.
func drawWebViewTexture(_ qui.GLState, _ uint32, _, _ int, _ qui.Rect, _ qui.Rect) {
	// TODO: bind samplerRect shader, apply scissor from clip,
	// glDrawArrays the quad. Until then this is a no-op even on darwin
	// with the webview_cef tag, because the CEF backend stub returns
	// no frames.
}
