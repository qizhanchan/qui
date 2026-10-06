package qui

import (
	"image"
	"image/color"
	"time"
)

// Helpers the widgets subpackage needs to exercise Window-level behavior
// in tests (overlay pushing, hover-path driven tooltips, drag/drop, etc.)
// without spinning up GLFW. Kept in a dedicated file so production code
// stays free of test-only surface.

// RecordingCanvas captures draw calls for test verification. It
// implements Canvas without any rasterization, so tests can assert on
// call counts / args without booting a GL context. Shared across root
// and subpackage tests to keep the stub surface in one place as new
// Canvas methods are added.
//
// The embedded *canvasState carries the state stack — tests that call
// Save / ClipRect / Restore exercise the same code paths the
// production imageCanvas does. Zero-value RecordingCanvas{} is safe;
// the first state-stack op lazily allocates an unbounded clip.
type RecordingCanvas struct {
	*canvasState

	Clears    []Color
	Fills     []Rect
	Rounds    []RoundedRecord
	Strokes   []Rect
	Texts     []Rect
	Images    []Rect
	Vectors   []Rect
	Lines     []LineRecord
	Polylines [][]Point
	Shadows   []ShadowRecord
	Layers    []LayerRecord
	Paths     []PathRecord
}

// LayerRecord captures a SaveLayer call — the requested bounds, the
// paint (BlendMode + Alpha for composite), and the save depth returned.
type LayerRecord struct {
	Bounds Rect
	Paint  Paint
	Depth  int
}

// ensureState lazily attaches an "infinite-bound" canvasState so zero-
// value RecordingCanvas literals work without an explicit constructor.
// Tests that need a specific clip can set up RecordingCanvas{canvasState:
// newCanvasState(rect)} explicitly.
func (r *RecordingCanvas) ensureState() {
	if r.canvasState == nil {
		r.canvasState = newCanvasState(Rect{X: -1e9, Y: -1e9, W: 2e9, H: 2e9})
	}
}

func (r *RecordingCanvas) Save() int {
	r.ensureState()
	return r.canvasState.Save()
}
func (r *RecordingCanvas) Restore() {
	r.ensureState()
	r.canvasState.Restore()
}
func (r *RecordingCanvas) RestoreTo(depth int) {
	r.ensureState()
	r.canvasState.RestoreTo(depth)
}
func (r *RecordingCanvas) ClipRect(rect Rect) {
	r.ensureState()
	r.canvasState.ClipRect(rect)
}
func (r *RecordingCanvas) Scale(sx, sy float32) {
	r.ensureState()
	r.canvasState.Scale(sx, sy)
}
func (r *RecordingCanvas) Translate(dx, dy float32) {
	r.ensureState()
	r.canvasState.Translate(dx, dy)
}
func (r *RecordingCanvas) Rotate(theta float32) {
	r.ensureState()
	r.canvasState.Rotate(theta)
}
func (r *RecordingCanvas) Concat(m Matrix) {
	r.ensureState()
	r.canvasState.Concat(m)
}
func (r *RecordingCanvas) CurrentMatrix() Matrix {
	r.ensureState()
	return r.canvasState.CurrentMatrix()
}
func (r *RecordingCanvas) ClipBounds() Rect {
	r.ensureState()
	return r.canvasState.ClipBounds()
}
func (r *RecordingCanvas) ClipBoundsLogical() Rect {
	r.ensureState()
	return r.canvasState.ClipBoundsLogical()
}

