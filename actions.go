package qui

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Action primitives — Click / Type / Hover / Focus / Scroll / Drag /
// SendKey / SendChord. Each is selector-targeted (see selector.go)
// and respects the same dispatch + modal-trap discipline as a real
// user input.
//
// Threading: actions are *synchronous* from the caller's point of
// view. On a production Window (with a GLFW handle) they PostJob
// onto the main goroutine and block until the job has run. On a
// TestWindow (handle == nil) they run inline — tests can drive the
// agent surface without a frame pump.
//
// Errors are returned as concrete sentinel values so the agent
// layer's JSON encoder can map them to stable error codes.

// ErrNoMatch is returned when a selector matches no widget.
var ErrNoMatch = errors.New("qui: selector matched no widget")

// ErrAmbiguous is returned by helpers that demand a unique match
// (currently unused; Find returns the first match by default).
var ErrAmbiguous = errors.New("qui: selector matched multiple widgets")

// ErrModalBlocked is returned when a click / hover / focus action
// would hit a widget that's currently shielded by a modal overlay.
// The blocking overlay's path is in BlockingPath so the agent can
// dismiss it or address it directly.
type ErrModalBlocked struct {
	Target       string
	BlockingPath string
}

func (e *ErrModalBlocked) Error() string {
	return fmt.Sprintf("qui: target %q is blocked by modal %s", e.Target, e.BlockingPath)
}

// ErrDisabled is returned when a click / hover would land on a control
// that is currently disabled.
//
// Dispatching the click anyway is worse than useless: the widget ignores
// it, the action reports success, and the script carries on believing the
// command ran. That is the same silent-failure shape as clicking a
// selector that matched nothing — which this layer already refuses — so a
// disabled target refuses too.
type ErrDisabled struct{ Target string }

func (e *ErrDisabled) Error() string {
	return fmt.Sprintf("qui: target %q is disabled", e.Target)
}

// ErrActionTimeout is returned when an action could not complete
// within the configured deadline. Most commonly happens when the
// main event loop isn't pumping (e.g. blocked on an external
// resource).
var ErrActionTimeout = errors.New("qui: action timed out")

// ErrWindowClosed is returned when a synchronized action targets a
// production window whose platform window has already been destroyed.
var ErrWindowClosed = errors.New("qui: window is closed")

// ErrNotVisible is returned when a selector resolves to a hidden or
// collapsed node. Addressing hidden structure is useful for inspection, but
// synthesizing input there cannot model a real user action.
var ErrNotVisible = errors.New("qui: target is not visible")

// ErrNotTextTarget is returned by the Type action when the resolved
// widget can neither sink text programmatically (TextSink, see roles.go)
// nor hold keyboard focus to receive characters. Typing at such a target
// has no meaning, and guessing — dispatching the text at whatever
// happens to be focused, or writing it into a container's own text —
// silently corrupts the UI under inspection. Fail loudly instead: the
// agent should re-read the tree and address a control.
var ErrNotTextTarget = errors.New("qui: target does not accept text input")

// actionTimeout is the default deadline for synchronous actions
// that wait on the main loop. Generous enough for any reasonable
// frame, short enough that a deadlocked agent doesn't hang forever.
const actionTimeout = 5 * time.Second

// ClickOptions controls Click / DoubleClick / RightClick.
type ClickOptions struct {
	// At overrides the click point. The default is the target's
	// center in widget-local coordinates (converted to window
	// coordinates by adding the widget's Bounds origin).
	At *Point

	// ScrollIntoView (default true) walks up the parent chain
	// looking for a ScrollIntoViewable ancestor and asks it to make
	// the target visible before hit-testing.
	ScrollIntoView bool

	// Modifiers OR-ed into the synthesized MouseEvent.Mods.
	Modifiers Modifiers
}

// TypeOptions controls Type.
type TypeOptions struct {
	// Raw bypasses TextSink and synthesizes per-rune CharEvent
	// dispatch instead. Use when you specifically need to exercise
	// IME / per-key handlers; for the common "fill this field"
	// case the default fast path is preferable.
	Raw bool

	// ScrollIntoView is the same as ClickOptions.ScrollIntoView.
	ScrollIntoView bool
}

