package qui

import "testing"

func TestPhysicalScissorIdentity(t *testing.T) {
	state := GLState{
		FramebufferSize: Size{W: 800, H: 600},
		LogicalSize:     Size{W: 800, H: 600},
	}
	clip := Rect{X: 100, Y: 50, W: 200, H: 100}
	x, y, w, h, ok := PhysicalScissor(state, clip)
	if !ok {
		t.Fatal("ok = false; want true")
	}
	if x != 100 || w != 200 || h != 100 {
		t.Errorf("got (%d,%d,%d,%d); want x=100 w=200 h=100", x, y, w, h)
	}
	// Y-flip: top-origin (50) with height 100 in a 600px framebuffer
	// becomes bottom-origin 600 - (50 + 100) = 450.
	if y != 450 {
		t.Errorf("y = %d; want 450 (Y-flipped from top-origin 50)", y)
	}
}

func TestPhysicalScissorHiDPI(t *testing.T) {
	// 2x DPR: 800x600 logical → 1600x1200 physical.
	state := GLState{
		FramebufferSize: Size{W: 1600, H: 1200},
		LogicalSize:     Size{W: 800, H: 600},
	}
	clip := Rect{X: 100, Y: 50, W: 200, H: 100}
	x, y, w, h, ok := PhysicalScissor(state, clip)
	if !ok {
		t.Fatal("ok = false; want true")
	}
	if x != 200 || w != 400 || h != 200 {
		t.Errorf("got (%d,%d,%d,%d); want x=200 w=400 h=200", x, y, w, h)
	}
	// Y-flip in physical space: 1200 - (100 + 200) = 900.
	if y != 900 {
		t.Errorf("y = %d; want 900", y)
	}
}

func TestPhysicalScissorEmptyClip(t *testing.T) {
	state := GLState{
		FramebufferSize: Size{W: 800, H: 600},
		LogicalSize:     Size{W: 800, H: 600},
	}
	if _, _, _, _, ok := PhysicalScissor(state, Rect{}); ok {
		t.Error("empty clip should return ok=false")
	}
	if _, _, _, _, ok := PhysicalScissor(state, Rect{X: 10, Y: 10, W: 0, H: 100}); ok {
		t.Error("zero-width clip should return ok=false")
	}
	if _, _, _, _, ok := PhysicalScissor(state, Rect{X: 10, Y: 10, W: 100, H: -1}); ok {
		t.Error("negative-height clip should return ok=false")
	}
}

func TestPhysicalScissorZeroLogicalSize(t *testing.T) {
	// Defensive: if LogicalSize is zero (renderer not initialized
	// for the frame), fall back to 1x scale rather than dividing by zero.
	state := GLState{
		FramebufferSize: Size{W: 800, H: 600},
		LogicalSize:     Size{},
	}
	clip := Rect{X: 0, Y: 0, W: 100, H: 100}
	x, _, w, h, ok := PhysicalScissor(state, clip)
	if !ok {
		t.Fatal("ok = false; want true")
	}
	if x != 0 || w != 100 || h != 100 {
		t.Errorf("got (%d,_,%d,%d); want x=0 w=100 h=100", x, w, h)
	}
}
