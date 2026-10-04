package qui

import "math"

// Viewport zoom — the framework's answer to "make the whole page bigger".
//
// # Semantics: browser page zoom, not a magnifier
//
// Zooming shrinks the CONTENT VIEWPORT and scales the canvas by the same
// factor: at 200% a 1000pt-wide window lays out as a 500pt viewport drawn
// at 2×. So the tree REFLOWS — text re-wraps, flex redistributes,
// percentages resolve against the smaller viewport — exactly like Cmd+/
// Cmd− in a browser, and unlike a magnifier that scales finished pixels
// and requires panning.
//
// Reflow is the whole point. A magnifier makes long lines run off the
// right edge; page zoom keeps everything reachable, which is what a user
// asking for "bigger" actually wants.
//
// # Why this costs almost nothing in the engine
//
// The frame loop already scales the canvas once per frame by the device
// pixel ratio, derived as framebuffer/logical. Feeding it the viewport
// size instead of the window size makes that ratio dpr×zoom with no new
// multiply anywhere, and everything downstream — text rasterizing at the
// right size, GL scissor rects via GLState.LogicalSize, dirty-region
// clipping — follows for free.
//
// The one real decision is what Size() means. It now returns the
// VIEWPORT, because every widget-side caller (dialog centering, popup
// measurement, menu clamping, the CSS vw unit) wants "the coordinate
// space I live in". Callers that genuinely mean the OS window — placing
// it on a monitor, saving windowed geometry — use WindowSize. At zoom=1
// the two are identical, which is why this doesn't disturb existing code.
//
// # Input
//
// Platform events arrive in window-logical coordinates and are divided by
// zoom on the way in (window_handler.go). Nothing downstream —
// dispatch, hit-testing, actions.go, the agent — knows zoom exists.

// zoomMin / zoomMax bound the factor. The range matches what browsers
// offer; beyond it layout stops being useful rather than merely ugly.
const (
	zoomMin = 0.25
	zoomMax = 5.0
)

// zoomQuantum snaps zoom to 2.5% steps.
//
// This is not cosmetic. Text measurement is memoized with the font size in
// the cache key (see text.go), so every distinct zoom value re-measures
// the entire tree. A continuous trackpad pinch produces a new factor every
// frame, which would miss the cache on every frame of the gesture and
// evict the entries the next frame needs. Quantizing bounds the whole
// zoom range to ~190 distinct values, so a pinch settles into cache hits
// almost immediately.
const zoomQuantum = 0.025

// zoomSteps is 1/zoomQuantum, used for the integer-step arithmetic in
// quantizeZoom.
const zoomSteps = 40.0

// zoomLadder is the sequence ZoomIn / ZoomOut step through — essentially
// the stops Chrome and Firefox use. A fixed ladder rather than a
// multiplier so repeated presses land on round, recognizable percentages
// and round-trip exactly (in then out returns where you started).
//
// INVARIANT: every stop must survive quantizeZoom unchanged, which is why
// this reads 0.325 / 0.675 rather than a browser's 0.33 / 0.67. A stop
// that quantized to something else would make stepping onto it a no-op —
// SetZoom would snap back to the value we were trying to leave and the
// ladder would dead-end. TestZoomLadderSurvivesQuantization pins this.
var zoomLadder = []float32{
	0.25, 0.325, 0.5, 0.675, 0.75, 0.8, 0.9,
	1.0,
	1.1, 1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 4.0, 5.0,
}

// zoomStepEpsilon is the tolerance for "is this stop past the current
// value". It has to exceed float32 rounding noise from quantization while
// staying well below the smallest gap between ladder stops (0.05).
const zoomStepEpsilon = 1e-3

// Zoom returns the current viewport zoom factor (1 == 100%).
func (w *Window) Zoom() float32 {
	if w == nil {
		return 1
	}
	w.assertUIThread("Window.Zoom")
	return w.effectiveZoom()
}

// effectiveZoom is Zoom with the zero value treated as 100%, so a Window
// that was never explicitly initialized behaves as unzoomed.
func (w *Window) effectiveZoom() float32 {
	if w == nil || w.zoom <= 0 {
		return 1
	}
	return w.zoom
}