// Click synthesizes a left-button click on the widget matching
// target. Failures return without dispatching anything.
func (w *Window) Click(target string, opts ClickOptions) error {
	return w.synchronously(func() error { return w.runClick(target, MouseButtonLeft, 1, opts) })
}

// DoubleClick synthesizes a left-button double-click (two MouseDown
// pairs with the same coordinates). Exactly two presses — the window's
// selection controller counts presses for click granularity (2 = word,
// 3 = line), so an extra "focusing" click would turn this into a
// triple-click.
func (w *Window) DoubleClick(target string, opts ClickOptions) error {
	return w.synchronously(func() error {
		return w.runClick(target, MouseButtonLeft, 2, opts)
	})
}

// RightClick synthesizes a secondary-button click.
func (w *Window) RightClick(target string, opts ClickOptions) error {
	return w.synchronously(func() error { return w.runClick(target, MouseButtonRight, 1, opts) })
}

// Hover synthesizes a MouseMove event to the target's center,
// triggering EnterEvent for the widget and its ancestors. Useful for
// tooltips and hover-only UI states.
func (w *Window) Hover(target string, opts ClickOptions) error {
	return w.synchronously(func() error { return w.runHover(target, opts) })
}

// Focus moves keyboard focus to the target widget. Returns
// ErrNoMatch / ErrModalBlocked on failure.
func (w *Window) Focus(target string) error {
	return w.synchronously(func() error { return w.runFocus(target) })
}

// Blur clears any active focus.
func (w *Window) Blur() error {
	return w.synchronously(func() error { w.SetFocus(nil); return nil })
}

// Type writes text to a TextSink-implementing target. The fast path
// calls SetText directly (one undo entry, no IME). Pass
// TypeOptions{Raw: true} to exercise the per-rune dispatch path
// instead.
func (w *Window) Type(target, text string, opts TypeOptions) error {
	return w.synchronously(func() error { return w.runType(target, text, opts) })
}

// SendKey synthesizes a KeyDown + KeyUp on the target (or the
// currently-focused widget if target is "").
func (w *Window) SendKey(target string, key Key, mods Modifiers) error {
	return w.synchronously(func() error { return w.runSendKey(target, key, mods) })
}

// SendChord parses chord strings like "Cmd+Shift+Z" / "Ctrl+S" and
// dispatches the corresponding KeyDown + KeyUp.
func (w *Window) SendChord(target, chord string) error {
	return w.synchronously(func() error {
		key, mods, err := ParseShortcut(chord)
		if err != nil {
			return err
		}
		return w.runSendKey(target, key, mods)
	})
}

// Scroll synthesizes a scroll event on the target.
func (w *Window) Scroll(target string, dx, dy float32) error {
	return w.synchronously(func() error { return w.runScroll(target, dx, dy) })
}

// Pinch synthesizes a complete two-finger magnify gesture on the target:
// Began → Changed → Ended, ending at a cumulative magnification of
// `scale` (1.5 = pinch out to 150%, 0.5 = pinch in to half).
//
// This is how an agent drives zoom without a trackpad — the events are
// shaped exactly like a real single-step gesture, so a widget that works
// under Pinch works under fingers.
func (w *Window) Pinch(target string, scale float32) error {
	return w.synchronously(func() error { return w.runPinch(target, scale) })
}

// DragOptions controls Drag.
type DragOptions struct {
	// Steps is the number of intermediate MouseMove events between
	// MouseDown and MouseUp. >= 1; 0 is treated as 1. The dispatch
	// model requires at least one move past the 4px dead-zone to
	// fire EventDragStart.
	Steps int

	// Modifiers OR-ed into all synthesized events.
	Modifiers Modifiers
}

