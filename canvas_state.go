package qui

import (
	"image"
	"math"
)

// canvasState is the per-Canvas drawing-state stack — what Skia models
// as SkCanvas's matrix/clip stack. Every Canvas implementation owns one
// (shared by value-receiver methods through a pointer field) so Save /
// Restore / ClipRect / Scale / Translate / Rotate / Concat work
// uniformly across the imageCanvas, noopCanvas, RecordingCanvas, and
// any future backend.
//
// The stack is never empty: the bottom-most frame holds the identity
// state (identity matrix, no clip) the canvas was built with.
//
// Two key invariants the draw methods rely on:
//   - `top.clip` is the current effective clip in PHYSICAL pixels, the
//     same space the backend rasterizes in. ClipRect's caller passes a
//     rect in LOGICAL coords; we transform it to physical at push time
//     and AABB-intersect with the current clip.
//   - `top.matrix` is the cumulative 2x3 affine that maps LOGICAL coords
//     to PHYSICAL pixels. Translate / Scale / Rotate / Concat all
//     post-multiply (drawing happens "after" the new transform), matching
//     SkCanvas's behavior.
//
// Save / RestoreTo are O(1) — push copies the current top frame, pop
// truncates the slice.
type canvasState struct {
	stack []stateFrame
}

// Matrix is a 2x3 affine transform stored row-major: [a b tx; c d ty].
//
//	x' = a*x + b*y + tx
//	y' = c*x + d*y + ty
//
// A diagonal matrix (b == 0, c == 0) is axis-aligned — the
// fast paths in imageCanvas detect this case and fall through to the
// scale-and-translate rasterizers. Off-diagonal terms (rotation, skew)
// route through path-based rasterization.
type Matrix struct {
	A, B, TX float32
	C, D, TY float32
}

// IdentityMatrix returns the identity (no-op) matrix.
func IdentityMatrix() Matrix {
	return Matrix{A: 1, D: 1}
}

// TranslateMatrix returns a translation matrix.
func TranslateMatrix(dx, dy float32) Matrix {
	return Matrix{A: 1, D: 1, TX: dx, TY: dy}
}

// ScaleMatrix returns a scale matrix.
func ScaleMatrix(sx, sy float32) Matrix {
	return Matrix{A: sx, D: sy}
}

// RotateMatrix returns a rotation matrix (radians).
func RotateMatrix(theta float32) Matrix {
	c := float32(math.Cos(float64(theta)))
	s := float32(math.Sin(float64(theta)))
	return Matrix{A: c, B: -s, C: s, D: c}
}

// Concat returns m * n (apply n then m). When used to compose draw-time
// transforms, post-multiply: existing * new — meaning calling
// canvas.Translate then canvas.Scale draws as if "translate to origin,
// then scale around that origin", the same as SkCanvas.
func (m Matrix) Concat(n Matrix) Matrix {
	return Matrix{
		A:  m.A*n.A + m.B*n.C,
		B:  m.A*n.B + m.B*n.D,
		TX: m.A*n.TX + m.B*n.TY + m.TX,
		C:  m.C*n.A + m.D*n.C,
		D:  m.C*n.B + m.D*n.D,
		TY: m.C*n.TX + m.D*n.TY + m.TY,
	}
}

// TransformPoint maps a point through the matrix.
func (m Matrix) TransformPoint(p Point) Point {
	return Point{
		X: m.A*p.X + m.B*p.Y + m.TX,
		Y: m.C*p.X + m.D*p.Y + m.TY,
	}
}

