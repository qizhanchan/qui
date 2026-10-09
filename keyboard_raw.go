package qui

import (
	"time"
	"unicode"
)

// Raw keyboard capture.
//
// A remote-desktop viewer, a terminal emulator or a VM console wants the
// keyboard as a keyboard, not as a text-entry device: every key as a
// KeyDown/KeyUp with its native code, Command-Q included. Normally three
// layers stand in the way — the input method turns keystrokes into
// composed text, the window consumes Tab for focus navigation and
// unhandled keys for accelerators, and on macOS the menu bar takes key
// equivalents (Cmd+Q, Cmd+W, Cmd+H …) before the view ever sees them.
//
// While the focused widget implements RawKeyboardTarget and returns true,
// the window is in raw mode:
//
//   - keys skip the input method (no marked text); each printable
//     keystroke additionally produces a CharEvent with Raw set, right after
//     its KeyDown, for targets that need the character (typing into the
//     remote with the local layout);
//   - Tab does not move focus, Cmd/Ctrl+C / +A are not intercepted for
//     cross-widget selection, and window accelerators, zoom and overlay
//     shortcuts do not fire;
//   - on macOS (both backends) menu key equivalents are not consumed: the
//     keystroke is delivered to the target instead;
//   - one escape chord (SetRawKeyboardEscape, default Ctrl+Alt+Escape) is
//     exempt: it never reaches the target and runs the OnRawKeyboardEscape
//     callback, or the window's accelerators if there is none — so the
//     user always has a way out of the viewer.
//
// Shortcuts the OS handles before any application sees them (Cmd+Tab,
// Cmd+Space, Mission Control) need SetSystemKeyCapture on top (macOS,
// Accessibility permission).

// RawKeyboardTarget is implemented by focusable widgets that can take the
// keyboard raw. Return true while raw capture is wanted; the answer is
// consulted on every keystroke, so it may change at any time.
type RawKeyboardTarget interface {
	WantsRawKeyboard() bool
}

// DefaultRawKeyboardEscape is the escape chord used until
// SetRawKeyboardEscape is called.
const DefaultRawKeyboardEscape = "Ctrl+Alt+Escape"

// rawKeyboardState is the per-window raw-mode configuration.
type rawKeyboardState struct {
	configured bool // false: use DefaultRawKeyboardEscape
	escKey     Key  // KeyUnknown: no escape chord
	escMods    Modifiers
	onEscape   func()
	// escDown is the key of an escape chord whose KeyDown was taken, so its
	// KeyUp is taken too rather than reaching the target unpaired.
	escDown Key
}

func (s *rawKeyboardState) chord() (Key, Modifiers) {
	if !s.configured {
		return KeyEscape, ModControl | ModAlt
	}
	return s.escKey, s.escMods
}

// rawKeyboardTarget returns the focused widget when it currently wants
// raw keyboard capture and is not behind a modal, else nil.
func (w *Window) rawKeyboardTarget() Widget {
	if w == nil || w.focused == nil {
		return nil
	}
	rt, ok := w.focused.(RawKeyboardTarget)
	if !ok || !rt.WantsRawKeyboard() {
		return nil
	}
	if w.keyTargetBehindModal(w.focused) {
		return nil
	}
	return w.focused
}

// RawKeyboardActive reports whether the window is in raw keyboard mode:
// the focused widget is a RawKeyboardTarget that wants it. UI thread.
func (w *Window) RawKeyboardActive() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.RawKeyboardActive")
	return w.rawKeyboardTarget() != nil
}

// SetRawKeyboardEscape sets the chord that escapes raw keyboard capture,
// in accelerator syntax ("Ctrl+Alt+Escape", "Cmd+Shift+F12"). An empty
// string removes the escape chord altogether. UI thread.
func (w *Window) SetRawKeyboardEscape(shortcut string) error {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.SetRawKeyboardEscape")
	if shortcut == "" {
		w.raw.configured, w.raw.escKey, w.raw.escMods = true, KeyUnknown, 0
		return nil
	}
	key, mods, err := ParseShortcut(shortcut)
	if err != nil {
		return err
	}
	w.raw.configured, w.raw.escKey, w.raw.escMods = true, key, mods
	return nil
}

// RawKeyboardEscape returns the current escape chord (KeyUnknown when
// there is none).
func (w *Window) RawKeyboardEscape() (Key, Modifiers) {
	if w == nil {
		return KeyUnknown, 0
	}
	return w.raw.chord()
}

// OnRawKeyboardEscape sets what the escape chord does while raw capture is
// active. Typical handlers move focus off the viewer or leave fullscreen.
// With no handler the chord goes to the window's accelerators instead.
// nil removes it. UI thread.
func (w *Window) OnRawKeyboardEscape(fn func()) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.OnRawKeyboardEscape")
	w.raw.onEscape = fn
}

// isRawEscape reports whether key+mods is the escape chord.
func (w *Window) isRawEscape(key Key, mods Modifiers) bool {
	k, m := w.raw.chord()
	return k != KeyUnknown && key == k && mods == m
}

// handleRawEscape takes the escape chord's KeyDown (running the escape
// action) and its matching KeyUp. Returns true when the event was taken.
func (w *Window) handleRawEscape(e KeyEvent) bool {
	switch e.eventType {
	case EventKeyDown:
		if !w.isRawEscape(e.Key, e.Mods) {
			return false
		}
		w.raw.escDown = e.Key
		if e.Repeat {
			return true
		}
		if fn := w.raw.onEscape; fn != nil {
			fn()
		} else if w.accels != nil {
			w.accels.match(e, w.focused, w.topModalIndex() >= 0)
		}
		return true
	case EventKeyUp:
		if w.raw.escDown != KeyUnknown && e.Key == w.raw.escDown {
			w.raw.escDown = KeyUnknown
			return true
		}
	}
	return false
}

// rawKeyMode is what the platform layer asks before routing a keystroke:
// 0 not raw (normal processing), 1 capture (deliver straight to the view,
// past menus and the input method), 2 the escape chord (normal
// processing, so menu key equivalents and accelerators can see it).
func (w *Window) rawKeyMode(key Key, mods Modifiers) int {
	if w.rawKeyboardTarget() == nil {
		return 0
	}
	if w.isRawEscape(key, mods) || (w.raw.escDown != KeyUnknown && key == w.raw.escDown) {
		return 2
	}
	return 1
}

// onRawChars delivers the characters of a keystroke taken raw (the
// platform layer bypassed the input method) as Raw CharEvents. Control
// characters and the private-use code points AppKit uses for function and
// arrow keys are dropped — those keys are KeyEvents only.
func (w *Window) onRawChars(text string, mods Modifiers) {
	for _, r := range text {
		if r < 0x20 || r == 0x7f || (r >= 0xF700 && r <= 0xF8FF) || (!unicode.IsPrint(r) && r != ' ') {
			continue
		}
		w.dispatch(CharEvent{
			baseEvent: baseEvent{shared: &eventState{}},
			When:      time.Now(),
			Rune:      r,
			Mods:      mods,
			Raw:       true,
		})
	}
}
