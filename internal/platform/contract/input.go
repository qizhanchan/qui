// Package contract is the boundary between the qui engine and its
// platform backends (internal/platform/*).
//
// It holds what both sides must agree on and nothing else: the input
// vocabulary (keys, modifiers, buttons, cursor shapes, gesture phases,
// window kinds — re-exported from package qui as type aliases, so app code
// never sees this package), the interfaces a backend implements, the
// backend registry, and Host, the few engine services a backend calls back
// into. It imports nothing from qui, which is what lets qui import the
// backends without a cycle.
package contract

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
	// Modifier keys, left and right. They arrive as KeyDown/KeyUp of their
	// own (as well as in every event's Mods).
	KeyLeftShift
	KeyRightShift
	KeyLeftControl
	KeyRightControl
	KeyLeftAlt
	KeyRightAlt
	KeyLeftSuper
	KeyRightSuper
	// KeyCapsLock: KeyDown when Caps Lock turns on, KeyUp when it turns off.
	KeyCapsLock
	// KeyFn is the macOS Fn / Globe key (cocoa backend only; GLFW does not
	// report it).
	KeyFn
	KeyMenu
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	// Keypad.
	KeyKP0
	KeyKP1
	KeyKP2
	KeyKP3
	KeyKP4
	KeyKP5
	KeyKP6
	KeyKP7
	KeyKP8
	KeyKP9
	KeyKPDecimal
	KeyKPDivide
	KeyKPMultiply
	KeyKPSubtract
	KeyKPAdd
	KeyKPEqual
	// KeyNumLock is Num Lock, or the Clear key on a Mac keypad.
	KeyNumLock
)

// Modifiers represents shift/ctrl/alt/etc.
type Modifiers int

const (
	ModShift Modifiers = 1 << iota
	ModControl
	ModAlt
	ModSuper
)

// CursorShape is a semantic cursor style.
type CursorShape int

const (
	// CursorDefault is the platform default arrow cursor.
	CursorDefault CursorShape = iota
	// CursorText is the text I-beam cursor.
	CursorText
	// CursorCrosshair is a crosshair cursor.
	CursorCrosshair
	// CursorHand is a pointing hand cursor.
	CursorHand
	// CursorResizeEW is a horizontal resize cursor.
	CursorResizeEW
	// CursorResizeNS is a vertical resize cursor.
	CursorResizeNS
	// CursorHidden hides the pointer while it is over the widget that
	// resolves to it; it reappears over anything resolving to another
	// shape, or outside the window. See cursor_image.go, which also has
	// image cursors (NewImageCursor).
	CursorHidden
	// CursorResizeNWSE is a diagonal resize cursor for the top-left /
	// bottom-right corner of a frame (CSS nwse-resize). Backends without a
	// diagonal shape fall back to the arrow.
	CursorResizeNWSE
	// CursorResizeNESW is the top-right / bottom-left counterpart (CSS
	// nesw-resize).
	CursorResizeNESW
)

// WindowKind selects a window's role at the OS level. It is fixed at
// creation: whether a window can take keyboard focus, and whether its
// framebuffer has an alpha channel the compositor honors, are decided when
// the OS window and its GPU context are made.
type WindowKind int

const (
	// WindowNormal is an ordinary application window — title bar, in the
	// window cycle, takes keyboard focus when clicked.
	WindowNormal WindowKind = iota

	// WindowOverlayPanel is a borderless, transparent, always-on-top
	// surface that NEVER takes keyboard focus away from whatever the user
	// is typing into.
	//
	// This is not "a normal window with the chrome switched off". Three
	// properties make it a different kind of object, and all three are
	// load-bearing:
	//
	//   - Non-activating. Clicking the panel must not deactivate the app
	//     the user is typing into. An input method's candidate window that
	//     steals focus is not a candidate window — the host app's caret
	//     disappears and the next keystroke goes nowhere.
	//   - Transparent framebuffer. The panel draws its own rounded card and
	//     shadow, so the pixels outside that card must composite through to
	//     the desktop rather than being cleared to a background color.
	//   - Above everything, on every Space, including over fullscreen apps.
	//     A candidate window that is hidden behind the app it belongs to is
	//     useless.
	//
	// Because a panel cannot receive key events, it is a display-and-click
	// surface: mouse hit-testing works normally, keyboard input does not
	// reach it at all. The keystrokes belong to whatever the OS considers
	// focused; an input method receives them through its own OS channel and
	// drives the panel's widget tree from there.
	//
	// Not every windowing backend can offer this. NewOverlayPanel returns
	// an error rather than silently handing back a normal window: a
	// focus-stealing panel is worse than no panel, because the failure
	// shows up as the user's typing going to the wrong place rather than as
	// a missing feature.
	WindowOverlayPanel
)
