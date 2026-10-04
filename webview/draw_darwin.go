//go:build darwin && cgo && webview_cef

package webview

import "github.com/qizhanchan/qui"

// drawWebViewTexture is the darwin compositor for an IOSurface-backed
// GL_TEXTURE_RECTANGLE produced by CefRenderHandler::OnAcceleratedPaint.
// Phase A ships a stub: until the rect-shader and IOSurface binding
// land in iosurface_darwin.{go,m} (Phase D), the texture handle is
// always zero from the (also-stubbed) CEF backend and this is never
// called with meaningful arguments. Once Phase D lands, this body
// will set up the samplerRect shader, push uniforms, glScissor the
// physical clip via qui.PhysicalScissor, and draw a fullscreen quad.
func drawWebViewTexture(_ qui.GLState, _ uint32, _, _ int, _ qui.Rect, _ qui.Rect) {
	// TODO Phase D: bind samplerRect shader, apply scissor from clip,
	// glDrawArrays the quad. Until then this is a no-op even on darwin
	// with the webview_cef tag, because the CEF backend stub returns
	// no frames.
}