// SetZoom sets the viewport zoom factor, clamped to [0.25, 5] and snapped
// to 2.5% steps. 1 restores 100%.
//
// The tree re-lays-out and repaints on the next frame. Overlays are
// re-notified of the new viewport size, so an open dropdown re-anchors
// instead of floating away from its trigger.
func (w *Window) SetZoom(zoom float32) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetZoom")
	zoom = quantizeZoom(zoom)
	if zoom == w.effectiveZoom() {
		return
	}
	// Remember where we came from so smart-magnify can toggle back to it.
	// Only non-100% values are worth restoring: toggling 100% ↔ 100% is a
	// no-op that would feel like a dead gesture.
	if prev := w.effectiveZoom(); prev != 1 {
		w.zoomRestore = prev
	}
	w.zoom = zoom
	// Re-derive the viewport. resizeTo invalidates layout + paint and
	// schedules the overlay notification for after the layout pass.
	w.resizeTo(w.viewportSize())
	// A zoom change that happens to leave the viewport the same size
	// (rounding on a tiny window) still changes every rasterized glyph,
	// so repaint unconditionally.
	w.Invalidate()
	w.InvalidateLayout()
}

// ResetZoom returns to 100%.
func (w *Window) ResetZoom() { w.SetZoom(1) }

// ZoomIn / ZoomOut step to the next / previous stop on the zoom ladder.
func (w *Window) ZoomIn()  { w.stepZoom(+1) }
func (w *Window) ZoomOut() { w.stepZoom(-1) }

// stepZoom moves to the next ladder stop strictly past the current value.
//
// Expressed as "first stop beyond where we are" rather than "neighbour of
// the nearest stop" so it behaves correctly from an off-ladder value — a
// pinch leaves zoom at things like 1.325, and a nearest-stop implementation
// can then step onto the value it started from and stall.
func (w *Window) stepZoom(direction int) {
	if w == nil {
		return
	}
	current := w.effectiveZoom()
	if direction > 0 {
		for _, stop := range zoomLadder {
			if stop > current+zoomStepEpsilon {
				w.SetZoom(stop)
				return
			}
		}
		w.SetZoom(zoomLadder[len(zoomLadder)-1])
		return
	}
	for i := len(zoomLadder) - 1; i >= 0; i-- {
		if zoomLadder[i] < current-zoomStepEpsilon {
			w.SetZoom(zoomLadder[i])
			return
		}
	}
	w.SetZoom(zoomLadder[0])
}

// quantizeZoom clamps to the supported range and snaps to zoomQuantum.
//
// The arithmetic runs in float64 over zoomSteps (= 1/zoomQuantum) rather
// than multiplying float32s, so a value that is already a valid step maps
// to itself exactly. Without that, quantization is not idempotent and
// stepping onto a ladder stop can land a hair off it — which reads as the
// zoom controls freezing.
func quantizeZoom(zoom float32) float32 {
	if zoom <= 0 {
		return 1
	}
	zoom = clampFloat32(zoom, zoomMin, zoomMax)
	steps := math.Round(float64(zoom) * zoomSteps)
	snapped := float32(steps / zoomSteps)
	// Snapping can push a boundary value a hair outside the range.
	return clampFloat32(snapped, zoomMin, zoomMax)
}

// SmartZoom is the two-finger-double-tap behavior: toggle between 100%
// and the last zoom level the user had chosen. From 100% with no history
// it steps in one stop, so the gesture always does something visible.
func (w *Window) SmartZoom() {
	if w == nil {
		return
	}
	if w.effectiveZoom() != 1 {
		w.ResetZoom()
		return
	}
	if w.zoomRestore > 0 && quantizeZoom(w.zoomRestore) != 1 {
		w.SetZoom(w.zoomRestore)
		return
	}
	w.ZoomIn()
}

// WindowSize returns the OS window's logical (point) size, independent of
// zoom.
//
// Use this only for things that talk to the window manager — placing the
// window on a monitor, persisting windowed geometry. Anything laying out
// or hit-testing content wants Size / ViewportSize instead, which is the
// coordinate space widgets actually live in.
func (w *Window) WindowSize() Size {
	if w == nil {
		return Size{}
	}
	w.assertUIThread("Window.WindowSize")
	if w.windowSize.W <= 0 || w.windowSize.H <= 0 {
		// No platform window has reported a size yet (test windows never
		// will). The viewport is the best available answer and is exact at
		// zoom=1.
		return w.lastSize
	}
	return w.windowSize
}

