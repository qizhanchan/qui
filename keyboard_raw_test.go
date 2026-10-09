package qui

import "testing"

// rawProbe is a focusable raw keyboard target that records what reaches it
// and consumes nothing (the worst case for accelerators leaking through).
type rawProbe struct {
	BaseWidget
	raw   bool
	keys  []KeyEvent
	chars []CharEvent
}

func newRawProbe() *rawProbe {
	p := &rawProbe{BaseWidget: NewBaseWidget(), raw: true}
	p.SetSelf(p)
	return p
}

func (p *rawProbe) Focusable() bool        { return true }
func (p *rawProbe) SetFocused(bool)        {}
func (p *rawProbe) WantsRawKeyboard() bool { return p.raw }
func (p *rawProbe) Measure(Size) Size      { return Size{W: 50, H: 50} }
func (p *rawProbe) Handle(e Event) bool {
	if e.Phase() != PhaseTarget {
		return false
	}
	switch ev := e.(type) {
	case KeyEvent:
		p.keys = append(p.keys, ev)
	case CharEvent:
		p.chars = append(p.chars, ev)
	}
	return false
}

// rawFixture: root container with the raw probe and a second focusable, so
// Tab has somewhere to go.
func rawFixture(t *testing.T) (*Window, *rawProbe, *rawProbe) {
	t.Helper()
	w := NewTestWindow(Size{W: 400, H: 300})
	target := newRawProbe()
	other := newRawProbe()
	other.raw = false
	root := NewContainer(FlexLayout{Direction: Vertical}, target, other)
	w.SetRoot(root)
	root.Layout(Rect{W: 400, H: 300})
	w.SetFocus(target)
	return w, target, other
}

func key(kind EventType, k Key, mods Modifiers) KeyEvent { return NewKeyEvent(kind, k, mods) }

func TestRawKeyboardBypassesTabAndAccelerators(t *testing.T) {
	w, target, _ := rawFixture(t)
	fired := 0
	w.Accelerators().Register("Cmd+Q", func() { fired++ })
	w.Accelerators().Register("Ctrl+W", func() { fired++ })

	if !w.RawKeyboardActive() {
		t.Fatal("raw mode not active with a raw target focused")
	}
	w.DispatchTestEvent(key(EventKeyDown, KeyTab, 0))
	w.DispatchTestEvent(key(EventKeyDown, KeyQ, ModSuper))
	w.DispatchTestEvent(key(EventKeyDown, KeyW, ModControl))
	w.DispatchTestEvent(key(EventKeyDown, KeyEqual, ModSuper)) // zoom shortcut
	if fired != 0 {
		t.Fatalf("accelerators fired %d times in raw mode", fired)
	}
	if w.Focused() != Widget(target) {
		t.Fatal("Tab moved focus in raw mode")
	}
	if len(target.keys) != 4 || target.keys[0].Key != KeyTab || target.keys[1].Key != KeyQ {
		t.Fatalf("target saw %d keys: %+v", len(target.keys), target.keys)
	}

	// Leaving raw mode restores the normal pipeline.
	target.raw = false
	w.DispatchTestEvent(key(EventKeyDown, KeyQ, ModSuper))
	if fired != 1 {
		t.Fatalf("accelerator did not fire outside raw mode (fired=%d)", fired)
	}
}