// ClickAt synthesizes a left-button MouseDown+MouseUp at the raw
// window point (x, y) without resolving a selector. Escape hatch for
// callers that have already computed coordinates (custom-drawn
// surfaces, canvases, anything with no addressable AXNode). No
// hit-test reachability check is performed — the click goes to
// whatever the dispatch layer resolves at that point.
func (w *Window) ClickAt(x, y float32) error {
	return w.ClickAtWith(x, y, ClickOptions{})
}

// ClickAtWith is ClickAt with modifiers. A point click could not carry any,
// which made every modifier-click on a custom-drawn surface undrivable —
// Cmd+click to follow a link in a document canvas, Shift+click to extend a
// selection — for the exact class of surface the point escape hatch exists to
// reach. ScrollIntoView is ignored: a raw point is already where it is.
func (w *Window) ClickAtWith(x, y float32, opts ClickOptions) error {
	return w.synchronously(func() error {
		now := time.Now()
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: EventMouseDown,
			When:      now,
			X:         x,
			Y:         y,
			Button:    MouseButtonLeft,
			Mods:      opts.Modifiers,
		})
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: EventMouseUp,
			When:      now,
			X:         x,
			Y:         y,
			Button:    MouseButtonLeft,
			Mods:      opts.Modifiers,
		})
		return nil
	})
}

// DragAt synthesizes a left-button drag between two raw window points:
// MouseDown at (x0,y0), steps interpolated MouseMove events, MouseUp at
// (x1,y1). The point-based counterpart to ClickAt, for drag handles that
// aren't addressable widgets — ruler margin markers, canvas resize grips,
// chart pan gestures. steps < 1 is treated as 1; the dispatch model needs
// at least one move past the 4px dead-zone to fire EventDragStart.
func (w *Window) DragAt(x0, y0, x1, y1 float32, steps int) error {
	return w.synchronously(func() error {
		w.dragPoints(Point{X: x0, Y: y0}, Point{X: x1, Y: y1}, steps, 0)
		return nil
	})
}

// -------------------------------------------------------------------
// Widget-targeted action variants.
//
// These mirror the selector-based actions above but take a live Widget
// the caller has already resolved. They exist so higher-level selector
// engines (notably the htmlcss DOM agent layer, which resolves a CSS
// selector to a live El widget) can act without re-expressing the
// target in the widget-selector grammar. Behavior — scroll-into-view,
// modal-reachability check, focus-before-type, dispatch — is identical
// to the string-target path; only target resolution differs.

// ClickWidget clicks an already-resolved widget.
func (w *Window) ClickWidget(widget Widget, opts ClickOptions) error {
	return w.synchronously(func() error { return w.clickWidget(widget, MouseButtonLeft, 1, opts) })
}

// DoubleClickWidget double-clicks an already-resolved widget.
func (w *Window) DoubleClickWidget(widget Widget, opts ClickOptions) error {
	return w.synchronously(func() error { return w.clickWidget(widget, MouseButtonLeft, 2, opts) })
}

// RightClickWidget secondary-clicks an already-resolved widget.
func (w *Window) RightClickWidget(widget Widget, opts ClickOptions) error {
	return w.synchronously(func() error { return w.clickWidget(widget, MouseButtonRight, 1, opts) })
}

// HoverWidget moves the mouse to an already-resolved widget's center.
func (w *Window) HoverWidget(widget Widget, opts ClickOptions) error {
	return w.synchronously(func() error {
		pt, err := w.reachablePoint(widget, opts.At, opts.ScrollIntoView || !targetingExplicit(opts.At))
		if err != nil {
			return err
		}
		w.dispatchHoverAt(pt, opts.Modifiers)
		return nil
	})
}

// FocusWidget moves keyboard focus to an already-resolved widget.
func (w *Window) FocusWidget(widget Widget) error {
	return w.synchronously(func() error {
		if widget == nil {
			return ErrNoMatch
		}
		widget = textActionTarget(widget)
		if _, ok := widget.(registerFocusable); !ok {
			return fmt.Errorf("qui: widget %s is not focusable", widgetTypeName(widget))
		}
		w.SetFocus(widget)
		return nil
	})
}

