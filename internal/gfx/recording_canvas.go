package gfx

import (
	"image"
)

// RecordingCanvas captures draw calls for test verification. It
// implements Canvas without any rasterization, so tests can assert on
// call counts / args without booting a GL context. Shared across root
// and subpackage tests to keep the stub surface in one place as new
// Canvas methods are added.
//
// The embedded *CanvasState carries the state stack — tests that call
// Save / ClipRect / Restore exercise the same code paths the
// production ImageCanvas does. Zero-value RecordingCanvas{} is safe;
// the first state-stack op lazily allocates an unbounded clip.
type RecordingCanvas struct {
	*CanvasState

	Clears     []Color
	ClearRects []Rect // physical, like Fills
	Fills      []Rect
	Rounds     []RoundedRecord
	Strokes    []Rect
	Texts      []Rect
	Images     []Rect
	Vectors    []Rect
	Lines      []LineRecord
	Polylines  [][]Point
	Shadows    []ShadowRecord
	Layers     []LayerRecord
	Paths      []PathRecord
}

// LayerRecord captures a SaveLayer call — the requested bounds, the
// paint (BlendMode + Alpha for composite), and the save depth returned.
type LayerRecord struct {
	Bounds Rect
	Paint  Paint
	Depth  int
}

// ensureState lazily attaches an "infinite-bound" CanvasState so zero-
// value RecordingCanvas literals work without an explicit constructor.
// Tests that need a specific clip can set up RecordingCanvas{CanvasState:
// NewCanvasState(rect)} explicitly.
func (r *RecordingCanvas) ensureState() {
	if r.CanvasState == nil {
		r.CanvasState = NewCanvasState(Rect{X: -1e9, Y: -1e9, W: 2e9, H: 2e9})
	}
}

func (r *RecordingCanvas) Save() int {
	r.ensureState()
	return r.CanvasState.Save()
}

func (r *RecordingCanvas) Restore() {
	r.ensureState()
	r.CanvasState.Restore()
}

func (r *RecordingCanvas) RestoreTo(depth int) {
	r.ensureState()
	r.CanvasState.RestoreTo(depth)
}

func (r *RecordingCanvas) ClipRect(rect Rect) {
	r.ensureState()
	r.CanvasState.ClipRect(rect)
}

func (r *RecordingCanvas) Scale(sx, sy float32) {
	r.ensureState()
	r.CanvasState.Scale(sx, sy)
}

func (r *RecordingCanvas) Translate(dx, dy float32) {
	r.ensureState()
	r.CanvasState.Translate(dx, dy)
}

func (r *RecordingCanvas) Rotate(theta float32) {
	r.ensureState()
	r.CanvasState.Rotate(theta)
}

func (r *RecordingCanvas) Concat(m Matrix) {
	r.ensureState()
	r.CanvasState.Concat(m)
}

func (r *RecordingCanvas) CurrentMatrix() Matrix {
	r.ensureState()
	return r.CanvasState.CurrentMatrix()
}

func (r *RecordingCanvas) ClipBounds() Rect {
	r.ensureState()
	return r.CanvasState.ClipBounds()
}

func (r *RecordingCanvas) ClipBoundsLogical() Rect {
	r.ensureState()
	return r.CanvasState.ClipBoundsLogical()
}

// SaveLayer records the layer push and its paint, then does a normal
// clip-Save so subsequent Restore behavior matches a real Canvas.
// Tests that assert SaveLayer invocation read from r.Layers.
func (r *RecordingCanvas) SaveLayer(bounds Rect, paint Paint) int {
	r.ensureState()
	depth := r.CanvasState.Save()
	r.CanvasState.ClipRect(bounds)
	r.Layers = append(r.Layers, LayerRecord{Bounds: bounds, Paint: paint, Depth: depth})
	return depth
}

// ClipPath narrows the current clip to the path's bounding box for
// this recorder — good enough for tests that assert bounding-box
// intersections; pixel-exact clip fidelity requires a real Canvas.
func (r *RecordingCanvas) ClipPath(path *Path) {
	r.ensureState()
	if path == nil || path.IsEmpty() {
		return
	}
	r.CanvasState.ClipRect(path.Bounds())
}

// ShadowRecord captures a DrawShadow call.
type ShadowRecord struct {
	Rect
	Radius float32
	Spec   ElevationSpec
}

// RoundedRecord captures a FillRoundedRect call.
type RoundedRecord struct {
	Rect
	Radius float32
}

// LineRecord captures a DrawLine call.
type LineRecord struct {
	P1, P2 Point
	Width  float32
}

// projectRect mirrors the ImageCanvas's logical→physical pipeline:
// scale by the current frame and intersect with the current clip.
// Empty result means "no draw would happen" — RecordingCanvas drops the
// record, so tests asserting on Fills slice see the clipped state.
func (r *RecordingCanvas) projectRect(rect Rect) (Rect, bool) {
	r.ensureState()
	top := r.CanvasState.TopCopy()
	physR := top.Matrix.TransformRect(rect)
	eff := physR.Intersect(top.Clip)
	if eff.IsEmpty() {
		return Rect{}, false
	}
	return eff, true
}