func TestRawKeyboardEscapeChord(t *testing.T) {
	w, target, _ := rawFixture(t)
	escaped := 0
	w.OnRawKeyboardEscape(func() { escaped++ })

	// Default chord: Ctrl+Alt+Escape. Neither its KeyDown nor its KeyUp
	// reaches the target.
	w.DispatchTestEvent(key(EventKeyDown, KeyEscape, ModControl|ModAlt))
	w.DispatchTestEvent(key(EventKeyUp, KeyEscape, ModControl))
	if escaped != 1 || len(target.keys) != 0 {
		t.Fatalf("escape=%d, target saw %+v", escaped, target.keys)
	}
	// Plain Escape is just a key for the remote.
	w.DispatchTestEvent(key(EventKeyDown, KeyEscape, 0))
	if escaped != 1 || len(target.keys) != 1 {
		t.Fatalf("plain Escape: escape=%d keys=%d", escaped, len(target.keys))
	}

	// A custom chord, handled by an accelerator when no callback is set.
	if err := w.SetRawKeyboardEscape("Cmd+Shift+F12"); err != nil {
		t.Fatal(err)
	}
	w.OnRawKeyboardEscape(nil)
	accel := 0
	w.Accelerators().Register("Cmd+Shift+F12", func() { accel++ })
	w.DispatchTestEvent(key(EventKeyDown, KeyF12, ModSuper|ModShift))
	if accel != 1 {
		t.Fatalf("escape chord did not reach the accelerators (accel=%d)", accel)
	}
	if k, m := w.RawKeyboardEscape(); k != KeyF12 || m != ModSuper|ModShift {
		t.Fatalf("RawKeyboardEscape = %v %v", k, m)
	}
	if err := w.SetRawKeyboardEscape("Cmd+Nope+X"); err == nil {
		t.Fatal("bad chord accepted")
	}
	// No chord at all.
	_ = w.SetRawKeyboardEscape("")
	n := len(target.keys)
	w.DispatchTestEvent(key(EventKeyDown, KeyF12, ModSuper|ModShift))
	if len(target.keys) != n+1 {
		t.Fatal("with no escape chord every key must reach the target")
	}
}

func TestRawKeyModeForPlatform(t *testing.T) {
	w, target, other := rawFixture(t)
	if m := w.rawKeyMode(KeyQ, ModSuper); m != 1 {
		t.Fatalf("Cmd+Q in raw mode: mode %d, want capture", m)
	}
	if m := w.rawKeyMode(KeyEscape, ModControl|ModAlt); m != 2 {
		t.Fatalf("escape chord: mode %d, want 2", m)
	}
	w.SetFocus(other)
	if m := w.rawKeyMode(KeyQ, ModSuper); m != 0 {
		t.Fatalf("non-raw focus: mode %d", m)
	}
	_ = target
}

func TestRawCharsFiltered(t *testing.T) {
	w, target, _ := rawFixture(t)
	// 'a', a control char, an AppKit function-key code point (F700 = up
	// arrow), and a non-BMP rune.
	w.onRawChars("a\x1b😀", ModSuper)
	if len(target.chars) != 2 {
		t.Fatalf("raw chars = %+v", target.chars)
	}
	if c := target.chars[0]; c.Rune != 'a' || !c.Raw || c.Mods != ModSuper {
		t.Fatalf("raw char = %+v", c)
	}
	if target.chars[1].Rune != '😀' {
		t.Fatalf("second rune = %q", target.chars[1].Rune)
	}
}

func TestRawTargetBehindModalIsNotRaw(t *testing.T) {
	w, _, _ := rawFixture(t)
	modal := newKeySpyModal()
	modal.Layout(Rect{W: 200, H: 200})
	w.PushOverlay(modal)
	w.SetFocus(w.focused) // focus stays put programmatically
	if w.RawKeyboardActive() {
		t.Fatal("raw mode must not apply to a target behind a modal")
	}
}

func TestRawEscapeKeyUpTakenAfterLeavingRawMode(t *testing.T) {
	w, target, _ := rawFixture(t)
	w.OnRawKeyboardEscape(func() { target.raw = false }) // the usual handler
	w.DispatchTestEvent(key(EventKeyDown, KeyEscape, ModControl|ModAlt))
	if w.RawKeyboardActive() {
		t.Fatal("escape handler should have ended raw mode")
	}
	w.DispatchTestEvent(key(EventKeyUp, KeyEscape, ModControl|ModAlt))
	if len(target.keys) != 0 {
		t.Fatalf("the chord's KeyUp leaked to the target: %+v", target.keys)
	}
	w.DispatchTestEvent(key(EventKeyUp, KeyEscape, 0))
	if len(target.keys) != 1 {
		t.Fatal("only the chord's own KeyUp is taken")
	}
}
