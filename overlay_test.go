package qui

import (
	"testing"
)

func TestPushPopOverlay(t *testing.T) {
	w := &Window{lastSize: Size{W: 100, H: 100}}
	a := newPhaseSpy("a", Rect{X: 10, Y: 10, W: 20, H: 20}, nil)
	b := newPhaseSpy("b", Rect{X: 40, Y: 40, W: 20, H: 20}, nil)

	w.PushOverlay(a)
	w.PushOverlay(b)
	if len(w.overlays) != 2 {
		t.Fatalf("expected 2 overlays, got %d", len(w.overlays))
	}
	if w.overlays[0] != a || w.overlays[1] != b {
		t.Errorf("overlay stack order wrong: %+v", w.overlays)
	}

	top := w.PopOverlay()
	if top != b {
		t.Errorf("PopOverlay returned %v, want b", top)
	}
	if len(w.overlays) != 1 {
		t.Errorf("after pop, expected 1 overlay, got %d", len(w.overlays))
	}
}

func TestPopOverlayEmptyIsSafe(t *testing.T) {
	w := &Window{lastSize: Size{W: 100, H: 100}}
	if got := w.PopOverlay(); got != nil {
		t.Errorf("PopOverlay on empty stack should return nil, got %v", got)
	}
}

func TestRemoveOverlayFindsAndErases(t *testing.T) {
	w := &Window{lastSize: Size{W: 100, H: 100}}
	a := newPhaseSpy("a", Rect{X: 0, Y: 0, W: 10, H: 10}, nil)
	b := newPhaseSpy("b", Rect{X: 20, Y: 20, W: 10, H: 10}, nil)
	c := newPhaseSpy("c", Rect{X: 40, Y: 40, W: 10, H: 10}, nil)
	w.PushOverlay(a)
	w.PushOverlay(b)
	w.PushOverlay(c)

	if !w.RemoveOverlay(b) {
		t.Error("RemoveOverlay(b) returned false")
	}
	if len(w.overlays) != 2 || w.overlays[0] != a || w.overlays[1] != c {
		t.Errorf("stack after removing b: %+v, want [a, c]", w.overlays)
	}
	if w.RemoveOverlay(b) {
		t.Error("RemoveOverlay(b) second time should return false")
	}
}

func TestHitTestAllPrefersTopmostOverlay(t *testing.T) {
	// Main tree has a widget at (0,0,100,100). Overlay at same spot.
	// Click inside the overlay should hit the overlay, not the main.
	log := []phaseLogEntry{}
	main := newPhaseSpy("main", Rect{X: 0, Y: 0, W: 100, H: 100}, &log)
	root := NewContainer(nil, main)
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}

	ov := newPhaseSpy("overlay", Rect{X: 20, Y: 20, W: 40, H: 40}, &log)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.PushOverlay(ov)

	hit := w.hitTestAll(Point{X: 30, Y: 30})
	if hit != ov {
		t.Errorf("hit at (30,30) should prefer overlay; got %v", hit)
	}

	// Click outside overlay falls through to main.
	hit = w.hitTestAll(Point{X: 5, Y: 5})
	if hit != main {
		t.Errorf("hit at (5,5) should fall through to main; got %v", hit)
	}
}

func TestMouseDownDispatchesToOverlay(t *testing.T) {
	log := []phaseLogEntry{}
	main := newPhaseSpy("main", Rect{X: 0, Y: 0, W: 100, H: 100}, &log)
	root := NewContainer(nil, main)
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	ov := newPhaseSpy("overlay", Rect{X: 20, Y: 20, W: 40, H: 40}, &log)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.PushOverlay(ov)

	// Click inside overlay — the overlay logs, main should NOT.
	w.dispatch(newPhaseEvent(EventMouseDown, 30, 30))

	hitMain := false
	hitOverlay := false
	for _, l := range log {
		if l.name == "main" {
			hitMain = true
		}
		if l.name == "overlay" {
			hitOverlay = true
		}
	}
	if !hitOverlay {
		t.Errorf("overlay should have received MouseDown; log=%+v", log)
	}
	if hitMain {
		t.Errorf("main should NOT have received MouseDown through overlay; log=%+v", log)
	}
}

func TestOverlayFocusableIncludedInTabCycle(t *testing.T) {
	mainFocusable := newFocusableSpy("main")
	overlayFocusable := newFocusableSpy("overlay")

	root := NewContainer(nil, mainFocusable)
	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.PushOverlay(overlayFocusable)

	list := w.CollectFocusables()
	if len(list) != 2 {
		t.Fatalf("expected 2 focusables (main + overlay), got %d", len(list))
	}
	// Main comes first (DFS), overlay second.
	if list[0] != mainFocusable || list[1] != overlayFocusable {
		t.Errorf("focus order wrong: want [main, overlay], got %+v", list)
	}
}