// ViewportSize returns the logical size of the content area — the space
// the widget tree is laid out in. Identical to Size; named explicitly for
// call sites where the distinction from WindowSize is the point.
func (w *Window) ViewportSize() Size {
	if w == nil {
		return Size{}
	}
	w.assertUIThread("Window.ViewportSize")
	return w.lastSize
}

// viewportSize derives the content viewport from the current window size
// and zoom. Used when either changes.
func (w *Window) viewportSize() Size {
	base := w.windowSize
	if base.W <= 0 || base.H <= 0 {
		base = w.lastSize
	}
	zoom := w.effectiveZoom()
	return Size{W: base.W / zoom, H: base.H / zoom}
}

// noteWindowSize records a new OS window size and returns the resulting
// content viewport, keeping the two in sync from the single place each
// caller has the platform's number.
func (w *Window) noteWindowSize(width, height int) Size {
	w.windowSize = Size{W: float32(width), H: float32(height)}
	return w.viewportSize()
}

// EffectiveScale is the total logical→physical factor: device pixel ratio
// times zoom.
//
// This is the number that converts a widget coordinate to a pixel in a
// snapshot or a GL scissor box. DevicePixelRatio deliberately does NOT
// include zoom — it stays the display's true ratio, which is what code
// persisting or comparing display properties needs.
func (w *Window) EffectiveScale() float32 {
	if w == nil {
		return 1
	}
	w.assertUIThread("Window.EffectiveScale")
	return w.DevicePixelRatio() * w.effectiveZoom()
}

// SetViewportZoomEnabled turns on window-level zoom handling: Cmd/Ctrl+=,
// Cmd/Ctrl+−, Cmd/Ctrl+0, and a trackpad pinch or two-finger double-tap
// that no widget consumed.
//
// Opt-in rather than automatic. Zoom is a whole-application behavior with
// a visible effect on every existing app, and plenty of apps have their
// own idea of what pinch means — a chart zooms an axis, for example.
// Turning it on for everyone would hand those
// apps a second, competing zoom. Apps that want the browser default say
// so in one line; apps that already handle pinch consume the event and
// this never fires anyway.
func (w *Window) SetViewportZoomEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetViewportZoomEnabled")
	w.zoomEnabled = enabled
}

// ViewportZoomEnabled reports whether window-level zoom handling is on.
func (w *Window) ViewportZoomEnabled() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.ViewportZoomEnabled")
	return w.zoomEnabled
}

// handleZoomShortcut applies the standard zoom keys.
//
// Handled here rather than through AcceleratorRegistry for the same reason
// the agent and debug-overlay shortcuts are: it must work whether or not
// the app installed a registry, and it must not be silently replaced when
// an app swaps its registry out. Runs only after widget dispatch declined
// the key, so a focused widget's own Cmd+0 still wins.
func (w *Window) handleZoomShortcut(evt KeyEvent) bool {
	if w == nil || !w.zoomEnabled || !IsCommandMod(evt.Mods) {
		return false
	}
	switch evt.Key {
	case KeyEqual: // Cmd+= and Cmd++ are the same physical key
		w.ZoomIn()
		return true
	case KeyMinus:
		w.ZoomOut()
		return true
	case Key0:
		w.ResetZoom()
		return true
	}
	return false
}

// applyGestureZoom is the window-level fallback for a gesture no widget
// consumed. Reports whether it acted.
//
// Pinch multiplies by the event's increment rather than assigning its
// cumulative Scale: the increment composes correctly with the quantization
// in SetZoom, whereas assigning a cumulative value would fight it (each
// event would re-derive from the gesture's start and undo the snap).
func (w *Window) applyGestureZoom(evt GestureEvent) bool {
	if w == nil || !w.zoomEnabled {
		return false
	}
	switch evt.eventType {
	case EventGesturePinch:
		if evt.DScale == 0 {
			return false
		}
		w.SetZoom(w.effectiveZoom() * (1 + evt.DScale))
		return true
	case EventGestureSmartMagnify:
		w.SmartZoom()
		return true
	}
	return false
}

// toViewport converts a platform coordinate (window-logical points) into
// the viewport space widgets are laid out in.
func (w *Window) toViewport(x, y float64) (float32, float32) {
	zoom := w.effectiveZoom()
	return float32(x) / zoom, float32(y) / zoom
}
