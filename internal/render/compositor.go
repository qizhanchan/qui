package render

import (
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Retained compositor draws — video-like content (decoded frames, pushed
// RGBA frames) composited UNDER the 2D layer, which shows it through the
// holes Canvas.ClearRect opened. WebGPURenderer supports them; on the CPU
// path (no Dawn) media.FrameView draws its frames into the 2D layer and
// media.VideoView shows only its background.
//
// A retained draw re-runs on every later frame whose damage does not touch
// its bounds, and on every RequestGPURedraw frame, until a frame repaints
// part of its bounds without the owner re-queuing it (RetainedSet).
//
// The draw gets a Compositor rather than raw API access: the renderer owns
// devices, pipelines and texture caches.

// Compositor composites textures into the frame being presented.
// Rectangles are physical framebuffer pixels, top-left origin; src is in
// the source's own pixels. Valid only inside the draw callback.
type Compositor interface {
	// DrawPixelBuffer shows the src part of a CVPixelBufferRef (32BGRA,
	// IOSurface-backed: zero copy) at dst. False when the buffer cannot
	// be bound as a texture; the caller may then upload its pixels.
	DrawPixelBuffer(pixelBuffer unsafe.Pointer, src, dst gfx.Rect) bool
	// DrawPixels uploads a w×h frame of 32-bit pixels (RGBA, or BGRA when
	// bgra) into a texture cached under key and shows its src part at dst.
	// version names the content: a nonzero version equal to the one the
	// cached texture was last uploaded with skips the upload (a retained
	// draw replaying an unchanged frame); 0 always uploads.
	// ForgetRetainedDraw(key) releases that texture too.
	DrawPixels(key any, version uint64, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst gfx.Rect) bool
	// DrawYUV420 uploads planar YCbCr 4:2:0 (one 8-bit plane each, chroma
	// at half size) into textures cached under key and shows the src part
	// (luma pixels) at dst, converting on the GPU: rgb = m · ((yuv − off)
	// ∘ scale), m column-major. version skips the upload as in DrawPixels.
	// ForgetRetainedDraw(key) releases them.
	DrawYUV420(key any, version uint64, planes [3]YUVPlane, m [9]float32, off, scale [3]float32, src, dst gfx.Rect) bool
	// FramebufferSize is the frame's physical size.
	FramebufferSize() gfx.Size
}

// YUVPlane is one 8-bit plane of a planar YCbCr picture.
type YUVPlane struct {
	Pix          unsafe.Pointer
	Stride, W, H int
}

// RetainedCompositorCanvas is implemented by the canvases of renderers
// that present through the GPU. Type-assert the canvas passed to Draw.
type RetainedCompositorCanvas interface {
	// QueueRetainedDraw registers fn under key for this and later frames
	// (see above). physBounds is the area fn paints, physical pixels,
	// top-left origin. fn runs on the UI thread and should read the
	// content it shows at call time, so a RequestGPURedraw frame picks up
	// new content.
	QueueRetainedDraw(key any, physBounds gfx.Rect, fn func(Compositor))
}

// CrossThreadRetainedCanvas is implemented by a page window's canvas
// (RFC 0005): retained draws queued here are composited UNDER the 2D layer
// by the host window, on the host's threads, concurrently with the owner's
// own thread. fn must therefore read only state it synchronizes itself (or
// captured at queue time). Owners that can promise that (media.VideoView,
// media.FrameView) prefer it over RetainedCompositorCanvas.
type CrossThreadRetainedCanvas interface {
	QueueCrossThreadRetainedDraw(key any, physBounds gfx.Rect, fn func(Compositor))
}

// RetainedOverlayCanvas is implemented by the canvases of renderers that
// can composite retained draws OVER the 2D layer, the way a browser's
// compositor draws a layer it holds as a texture over the page: an
// animated layer (a CSS marquee) moves by replaying such a draw at a new
// offset (Window.RequestGPURedraw) with nothing rasterized or uploaded.
// The draw covers whatever the 2D layer shows in physBounds, so its owner
// must make sure nothing that paints after it overlaps there. Lifetime
// rules are those of QueueRetainedDraw.
type RetainedOverlayCanvas interface {
	QueueRetainedOverlayDraw(key any, physBounds gfx.Rect, fn func(Compositor))
}

// CompositorOverlayCanvas queues an overlay draw the renderer may also run
// on its compositor thread, to keep a layer moving while the UI thread is
// busy (RFC 0004 R4). fn must read only data it owns or that is immutable
// — a snapshot committed by the UI thread — and take the frame's time
// from the Compositor (FrameTimer), never from widgets.
type CompositorOverlayCanvas interface {
	QueueCompositorOverlayDraw(key any, physBounds gfx.Rect, fn func(Compositor))
}

// FrameTimer is implemented by Compositors: the time of the frame being
// composited (the UI thread's frame clock, or the compositor thread's
// vsync estimate).
type FrameTimer interface {
	FrameTime() time.Time
}

// CompositorRenderer is a renderer whose compositor-safe overlay draws can
// be presented from another thread.
type CompositorRenderer interface {
	// CompositorWanted reports whether every retained draw of the last
	// frame is compositor-safe and one of them exists. Any thread.
	CompositorWanted() bool
	// RedrawFromCompositor presents the last frame with its retained
	// draws replayed at time t; false when the UI thread holds the
	// renderer (it is painting) or the draws are not all compositor-safe.
	// Called on the compositor thread.
	RedrawFromCompositor(t time.Time) bool
	// SetFrameTime sets the time the UI thread's next frame composites at.
	SetFrameTime(t time.Time)
}

// RetainedRenderer replays retained draws without rasterizing
// (Window.RequestGPURedraw) and forgets them on request.
type RetainedRenderer interface {
	ForgetRetainedDraw(key any)
	RedrawRetained() bool
}