// TypeWidget writes text to an already-resolved widget (see Type).
func (w *Window) TypeWidget(widget Widget, text string, opts TypeOptions) error {
	return w.synchronously(func() error {
		if _, err := w.reachablePoint(widget, nil, opts.ScrollIntoView); err != nil {
			return err
		}
		return w.typeInto(widget, widgetTypeName(widget), text, opts)
	})
}

// ScrollWidget scrolls an already-resolved widget by (dx, dy).
func (w *Window) ScrollWidget(widget Widget, dx, dy float32) error {
	return w.synchronously(func() error {
		pt, err := w.reachablePoint(widget, nil, true)
		if err != nil {
			return err
		}
		w.dispatchScrollAt(pt, dx, dy)
		return nil
	})
}

// clickWidget is the shared body for the *ClickWidget variants.
func (w *Window) clickWidget(widget Widget, button MouseButton, clickCount int, opts ClickOptions) error {
	pt, err := w.reachablePoint(widget, opts.At, opts.ScrollIntoView || !targetingExplicit(opts.At))
	if err != nil {
		return err
	}
	w.dispatchClicks(pt, button, clickCount, opts.Modifiers)
	return nil
}

// reachablePoint resolves the dispatch point for an already-resolved
// widget and verifies it isn't blocked by a modal. Mirrors the
// resolveActionTarget + assertReachable pair used by the string path.
func (w *Window) reachablePoint(widget Widget, override *Point, scrollIntoView bool) (Point, error) {
	if widget == nil {
		return Point{}, ErrNoMatch
	}
	if isWidgetEffectivelyHidden(widget) || InteractionBoundsOf(widget).IsEmpty() {
		return Point{}, fmt.Errorf("%w: %s", ErrNotVisible, widgetTypeName(widget))
	}
	pt := w.pointForWidget(widget, override, scrollIntoView)
	if err := w.assertReachable(widget, widgetTypeName(widget), pt); err != nil {
		return Point{}, err
	}
	return pt, nil
}

// Drag synthesizes a drag from the center of "from" to the center
// of "to": MouseDown on from, Steps interpolated MouseMove events,
// MouseUp on to.
func (w *Window) Drag(from, to string, opts DragOptions) error {
	return w.synchronously(func() error { return w.runDrag(from, to, opts) })
}

// -------------------------------------------------------------------
// internals

// synchronously runs fn on the main goroutine if a GLFW handle is
// present (production) or inline (test). Returns whatever fn
// returned. Blocks up to actionTimeout in the production path.
func (w *Window) synchronously(fn func() error) error {
	if w == nil {
		return errors.New("qui: nil window")
	}
	w.platMu.RLock()
	hasPlatform := w.plat != nil
	platformBacked := w.platformBacked
	w.platMu.RUnlock()
	if !hasPlatform {
		if platformBacked {
			return ErrWindowClosed
		}
		return fn()
	}
	done := make(chan error, 1)
	// Synchronous framework work must not use the bounded normal lane:
	// dropping it would strand the caller until actionTimeout. The priority
	// lane is unbounded and reserved for exactly these continuations.
	w.PostPriorityJob(func() { done <- fn() })
	select {
	case err := <-done:
		return err
	case <-time.After(actionTimeout):
		return ErrActionTimeout
	}
}

func (w *Window) runClick(target string, button MouseButton, clickCount int, opts ClickOptions) error {
	widget, point, err := w.resolveActionTarget(target, opts.At, opts.ScrollIntoView || !targetingExplicit(opts.At))
	if err != nil {
		return err
	}
	if err := w.assertEnabled(target); err != nil {
		return err
	}
	if err := w.assertReachable(widget, target, point); err != nil {
		return err
	}
	w.dispatchClicks(point, button, clickCount, opts.Modifiers)
	return nil
}

