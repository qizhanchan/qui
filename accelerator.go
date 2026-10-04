package qui

import (
	"fmt"
	"strconv"
	"strings"
)

// AcceleratorRegistry holds window-level keyboard shortcuts. A menu bar
// typically populates it from its items so pressing Cmd+S fires File >
// Save without every widget having to intercept the key. Registration
// order decides match order: the first entry whose (Key, Mods) matches
// wins. Unregistered entries (nil fn) are a no-op at match time.
//
// Accelerators are consulted by Window.dispatch AFTER normal widget
// dispatch, and only fire when no widget has consumed the event. This
// keeps per-widget shortcuts (e.g. Input's Cmd+Z undo) functional
// while still giving menu actions a global fallback.
type AcceleratorRegistry struct {
	entries []acceleratorEntry
}

type acceleratorEntry struct {
	key      Key
	mods     Modifiers
	fn       func()
	shortcut string // the string as registered, for introspection
}

// NewAcceleratorRegistry returns an empty registry.
func NewAcceleratorRegistry() *AcceleratorRegistry { return &AcceleratorRegistry{} }

// Register parses shortcut (e.g. "Cmd+S", "Ctrl+Shift+Z") and binds fn.
// An empty shortcut or nil fn is silently ignored so callers can pass
// menu items through verbatim without filtering.
func (r *AcceleratorRegistry) Register(shortcut string, fn func()) error {
	if r == nil || shortcut == "" || fn == nil {
		return nil
	}
	key, mods, err := ParseShortcut(shortcut)
	if err != nil {
		return err
	}
	r.entries = append(r.entries, acceleratorEntry{key: key, mods: mods, fn: fn, shortcut: shortcut})
	return nil
}

// Match runs the first entry whose (Key, Mods) matches evt. Returns
// true if an accelerator fired, false otherwise. Modifier bits are
// compared for exact equality — "Cmd+S" does not match "Cmd+Shift+S".
func (r *AcceleratorRegistry) Match(evt KeyEvent) bool {
	if r == nil || evt.Type() != EventKeyDown {
		return false
	}
	for _, e := range r.entries {
		if e.key == evt.Key && e.mods == evt.Mods {
			if e.fn != nil {
				e.fn()
			}
			return true
		}
	}
	return false
}

// Shortcuts returns the registered accelerators as the strings they were
// registered with, in registration order.
//
// Window-level accelerators are real, user-visible commands, but unlike menu
// items and toolbar buttons they have no widget — so without this they are
// invisible to the AX tree and to anything comparing an app's keyboard
// surface against a reference. A shortcut that exists but can't be discovered
// looks identical to one that was never bound.
func (r *AcceleratorRegistry) Shortcuts() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.shortcut)
	}
	return out
}

// Len returns how many accelerators are registered. Useful for tests.
func (r *AcceleratorRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.entries)
}

// ParseShortcut converts a human-readable shortcut string like "Cmd+S"
// or "Ctrl+Shift+Z" into its (Key, Modifiers) pair.
//
// Modifier tokens recognized (case-insensitive):
//
//	Cmd / Command / Super / Meta        → ModSuper
//	Ctrl / Control                      → ModControl
//	Shift                               → ModShift
//	Alt / Option / Opt                  → ModAlt
//
// Key tokens: single letters A-Z, digits 0-9, function keys "F1".."F12",
// or names "Enter", "Tab", "Escape", "Backspace", "Delete", "Space",
// "Left", "Right", "Up", "Down", "Home", "End", "Minus", "Equal" /
// "Plus", "Slash". Matching is case-insensitive.
func ParseShortcut(s string) (Key, Modifiers, error) {
	var mods Modifiers
	parts := strings.Split(s, "+")
	if len(parts) == 0 {
		return KeyUnknown, 0, fmt.Errorf("empty shortcut")
	}
	for _, tok := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case "cmd", "command", "super", "meta":
			mods |= ModSuper
		case "ctrl", "control":
			mods |= ModControl
		case "shift":
			mods |= ModShift
		case "alt", "option", "opt":
			mods |= ModAlt
		default:
			return KeyUnknown, 0, fmt.Errorf("unknown modifier %q in shortcut %q", tok, s)
		}
	}
	keyTok := strings.TrimSpace(parts[len(parts)-1])
	key, err := parseKeyToken(keyTok)
	if err != nil {
		return KeyUnknown, 0, fmt.Errorf("in shortcut %q: %w", s, err)
	}
	return key, mods, nil
}

func parseKeyToken(tok string) (Key, error) {
	if len(tok) == 1 {
		ch := tok[0]
		switch {
		case ch >= 'a' && ch <= 'z':
			return KeyA + Key(ch-'a'), nil
		case ch >= 'A' && ch <= 'Z':
			return KeyA + Key(ch-'A'), nil
		case ch >= '0' && ch <= '9':
			return Key0 + Key(ch-'0'), nil
		case ch == '-':
			return KeyMinus, nil
		case ch == '=':
			return KeyEqual, nil
		case ch == '+':
			// '+' in the key slot means the literal plus sign — since we
			// split on '+', the empty tail produced by "Cmd++" means the
			// user wanted plus. Map to KeyEqual (the unshifted glyph on
			// most US layouts).
			return KeyEqual, nil
		case ch == '/':
			return KeySlash, nil
		case ch == ',':
			return KeyComma, nil
		case ch == '.':
			return KeyPeriod, nil
		case ch == ';':
			return KeySemicolon, nil
		case ch == '\'':
			return KeyApostrophe, nil
		case ch == '[':
			return KeyLeftBracket, nil
		case ch == ']':
			return KeyRightBracket, nil
		case ch == '\\':
			return KeyBackslash, nil
		case ch == '`', ch == '~':
			return KeyGrave, nil
		}
	}
	switch strings.ToLower(tok) {
	case "enter", "return":
		return KeyEnter, nil
	case "tab":
		return KeyTab, nil
	case "escape", "esc":
		return KeyEscape, nil
	case "backspace":
		return KeyBackspace, nil
	case "delete", "del":
		return KeyDelete, nil
	case "space":
		return KeySpace, nil
	case "left":
		return KeyLeft, nil
	case "right":
		return KeyRight, nil
	case "up":
		return KeyUp, nil
	case "down":
		return KeyDown, nil
	case "home":
		return KeyHome, nil
	case "end":
		return KeyEnd, nil
	case "minus":
		return KeyMinus, nil
	case "equal", "plus":
		return KeyEqual, nil
	case "slash":
		return KeySlash, nil
	case "comma":
		return KeyComma, nil
	case "period", "dot":
		return KeyPeriod, nil
	case "semicolon":
		return KeySemicolon, nil
	case "apostrophe", "quote":
		return KeyApostrophe, nil
	case "leftbracket", "lbracket":
		return KeyLeftBracket, nil
	case "rightbracket", "rbracket":
		return KeyRightBracket, nil
	case "backslash":
		return KeyBackslash, nil
	case "grave", "backtick", "tilde":
		return KeyGrave, nil
	}
	// Function keys. KeyF1..KeyF12 have existed (and been delivered by the
	// platform layer) all along, but no spelling reached them here — so an
	// app could handle F11 in a widget yet could not BIND it as an
	// accelerator, which is where menu-level shortcuts like Shift+F11 live.
	if len(tok) >= 2 && (tok[0] == 'f' || tok[0] == 'F') {
		if n, err := strconv.Atoi(tok[1:]); err == nil && n >= 1 && n <= 12 {
			return KeyF1 + Key(n-1), nil
		}
	}
	return KeyUnknown, fmt.Errorf("unknown key %q", tok)
}