// TransformRect maps a rect through the matrix, returning the axis-
// aligned bounding box of the four transformed corners. For a diagonal
// matrix this is the exact rect; for rotated matrices it's a conservative
// AABB (used by clip-rect intersection and dirty-region computation).
func (m Matrix) TransformRect(r Rect) Rect {
	if m.B == 0 && m.C == 0 {
		// Axis-aligned fast path — preserves sign on negative scales by
		// normalizing into a positive-extent rect.
		x0 := m.A*r.X + m.TX
		y0 := m.D*r.Y + m.TY
		x1 := m.A*(r.X+r.W) + m.TX
		y1 := m.D*(r.Y+r.H) + m.TY
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		if y1 < y0 {
			y0, y1 = y1, y0
		}
		return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	}
	corners := [4]Point{
		m.TransformPoint(Point{X: r.X, Y: r.Y}),
		m.TransformPoint(Point{X: r.X + r.W, Y: r.Y}),
		m.TransformPoint(Point{X: r.X + r.W, Y: r.Y + r.H}),
		m.TransformPoint(Point{X: r.X, Y: r.Y + r.H}),
	}
	minX, minY := corners[0].X, corners[0].Y
	maxX, maxY := corners[0].X, corners[0].Y
	for i := 1; i < 4; i++ {
		if corners[i].X < minX {
			minX = corners[i].X
		}
		if corners[i].X > maxX {
			maxX = corners[i].X
		}
		if corners[i].Y < minY {
			minY = corners[i].Y
		}
		if corners[i].Y > maxY {
			maxY = corners[i].Y
		}
	}
	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// IsAxisAligned reports whether the matrix is a translate-and-scale
// (no rotation, no skew). Fast-path rasterizers gate on this — the
// existing rect / rounded-rect AA pipelines only handle axis-aligned
// shapes; rotated shapes route through path flattening.
func (m Matrix) IsAxisAligned() bool {
	return m.B == 0 && m.C == 0
}

// Invert returns the inverse of m. The boolean is false when the matrix
// is singular (determinant ~ 0); the returned matrix is undefined in
// that case. Used by hit-testing code that needs to map screen coords
// back to canvas-local coords under a custom transform.
func (m Matrix) Invert() (Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if det == 0 {
		return Matrix{}, false
	}
	inv := 1 / det
	return Matrix{
		A:  m.D * inv,
		B:  -m.B * inv,
		C:  -m.C * inv,
		D:  m.A * inv,
		TX: (m.B*m.TY - m.D*m.TX) * inv,
		TY: (m.C*m.TX - m.A*m.TY) * inv,
	}, true
}

// avgScale returns the geometric-mean scale (sqrt(|det|)) of the
// matrix — what stroke-width / corner-radius rasterization uses to
// pick a physical-pixel size for resolution-independent properties.
// For an axis-aligned matrix this equals sqrt(|sx*sy|).
func (m Matrix) avgScale() float32 {
	det := m.A*m.D - m.B*m.C
	if det < 0 {
		det = -det
	}
	return float32(math.Sqrt(float64(det)))
}

type stateFrame struct {
	// clip is in PHYSICAL pixels (already pre-transformed). All draw
	// methods expect a rect they can directly Intersect with c.img.Bounds().
	clip Rect
	// matrix is the cumulative 2x3 affine from logical → physical
	// coordinates. The bottom-most frame is the identity; window.go
	// pushes a Scale(dpr, dpr) at frame start for HiDPI.
	matrix Matrix
}

// scaleX / scaleY accessors keep the historical "axis-aligned scale"
// vocabulary working for code paths that pre-date the matrix
// refactor. They return the diagonal terms — correct for the
// axis-aligned majority of frames, and a reasonable approximation
// for rotated frames (used only for stroke width / corner radius
// quantization, where rough is fine).
func (f stateFrame) scaleX() float32 {
	if f.matrix.B == 0 && f.matrix.C == 0 {
		return f.matrix.A
	}
	// Column length — preserves magnitude under rotation.
	return float32(math.Sqrt(float64(f.matrix.A*f.matrix.A + f.matrix.C*f.matrix.C)))
}

func (f stateFrame) scaleY() float32 {
	if f.matrix.B == 0 && f.matrix.C == 0 {
		return f.matrix.D
	}
	return float32(math.Sqrt(float64(f.matrix.B*f.matrix.B + f.matrix.D*f.matrix.D)))
}

// newCanvasState constructs a state stack with one bottom frame whose
// clip is `physicalBounds` — the full extent of the backend's pixel
// surface. The matrix starts as identity; window.go pushes a
// Scale(dpr, dpr) at frame start when HiDPI is in play.
func newCanvasState(physicalBounds Rect) *canvasState {
	return &canvasState{
		stack: []stateFrame{{
			clip:   physicalBounds,
			matrix: IdentityMatrix(),
		}},
	}
}

// surfaceBounds returns the backend's full pixel extent — the bottom
// frame's clip, which no Save/ClipRect ever widens.
func (s *canvasState) surfaceBounds() Rect { return s.stack[0].clip }

// setClip REPLACES the current frame's clip (physical pixels) instead of
// intersecting into it. Deliberately unexported and near-unused: widening
// a clip is wrong for every drawing primitive, since anything it lets
// through lands on pixels the frame never invalidated. SaveLayer with an
// ImageFilter is the one legitimate caller — the widened clip applies to
// an offscreen whose composite re-imposes the outer clip.
func (s *canvasState) setClip(physical Rect) { s.top().clip = physical }

// top returns the current (mutable) frame. The pointer is only valid
// until the next Save() / RestoreTo(): both can reallocate the
// underlying slice. Callers should re-fetch after any stack op.
func (s *canvasState) top() *stateFrame {
	return &s.stack[len(s.stack)-1]
}

// topCopy is the read-only sibling — returns a value of the current
// frame, safe to keep across other state ops. Draw methods that need
// matrix + clip in one shot call this.
func (s *canvasState) topCopy() stateFrame {
	return s.stack[len(s.stack)-1]
}

// Save pushes a copy of the current frame and returns the depth that
// RestoreTo should be passed to unwind back here. Matches Skia's
// save() / restoreToCount(n) contract: `defer c.RestoreTo(c.Save())`
// is the canonical scope guard.
func (s *canvasState) Save() int {
	depth := len(s.stack)
	s.stack = append(s.stack, s.stack[depth-1])
	return depth
}

// Restore pops one frame. No-op if only the bottom frame is left —
// production code should never reach that, but we don't panic so
// tests / shutdown paths stay forgiving.
func (s *canvasState) Restore() {
	if len(s.stack) <= 1 {
		return
	}
	s.stack = s.stack[:len(s.stack)-1]
}

// RestoreTo unwinds the stack until exactly `depth` frames remain. The
// argument is the value returned by Save. Out-of-range or negative
// depths clamp to a safe range so a stray defer doesn't blow up the
// next frame.
func (s *canvasState) RestoreTo(depth int) {
	if depth < 1 {
		depth = 1
	}
	if depth >= len(s.stack) {
		return
	}
	s.stack = s.stack[:depth]
}

// ClipRect intersects the current frame's clip with `r` (in logical
// coords). The rect is transformed to physical pixels first so the stored
// clip is directly usable by the rasterizer. Under rotation the result
// is the AABB of the four transformed corners — a conservative
// over-estimate for rotated subtrees (Skia uses an SkRasterClip region
// for tighter clipping; ours stays AABB until ClipPath lands).
func (s *canvasState) ClipRect(r Rect) {
	top := s.top()
	physR := top.matrix.TransformRect(r)
	top.clip = top.clip.Intersect(physR)
}

// Scale post-multiplies the current frame's matrix by a scale.
// Doesn't push a frame — wrap in Save/Restore if you want to undo it.
func (s *canvasState) Scale(sx, sy float32) {
	top := s.top()
	top.matrix = top.matrix.Concat(ScaleMatrix(sx, sy))
}

// Translate post-multiplies the current frame's matrix by a translation.
// Drawing primitives that follow are offset by (dx, dy) in the local
// space (the same space their coordinates are quoted in). Doesn't push a
// frame.
func (s *canvasState) Translate(dx, dy float32) {
	top := s.top()
	top.matrix = top.matrix.Concat(TranslateMatrix(dx, dy))
}

// Rotate post-multiplies the current frame's matrix by a rotation
// (radians, clockwise in screen-space because Y grows downward).
// Drawing primitives that follow rotate around the local origin.
// Doesn't push a frame.
func (s *canvasState) Rotate(theta float32) {
	top := s.top()
	top.matrix = top.matrix.Concat(RotateMatrix(theta))
}

// Concat post-multiplies the current frame's matrix by m. Useful for
// applying a precomputed transform (e.g. an SVG node's transform attr)
// in one shot. Doesn't push a frame.
func (s *canvasState) Concat(m Matrix) {
	top := s.top()
	top.matrix = top.matrix.Concat(m)
}

// ClipBounds returns the current effective clip in physical pixels.
// Used by widgets that need a pixel-aligned scissor (webview's GL
// scissor, GL blit destination clamp). Logical-coord consumers that
// compare against widget Bounds() must use ClipBoundsLogical instead.
func (s *canvasState) ClipBounds() Rect {
	return s.stack[len(s.stack)-1].clip
}

// ClipBoundsLogical returns the current effective clip mapped back into
// LOGICAL coordinates — the same space widget Bounds() live in. This is
// what dirty-region consumers (Container.Draw culling, ScrollView's
// content clip) want: they intersect the clip with logical widget rects
// and then either skip drawing or re-clip in logical space.
//
// The stored clip is in physical pixels (matrix-transformed at ClipRect
// time); we invert the top frame's matrix to project it back. A singular
// matrix (degenerate scale) falls back to the physical clip unchanged so
// callers still get a non-empty rect rather than a collapsed one.
func (s *canvasState) ClipBoundsLogical() Rect {
	top := s.stack[len(s.stack)-1]
	inv, ok := top.matrix.Invert()
	if !ok {
		return top.clip
	}
	return inv.TransformRect(top.clip)
}

// CurrentMatrix returns the cumulative 2x3 affine for the top frame.
// Exposed so hit-testing code (selection handles in a flowchart editor,
// canvas panning) can map screen coords back to canvas-local coords.
func (s *canvasState) CurrentMatrix() Matrix {
	return s.stack[len(s.stack)-1].matrix
}

// currentScale returns (scaleX, scaleY) of the top frame. Internal
// helper for backends that need to project geometry alongside a rect.
// Under rotation the returned components are column-length magnitudes —
// approximate but adequate for stroke-width / corner-radius scaling.
func (s *canvasState) currentScale() (float32, float32) {
	top := s.stack[len(s.stack)-1]
	return top.scaleX(), top.scaleY()
}

// snapRectOutward grows a logical rect so both edges land on whole
// physical pixels under a (scaleX, scaleY) device transform. Used for the
// per-frame dirty region: a clip that splits a pixel makes AA fills and
// per-pixel blits disagree about who owns it.
func snapRectOutward(r Rect, scaleX, scaleY float32) Rect {
	if scaleX <= 0 || scaleY <= 0 {
		return r
	}
	x0 := float32(math.Floor(float64(r.X*scaleX))) / scaleX
	y0 := float32(math.Floor(float64(r.Y*scaleY))) / scaleY
	x1 := float32(math.Ceil(float64((r.X+r.W)*scaleX))) / scaleX
	y1 := float32(math.Ceil(float64((r.Y+r.H)*scaleY))) / scaleY
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// physicalRect converts a logical rect to the device pixels that cover it,
// rounding outward.
func physicalRect(r Rect, scaleX, scaleY float32) image.Rectangle {
	return image.Rect(
		int(math.Floor(float64(r.X*scaleX))), int(math.Floor(float64(r.Y*scaleY))),
		int(math.Ceil(float64((r.X+r.W)*scaleX))), int(math.Ceil(float64((r.Y+r.H)*scaleY))),
	)
}