// dispatchClicks synthesizes clickCount MouseDown+MouseUp pairs at pt.
// Shared by the selector path (runClick) and ClickWidget.
func (w *Window) dispatchClicks(pt Point, button MouseButton, clickCount int, mods Modifiers) {
	now := time.Now()
	for i := 0; i < clickCount; i++ {
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: EventMouseDown,
			When:      now,
			X:         pt.X,
			Y:         pt.Y,
			Button:    button,
			Mods:      mods,
		})
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: EventMouseUp,
			When:      now,
			X:         pt.X,
			Y:         pt.Y,
			Button:    button,
			Mods:      mods,
		})
	}
}

func (w *Window) runHover(target string, opts ClickOptions) error {
	widget, point, err := w.resolveActionTarget(target, opts.At, opts.ScrollIntoView)
	if err != nil {
		return err
	}
	if err := w.assertReachable(widget, target, point); err != nil {
		return err
	}
	w.dispatchHoverAt(point, opts.Modifiers)
	return nil
}

// dispatchHoverAt synthesizes a MouseMove at pt. Shared by runHover and
// HoverWidget.
func (w *Window) dispatchHoverAt(pt Point, mods Modifiers) {
	w.dispatch(MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventMouseMove,
		When:      time.Now(),
		X:         pt.X,
		Y:         pt.Y,
		Mods:      mods,
	})
}

func (w *Window) runFocus(target string) error {
	widget, err := w.Find(target)
	if err != nil {
		return err
	}
	if widget == nil {
		return ErrNoMatch
	}
	// A form element hands focus to the control it wraps, so focusing
	// `#email` lands on the editor that a following sendkey needs.
	widget = textActionTarget(widget)
	if _, ok := widget.(registerFocusable); !ok {
		return fmt.Errorf("qui: widget %q is not focusable", target)
	}
	w.SetFocus(widget)
	return nil
}

func (w *Window) runType(target, text string, opts TypeOptions) error {
	scroll := opts.ScrollIntoView
	widget, point, err := w.resolveActionTarget(target, nil, scroll)
	if err != nil {
		return err
	}
	if err := w.assertReachable(widget, target, point); err != nil {
		return err
	}
	return w.typeInto(widget, target, text, opts)
}

// typeInto is the shared Type body for the selector path (runType) and
// the widget-targeted variant (TypeWidget). label names the target in
// error messages.
//
// Two routes, in order of preference:
//
//  1. TextSink — one SetText call, one undo entry (see roles.go for why
//     the sink has to prove itself with TextEditable / Optioned).
//  2. Character dispatch — for widgets that own their editing (a sheet
//     grid starting an inline cell editor, a canvas-drawn editor). The
//     characters travel the focus route, so the target must be able to
//     HOLD focus; otherwise they would land on whatever was focused
//     before, or bubble from the root, which is not typing at the
//     target in any sense.
func (w *Window) typeInto(widget Widget, label, text string, opts TypeOptions) error {
	widget = textActionTarget(widget)
	if !opts.Raw {
		if sink, ok := typedTextSink(widget); ok {
			// Focus first so OnSubmit / IME handlers see a focused widget.
			w.SetFocus(widget)
			sink.SetText(text)
			// SetText is silent by design; tell the widget to publish the
			// change so the app's OnChange runs as it would for typing.
			if n, ok := widget.(TextChangeNotifier); ok {
				n.NotifyTextChanged()
			}
			return nil
		}
	}
	if f, ok := widget.(registerFocusable); !ok || !f.Focusable() {
		return fmt.Errorf("%w: %s cannot hold keyboard focus", ErrNotTextTarget, label)
	}
	w.SetFocus(widget)
	now := time.Now()
	for i, r := range []rune(text) {
		state := &eventState{}
		w.dispatch(CharEvent{
			baseEvent: baseEvent{shared: state},
			Rune:      r,
			Mods:      0,
			When:      now,
		})
		// Nobody consumed the first character: the target can hold focus
		// but doesn't take text (a selectable Label, a focus-styled div).
		// Say so instead of quietly spraying the rest into the void — an
		// agent's whole read of the app depends on failures being visible.
		if i == 0 && !state.stopped {
			return fmt.Errorf("%w: %s ignored the typed characters", ErrNotTextTarget, label)
		}
	}
	return nil
}

