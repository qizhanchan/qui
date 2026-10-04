//go:build darwin && cgo

package qui

import "testing"

// The keycode table and the enum translations are the parts of the Cocoa
// backend that can be wrong without crashing — a mis-mapped key just makes
// one shortcut mysteriously dead. They're pure data, so test them directly
// rather than through a window.

func TestCocoaBackendIsRegistered(t *testing.T) {
	if _, ok := platformBackends["cocoa"]; !ok {
		t.Fatalf("cocoa backend not registered; available: %v", availablePlatformBackends())
	}
}

func TestCocoaKeyTableCoversTheShortcutAlphabet(t *testing.T) {
	// These are the keycodes qui's own shortcuts depend on. A regression
	// here breaks Cmd+S / Cmd+Z / Cmd+C-V-X and the arrow keys — the
	// difference between "usable" and "looks fine, does nothing".
	want := map[int]Key{
		0x00: KeyA, 0x01: KeyS, 0x06: KeyZ, 0x07: KeyX,
		0x08: KeyC, 0x09: KeyV, 0x0C: KeyQ,
		0x24: KeyEnter, 0x30: KeyTab, 0x31: KeySpace,
		0x33: KeyBackspace, 0x35: KeyEscape, 0x75: KeyDelete,
		0x7B: KeyLeft, 0x7C: KeyRight, 0x7D: KeyDown, 0x7E: KeyUp,
	}
	for code, key := range want {
		if got := mapCocoaKey(code); got != key {
			t.Errorf("keycode 0x%02X mapped to %v, want %v", code, got, key)
		}
	}
}

func TestCocoaKeyTableIsInjectiveExceptForEnter(t *testing.T) {
	// Two keycodes mapping to the same Key is almost always a copy-paste
	// slip in a 70-entry table. The one legitimate collision is Return and
	// keypad Enter, which qui deliberately does not distinguish.
	seen := map[Key][]int{}
	for code, key := range cocoaKeyCodes {
		seen[key] = append(seen[key], code)
	}
	for key, codes := range seen {
		if len(codes) == 1 {
			continue
		}
		if key == KeyEnter && len(codes) == 2 {
			continue // Return + keypad Enter
		}
		t.Errorf("%v is produced by %d keycodes %#x; expected exactly one", key, len(codes), codes)
	}
}

func TestCocoaKeyTableRejectsUnknownCodes(t *testing.T) {
	// 0x3B is Left Control — a modifier. Modifiers arrive via the mods
	// bitmask, not as keys, and must not be mistaken for a character key.
	for _, code := range []int{0x3B, 0x38, 0x37, 0x3A, 0xFF, -1} {
		if got := mapCocoaKey(code); got != KeyUnknown {
			t.Errorf("keycode 0x%02X mapped to %v, want KeyUnknown", code, got)
		}
	}
}

func TestCocoaCursorShapesAreDistinct(t *testing.T) {
	// The .m switches on these integers; two shapes collapsing onto one
	// value would silently show the wrong cursor.
	shapes := []CursorShape{
		CursorDefault, CursorText, CursorCrosshair,
		CursorHand, CursorResizeEW, CursorResizeNS,
	}
	seen := map[int]CursorShape{}
	for _, shape := range shapes {
		code := cocoaCursorShape(shape)
		if prev, dup := seen[code]; dup {
			t.Errorf("cursor shapes %v and %v both map to %d", prev, shape, code)
		}
		seen[code] = shape
	}
}

func TestCocoaGestureKindDecoding(t *testing.T) {
	cases := map[int]EventType{
		0: EventGesturePinch,
		1: EventGestureRotate,
		2: EventGestureSmartMagnify,
	}
	for code, want := range cases {
		if got := cocoaGestureKind(code); got != want {
			t.Errorf("gesture kind %d decoded to %v, want %v", code, got, want)
		}
	}
}

func TestCocoaCapsReportNativeGestures(t *testing.T) {
	// The Cocoa backend implements magnifyWithEvent: directly, so unlike
	// the GLFW backend this is not conditional on a swizzle landing.
	caps := (&cocoaWindow{}).caps()
	if !caps.NativeGestures {
		t.Error("cocoa backend should report NativeGestures")
	}
	if !caps.AbsolutePosition {
		t.Error("macOS has global window coordinates")
	}
	if caps.ClipboardNeedsInputSerial {
		t.Error("NSPasteboard needs no input serial; that is a Wayland constraint")
	}
}

// A window whose destroy has run must report shouldClose even though its C
// side is gone: the main loop polls shouldClose to decide when to reap, and
// a destroyed window answering "no" would keep the app alive forever.
func TestDestroyedCocoaWindowReportsShouldClose(t *testing.T) {
	w := &cocoaWindow{destroyed: true}
	if !w.shouldClose() {
		t.Error("a destroyed window must report shouldClose")
	}
}

// Drop paths accumulate across per-file callbacks and must be handed over
// exactly once — a leftover list would attach the previous drag's files to
// the next drop.
func TestCocoaDropPathsAreConsumedOnCommit(t *testing.T) {
	w := &cocoaWindow{}
	probe := &dropProbe{}
	w.setHandler(probe)

	w.noteDropPath("/tmp/a.txt")
	w.noteDropPath("/tmp/b.txt")
	w.commitDrop(12, 34)

	if len(probe.paths) != 2 || probe.paths[0] != "/tmp/a.txt" || probe.paths[1] != "/tmp/b.txt" {
		t.Fatalf("drop delivered %q, want both paths in order", probe.paths)
	}
	if probe.x != 12 || probe.y != 34 {
		t.Errorf("drop point = (%v,%v), want (12,34)", probe.x, probe.y)
	}
	if w.dropPaths != nil {
		t.Errorf("dropPaths should be cleared after commit, still holds %q", w.dropPaths)
	}

	// A second commit with nothing pending must not re-deliver.
	probe.paths = nil
	w.commitDrop(1, 2)
	if probe.paths != nil {
		t.Errorf("empty commit re-delivered %q", probe.paths)
	}
}

// dropProbe records the one handler call this test cares about. It embeds
// silentHandler so adding a method to platformHandler doesn't break it.
type dropProbe struct {
	silentHandler
	paths []string
	x, y  float64
}

func (p *dropProbe) onFileDrop(paths []string, x, y float64) {
	p.paths = append(p.paths, paths...)
	p.x, p.y = x, y
}

// silentHandler satisfies platformHandler by ignoring everything.
type silentHandler struct{}

func (silentHandler) onMouseMove(float64, float64, Modifiers)                              {}
func (silentHandler) onMouseButton(MouseButton, bool, Modifiers)                           {}
func (silentHandler) onScroll(float64, float64, float64, float64, Modifiers, GesturePhase) {}
func (silentHandler) onGesture(EventType, GesturePhase, float32, float32, Modifiers)       {}
func (silentHandler) onKey(Key, int, bool, bool, Modifiers)                                {}
func (silentHandler) onChar(rune)                                                          {}
func (silentHandler) onFocus(bool)                                                         {}
func (silentHandler) onResize(int, int)                                                    {}
func (silentHandler) onFramebufferResize(int, int)                                         {}
func (silentHandler) onRefresh()                                                           {}
func (silentHandler) onFileDrop([]string, float64, float64)                                {}
