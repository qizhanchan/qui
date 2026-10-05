package qui

import "time"

// EventType enumerates supported input events.
type EventType int

const (
	EventMouseMove EventType = iota
	EventMouseDown
	EventMouseUp
	EventScroll
	EventKeyDown
	EventKeyUp
	EventChar
	EventFocus
	EventDragStart
	EventDragMove
	// EventDragOver fires on the droppable under the cursor during a drag
	// (standard DnD dragover) so a target can show a drop indicator. The
	// event's Source is the widget being dragged.
	EventDragOver
	EventDragEnd
	EventDrop
	// EventMouseEnter fires on a widget the first frame the cursor hit
	// path includes it. Does not bubble — targets the specific widget
	// that was entered. Matches DOM mouseenter semantics.
	EventMouseEnter
	// EventMouseLeave fires on a widget the first frame the cursor hit
	// path no longer includes it. Does not bubble.
	EventMouseLeave
	// EventGesturePinch is a two-finger magnify gesture (trackpad pinch,
	// or Ctrl/Cmd+wheel synthesis). Carried by GestureEvent.
	EventGesturePinch
	// EventGestureRotate is a two-finger rotation gesture. Carried by
	// GestureEvent; Rotation/DRotation are degrees.
	EventGestureRotate
	// EventGestureSmartMagnify is the platform "smart zoom" gesture
	// (two-finger double-tap on macOS). One-shot: no Began/Ended pair,
	// and no Scale — the receiver decides what to zoom to (typically
	// toggling between 100% and a fitted scale).
	EventGestureSmartMagnify
	// EventDragEnter fires on a droppable when a drag first moves over it
	// (and it accepts the payload — see DropAcceptor). EventDragLeave fires
	// when the drag moves off it, ends elsewhere, or is cancelled. Both
	// carry DragEvent and pair one-to-one.
	EventDragEnter
	EventDragLeave
)

// EventPhase identifies which dispatch phase currently carries the event.
// Follows the DOM model: capture runs ancestors top-down, target fires at
// the deepest receiver, bubble runs ancestors bottom-up. Widgets that
// only care about target+bubble can ignore Phase.
type EventPhase int

const (
	PhaseNone EventPhase = iota
	PhaseCapture
	PhaseTarget
	PhaseBubble
)

// eventState is the mutable dispatch state shared by all copies of an
// event value within a single dispatch. Events are value types, so the
// pointer is how handlers mutate "stopped" and how dispatch updates
// phase/currentTarget visible to subsequent handlers.
type eventState struct {
	stopped       bool
	phase         EventPhase
	target        Widget
	currentTarget Widget
}

// baseEvent carries the dispatch state pointer. Embedded into every
// concrete event type so they all gain the StopPropagation / Phase /
// Target accessors uniformly.
type baseEvent struct {
	shared *eventState
}

func (b baseEvent) StopPropagation() {
	if b.shared != nil {
		b.shared.stopped = true
	}
}

func (b baseEvent) IsPropagationStopped() bool {
	return b.shared != nil && b.shared.stopped
}

func (b baseEvent) Phase() EventPhase {
	if b.shared == nil {
		return PhaseNone
	}
	return b.shared.phase
}

func (b baseEvent) Target() Widget {
	if b.shared == nil {
		return nil
	}
	return b.shared.target
}

func (b baseEvent) CurrentTarget() Widget {
	if b.shared == nil {
		return nil
	}
	return b.shared.currentTarget
}

func (b baseEvent) state() *eventState { return b.shared }

// Event is the base interface for all events. The unexported state()
// method keeps dispatch internals library-private — user code can observe
// phase/target but cannot forge synthetic events bypassing the normal
// pipeline.
type Event interface {
	Type() EventType
	Timestamp() time.Time
	StopPropagation()
	IsPropagationStopped() bool
	Phase() EventPhase
	Target() Widget
	CurrentTarget() Widget
	state() *eventState
}

// GesturePhase tracks the lifecycle of a continuous input gesture —
// trackpad pinch/rotate, and (on platforms that report it) scrolling.
//
// The phase is what lets a receiver distinguish "the user just started
// pinching" from "the user is mid-pinch": accumulators reset on Began,
// and inertia / snap-back animations start on Ended.
//
// Discrete sources have no lifecycle to report: a mouse wheel notch or
// a synthesized Ctrl+wheel pinch arrives as a self-contained
// GesturePhaseChanged with no Began/Ended pair. Receivers must therefore
// treat DScale (the per-event increment) as the primary signal and use
// the cumulative Scale only as a convenience for real gestures.
type GesturePhase int

