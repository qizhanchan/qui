package render

import (
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Retained Metal draws — the Metal renderer's counterpart of retained GL
// draws (gl_retained.go), with the same lifetime rule: a draw re-runs on
// every later frame whose damage does not touch its bounds, and on every
// RequestGLRedraw frame, until a frame repaints part of its bounds without
// the owner re-queuing it.
//
// Instead of raw API access the draw gets a MetalCompositor: the renderer
// owns devices, pipelines and texture caches, and offers the compositing
// primitives widgets actually need (a video frame, a pushed RGBA frame).
// Like a GL draw it lands UNDER the 2D layer, which shows it through the
// holes Canvas.ClearRect opened.

// MetalCompositor composites textures into the frame being presented.
// Rectangles are physical framebuffer pixels, top-left origin; src is in
// the source's own pixels. Valid only inside the draw callback.
type MetalCompositor interface {
	// DrawPixelBuffer shows the src part of a CVPixelBufferRef (32BGRA,
	// IOSurface-backed: zero copy) at dst. False when the buffer cannot
	// be bound as a texture; the caller may then upload its pixels.
	DrawPixelBuffer(pixelBuffer unsafe.Pointer, src, dst gfx.Rect) bool
	// DrawPixels uploads a w×h frame of 32-bit pixels (RGBA, or BGRA when
	// bgra) into a texture cached under key and shows its src part at dst.
	// ForgetRetainedGLDraw(key) releases that texture too.
	DrawPixels(key any, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst gfx.Rect) bool
	// DrawYUV420 uploads planar YCbCr 4:2:0 (one 8-bit plane each, chroma
	// at half size) into textures cached under key and shows the src part
	// (luma pixels) at dst, converting on the GPU: rgb = m · ((yuv − off)
	// ∘ scale), m column-major. ForgetRetainedGLDraw(key) releases them.
	DrawYUV420(key any, planes [3]YUVPlane, m [9]float32, off, scale [3]float32, src, dst gfx.Rect) bool
	// FramebufferSize is the frame's physical size.
	FramebufferSize() gfx.Size
}

// YUVPlane is one 8-bit plane of a planar YCbCr picture.
type YUVPlane struct {
	Pix          unsafe.Pointer
	Stride, W, H int
}

// RetainedMetalCanvas is implemented by the Metal renderer's canvas.
// Type-assert the canvas passed to Draw — before RetainedGLCanvas, which
// the same canvas implements only to detect GL-only widgets.
type RetainedMetalCanvas interface {
	// QueueRetainedMetalDraw registers fn under key for this and later
	// frames (see above). physBounds is the area fn paints, physical
	// pixels, top-left origin. fn runs on the UI thread and should read
	// the content it shows at call time, so a RequestGLRedraw frame picks
	// up new content.
	QueueRetainedMetalDraw(key any, physBounds gfx.Rect, fn func(MetalCompositor))
}
