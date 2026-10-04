package qui

import "image"

// VectorSource is something that can rasterize itself at an arbitrary
// pixel size. The svg subpackage's Icon type is the canonical
// implementation, but the interface is intentionally minimal so other
// resolution-independent assets (e.g. an emoji vector path, a future
// path/glyph type) can plug in without depending on a specific format.
//
// Rasterize must return an image whose Bounds().Dx() == width and
// Bounds().Dy() == height. tint applies as a monochrome recolor for
// single-color glyphs — implementations are free to ignore it for
// multi-color sources. Implementations are expected to cache results
// keyed by (width, height, tint), since DrawVector is called every
// repaint.
type VectorSource interface {
	Rasterize(width, height int, tint Color) image.Image
}

// DrawVector is a thin shim that forwards to the canvas's own DrawVector
// method. Kept as a free function for the historical call sites (svg
// examples, widget helpers, embedded apps) that pre-date DrawVector
// being part of Canvas. New code should call canvas.DrawVector(…)
// directly.
func DrawVector(canvas Canvas, src VectorSource, rect Rect, tint Color) {
	if canvas == nil || src == nil {
		return
	}
	canvas.DrawVector(src, rect, tint)
}

// Canvas is the drawing surface. Every draw method honors the current
// state-stack frame: input rects are in LOGICAL coordinates, the canvas
// scales them by the cumulative scale and intersects with the current
// clip before rasterizing.
//
// State stack (Save/Restore/ClipRect/Scale) is the Skia model — it
// replaced the previous clipCanvas / scaleCanvas wrappers. Lexical
// scoping is the idiomatic usage:
//
//	id := canvas.Save()
//	canvas.ClipRect(innerRect)
//	defer canvas.RestoreTo(id)
//	// ... drawing inside innerRect, in logical coords ...
//
// Adding a new draw primitive means adding one method here (and to
// every backend); the rasterizer at the bottom is the only place that
// has to know about clip / scale composition.
type Canvas interface {
	// State stack — matches Skia's SkCanvas matrix/clip stack contract.
	// Translate / Scale / Rotate / Concat all post-multiply the current
	// frame's matrix (drawing happens "after" the new transform).
	Save() int
	Restore()
	RestoreTo(depth int)
	ClipRect(rect Rect)
	Scale(sx, sy float32)
	Translate(dx, dy float32)
	Rotate(theta float32)
	Concat(m Matrix)
	ClipBounds() Rect
	CurrentMatrix() Matrix

	// SaveLayer pushes an offscreen compositing layer. All draw calls
	// between this point and the matching Restore/RestoreTo write into
	// a fresh RGBA sized to `bounds` (intersected with the current
	// clip); on unwind the offscreen composites back through
	// paint.BlendMode with paint.effectiveAlpha() as an extra opacity
	// multiplier. Returns the depth to pass to RestoreTo.
	//
	// This is the primitive Skia uses for blur / drop-shadow /
	// glassmorphism / faded-widget transitions. Nested SaveLayer is
	// supported; the innermost active layer receives all draws.
	SaveLayer(bounds Rect, paint Paint) int

	// ClipPath narrows the current clip to the interior of `path`
	// (non-zero winding rule + 4x AA coverage). Implemented as an
	// internal SaveLayer + DstIn mask on Restore, so nested clip paths
	// compose correctly and every rasterizer respects the mask without
	// per-rasterizer changes. The caller must wrap in Save/Restore
	// (same convention as ClipRect); a bare ClipPath is not
	// self-undoable.
	ClipPath(path *Path)

	// DrawShape is the unified geometry+paint primitive. Every
	// backend's rasterization funnels through this one method, so
	// adding a new visual property (AntiAlias, BlendMode, …) means
	// adding a field to Paint — not a new method to every backend.
	// The convenience methods below (FillRect, StrokeRoundedRect, …)
	// are thin wrappers that construct a Shape + Paint pair and call
	// DrawShape; widget code can use either surface.
	DrawShape(shape Shape, paint Paint)

	// Draw primitives — all auto-clipped to the current stack frame.
	Clear(color Color)
	FillRect(rect Rect, color Color)
	FillRoundedRect(rect Rect, radius float32, color Color)
	StrokeRect(rect Rect, color Color, width float32)
	StrokeRoundedRect(rect Rect, radius float32, color Color, width float32)
	DrawText(text string, rect Rect, color Color, font Font)
	DrawImage(img image.Image, rect Rect)
	DrawShadow(rect Rect, radius float32, spec ElevationSpec, color Color)
	DrawLine(p1, p2 Point, color Color, width float32)
	DrawPolyline(points []Point, color Color, width float32)
	DrawVector(src VectorSource, rect Rect, tint Color)
}

