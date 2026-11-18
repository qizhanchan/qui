package qui

// IME (Input Method Editor) integration for non-Latin text entry —
// Chinese / Japanese / Korean / etc. The OS-native IME produces a
// multi-keypress "composition" that eventually becomes a final
// "commit" string. Widgets that accept text must render the in-flight
// composition (preedit) at the caret, then absorb the commit when
// the user selects from the candidate window.
//
// Architecture:
//
//	macOS NSTextInputClient             Window.SetPreedit / CommitIME
//	    │                                         │
//	(Cgo bridge) ──────────────────────────────────┤
//	                                              ↓
//	                                  focused Widget (IMEClient)
//	                                    - SetPreedit(text, cursor)
//	                                    - CommitIME(text)
//	                                    - CaretRect() for candidate window
//
// Non-IME input still flows through the normal CharEvent callback —
// when no IME is active, the OS delivers char events directly.

// IMEClient is implemented by widgets that accept IME composition.
// The framework forwards preedit/commit notifications from the OS to
// the currently-focused IMEClient. CaretRect feeds the candidate
// window's screen position.
type IMEClient interface {
	// SetPreedit updates the currently-composing (not yet committed)
	// text. The widget should render it at the caret with a distinct
	// visual cue (underline is conventional). Cursor is the rune
	// index within `text` where the composition caret sits.
	// Called with ("", 0) to clear.
	SetPreedit(text string, cursor int)

	// CommitIME replaces the current preedit with `text` as final
	// input — the widget should insert `text` at its caret, clear
	// preedit state, and fire any OnChange callbacks. The IME's
	// candidate window disappears after this.
	CommitIME(text string)

	// CaretRect returns the rectangle of the caret so the OS can anchor
	// the IME candidate window near it. Returned rect is in the WIDGET's
	// own coordinate space (the same space as its Bounds); Window.CaretRect
	// maps it to window coordinates and the OS bridge converts those to
	// screen coords using the window's origin.
	CaretRect() Rect
}

// IMEActivity is implemented by an IMEClient that accepts composition
// only some of the time — a widget type shared by editable and plain
// content (htmlcss elements: only an editing host composes). While
// IMEActive reports false the widget is not treated as an IME client: no
// preedit or commit reaches it, it has no caret rect, and the platform
// bridge keeps the input method out of its keystrokes (macOS delivers the
// typed characters directly instead of opening a composition), so a page's
// key shortcuts and Space-to-scroll keep working with a CJK IME on.
type IMEActivity interface {
	IMEActive() bool
}

// imeClientOf returns w as an active IME client.
func imeClientOf(w Widget) (IMEClient, bool) {
	c, ok := w.(IMEClient)
	if !ok {
		return nil, false
	}
	if a, ok := w.(IMEActivity); ok && !a.IMEActive() {
		return nil, false
	}
	return c, true
}

// IMEBypassed reports whether the focused widget opted out of the input
// method (an IMEClient whose IMEActive is false): the platform then feeds
// keystrokes to it as plain characters, never as a composition.
func (w *Window) IMEBypassed() bool {
	if w == nil || w.focused == nil {
		return false
	}
	if _, ok := w.focused.(IMEClient); !ok {
		return false
	}
	_, active := imeClientOf(w.focused)
	return !active
}

// SetPreedit forwards a composition update to the focused widget if
// it implements IMEClient. Safe to call when nothing is focused —
// it silently no-ops.
//
// Intended caller: the platform IME bridge (e.g., macOS NSTextInputClient
// delegate). Also useful in tests to simulate IME input without a
// real backend.
func (w *Window) SetPreedit(text string, cursor int) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPreedit")
	if client, ok := imeClientOf(w.focused); ok {
		client.SetPreedit(text, cursor)
	}
	w.Invalidate()
}

// CommitIME forwards a commit event to the focused widget — the
// point where composition finalizes into actual input.
func (w *Window) CommitIME(text string) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.CommitIME")
	if text == "" {
		return
	}
	if client, ok := imeClientOf(w.focused); ok {
		client.CommitIME(text)
	}
	w.Invalidate()
}

// CaretRect returns the focused widget's caret rectangle in window
// coordinates, or an empty Rect if the focused widget isn't an IME
// client (the OS bridge should hide the candidate window in that case).
func (w *Window) CaretRect() Rect {
	if w == nil {
		return Rect{}
	}
	w.assertUIThread("Window.CaretRect")
	if client, ok := imeClientOf(w.focused); ok {
		// The client reports the caret in its own coordinate space (derived
		// from its Bounds + text layout); map it out so the OS candidate
		// window lands on screen even when the field is inside a scroll
		// container or a transformed subtree.
		return InteractionRectFor(w.focused, client.CaretRect())
	}
	return Rect{}
}