// SaveLayer records the layer push and its paint, then does a normal
// clip-Save so subsequent Restore behavior matches a real Canvas.
// Tests that assert SaveLayer invocation read from r.Layers.
func (r *RecordingCanvas) SaveLayer(bounds Rect, paint Paint) int {
	r.ensureState()
	depth := r.canvasState.Save()
	r.canvasState.ClipRect(bounds)
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
	r.canvasState.ClipRect(path.Bounds())
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

// projectRect mirrors the imageCanvas's logical→physical pipeline:
// scale by the current frame and intersect with the current clip.
// Empty result means "no draw would happen" — RecordingCanvas drops the
// record, so tests asserting on Fills slice see the clipped state.
func (r *RecordingCanvas) projectRect(rect Rect) (Rect, bool) {
	r.ensureState()
	top := r.canvasState.topCopy()
	physR := top.matrix.TransformRect(rect)
	eff := physR.Intersect(top.clip)
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
	top := r.canvasState.topCopy()
	minX, maxX := p1.X, p2.X
	if maxX < minX {
		minX, maxX = maxX, minX
	}
	minY, maxY := p1.Y, p2.Y
	if maxY < minY {
		minY, maxY = maxY, minY
	}
	bbox := top.matrix.TransformRect(Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY})
	if !bbox.Intersects(top.clip) {
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

// NewImageCanvas wraps an *image.RGBA into a Canvas suitable for headless
// rendering — same rasterizer as the production CPU path, but with no
// Window / GLFW / OpenGL dependency. Use this in tests and golden-image
// snapshot harnesses. The returned canvas comes pre-initialized with a
// state stack whose bottom frame's clip equals the image's full bounds,
// so test code can immediately FillRect / DrawText / Save / ClipRect
// without manual setup.
func NewImageCanvas(img *image.RGBA) Canvas {
	b := img.Bounds()
	backend := newCPUBackend(img)
	return imageCanvas{
		canvasState: newCanvasState(Rect{
			X: float32(b.Min.X), Y: float32(b.Min.Y),
			W: float32(b.Dx()), H: float32(b.Dy()),
		}),
		backend: backend,
		layers:  newFrontendLayerTracker(),
	}
}

// newImageCanvasForTest is the unexported sibling so internal tests can
// build a state-initialized imageCanvas without going through the
// Canvas interface boxing.
func newImageCanvasForTest(img *image.RGBA) imageCanvas {
	b := img.Bounds()
	backend := newCPUBackend(img)
	return imageCanvas{
		canvasState: newCanvasState(Rect{
			X: float32(b.Min.X), Y: float32(b.Min.Y),
			W: float32(b.Dx()), H: float32(b.Dy()),
		}),
		backend: backend,
		layers:  newFrontendLayerTracker(),
	}
}

// ColorToRGBA converts a qui.Color into the standard color.RGBA used by
// Go's image package — handy when callers need to fill an arbitrary
// *image.RGBA without going through the Canvas surface (e.g. to paint
// a background before handing the canvas to a widget for snapshotting).
func ColorToRGBA(c Color) color.RGBA { return toRGBA(c) }

// NewTestWindow returns a Window pre-sized but with no native handle or
// renderer — suitable for unit tests that exercise dispatch, overlay
// lifecycle, focus collection, etc. Do not call Step or Run on it.
func NewTestWindow(size Size) *Window {
	// windowSize mirrors lastSize so WindowSize / viewport derivation are
	// meaningful without a platform window: at zoom=1 they are the same
	// number, and SetZoom can then derive a viewport from something real.
	return &Window{lastSize: size, windowSize: size, zoom: 1, focusVisible: true}
}

// TickRequestForTest returns the earliest pending RequestTickAt deadline
// (zero if none), so tests in other packages can assert that a Tickable
// asks an idle window to wake it.
func (w *Window) TickRequestForTest() time.Time {
	if w == nil {
		return time.Time{}
	}
	return w.tickAt
}

// DispatchTestEvent routes an event through Window's normal dispatch
// pipeline (capture/target/bubble, mouse capture, focus updates).
// Intended for unit tests in other packages that need end-to-end
// event behavior without a GLFW loop.
func (w *Window) DispatchTestEvent(event Event) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.DispatchTestEvent")
	w.dispatch(event)
}

// Overlays returns a shallow copy of the overlay stack (bottom-first).
// Use cases: tests that need to verify Popup.ShowAt / Dialog.Show pushed
// the expected overlay, or that Close removed it. The copy prevents
// external mutation of the underlying slice.
func (w *Window) Overlays() []Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.Overlays")
	out := make([]Widget, len(w.overlays))
	copy(out, w.overlays)
	return out
}

// HitTestForTest resolves the widget the window would target for a mouse
// event at p — overlays first, then the root tree, exactly as dispatch
// does. Intended for tests in other packages that need to assert WHERE an
// event lands (e.g. that a widget's HitTest returns its subclass pointer
// and not the embedded BaseWidget) without a GLFW loop.
func (w *Window) HitTestForTest(p Point) Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.HitTestForTest")
	return w.hitTestAll(p)
}

// ResizeForTest drives a logical window-size change through the same path
// Step uses on a live resize: it records the size (resizeTo) and fires the
// post-layout OverlayResizer notification. Intended for tests that assert
// resize behavior without a GLFW loop. (A real frame runs the layout pass
// between the two; tests supply their own anchor geometry instead.)
func (w *Window) ResizeForTest(size Size) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ResizeForTest")
	w.resizeTo(size)
	if w.overlayResizePending {
		w.overlayResizePending = false
		w.notifyOverlaysResize(w.lastSize)
	}
}

