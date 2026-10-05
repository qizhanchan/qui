package qui

import "time"

// Window's implementation of platformHandler (see platform.go) — the
// engine's side of the platform seam.
//
// Every method here translates a platform fact into an engine event and
// nothing more: no platform quirks, no backend state. That asymmetry is
// the contract. A backend that has to work around its own limitations
// (GLFW's scroll callback dropping modifiers, say) absorbs that below the
// seam so this file stays the same for every platform.

var _ platformHandler = (*Window)(nil)

// Coordinates arriving from a backend are window-logical points; widgets
// live in the content viewport. toViewport is the single conversion, so
// nothing past this file — dispatch, hit-testing, actions, the agent —
// needs to know viewport zoom exists. It is the identity at 100%.
func (w *Window) onMouseMove(x, y float64, mods Modifiers) {
	w.lastMods = mods
	vx, vy := w.toViewport(x, y)
	w.dispatch(MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventMouseMove,
		When:      time.Now(),
		X:         vx,
		Y:         vy,
		Mods:      mods,
	})
}

func (w *Window) onMouseButton(btn MouseButton, down bool, mods Modifiers) {
	x, y := w.cursorPos()
	eventType := EventMouseDown
	if !down {
		eventType = EventMouseUp
	}
	w.lastMods = mods
	w.dispatch(MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: eventType,
		When:      time.Now(),
		X:         x,
		Y:         y,
		Button:    btn,
		Mods:      mods,
	})
}

// onScroll divides the DELTAS by zoom as well as the position: a wheel
// notch should move the content the same visible distance whatever the
// zoom, which means fewer viewport units when zoomed in. Scaling only the
// position would make scrolling accelerate as the user zooms.
func (w *Window) onScroll(x, y, dx, dy float64, mods Modifiers, phase GesturePhase) {
	vx, vy := w.toViewport(x, y)
	ddx, ddy := w.toViewport(dx, dy)
	w.ingestScroll(vx, vy, ddx, ddy, mods, phase)
}

func (w *Window) onGesture(kind EventType, phase GesturePhase, dScale, dRotation float32, mods Modifiers) {
	w.ingestGesture(kind, phase, dScale, dRotation, mods)
}

func (w *Window) onKey(k Key, scancode int, down, _ bool, mods Modifiers) {
	w.lastMods = mods
	eventType := EventKeyDown
	if !down {
		eventType = EventKeyUp
	}
	w.dispatch(KeyEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: eventType,
		When:      time.Now(),
		Key:       k,
		ScanCode:  scancode,
		Mods:      mods,
	})
}

func (w *Window) onChar(r rune) {
	w.dispatch(CharEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		When:      time.Now(),
		Rune:      r,
	})
}

func (w *Window) onFocus(focused bool) {
	w.noteActivation(focused)
	w.dispatch(FocusEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		When:      time.Now(),
		Focus:     focused,
	})
}

// onResize / onFramebufferResize / onRefresh all Step synchronously.
//
// That is not laziness: on macOS a window resize runs inside a nested
// event-tracking loop that owns the thread until the drag ends, so a
// frame scheduled "for later" would not paint until the user let go. Any
// backend must therefore be able to re-enter Step from inside an event
// callback — worth knowing before writing the next one.
func (w *Window) onResize(width, height int) {
	w.resizeTo(w.noteWindowSize(width, height))
	w.Step()
}

func (w *Window) onFramebufferResize(_, _ int) {
	w.Invalidate()
	w.Step()
}

func (w *Window) onRefresh() {
	w.Invalidate()
	w.Step()
}

func (w *Window) onFileDrop(paths []string, x, y float64) {
	vx, vy := w.toViewport(x, y)
	if w.routeFileDrop(paths, vx, vy) {
		w.Invalidate()
		w.InvalidateLayout()
		return
	}
	fn := w.onDrop
	if fn == nil {
		return
	}
	fn(paths, vx, vy)
	// File drops usually trigger substantial state changes — a new model
	// loads, a texture swaps — so force a full repaint to reflect
	// whatever the callback mutated.
	w.Invalidate()
	w.InvalidateLayout()
}