func TestPushOverlayInvalidatesBounds(t *testing.T) {
	w := &Window{lastSize: Size{W: 200, H: 200}}
	ov := newPhaseSpy("ov", Rect{X: 30, Y: 40, W: 50, H: 60}, nil)
	w.PushOverlay(ov)

	// After pushing, the overlay's bounds must be in the dirty region.
	if !w.dirtyRegion.Intersects(ov.Bounds()) {
		t.Errorf("push did not invalidate overlay bounds; dirty=%+v bounds=%+v",
			w.dirtyRegion, ov.Bounds())
	}
}

func TestPopOverlayInvalidatesBoundsForErase(t *testing.T) {
	w := &Window{lastSize: Size{W: 200, H: 200}}
	ov := newPhaseSpy("ov", Rect{X: 30, Y: 40, W: 50, H: 60}, nil)
	w.PushOverlay(ov)
	w.dirtyRegion = Rect{} // clear so we observe only the pop's invalidation

	w.PopOverlay()
	if !w.dirtyRegion.Intersects(ov.Bounds()) {
		t.Errorf("pop did not invalidate overlay bounds for erase; dirty=%+v",
			w.dirtyRegion)
	}
}

func TestOverlayRemovalRestoresPreviousFocus(t *testing.T) {
	main := newFocusableSpy("main")
	root := NewContainer(nil, main)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(root)
	w.focusVisible = true
	w.SetFocus(main)

	overlayFocus := newFocusableSpy("overlay")
	overlay := NewContainer(nil, overlayFocus)
	overlay.SetSelf(overlay)
	w.PushOverlay(overlay)
	w.focusVisible = false
	w.SetFocus(overlayFocus)

	w.PopOverlay()

	if w.Focused() != main || !main.focused || overlayFocus.focused {
		t.Fatalf("focus after pop: window=%v main=%v overlay=%v", w.Focused(), main.focused, overlayFocus.focused)
	}
	if !w.focusVisible {
		t.Fatal("focus-visible state was not restored with the previous focus")
	}
}

func TestRemovingUnfocusedOverlayDoesNotStealFocus(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	root := NewContainer(nil, a, b)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(root)
	w.SetFocus(a)

	overlay := NewContainer(nil, newFocusableSpy("overlay"))
	overlay.SetSelf(overlay)
	w.PushOverlay(overlay)
	w.SetFocus(b)
	w.RemoveOverlay(overlay)

	if w.Focused() != b || !b.focused {
		t.Fatalf("removing unfocused overlay changed focus to %v", w.Focused())
	}
}

func TestRemovingMiddleOverlayRetargetsNestedFocusRestore(t *testing.T) {
	main := newFocusableSpy("main")
	root := NewContainer(nil, main)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(root)
	w.SetFocus(main)

	one := newFocusableSpy("one")
	overlayOne := NewContainer(nil, one)
	overlayOne.SetSelf(overlayOne)
	w.PushOverlay(overlayOne)
	w.SetFocus(one)

	two := newFocusableSpy("two")
	overlayTwo := NewContainer(nil, two)
	overlayTwo.SetSelf(overlayTwo)
	w.PushOverlay(overlayTwo)
	w.SetFocus(two)

	w.RemoveOverlay(overlayOne)
	if w.Focused() != two {
		t.Fatalf("removing lower overlay changed top focus to %v", w.Focused())
	}
	w.RemoveOverlay(overlayTwo)
	if w.Focused() != main || !main.focused {
		t.Fatalf("nested restore ended at %v, want main", w.Focused())
	}
}

func TestOverlayRestoreTargetDetachedBeforeCloseFallsBackToNil(t *testing.T) {
	main := newFocusableSpy("main")
	root := NewContainer(nil, main)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(root)
	w.SetFocus(main)

	overlayFocus := newFocusableSpy("overlay")
	overlay := NewContainer(nil, overlayFocus)
	overlay.SetSelf(overlay)
	w.PushOverlay(overlay)
	w.SetFocus(overlayFocus)
	root.RemoveChild(main)

	w.RemoveOverlay(overlay)

	if w.Focused() != nil {
		t.Fatalf("focus restored to detached target: %v", w.Focused())
	}
}

func TestOverlayRestoresAfterFocusedChildUnmountsFirst(t *testing.T) {
	main := newFocusableSpy("main")
	root := NewContainer(nil, main)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(root)
	w.SetFocus(main)

	overlayFocus := newFocusableSpy("overlay")
	overlay := NewContainer(nil, overlayFocus)
	overlay.SetSelf(overlay)
	w.PushOverlay(overlay)
	w.SetFocus(overlayFocus)

	overlay.RemoveChild(overlayFocus)
	if w.Focused() != nil {
		t.Fatalf("focus after child unmount = %v, want nil until scope closes", w.Focused())
	}
	w.RemoveOverlay(overlay)

	if w.Focused() != main || !main.focused {
		t.Fatalf("focus after scope close = %v, want main", w.Focused())
	}
}