// DirtyRegion returns the pending repaint region. Intended for tests that
// assert invalidation behavior without driving a native Step.
func (w *Window) DirtyRegion() Rect {
	if w == nil {
		return Rect{}
	}
	w.assertUIThread("Window.DirtyRegion")
	return w.dirtyRegion
}

// ClearDirtyRegion clears pending repaint state for invalidation tests.
func (w *Window) ClearDirtyRegion() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ClearDirtyRegion")
	w.dirtyRegion = Rect{}
}

// SetHoverPath overrides the hover chain used by tooltip routing.
// Production code calls syncHoverPath from dispatch; tests use this to
// synthesize a hover without a full event pump.
func (w *Window) SetHoverPath(path []Widget) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetHoverPath")
	w.hoverPath = path
}

// NewMouseEvent builds a synthetic MouseEvent — primarily for widget
// tests that exercise Handle without going through the full dispatch
// pipeline. Production code builds events inline inside window.go.
func NewMouseEvent(kind EventType, x, y float32, button MouseButton, mods Modifiers) MouseEvent {
	return MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: kind,
		X:         x, Y: y,
		Button: button,
		Mods:   mods,
	}
}

// NewScrollEvent builds a synthetic scroll-wheel MouseEvent.
func NewScrollEvent(x, y, deltaX, deltaY float32, mods Modifiers) MouseEvent {
	return MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventScroll,
		X:         x, Y: y,
		DeltaX: deltaX, DeltaY: deltaY,
		Mods: mods,
	}
}

// NewGestureEvent builds a synthetic GestureEvent for widget tests.
// kind is EventGesturePinch / EventGestureRotate / EventGestureSmartMagnify.
//
// The cumulative fields are derived from the increments (Scale = 1+dScale,
// Rotation = dRotation) — right for the single-step gestures most widget
// tests want. To exercise multi-step accumulation and gesture capture,
// drive Window.IngestGestureForTest instead, which runs the real path.
func NewGestureEvent(kind EventType, x, y float32, phase GesturePhase,
	dScale, dRotation float32, mods Modifiers) GestureEvent {
	return GestureEvent{
		baseEvent:    baseEvent{shared: &eventState{}},
		eventType:    kind,
		When:         time.Now(),
		X:            x,
		Y:            y,
		GesturePhase: phase,
		Scale:        1 + dScale,
		DScale:       dScale,
		Rotation:     dRotation,
		DRotation:    dRotation,
		Mods:         mods,
	}
}

// IngestGestureForTest drives the full platform-ingestion path for a
// gesture: accumulation into cumulative Scale/Rotation, gesture capture,
// and three-phase dispatch. This is the entry point a platform bridge
// calls, so tests using it cover what a real trackpad exercises.
//
// The anchor is the window's cursor position, which is (0,0) for a
// handle-less test window — pass coordinates via NewGestureEvent +
// DispatchTestEvent when the anchor matters.
func (w *Window) IngestGestureForTest(kind EventType, phase GesturePhase,
	dScale, dRotation float32, mods Modifiers) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.IngestGestureForTest")
	w.ingestGesture(kind, phase, dScale, dRotation, mods)
}

// IngestScrollForTest drives the real scroll-ingestion path, including
// the Ctrl/Cmd+wheel-to-pinch synthesis and its fall-through to a plain
// scroll when no widget consumes the pinch. mods and phase stand in for
// what a platform bridge would have snapshotted from the native event.
func (w *Window) IngestScrollForTest(x, y, dx, dy float32, mods Modifiers, phase GesturePhase) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.IngestScrollForTest")
	w.ingestScroll(x, y, dx, dy, mods, phase)
}

// NewDragEvent builds a synthetic DragEvent (EventDragStart/Move/End/Drop)
// for drag-and-drop widget tests.
func NewDragEvent(kind EventType, x, y float32, source Widget) DragEvent {
	return DragEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: kind,
		X:         x, Y: y,
		Source: source,
		Data:   &DragData{},
	}
}

// NewKeyEvent builds a synthetic KeyEvent for widget tests.
func NewKeyEvent(kind EventType, key Key, mods Modifiers) KeyEvent {
	return KeyEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: kind,
		Key:       key,
		Mods:      mods,
	}
}

// NewCharEvent builds a synthetic CharEvent for widget tests.
func NewCharEvent(r rune, mods Modifiers) CharEvent {
	return CharEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		Rune:      r,
		Mods:      mods,
	}
}
