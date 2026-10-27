package qui

import "testing"

func TestParseShortcutLetter(t *testing.T) {
	key, mods, err := ParseShortcut("Cmd+S")
	if err != nil {
		t.Fatalf("ParseShortcut: %v", err)
	}
	if key != KeyS {
		t.Errorf("key = %v, want KeyS", key)
	}
	if mods != ModSuper {
		t.Errorf("mods = %v, want ModSuper", mods)
	}
}

func TestParseShortcutMulti(t *testing.T) {
	key, mods, err := ParseShortcut("Ctrl+Shift+Z")
	if err != nil {
		t.Fatalf("ParseShortcut: %v", err)
	}
	if key != KeyZ {
		t.Errorf("key = %v, want KeyZ", key)
	}
	if mods != ModControl|ModShift {
		t.Errorf("mods = %v, want ModControl|ModShift", mods)
	}
}

func TestParseShortcutDigit(t *testing.T) {
	key, _, err := ParseShortcut("Cmd+0")
	if err != nil {
		t.Fatalf("ParseShortcut: %v", err)
	}
	if key != Key0 {
		t.Errorf("key = %v, want Key0", key)
	}
}

func TestParseShortcutSymbolKeys(t *testing.T) {
	cases := map[string]Key{
		"Cmd+-":      KeyMinus,
		"Cmd+=":      KeyEqual,
		"Cmd+/":      KeySlash,
		"Ctrl+`":     KeyGrave,
		"Ctrl+tilde": KeyGrave,
		"Cmd+Plus":   KeyEqual,
		"Cmd+Minus":  KeyMinus,
	}
	for shortcut, want := range cases {
		got, _, err := ParseShortcut(shortcut)
		if err != nil {
			t.Errorf("ParseShortcut(%q): %v", shortcut, err)
			continue
		}
		if got != want {
			t.Errorf("ParseShortcut(%q) = %v, want %v", shortcut, got, want)
		}
	}
}

func TestParseShortcutNamedKey(t *testing.T) {
	key, _, err := ParseShortcut("Cmd+Enter")
	if err != nil {
		t.Fatalf("ParseShortcut: %v", err)
	}
	if key != KeyEnter {
		t.Errorf("key = %v, want KeyEnter", key)
	}
}

func TestParseShortcutFunctionKeys(t *testing.T) {
	// F-keys are delivered by the platform layer and handled by widgets, but
	// for a long time no spelling reached them here — so a menu shortcut
	// like Shift+F11 could not be bound at all.
	cases := map[string]Key{
		"F1":        KeyF1,
		"f5":        KeyF5,
		"Shift+F11": KeyF11,
		"Cmd+F12":   KeyF12,
	}
	for shortcut, want := range cases {
		got, _, err := ParseShortcut(shortcut)
		if err != nil {
			t.Errorf("ParseShortcut(%q): %v", shortcut, err)
			continue
		}
		if got != want {
			t.Errorf("ParseShortcut(%q) = %v, want %v", shortcut, got, want)
		}
	}
	// "F" alone is the letter key, and F13 is past what the Key enum covers.
	if got, _, err := ParseShortcut("Cmd+F"); err != nil || got != KeyF {
		t.Errorf(`ParseShortcut("Cmd+F") = %v, %v; want KeyF`, got, err)
	}
	if _, _, err := ParseShortcut("F13"); err == nil {
		t.Error("expected an error for F13 (outside KeyF1..KeyF12)")
	}
}

func TestParseShortcutUnknownModifier(t *testing.T) {
	if _, _, err := ParseShortcut("Foo+S"); err == nil {
		t.Error("expected error for unknown modifier")
	}
}

func TestParseShortcutUnknownKey(t *testing.T) {
	if _, _, err := ParseShortcut("Cmd+BlargleKey"); err == nil {
		t.Error("expected error for unknown key token")
	}
}

func TestAcceleratorRegistryMatch(t *testing.T) {
	r := NewAcceleratorRegistry()
	var saveFired, findFired int
	if err := r.Register("Cmd+S", func() { saveFired++ }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register("Cmd+F", func() { findFired++ }); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !r.Match(NewKeyEvent(EventKeyDown, KeyS, ModSuper)) {
		t.Error("expected match on Cmd+S")
	}
	if saveFired != 1 {
		t.Errorf("saveFired = %d, want 1", saveFired)
	}
	if r.Match(NewKeyEvent(EventKeyDown, KeyS, ModSuper|ModShift)) {
		t.Error("Cmd+Shift+S should not match Cmd+S accelerator")
	}
	if !r.Match(NewKeyEvent(EventKeyDown, KeyF, ModSuper)) {
		t.Error("expected match on Cmd+F")
	}
	if findFired != 1 {
		t.Errorf("findFired = %d, want 1", findFired)
	}
}

func TestAcceleratorRegistryIgnoresKeyUp(t *testing.T) {
	r := NewAcceleratorRegistry()
	fired := 0
	_ = r.Register("Cmd+S", func() { fired++ })
	if r.Match(NewKeyEvent(EventKeyUp, KeyS, ModSuper)) {
		t.Error("Match should not fire for EventKeyUp")
	}
	if fired != 0 {
		t.Errorf("fired = %d, want 0", fired)
	}
}

func TestAcceleratorRegistryNilSafe(t *testing.T) {
	var r *AcceleratorRegistry
	if err := r.Register("Cmd+S", func() {}); err != nil {
		t.Errorf("nil Register: %v", err)
	}
	if r.Match(NewKeyEvent(EventKeyDown, KeyS, ModSuper)) {
		t.Error("nil Match should return false")
	}
	if r.Len() != 0 {
		t.Errorf("nil Len = %d, want 0", r.Len())
	}
}

func TestAcceleratorRegistryEmptyInputsIgnored(t *testing.T) {
	r := NewAcceleratorRegistry()
	if err := r.Register("", func() {}); err != nil {
		t.Errorf("empty shortcut Register: %v", err)
	}
	if err := r.Register("Cmd+S", nil); err != nil {
		t.Errorf("nil fn Register: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
}

func TestWindowAcceleratorFiresOnUnhandledKey(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	reg := NewAcceleratorRegistry()
	fired := 0
	_ = reg.Register("Cmd+S", func() { fired++ })
	win.SetAcceleratorRegistry(reg)

	// No root means dispatch short-circuits. Install a simple container.
	root := NewContainer(FlexLayout{Direction: Vertical})
	win.SetRoot(root)
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyS, ModSuper))
	if fired != 1 {
		t.Errorf("fired = %d, want 1", fired)
	}
}