const (
	// GesturePhaseNone means the platform reported no phase — either it
	// cannot (X11 scroll wheels) or the event is a one-shot.
	GesturePhaseNone GesturePhase = iota
	// GesturePhaseBegan is the first event of a gesture. Accumulators
	// (Scale, Rotation) are reset to identity at this point.
	GesturePhaseBegan
	// GesturePhaseChanged is an update within an active gesture.
	GesturePhaseChanged
	// GesturePhaseEnded fires when the fingers lift.
	GesturePhaseEnded
	// GesturePhaseCancelled fires when the system abandons the gesture
	// (palm rejection, focus loss). Receivers should revert, not commit.
	GesturePhaseCancelled
	// GesturePhaseMomentum marks post-release inertial scrolling (macOS
	// momentum scroll). Only reported for scroll, never for pinch.
	GesturePhaseMomentum
)

// MouseEvent represents mouse movement or buttons.
type MouseEvent struct {
	baseEvent
	eventType EventType
	When      time.Time
	X, Y      float32
	Button    MouseButton
	Mods      Modifiers
	DeltaX    float32
	DeltaY    float32

	// AfterDrag marks the MouseUp that ENDED a widget drag. The release
	// still reaches widgets (a pressed button has to un-press), but click
	// semantics must not fire from it: dropping a row where you wanted it
	// is not also a click on it. Matches the DOM, which fires no click
	// after a drag.
	AfterDrag bool

	// Clicks is the press count for EventMouseDown / EventMouseUp: 1 for a
	// single click, 2 for the second press of a double-click, and so on
	// (presses of the same button within 400ms and 4px of each other).
	// The window fills it in during dispatch, so double-click-to-open is a
	// `me.Clicks == 2` check rather than a hand-rolled timer. Zero on other
	// event types.
	Clicks int

	// ScrollPhase is the scroll gesture's lifecycle stage, for
	// EventScroll only. GesturePhaseNone on platforms that don't report
	// it; GesturePhaseMomentum distinguishes inertial scrolling from
	// fingers-still-down scrolling, which matters for anything that
	// snaps or commits on release.
	//
	// Named ScrollPhase rather than Phase because baseEvent already
	// promotes a Phase() method for the capture/target/bubble stage.
	ScrollPhase GesturePhase
}

func (e MouseEvent) Type() EventType      { return e.eventType }
func (e MouseEvent) Timestamp() time.Time { return e.When }

// eventInWidgetSpace returns event with its positional fields mapped from
// window-logical coordinates into widget's own coordinate space — the space
// widget.Bounds() and the widget's internal geometry (text layout, ripple
// origin, drop indicators) live in.
//
// Widgets compare event coordinates against their own bounds, so dispatch
// hands every node on the capture/target/bubble path its own view of the
// same event instead of one shared window-space copy. A widget inside a
// scroll container has bounds in CONTENT coordinates (the offset is a paint
// transform, not a re-layout), and a widget under a CSS transform has
// pre-transform bounds; both would otherwise be off by that transform.
//
// The shared event state is carried over unchanged, so phase, target, and
// StopPropagation still work across the localized copies. Untransformed
// trees — the overwhelming majority — take the identity fast path and
// dispatch the original event value.
func eventInWidgetSpace(event Event, widget Widget) Event {
	if widget == nil {
		return event
	}
	switch event.(type) {
	case MouseEvent, GestureEvent, DragEvent:
	default:
		return event // no positional fields to map
	}
	m := InteractionMatrixOf(widget)
	if m == IdentityMatrix() {
		return event
	}
	inv, ok := m.Invert()
	if !ok {
		return event
	}
	switch e := event.(type) {
	case MouseEvent:
		p := inv.TransformPoint(Point{X: e.X, Y: e.Y})
		e.X, e.Y = p.X, p.Y
		return e
	case GestureEvent:
		p := inv.TransformPoint(Point{X: e.X, Y: e.Y})
		e.X, e.Y = p.X, p.Y
		return e
	case DragEvent:
		p := inv.TransformPoint(Point{X: e.X, Y: e.Y})
		e.X, e.Y = p.X, p.Y
		return e
	}
	return event
}

// KeyEvent represents keyboard press/release.
type KeyEvent struct {
	baseEvent
	eventType EventType
	When      time.Time
	Key       Key
	ScanCode  int
	Mods      Modifiers
}

func (e KeyEvent) Type() EventType      { return e.eventType }
func (e KeyEvent) Timestamp() time.Time { return e.When }

// CharEvent represents text input.
type CharEvent struct {
	baseEvent
	When time.Time
	Rune rune
	Mods Modifiers
}

func (e CharEvent) Type() EventType      { return EventChar }
func (e CharEvent) Timestamp() time.Time { return e.When }

// FocusEvent represents focus enter/leave.
type FocusEvent struct {
	baseEvent
	When  time.Time
	Focus bool
}