// textActionTarget follows one TextTargetProvider hop so a wrapper widget
// (an htmlcss <input> element) hands the action to the control that
// actually edits text. One hop only — a backing control that reported
// another target would be a cycle risk for no known use case.
func textActionTarget(widget Widget) Widget {
	if p, ok := widget.(TextTargetProvider); ok {
		if inner := p.TextTarget(); inner != nil {
			return inner
		}
	}
	return widget
}

// typedTextSink returns widget's TextSink if — and only if — the widget
// also exposes the text or option state an agent can read the written
// value back from. See the TextSink docs: SetText alone is too weak a
// signal.
func typedTextSink(widget Widget) (TextSink, bool) {
	sink, ok := widget.(TextSink)
	if !ok {
		return nil, false
	}
	switch widget.(type) {
	case TextEditable, Optioned:
		return sink, true
	}
	return nil, false
}

func (w *Window) runSendKey(target string, key Key, mods Modifiers) error {
	if target != "" {
		widget, err := w.Find(target)
		if err != nil {
			return err
		}
		if widget == nil {
			return ErrNoMatch
		}
		w.SetFocus(widget)
	}
	now := time.Now()
	w.dispatch(KeyEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventKeyDown,
		When:      now,
		Key:       key,
		Mods:      mods,
	})
	w.dispatch(KeyEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventKeyUp,
		When:      now,
		Key:       key,
		Mods:      mods,
	})
	return nil
}

func (w *Window) runScroll(target string, dx, dy float32) error {
	widget, point, err := w.resolveActionTarget(target, nil, true)
	if err != nil {
		return err
	}
	if err := w.assertReachable(widget, target, point); err != nil {
		return err
	}
	w.dispatchScrollAt(point, dx, dy)
	return nil
}

// dispatchScrollAt synthesizes a scroll event at pt. Shared by runScroll
// and ScrollWidget.
func (w *Window) dispatchScrollAt(pt Point, dx, dy float32) {
	w.dispatch(MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventScroll,
		When:      time.Now(),
		X:         pt.X,
		Y:         pt.Y,
		DeltaX:    dx,
		DeltaY:    dy,
	})
}

func (w *Window) runPinch(target string, scale float32) error {
	if scale <= 0 {
		return errors.New("pinch: scale must be > 0")
	}
	widget, point, err := w.resolveActionTarget(target, nil, true)
	if err != nil {
		return err
	}
	if err := w.assertReachable(widget, target, point); err != nil {
		return err
	}
	w.dispatchPinchAt(point, scale)
	return nil
}

// dispatchPinchAt synthesizes Began → Changed → Ended at pt. The
// cumulative Scale walks 1 → scale while DScale carries the one step
// that gets there, matching what ingestGesture would have produced for a
// trackpad pinch delivered in a single update.
//
// Routed through offerGesture, not dispatch, so an unclaimed pinch reaches
// the window's own zoom handling exactly as a trackpad one would. Using
// dispatch here would make the synthetic path quietly weaker than the real
// one, and this action exists precisely so the real behavior can be
// verified without a trackpad.
func (w *Window) dispatchPinchAt(pt Point, scale float32) {
	now := time.Now()
	emit := func(phase GesturePhase, cumulative, dScale float32) {
		w.offerGesture(GestureEvent{
			eventType:    EventGesturePinch,
			When:         now,
			X:            pt.X,
			Y:            pt.Y,
			GesturePhase: phase,
			Scale:        cumulative,
			DScale:       dScale,
		})
	}
	emit(GesturePhaseBegan, 1, 0)
	emit(GesturePhaseChanged, scale, scale-1)
	emit(GesturePhaseEnded, scale, 0)
}

func (w *Window) runDrag(from, to string, opts DragOptions) error {
	fromWidget, fromPt, err := w.resolveActionTarget(from, nil, true)
	if err != nil {
		return fmt.Errorf("drag from: %w", err)
	}
	if err := w.assertReachable(fromWidget, from, fromPt); err != nil {
		return err
	}
	toWidget, toPt, err := w.resolveActionTarget(to, nil, true)
	if err != nil {
		return fmt.Errorf("drag to: %w", err)
	}
	_ = toWidget
	w.dragPoints(fromPt, toPt, opts.Steps, opts.Modifiers)
	return nil
}