// DrawShape dispatches each shape into the existing
// per-primitive record (Fills / Rounds / Strokes / Lines / Polylines)
// so tests written against the convenience API see the same recorded
// effect when widget code switches to DrawShape directly.
func (r *RecordingCanvas) DrawShape(shape Shape, paint Paint) {
	switch s := shape.(type) {
	case ShapeRect:
		if paint.Style == PaintStroke {
			r.StrokeRect(Rect(s), paint.Color, paint.StrokeWidth)
		} else {
			r.FillRect(Rect(s), paint.Color)
		}
	case ShapeRRect:
		if paint.Style == PaintStroke {
			r.StrokeRoundedRect(s.Rect, s.Radius, paint.Color, paint.StrokeWidth)
		} else {
			r.FillRoundedRect(s.Rect, s.Radius, paint.Color)
		}
	case ShapeLine:
		r.DrawLine(s.P1, s.P2, paint.Color, paint.StrokeWidth)
	case ShapePolyline:
		r.DrawPolyline([]Point(s), paint.Color, paint.StrokeWidth)
	case ShapePath:
		if s.Path == nil || s.Path.IsEmpty() {
			return
		}
		if eff, ok := r.projectRect(s.Path.Bounds()); ok {
			r.Paths = append(r.Paths, PathRecord{
				Rect:        eff,
				Stroke:      paint.Style == PaintStroke,
				StrokeWidth: paint.StrokeWidth,
			})
		}
	}
}

// PathRecord captures a DrawShape(ShapePath) call — the path's (logical→
// physical) bounds plus whether it was a stroke.
type PathRecord struct {
	Rect
	Stroke      bool
	StrokeWidth float32
}

func (r *RecordingCanvas) Clear(c Color) { r.Clears = append(r.Clears, c) }

func (r *RecordingCanvas) ClearRect(rect Rect) {
	if eff, ok := r.projectRect(rect); ok {
		r.ClearRects = append(r.ClearRects, eff)
	}
}

func (r *RecordingCanvas) FillRect(rect Rect, _ Color) {
	if eff, ok := r.projectRect(rect); ok {
		r.Fills = append(r.Fills, eff)
	}
}

func (r *RecordingCanvas) FillRoundedRect(rect Rect, radius float32, _ Color) {
	if eff, ok := r.projectRect(rect); ok {
		r.Rounds = append(r.Rounds, RoundedRecord{Rect: eff, Radius: radius})
	}
}

func (r *RecordingCanvas) StrokeRect(rect Rect, _ Color, _ float32) {
	if eff, ok := r.projectRect(rect); ok {
		r.Strokes = append(r.Strokes, eff)
	}
}

func (r *RecordingCanvas) StrokeRoundedRect(rect Rect, _ float32, _ Color, _ float32) {
	if eff, ok := r.projectRect(rect); ok {
		r.Strokes = append(r.Strokes, eff)
	}
}

func (r *RecordingCanvas) DrawText(_ string, rect Rect, _ Color, _ Font) {
	if _, ok := r.projectRect(rect); ok {
		r.Texts = append(r.Texts, rect)
	}
}

func (r *RecordingCanvas) DrawImage(_ image.Image, rect Rect) {
	if _, ok := r.projectRect(rect); ok {
		r.Images = append(r.Images, rect)
	}
}

func (r *RecordingCanvas) DrawVector(_ VectorSource, rect Rect, _ Color) {
	if _, ok := r.projectRect(rect); ok {
		r.Vectors = append(r.Vectors, rect)
	}
}

func (r *RecordingCanvas) DrawLine(p1, p2 Point, _ Color, width float32) {
	r.ensureState()
	top := r.CanvasState.TopCopy()
	minX, maxX := p1.X, p2.X
	if maxX < minX {
		minX, maxX = maxX, minX
	}
	minY, maxY := p1.Y, p2.Y
	if maxY < minY {
		minY, maxY = maxY, minY
	}
	bbox := top.Matrix.TransformRect(Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY})
	// A perfectly vertical or horizontal line has a zero-width or zero-height
	// bounding box, which Rect.Intersect treats as empty. Grow the bbox by the
	// stroke footprint so axis lines and crosshairs are recorded like any
	// other visible draw call.
	halfStroke := width * .5
	if halfStroke < .5 {
		halfStroke = .5
	}
	bbox.X -= halfStroke
	bbox.Y -= halfStroke
	bbox.W += halfStroke * 2
	bbox.H += halfStroke * 2
	if !bbox.Intersects(top.Clip) {
		return
	}
	r.Lines = append(r.Lines, LineRecord{P1: p1, P2: p2, Width: width})
}

func (r *RecordingCanvas) DrawPolyline(points []Point, _ Color, _ float32) {
	cp := make([]Point, len(points))
	copy(cp, points)
	r.Polylines = append(r.Polylines, cp)
}

func (r *RecordingCanvas) DrawShadow(rect Rect, radius float32, spec ElevationSpec, _ Color) {
	if _, ok := r.projectRect(rect); ok {
		r.Shadows = append(r.Shadows, ShadowRecord{Rect: rect, Radius: radius, Spec: spec})
	}
}