// ClipAware is implemented by Canvas (the state stack carries the
// current clip). The interface stays exported because external widgets
// (webview's GL scissor, ScrollView dirty-region short-circuit) use it
// as a capability probe — every qui Canvas satisfies it.
type ClipAware interface {
	// ClipBounds is the current clip in PHYSICAL pixels (GL scissor /
	// image-blit consumers).
	ClipBounds() Rect
	// ClipBoundsLogical is the same clip in LOGICAL coordinates — what
	// widgets that intersect it against their own Bounds() must use.
	ClipBoundsLogical() Rect
}

// Renderer drives per-frame rendering and exposes a canvas.
type Renderer interface {
	Begin(windowSize Size) Canvas
	End()
}

// CPUFrameSource is an optional Renderer capability: the renderer can hand
// out the finished frame as a CPU image instead of presenting it itself.
//
// It exists so a platform surface that composites CPU pixels directly
// (CALayer contents on macOS, wl_shm on Wayland) can bypass the GPU
// round-trip entirely. Renderers that only know how to present through a
// GPU context simply don't implement it, and Window.present falls back to
// a buffer swap.
//
// The returned image is the renderer's own buffer and is reused on the next
// frame — a consumer must copy it or finish with it before returning.
type CPUFrameSource interface {
	// FrameImage returns the frame just rendered, or nil when this frame
	// has no CPU representation (a GPU-native backend that never touched
	// system memory).
	FrameImage() *image.RGBA
}

// NoopRenderer is a placeholder renderer that performs no drawing.
type NoopRenderer struct{}

type noopCanvas struct {
	*canvasState
}

func (NoopRenderer) Begin(s Size) Canvas {
	return noopCanvas{canvasState: newCanvasState(Rect{W: s.W, H: s.H})}
}
func (NoopRenderer) End() {}

func (c noopCanvas) Translate(dx, dy float32) { c.canvasState.Translate(dx, dy) }
func (c noopCanvas) Rotate(theta float32)     { c.canvasState.Rotate(theta) }
func (c noopCanvas) Concat(m Matrix)          { c.canvasState.Concat(m) }
func (c noopCanvas) CurrentMatrix() Matrix    { return c.canvasState.CurrentMatrix() }

// SaveLayer on noopCanvas degenerates to a plain Save — no offscreen
// buffer is allocated because noopCanvas never rasterizes, but the
// clip-stack behavior still matches so callers using
// `depth := c.SaveLayer(...); defer c.RestoreTo(depth)` unwind the
// same amount of state as they would on a real canvas.
func (c noopCanvas) SaveLayer(bounds Rect, _ Paint) int {
	depth := c.canvasState.Save()
	c.canvasState.ClipRect(bounds)
	return depth
}

// ClipPath on noopCanvas narrows the rectangular clip to the path's
// bounding box — a no-op for the rasterizer (there isn't one) but the
// clip-stack behavior stays symmetric with imageCanvas so tests that
// interleave ClipPath calls balance their Save/Restore pairs.
func (c noopCanvas) ClipPath(path *Path) {
	if path == nil || path.IsEmpty() {
		return
	}
	c.canvasState.ClipRect(path.Bounds())
}

func (noopCanvas) DrawShape(Shape, Paint)                          {}
func (noopCanvas) Clear(Color)                                     {}
func (noopCanvas) FillRect(Rect, Color)                            {}
func (noopCanvas) FillRoundedRect(Rect, float32, Color)            {}
func (noopCanvas) StrokeRect(Rect, Color, float32)                 {}
func (noopCanvas) StrokeRoundedRect(Rect, float32, Color, float32) {}
func (noopCanvas) DrawText(string, Rect, Color, Font)              {}
func (noopCanvas) DrawImage(image.Image, Rect)                     {}
func (noopCanvas) DrawVector(VectorSource, Rect, Color)            {}
func (noopCanvas) DrawShadow(Rect, float32, ElevationSpec, Color)  {}
func (noopCanvas) DrawLine(Point, Point, Color, float32)           {}
func (noopCanvas) DrawPolyline([]Point, Color, float32)            {}