// dragPoints synthesizes MouseDown → steps × MouseMove → MouseUp between
// two window points. Shared by the selector-resolving Drag and the
// point-based DragAt.
func (w *Window) dragPoints(fromPt, toPt Point, steps int, mods Modifiers) {
	if steps < 1 {
		steps = 1
	}
	now := time.Now()
	w.dispatch(MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventMouseDown,
		When:      now,
		X:         fromPt.X,
		Y:         fromPt.Y,
		Button:    MouseButtonLeft,
		Mods:      mods,
	})
	for i := 1; i <= steps; i++ {
		t := float32(i) / float32(steps)
		w.dispatch(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			eventType: EventMouseMove,
			When:      now,
			X:         fromPt.X + (toPt.X-fromPt.X)*t,
			Y:         fromPt.Y + (toPt.Y-fromPt.Y)*t,
			Button:    MouseButtonLeft,
			Mods:      mods,
		})
	}
	w.dispatch(MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventMouseUp,
		When:      now,
		X:         toPt.X,
		Y:         toPt.Y,
		Button:    MouseButtonLeft,
		Mods:      mods,
	})
}

// resolveActionTarget finds the widget, optionally scrolls it into
// view, and returns the hit point (in window coordinates) the
// caller should dispatch toward.
// assertEnabled refuses to drive a disabled control. Selector-addressed
// only: an explicit (x, y) click is the caller saying "hit this pixel", and
// there is no node whose state to consult.
func (w *Window) assertEnabled(target string) error {
	if target == "" {
		return nil
	}
	nodes, err := w.FindNodes(target)
	if err != nil || len(nodes) == 0 {
		return nil // resolution already succeeded; nothing more to say here
	}
	// AXNode.State is the rendered string form ("focused|disabled"), which
	// is what every consumer of the tree reads.
	for _, bit := range strings.Split(nodes[0].State, "|") {
		if bit == "disabled" {
			return &ErrDisabled{Target: target}
		}
	}
	return nil
}

func (w *Window) resolveActionTarget(target string, override *Point, scrollIntoView bool) (Widget, Point, error) {
	nodes, err := w.FindNodes(target)
	if err != nil {
		return nil, Point{}, err
	}
	if len(nodes) == 0 || nodes[0].Widget() == nil {
		return nil, Point{}, ErrNoMatch
	}
	node := nodes[0]
	if !node.Visible {
		return nil, Point{}, fmt.Errorf("%w: %s", ErrNotVisible, target)
	}
	widget := node.Widget()
	if scrollIntoView {
		w.scrollAncestorsIntoView(widget)
		// The node above was captured from the AX snapshot BEFORE the scroll,
		// so its rect is stale by exactly the distance we just scrolled — and
		// aiming there misses the target (or lands outside the window
		// entirely) whenever it started off-screen. Re-resolve so the aim
		// point below is computed from post-scroll geometry.
		if fresh, err := w.FindNodes(target); err == nil &&
			len(fresh) > 0 && fresh[0].Widget() == widget {
			node = fresh[0]
		}
	}
	if override != nil {
		return widget, *override, nil
	}
	// Aim at the NODE's rect, not the widget's. For an ordinary node the two
	// are the same; for a self-drawn child (AccessibleChildProvider) the node
	// is a small region inside a large host — a checklist box inside a whole
	// document view — and the widget's center would land somewhere else
	// entirely. Bounds are re-read after any scrolling above.
	b := node.Bounds
	wb := InteractionBoundsOf(widget)
	if node.widget == widget && b.W == 0 && b.H == 0 {
		b = AXRect{X: wb.X, Y: wb.Y, W: wb.W, H: wb.H}
	}
	// A published child can straddle its host's edge — the right-most column
	// of a sheet, a shape half off the canvas. The host clips what it draws,
	// so the part that exists on screen is the intersection, and that is what
	// to aim at: the geometric center of a half-clipped child sits outside
	// the host, where the click either finds no widget at all or lands on a
	// neighbour. For an ordinary node the rect IS the widget's, so this
	// intersection is the identity and nothing changes.
	if vis := (Rect{X: b.X, Y: b.Y, W: b.W, H: b.H}).Intersect(wb); !vis.IsEmpty() {
		b = AXRect{X: vis.X, Y: vis.Y, W: vis.W, H: vis.H}
	}
	return widget, Point{X: b.X + b.W/2, Y: b.Y + b.H/2}, nil
}

