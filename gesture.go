package qui

import "time"

// Multi-touch gesture ingestion.
//
// This file is the platform-neutral half: it owns accumulation (turning
// per-event increments into a cumulative Scale/Rotation), gesture
// capture, and the Ctrl/Cmd+wheel fallback that gives every platform a
// pinch even without a trackpad. Backends only translate native events
// into platformHandler.onGesture / onScroll calls — they hold no gesture
// state and make no policy decisions.
//
// Keeping the split here is deliberate: this is the input contract every
// platform backend implements (see platform.go), not a GLFW patch.
// Anything a platform can't report degrades to a documented default
// (GesturePhaseNone, synthesized pinch) instead of leaking upward.

// wheelPinchStep converts one unit of scroll delta into a magnification
// fraction for the Ctrl/Cmd+wheel fallback. Tuned so a single notch on
// a discrete wheel is a noticeable-but-not-jarring step, matching the
// ~5% per notch browsers use for Ctrl+wheel zoom.
const wheelPinchStep = 0.05

// maxSyntheticPinchStep clamps one synthesized step. High-resolution
// trackpad scroll can deliver large accumulated deltas in a single
// callback; without a clamp a fast Ctrl+swipe would slam zoom to the
// limit in one event.
const maxSyntheticPinchStep = 0.25

// gestureScaleFloor / gestureScaleCeil bound the cumulative accumulator
// so a long gesture can't drift into denormals or infinity. Receivers
// clamp to their own useful range; this only protects the accumulator.
const (
	gestureScaleFloor = 0.01
	gestureScaleCeil  = 100
)

// ingestScroll is the single entry point for scroll input, called from
// the platform's scroll callback with logical window coordinates.
//
// Ctrl/Cmd+wheel is first offered to the tree as a synthesized pinch
// (the browser convention, and the only way a mouse user or a
// non-darwin platform gets zoom at all). If nobody consumes the pinch,
// the plain scroll is dispatched unchanged — so existing Ctrl+scroll
// behaviors keep working and nothing is silently swallowed.
func (w *Window) ingestScroll(x, y, dx, dy float32, mods Modifiers, phase GesturePhase) {
	if dy != 0 && isPinchModifier(mods) {
		if w.synthesizePinch(x, y, dy, mods) {
			return
		}
	}
	w.dispatch(MouseEvent{
		baseEvent:   baseEvent{shared: &eventState{}},
		eventType:   EventScroll,
		When:        time.Now(),
		X:           x,
		Y:           y,
		DeltaX:      dx,
		DeltaY:      dy,
		Mods:        mods,
		ScrollPhase: phase,
	})
}

// isPinchModifier reports whether this modifier set means "the wheel is
// a zoom, not a scroll". Ctrl matches the cross-platform browser
// convention (and is what Windows precision touchpads send for a real
// pinch); the command modifier is accepted too so macOS mouse users get
// the Cmd+wheel they expect. IsCommandMod collapses to Ctrl off darwin,
// so this is one expression on every platform.
func isPinchModifier(mods Modifiers) bool {
	return mods&ModControl != 0 || IsCommandMod(mods)
}

// synthesizePinch manufactures a one-shot pinch from a wheel notch and
// reports whether a widget consumed it.
//
// Discrete wheels have no gesture lifecycle, so this is a self-contained
// GesturePhaseChanged: DScale carries the step and Scale is just
// 1+DScale. Receivers keying off DScale (as documented) work with both
// this and a real trackpad pinch; receivers keying off cumulative Scale
// would misread it, which is why the docs push DScale.
func (w *Window) synthesizePinch(x, y, dy float32, mods Modifiers) bool {
	dScale := clampFloat32(dy*wheelPinchStep, -maxSyntheticPinchStep, maxSyntheticPinchStep)
	if dScale == 0 {
		return false
	}
	return w.offerGesture(GestureEvent{
		eventType:    EventGesturePinch,
		When:         time.Now(),
		X:            x,
		Y:            y,
		GesturePhase: GesturePhaseChanged,
		Scale:        1 + dScale,
		DScale:       dScale,
		Mods:         mods,
		Synthetic:    true,
	})
}

// ingestGesture is the platform bridge's entry point for a real
// multi-touch gesture. dScale is a magnification fraction (NSEvent's
// magnification), dRotation is degrees.
//
// The anchor comes from the cursor position rather than the event:
// indirect touch devices report finger positions normalized to the
// trackpad, not to the window, so there is no meaningful on-screen
// gesture centroid. Browsers anchor trackpad zoom at the cursor for the
// same reason.
func (w *Window) ingestGesture(kind EventType, phase GesturePhase, dScale, dRotation float32, mods Modifiers) {
	if w == nil {
		return
	}
	x, y := w.cursorPos()
	if phase == GesturePhaseBegan || w.gestureScale <= 0 {
		w.gestureScale = 1
		w.gestureRotation = 0
	}
	w.gestureScale = clampFloat32(w.gestureScale*(1+dScale), gestureScaleFloor, gestureScaleCeil)
	w.gestureRotation += dRotation
	w.offerGesture(GestureEvent{
		eventType:    kind,
		When:         time.Now(),
		X:            x,
		Y:            y,
		GesturePhase: phase,
		Scale:        w.gestureScale,
		DScale:       dScale,
		Rotation:     w.gestureRotation,
		DRotation:    dRotation,
		Mods:         mods,
	})
}

// offerGesture gives a gesture to the widget tree first and, if nothing
// consumed it, to the window's own zoom handling.
//
// This ordering is the browser's: a pinch over an element that wants it
// (a map, a spreadsheet grid, a chart axis) belongs to that element, and
// only an unclaimed pinch means "zoom the page". It is also what keeps
// window zoom safe to enable in an app that already handles pinch
// somewhere — the two can't both fire.
//
// Window zoom only acts when explicitly enabled; see
// SetViewportZoomEnabled.
func (w *Window) offerGesture(ev GestureEvent) bool {
	if w == nil {
		return false
	}
	if w.dispatchGesture(ev) {
		return true
	}
	return w.applyGestureZoom(ev)
}

// dispatchGesture routes a gesture through the normal three-phase
// pipeline and reports whether a widget consumed it (StopPropagation or
// a true return from Handle). The bool is what lets the Ctrl+wheel
// fallback decide between "zoom happened" and "fall through to scroll".
func (w *Window) dispatchGesture(ev GestureEvent) bool {
	if w == nil {
		return false
	}
	state := &eventState{}
	ev.baseEvent = baseEvent{shared: state}
	w.dispatch(ev)
	return state.stopped
}

// updateGestureCapture implements gesture capture, the multi-touch
// analogue of mouse capture: Began locks the receiver and every later
// event in the gesture is redirected to it, so a pinch that starts on a
// document view keeps driving that view even when the fingers wander
// over a toolbar. Returns the widget the event should target.
//
// Capture is claimed regardless of whether the Began was consumed —
// same as mouseCaptured on MouseDown. An uninterested target simply
// lets the follow-ups bubble as they would have anyway.
func (w *Window) updateGestureCapture(ev GestureEvent, target Widget) Widget {
	switch ev.GesturePhase {
	case GesturePhaseBegan:
		w.gestureTarget = target
	case GesturePhaseChanged:
		if w.gestureTarget != nil {
			return w.gestureTarget
		}
	case GesturePhaseEnded, GesturePhaseCancelled:
		if w.gestureTarget != nil {
			target = w.gestureTarget
		}
		w.gestureTarget = nil
	}
	return target
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