func (e FocusEvent) Type() EventType      { return EventFocus }
func (e FocusEvent) Timestamp() time.Time { return e.When }

// GestureEvent represents a multi-touch gesture — trackpad pinch
// (EventGesturePinch), two-finger rotation (EventGestureRotate), or
// smart-zoom (EventGestureSmartMagnify).
//
// Routing follows the normal three-phase model, targeted at the widget
// under the anchor point, so a ScrollView / document view / chart can
// consume a pinch while an unhandled one bubbles to the root. A gesture
// is captured on Began: every subsequent Changed/Ended goes to the
// widget that accepted the Began, even if the fingers drift outside its
// bounds.
//
// Increment vs cumulative: DScale/DRotation are this event's delta,
// Scale/Rotation are cumulative since Began. Prefer the increments —
// they are the only fields a discrete source (Ctrl+wheel synthesis) can
// report meaningfully. See GesturePhase.
type GestureEvent struct {
	baseEvent
	eventType EventType
	When      time.Time
	// X, Y is the gesture anchor in window-logical coordinates: the
	// point that should stay put while the content scales around it.
	// Trackpads have no absolute position, so this is the cursor
	// position at gesture time — which is exactly what browsers use.
	X, Y float32
	// GesturePhase is the gesture lifecycle stage. Named with the type
	// prefix because baseEvent already promotes Phase() for the
	// capture/target/bubble stage.
	GesturePhase GesturePhase
	// Scale is the cumulative magnification since Began (1 = original
	// size). Meaningless for one-shot / synthesized events — see DScale.
	Scale float32
	// DScale is this event's magnification increment, as a fraction:
	// the receiver applies it as `zoom *= 1 + DScale`. 0 at Began.
	DScale float32
	// Rotation is the cumulative rotation in degrees since Began,
	// counter-clockwise positive. Only for EventGestureRotate.
	Rotation float32
	// DRotation is this event's rotation increment in degrees.
	DRotation float32
	Mods      Modifiers
	// Synthetic marks a gesture the framework manufactured from another
	// input (Ctrl/Cmd+wheel) rather than a real multi-touch device.
	// Receivers that want to feel different for a wheel than for a
	// trackpad (bigger steps, snapping) can branch on it.
	Synthetic bool
}

func (e GestureEvent) Type() EventType      { return e.eventType }
func (e GestureEvent) Timestamp() time.Time { return e.When }

// DragEvent represents drag/drop gestures.
type DragEvent struct {
	baseEvent
	eventType EventType
	When      time.Time
	X, Y      float32
	// Source is the widget being dragged; nil for an OS file drop.
	Source Widget
	// Data is the drag payload: what the source's DragDataProvider
	// supplied, or the dropped Files for an OS file drop. Never nil on
	// events the window synthesizes.
	Data *DragData
	// Accepted, on EventDragEnd, reports whether a drop target took the
	// drop — a source that moves rather than copies removes its item only
	// then.
	Accepted bool
}

func (e DragEvent) Type() EventType      { return e.eventType }
func (e DragEvent) Timestamp() time.Time { return e.When }

// MouseButton mirrors GLFW mouse button codes.
type MouseButton int

const (
	MouseButtonLeft MouseButton = iota
	MouseButtonRight
	MouseButtonMiddle
)

// Key represents keyboard key codes (subset).
type Key int

const (
	KeyUnknown Key = iota
	KeySpace
	KeyEnter
	KeyEscape
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyBackspace
	KeyTab
	KeyDelete
	KeyInsert
	KeyPageUp
	KeyPageDown
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	// Letter keys. Only the ones used by framework/app shortcuts are
	// wired up; extend as needed. Preserving the A–Z ordering keeps
	// switches readable.
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
	// Number row (used for "Cmd+0" reset zoom etc.).
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9
	// Common punctuation used in shortcuts ("Cmd+=" / "Cmd+-").
	KeyMinus
	KeyEqual
	KeySlash
	// Word processors bind the rest of the punctuation row: superscript /
	// subscript on "Cmd+." / "Cmd+,", clear formatting on "Cmd+\\",
	// indent / outdent on "Cmd+]" / "Cmd+[".
	KeyComma
	KeyPeriod
	KeySemicolon
	KeyApostrophe
	KeyLeftBracket
	KeyRightBracket
	KeyBackslash
	// KeyGrave is the backtick/tilde key. Spreadsheets bind Ctrl+` to
	// "show formulas"; editors use it for consoles.
	KeyGrave
)

// Modifiers represents shift/ctrl/alt/etc.
type Modifiers int

const (
	ModShift Modifiers = 1 << iota
	ModControl
	ModAlt
	ModSuper
)