// pointForWidget optionally scrolls widget into view and returns the
// window-coordinate hit point to dispatch toward: the caller-supplied
// override, or the widget's center. Shared by the selector-resolving
// action path (resolveActionTarget) and the widget-targeted variants
// (ClickWidget / HoverWidget / …) the htmlcss DOM layer drives.
func (w *Window) pointForWidget(widget Widget, override *Point, scrollIntoView bool) Point {
	if scrollIntoView {
		w.scrollAncestorsIntoView(widget)
	}
	if override != nil {
		return *override
	}
	b := InteractionBoundsOf(widget)
	return Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
}

// scrollAncestorsIntoView walks parent → root looking for the first
// ScrollIntoViewable and asks it to bring child into view. Only the
// innermost scrolling ancestor is asked — nested scrollers cascade
// naturally because the inner scroll changes child.Bounds(), and a
// follow-up call would just snap to the same position.
func (w *Window) scrollAncestorsIntoView(widget Widget) {
	if widget == nil {
		return
	}
	p := widget.Parent()
	for p != nil {
		if sv, ok := p.(ScrollIntoViewable); ok {
			sv.ScrollChildIntoView(widget)
			return
		}
		p = p.Parent()
	}
}

// assertReachable runs hitTestAll on the chosen point and verifies
// the point resolves into the target's own subtree, or into the
// widget that OWNS the target. Returns ErrModalBlocked when
// something else (a modal overlay, an unrelated sibling) intervenes.
//
// The "owner" case covers widgets that PROJECT children for
// introspection without putting them in the dispatch tree: a
// canvas-drawn item (a slide's page elements, a chart's series, rows
// a virtualized list paints itself) appears in the AX tree with real
// bounds, but hit-testing that point stops at the drawing widget
// because the projected child was never a dispatch target. Clicking
// there is still exactly right — the owner hit-tests the point and
// acts on the item under it — so an ancestor hit is reachable, not
// blocked. A modal overlay is never an ancestor of its victim, so
// the modal check keeps its teeth.
func (w *Window) assertReachable(target Widget, selector string, point Point) error {
	if target == nil {
		return ErrNoMatch
	}
	hit := w.hitTestAll(point)
	if hit == nil {
		return fmt.Errorf("qui: no widget at %v for target %q", point, selector)
	}
	if !widgetIsDescendant(hit, target) && !widgetIsDescendant(target, hit) {
		blocking := ""
		if hit != nil {
			blocking = widgetTypeName(hit)
		}
		return &ErrModalBlocked{Target: selector, BlockingPath: blocking}
	}
	return nil
}

// widgetIsDescendant reports whether candidate == ancestor or
// any of candidate's parents == ancestor. Used to validate that a
// hit-test result lands on the intended target or a child of it.
func widgetIsDescendant(candidate, ancestor Widget) bool {
	for c := candidate; c != nil; c = c.Parent() {
		if c == ancestor {
			return true
		}
	}
	return false
}

// targetingExplicit reports whether the caller pinned the click
// position; if so we skip auto-scroll-into-view (caller has
// presumably worked out their own coordinates).
func targetingExplicit(at *Point) bool { return at != nil }

// ensureNoStrayDashes is unused but reserved for future selector
// hygiene checks.
func ensureNoStrayDashes(s string) string { return strings.TrimSpace(s) }
