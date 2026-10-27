package qui

import (
	"time"
)

// Helpers the widgets subpackage needs to exercise Window-level behavior
// in tests (overlay pushing, hover-path driven tooltips, drag/drop, etc.)
// without spinning up GLFW. Kept in a dedicated file so production code
// stays free of test-only surface.

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
	w.ingestScroll(x, y, dx, dy, mods, phase, phase != GesturePhaseNone)
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
