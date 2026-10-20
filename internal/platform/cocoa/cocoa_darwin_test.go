//go:build darwin && cgo

package cocoa

import (
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// The keycode table and the enum translations are the parts of the Cocoa
// backend that can be wrong without crashing — a mis-mapped key just makes
// one shortcut mysteriously dead. They're pure data, so test them directly
// rather than through a window.

func TestCocoaBackendIsRegistered(t *testing.T) {
	if _, ok := contract.Lookup("cocoa"); !ok {
		t.Fatalf("cocoa backend not registered; available: %v", contract.Names())
	}
}

func TestCocoaCursorShapesAreDistinct(t *testing.T) {
	// The .m switches on these integers; two shapes collapsing onto one
	// value would silently show the wrong cursor.
	shapes := []contract.CursorShape{
		contract.CursorDefault, contract.CursorText, contract.CursorCrosshair,
		contract.CursorHand, contract.CursorResizeEW, contract.CursorResizeNS,
	}
	seen := map[int]contract.CursorShape{}
	for _, shape := range shapes {
		code := cocoaCursorShape(shape)
		if prev, dup := seen[code]; dup {
			t.Errorf("cursor shapes %v and %v both map to %d", prev, shape, code)
		}
		seen[code] = shape
	}
}

func TestCocoaGestureKindDecoding(t *testing.T) {
	cases := map[int]contract.GestureKind{
		0: contract.GesturePinch,
		1: contract.GestureRotate,
		2: contract.GestureSmartMagnify,
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
	caps := (&cocoaWindow{}).Caps()
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
	if !w.ShouldClose() {
		t.Error("a destroyed window must report shouldClose")
	}
}

// Drop paths accumulate across per-file callbacks and must be handed over
// exactly once — a leftover list would attach the previous drag's files to
// the next drop.
func TestCocoaDropPathsAreConsumedOnCommit(t *testing.T) {
	w := &cocoaWindow{}
	probe := &dropProbe{}
	w.SetHandler(probe)

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

func (p *dropProbe) OnFileDrop(paths []string, x, y float64) {
	p.paths = append(p.paths, paths...)
	p.x, p.y = x, y
}

// silentHandler satisfies platformHandler by ignoring everything.
type silentHandler struct{}

func (silentHandler) OnMouseMove(float64, float64, contract.Modifiers)             {}
func (silentHandler) OnMouseButton(contract.MouseButton, bool, contract.Modifiers) {}
func (silentHandler) OnScroll(float64, float64, float64, float64, contract.Modifiers, contract.GesturePhase, bool) {
}
func (silentHandler) OnGesture(contract.GestureKind, contract.GesturePhase, float32, float32, contract.Modifiers) {
}
func (silentHandler) OnKey(contract.Key, int, bool, bool, contract.Modifiers) {}
func (silentHandler) OnChar(rune)                                             {}
func (silentHandler) OnFocus(bool)                                            {}
func (silentHandler) OnResize(int, int)                                       {}
func (silentHandler) OnFramebufferResize(int, int)                            {}
func (silentHandler) OnRefresh()                                              {}
func (silentHandler) OnFileDrop([]string, float64, float64)                   {}
func (silentHandler) OnPreedit(string, int)                                   {}
func (silentHandler) OnCommitText(string)                                     {}
func (silentHandler) IMECaretRect() (float64, float64, float64, float64, bool) {
	return 0, 0, 0, 0, false
}
