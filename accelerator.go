package qui

import (
	"fmt"
	"strconv"
	"strings"
)

// AcceleratorRegistry holds window-level keyboard shortcuts. A menu bar
// typically populates it from its items so pressing Cmd+S fires File >
// Save without every widget having to intercept the key.
//
// Precedence: an entry scoped to a widget (RegisterScoped) fires only when
// the key's target — normally the focused widget — lies inside that scope,
// and a scoped match beats a window-wide one, the deepest scope first.
// Among entries at the same level the most recent registration wins, so a
// panel can override an app-wide binding for as long as it is registered
// and the old binding comes back when it unregisters.
//
// Accelerators are consulted by Window.dispatch AFTER normal widget
// dispatch, and only fire when no widget has consumed the event. This
// keeps per-widget shortcuts (e.g. Input's Cmd+Z undo) functional
// while still giving menu actions a global fallback. While a modal is
// shown only entries scoped inside it fire: Cmd+S must not save the
// document hidden behind a confirm dialog.
type AcceleratorRegistry struct {
	entries []acceleratorEntry
	nextID  int64
}

type acceleratorEntry struct {
	id       int64
	key      Key
	mods     Modifiers
	fn       func()
	scope    Widget // nil = window-wide
	shortcut string // the string as registered, for introspection
	// enabled, when set, is asked at match time: a binding that reports
	// false is skipped as if absent, so the key falls through to a lower
	// binding or keeps flowing (a disabled menu command).
	enabled func() bool
}

// NewAcceleratorRegistry returns an empty registry.
func NewAcceleratorRegistry() *AcceleratorRegistry { return &AcceleratorRegistry{} }

// Register parses shortcut (e.g. "Cmd+S", "CmdOrCtrl+Shift+Z") and binds
// fn window-wide. An empty shortcut or nil fn is silently ignored so
// callers can pass menu items through verbatim without filtering. Use Bind
// when the binding has to be removed again.
func (r *AcceleratorRegistry) Register(shortcut string, fn func()) error {
	_, err := r.add(shortcut, nil, fn)
	return err
}

// Bind is Register returning a function that removes exactly this binding
// (other bindings of the same shortcut are untouched).
func (r *AcceleratorRegistry) Bind(shortcut string, fn func()) (remove func(), err error) {
	return r.add(shortcut, nil, fn)
}

// BindIf is Bind for a command that can be unavailable: enabled is asked
// each time the shortcut is pressed, and while it reports false the
// binding is skipped as if it weren't registered — another binding of the
// same key can fire, or the key goes on unconsumed. A disabled menu item's
// shortcut works this way. A nil enabled behaves like Bind.
func (r *AcceleratorRegistry) BindIf(shortcut string, enabled func() bool, fn func()) (remove func(), err error) {
	remove, err = r.add(shortcut, nil, fn)
	if err == nil && enabled != nil && r != nil && len(r.entries) > 0 && shortcut != "" && fn != nil {
		r.entries[len(r.entries)-1].enabled = enabled
	}
	return remove, err
}

// RegisterScoped binds fn to shortcut only while the key's target lies
// inside scope (scope itself or a descendant) — a document pane's Cmd+F,
// a dialog's Cmd+Enter. The binding beats window-wide ones for the same
// key, and stays live inside a modal that contains scope. Returns the
// remover; call it when scope unmounts.
func (r *AcceleratorRegistry) RegisterScoped(shortcut string, scope Widget, fn func()) (remove func(), err error) {
	if scope == nil {
		return func() {}, fmt.Errorf("RegisterScoped %q: nil scope", shortcut)
	}
	return r.add(shortcut, scope, fn)
}

func (r *AcceleratorRegistry) add(shortcut string, scope Widget, fn func()) (func(), error) {
	if r == nil || shortcut == "" || fn == nil {
		return func() {}, nil
	}
	key, mods, err := ParseShortcut(shortcut)
	if err != nil {
		return func() {}, err
	}
	r.nextID++
	id := r.nextID
	r.entries = append(r.entries, acceleratorEntry{id: id, key: key, mods: mods, fn: fn, scope: scope, shortcut: shortcut})
	return func() { r.removeWhere(func(e acceleratorEntry) bool { return e.id == id }) }, nil
}

// Unregister removes every binding of shortcut (window-wide and scoped)
// and reports how many were removed. "Cmd+S" and "Command+S" name the same
// binding.
func (r *AcceleratorRegistry) Unregister(shortcut string) int {
	if r == nil {
		return 0
	}
	key, mods, err := ParseShortcut(shortcut)
	if err != nil {
		return 0
	}
	return r.removeWhere(func(e acceleratorEntry) bool { return e.key == key && e.mods == mods })
}

// UnregisterScope removes every binding scoped to scope.
func (r *AcceleratorRegistry) UnregisterScope(scope Widget) int {
	if r == nil || scope == nil {
		return 0
	}
	return r.removeWhere(func(e acceleratorEntry) bool { return e.scope == scope })
}

func (r *AcceleratorRegistry) removeWhere(drop func(acceleratorEntry) bool) int {
	kept := r.entries[:0]
	n := 0
	for _, e := range r.entries {
		if drop(e) {
			n++
			continue
		}
		kept = append(kept, e)
	}
	for i := len(kept); i < len(r.entries); i++ {
		r.entries[i] = acceleratorEntry{}
	}
	r.entries = kept
	return n
}

// Match runs the window-wide entry matching evt (scoped entries need a
// target — see MatchFor). Returns true if an accelerator fired. Modifier
// bits are compared for exact equality — "Cmd+S" does not match
// "Cmd+Shift+S".
func (r *AcceleratorRegistry) Match(evt KeyEvent) bool {
	return r.MatchFor(evt, nil)
}

// MatchFor runs the best entry for evt aimed at target: the deepest scope
// containing target, else the newest window-wide binding. Returns true if
// an accelerator fired.
func (r *AcceleratorRegistry) MatchFor(evt KeyEvent, target Widget) bool {
	return r.match(evt, target, false)
}

func (r *AcceleratorRegistry) match(evt KeyEvent, target Widget, scopedOnly bool) bool {
	if r == nil || evt.Type() != EventKeyDown {
		return false
	}
	best := -1
	bestDepth := -1
	for i := len(r.entries) - 1; i >= 0; i-- {
		e := r.entries[i]
		if e.key != evt.Key || e.mods != evt.Mods {
			continue
		}
		if e.enabled != nil && !e.enabled() {
			continue
		}
		depth := 0
		if e.scope != nil {
			d, ok := scopeDepth(e.scope, target)
			if !ok {
				continue
			}
			depth = d + 1
		} else if scopedOnly {
			continue
		}
		if depth > bestDepth {
			best, bestDepth = i, depth
		}
	}
	if best < 0 {
		return false
	}
	if fn := r.entries[best].fn; fn != nil {
		fn()
	}
	return true
}

// scopeDepth reports whether target lies in scope's subtree, and scope's
// depth from the tree root (so a nested scope outranks its ancestor).
func scopeDepth(scope, target Widget) (int, bool) {
	inside := false
	for cur := target; cur != nil; cur = cur.Parent() {
		if cur == scope {
			inside = true
			break
		}
	}
	if !inside {
		return 0, false
	}
	depth := 0
	for cur := scope.Parent(); cur != nil; cur = cur.Parent() {
		depth++
	}
	return depth, true
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
//	CmdOrCtrl / CommandOrControl / Mod  → ModSuper on macOS, ModControl elsewhere
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
		case "cmdorctrl", "commandorcontrol", "cmdorcontrol", "commandorctrl", "mod", "primary":
			mods |= CommandMod()
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
