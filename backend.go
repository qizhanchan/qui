package qui

import "image"

// RasterBackend is the abstraction below the Canvas frontend. Everything
// that reads state (matrix, clip stack, layer bookkeeping) and dispatches
// on Shape lives on the imageCanvas frontend. Everything that TURNS
// primitives INTO PIXELS lives on a RasterBackend implementation.
//
// Two invariants the backend can assume every method reads by:
//
//   - Every rect / point argument is in PHYSICAL PIXEL coordinates. The
//     canvas frontend has already applied the current stateFrame's
//     matrix.
//   - Every method that draws also receives a `clip` in physical pixels;
//     the backend must not paint outside it. (Physical scissoring on GPU,
//     Intersect on CPU — the guarantee is that pixels outside `clip` stay
//     untouched.)
//
// New backends are additive — the CPU rasterizer (cpuBackend) is the
// reference implementation. The GPU backend swaps into the same slot and
// interleaves with the CPU one via layer buffers when a specific
// primitive doesn't have a fast-path implementation.
//
// Text / image / vector / shadow are kept on the interface even though
// their GPU implementations will lean on glyph atlases / texture blits
// rather than shader math — the frontend shouldn't have to know which
// backend it's talking to.
type RasterBackend interface {
	// Begin is called at the start of every frame with the physical
	// framebuffer size. Backends allocate/resize their scratch buffers
	// here. Clear/state resets belong here too — anything End() left
	// stale.
	Begin(size Size)
	// End is called after the frame is fully rendered. The CPU backend's
	// output image is available via CPUImage() for the GL blit step;
	// GPU backends flush pending batches inside End.
	End()

	// CPUImage exposes the CPU backend's RGBA framebuffer to the GL blit
	// step at End(). GPU-native backends return nil to signal "no CPU
	// blit needed; framebuffer already holds final pixels."
	//
	// This is a free, live view — never a conversion. A backend whose
	// pixels are not in system memory must return nil here and implement
	// CPUReadback instead, so per-frame paths (the blit, presentation)
	// can't accidentally pay for a GPU→CPU transfer.
	CPUImage() *image.RGBA

	// CompositeToDefault blits the backend's accumulated 2D layer onto
	// the currently-bound GL framebuffer (typically the default one).
	// Called from GLRenderer.End() AFTER the framebuffer clear + any
	// pre-hook / deferred-GL replay, so the 2D layer lands on top of
	// scene3d / video / webview textures that live under it.
	//
	// CPU backend uploads its RGBA into the shared texture and issues
	// the existing full-screen quad draw. GPU backend binds its FBO's
	// color texture (contents already accumulated during widget.Draw)
	// and issues the same draw. GLRenderer owns the shader / VAO;
	// backends borrow them via the *GLRenderer handle.
	CompositeToDefault(r *GLRenderer)

	// -- Draw primitives — physical coords, physical clip -------------

	// Clear paints `color` over the ENTIRE current target (not just the
	// clip). Used by Canvas.Clear.
	Clear(color Color)

	// FillRect writes `color` into `dst` (already intersected with the
	// physical clip by the frontend, so no additional clipping needed).
	FillRect(dst image.Rectangle, color Color)

	// FillRoundedRect writes `color` inside `rect` with `radius` corners,
	// clipped to `clip`. Both `rect` and `clip` are in physical pixels.
	FillRoundedRect(rect Rect, radius float32, clip Rect, color Color)

	// StrokeRect paints a hollow rectangle outline `width` px wide,
	// inside `clip`. Coordinates are physical pixels.
	StrokeRect(rect Rect, clip Rect, color Color, width float32)

	// StrokeRoundedRect paints a hollow rounded outline. See
	// FillRoundedRect for coordinate conventions.
	StrokeRoundedRect(rect Rect, radius float32, clip Rect, color Color, width float32)

	// FillPath fills the interior of `subs` (a set of flattened
	// subpaths already in physical coords) with either paint.Color or
	// paint.Shader. `invMatrix` maps physical pixel back to logical
	// (shader) space; only consumed when paint.Shader != nil.
	// `clip` is the physical pixel clip rect (image.Rectangle so
	// backends can Intersect directly).
	FillPath(subs [][]Point, clip image.Rectangle, paint Paint, invMatrix Matrix)

	// StrokePath paints the outline of `subs` (points already in
	// physical coords) at `width` physical pixels. `closedFlags` marks
	// which subs originated from Close() so ends draw joins instead of
	// caps. `paint` carries StrokeCap / StrokeJoin / MiterLimit / Color.
	StrokePath(subs [][]Point, closedFlags []bool, clip image.Rectangle, width float32, paint Paint)

	// DrawLine paints a single line segment. Cap comes from `cap`.
	// Coordinates and width are physical.
	DrawLine(p1, p2 Point, clip Rect, color Color, width float32, cap StrokeCap)

	// DrawPolyline paints an open polyline. Coordinates already in
	// physical pixels — `m` is passed for backends that need it (the CPU
	// backend uses identity here since projection already happened).
	DrawPolyline(pts []Point, clip Rect, color Color, width float32)

	// DrawShadow paints a soft blurred shadow under a rounded rect. Kept
	// as its own primitive (not composed of others) because the GPU
	// implementation will use a specialized shader; the CPU one uses
	// separable box blur.
	DrawShadow(rect Rect, radius float32, spec ElevationSpec, shadowColor Color, clip Rect)

	// DrawText renders `text` inside `rect` (physical) clipped to `clip`,
	// using the resolved `fontSpec`. Font-size scaling was applied by
	// the frontend before the call.
	DrawText(text string, rect Rect, clip Rect, color Color, fontSpec Font)

	// DrawImage draws `img` into `dst` (physical) clipped to `clip`,
	// bilinearly resampled if `dst`'s size differs from `img.Bounds()`.
	DrawImage(img image.Image, dst Rect, clip Rect)

	// DrawImageTransformed draws `img` through the affine `m`, which maps
	// the image's OWN PIXEL SPACE — (0,0) to (w,h) — onto physical device
	// space. Clipped to `clip` like every other primitive.
	//
	// This is the rotated / skewed counterpart to DrawImage, and the one
	// place non-axis-aligned raster content is produced. The frontend
	// routes here whenever `Matrix.IsAxisAligned()` is false, first
	// rasterizing text and vectors into an offscreen RGBA — so this single
	// primitive covers all three of text / image / vector, and a backend
	// only has to get one inverse map right instead of three.
	//
	// (Shapes need no equivalent: imageCanvas.DrawShape already converts
	// rotated rects to a Path and lets the path rasterizer handle them.)
	DrawImageTransformed(img image.Image, m Matrix, clip Rect)

	// DrawVector rasterizes `src` at `dst`'s physical size, tinted by
	// `tint`, and blits into `dst` clipped to `clip`.
	DrawVector(src VectorSource, dst Rect, clip Rect, tint Color)

	// -- Layer management --------------------------------------------
	//
	// The backend owns its own layer stack. Frontend Canvas.SaveLayer
	// tracks save-depth-to-layer-id mapping, and calls PopLayer at the
	// depth boundary. `mask` (optional) is a coverage alpha, aligned
	// with the layer buffer; ClipPath is the only current caller.

	// PushLayer allocates an offscreen sized to `physBounds` and makes
	// subsequent draw primitives write into it. `paint` is remembered
	// for the eventual PopLayer composite (BlendMode, Alpha, ColorFilter,
	// ImageFilter).
	//
	// `clip` is the clip in effect at push time, and the composite must
	// honor it like every other method here honors its own — it is NOT
	// implied by physBounds. An ImageFilter (blur, drop shadow) legally
	// produces an image LARGER than the layer it was given, so the
	// composite rect is filter output ∩ clip, not filter output ∩
	// physBounds: growing past the layer is the filter's whole purpose,
	// growing past the clip paints pixels the frame never invalidated
	// and therefore never cleans up again.
	PushLayer(physBounds, clip image.Rectangle, paint Paint)

	// PopLayer composites the innermost layer back onto its parent
	// using the paint stored at PushLayer time. `mask` (nullable) is
	// applied to the source pre-composite — ClipPath's mechanism.
	PopLayer(mask *image.Alpha)
}

// CPUReadback is the optional ability to produce a CPU-side copy of the
// finished frame when the backend's pixels do NOT live in system memory.
//
// It exists for the snapshot family (Window.Snapshot, SnapshotScaled,
// SnapshotRegion, SnapshotAnnotated, golden captures, agent screenshots),
// which needs real pixels regardless of where the rasterizer put them.
// Without it, a GPU-native backend reports "no CPU image" via CPUImage and
// a snapshot silently captures an empty buffer — the pixels exist, but only
// on the GPU.
//
// Kept separate from CPUImage rather than folded into it precisely because
// the costs differ by orders of magnitude: CPUImage is a pointer, this is a
// synchronous GPU→CPU transfer that stalls the pipeline. Per-frame code
// (the End blit, presentation) must only ever use CPUImage, so that the
// expensive path cannot be reached by accident.
//
// Implementations must return a fresh image the caller owns, in top-origin
// row order (GL reads bottom-up — flip), and must return nil rather than a
// partly-filled image if they cannot read back. A backend whose pixels are
// already in system memory should NOT implement this: falling through to
// CPUImage is both correct and free.
type CPUReadback interface {
	ReadbackCPU() *image.RGBA
}
