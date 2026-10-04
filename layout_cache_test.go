package qui

import "testing"

// The key invariants:
//   1. Only root tracks dirty; child InvalidateLayout bubbles.
//   2. ClearLayoutDirty resets the flag; repeated invalidations reset it.
//   3. Container.AddChild marks the root dirty so count changes trigger
//      re-layout.
//   4. Window.SetRoot marks layout dirty so the first frame lays out.
//   5. Window.InvalidateLayout (public API) works on the root.
//   6. Paint-only invalidation (Window.Invalidate) does NOT mark layout dirty
//      — cursor-blink-style frames skip the layout pass.

func TestInvalidateLayoutBubblesToRoot(t *testing.T) {
	leaf := newPhaseSpy("leaf", Rect{}, nil)
	mid := NewContainer(nil, leaf)
	root := NewContainer(nil, mid)

	// Initial state: root not dirty (before any invalidate).
	root.ClearLayoutDirty()
	if root.IsLayoutDirty() {
		t.Fatal("precondition: root should start clean after explicit clear")
	}

	leaf.InvalidateLayout()
	if !root.IsLayoutDirty() {
		t.Errorf("InvalidateLayout at leaf should mark root dirty")
	}
	// Child itself is not marked — we only track root.
	if leaf.IsLayoutDirty() {
		t.Errorf("leaf should not record its own dirty flag (only root matters)")
	}
	if mid.IsLayoutDirty() {
		t.Errorf("mid (non-root ancestor) should not record dirty either")
	}
}

func TestClearLayoutDirtyResets(t *testing.T) {
	root := NewContainer(nil)
	root.InvalidateLayout()
	if !root.IsLayoutDirty() {
		t.Fatal("precondition")
	}
	root.ClearLayoutDirty()
	if root.IsLayoutDirty() {
		t.Error("ClearLayoutDirty should reset the flag")
	}
}

func TestReInvalidateAfterClear(t *testing.T) {
	root := NewContainer(nil)
	root.InvalidateLayout()
	root.ClearLayoutDirty()
	root.InvalidateLayout()
	if !root.IsLayoutDirty() {
		t.Error("invalidating after clear should re-mark")
	}
}

func TestContainerAddChildMarksRootLayoutDirty(t *testing.T) {
	root := NewContainer(nil)
	root.ClearLayoutDirty()

	child := newPhaseSpy("c", Rect{}, nil)
	root.AddChild(child)

	if !root.IsLayoutDirty() {
		t.Errorf("AddChild should mark root dirty; tree structure changed")
	}
}

func TestInvalidateLayoutOnIsolatedWidgetMarksSelf(t *testing.T) {
	// Widget with no parent acts as its own root.
	solo := newPhaseSpy("s", Rect{}, nil)
	solo.InvalidateLayout()
	if !solo.IsLayoutDirty() {
		t.Error("isolated widget should mark itself on InvalidateLayout")
	}
}

func TestInvalidateLayoutIdempotent(t *testing.T) {
	// Multiple invalidations on different leaves should all end up
	// marking root exactly once (observable: it's dirty).
	leaf1 := newPhaseSpy("l1", Rect{}, nil)
	leaf2 := newPhaseSpy("l2", Rect{}, nil)
	leaf3 := newPhaseSpy("l3", Rect{}, nil)
	root := NewContainer(nil, leaf1, leaf2, leaf3)
	root.ClearLayoutDirty()

	leaf1.InvalidateLayout()
	leaf2.InvalidateLayout()
	leaf3.InvalidateLayout()

	if !root.IsLayoutDirty() {
		t.Error("root should be dirty after multiple child invalidations")
	}
}

func TestWindowSetRootMarksLayoutDirty(t *testing.T) {
	w := &Window{lastSize: Size{W: 100, H: 100}}
	root := NewContainer(nil)
	root.ClearLayoutDirty()
	w.SetRoot(root)
	if !root.IsLayoutDirty() {
		t.Error("SetRoot should mark the new root dirty")
	}
}

func TestWindowSetRootAttachesWindowToTree(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	child := newPhaseSpy("child", Rect{}, nil)
	root := NewContainer(nil, child)

	w.SetRoot(root)

	if root.Window() != w {
		t.Fatalf("root Window() = %p, want %p", root.Window(), w)
	}
	if child.Window() != w {
		t.Fatalf("child Window() = %p, want %p", child.Window(), w)
	}
}

func TestContainerAddChildAttachesMountedWindow(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	root := NewContainer(nil)
	w.SetRoot(root)
	w.dirtyRegion = Rect{}

	child := newPhaseSpy("child", Rect{}, nil)
	root.AddChild(child)

	if child.Window() != w {
		t.Fatalf("new child Window() = %p, want %p", child.Window(), w)
	}
	// The paint wake happens through the layout pass: AddChild marks the
	// root layout-dirty; Step's relayout then either moves widgets
	// (promoted to a full repaint) or leaves the scoped dirty rects.
	if !root.IsLayoutDirty() {
		t.Fatal("AddChild on a mounted tree should mark the root layout-dirty")
	}
}

func TestInvalidateLayoutOnMountedChildInvalidatesPaint(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	child := newPhaseSpy("child", Rect{X: 10, Y: 10, W: 30, H: 20}, nil)
	root := NewContainer(nil, child)
	w.SetRoot(root)
	child.Layout(Rect{X: 10, Y: 10, W: 30, H: 20})
	root.ClearLayoutDirty()
	w.dirtyRegion = Rect{}

	child.InvalidateLayout()

	if !root.IsLayoutDirty() {
		t.Fatal("child InvalidateLayout should mark root dirty")
	}
	// Repaint scoping: only the caller's own extent is dirtied — the
	// layout pass promotes to a full repaint only if something moves.
	if w.dirtyRegion.IsEmpty() {
		t.Fatal("layout invalidation on a laid-out child should dirty its rect")
	}
	if got := w.dirtyRegion; got.W > 30+1 || got.H > 20+1 {
		t.Fatalf("dirty region %+v exceeds the child's bounds — repaint not scoped", got)
	}
}

// A bounds change during the frame's layout pass flags the frame for a
// full repaint; the same change outside the pass invalidates the old and
// new extents directly (caller-positioned overlays, tests).
func TestBoundsChangePromotion(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	child := newPhaseSpy("child", Rect{}, nil)
	root := NewContainer(nil, child)
	w.SetRoot(root)
	child.Layout(Rect{X: 0, Y: 0, W: 10, H: 10})
	w.dirtyRegion = Rect{}

	// Outside a layout pass: the move dirty-rects both positions.
	child.Layout(Rect{X: 50, Y: 50, W: 10, H: 10})
	if w.boundsChanged {
		t.Fatal("out-of-pass Layout must not set the promotion flag")
	}
	if d := w.dirtyRegion; d.IsEmpty() || !d.Contains(Point{X: 5, Y: 5}) || !d.Contains(Point{X: 55, Y: 55}) {
		t.Fatalf("out-of-pass move should dirty old+new extents, got %+v", d)
	}

	// Inside the layout pass: the move sets the promotion flag instead.
	w.dirtyRegion = Rect{}
	w.inLayoutPass = true
	child.Layout(Rect{X: 20, Y: 20, W: 10, H: 10})
	w.inLayoutPass = false
	if !w.boundsChanged {
		t.Fatal("in-pass bounds change must set the promotion flag")
	}
	if !w.dirtyRegion.IsEmpty() {
		t.Fatalf("in-pass move must not touch the dirty region directly, got %+v", w.dirtyRegion)
	}

	// Same-rect layout is a no-op either way.
	w.boundsChanged = false
	w.inLayoutPass = true
	child.Layout(Rect{X: 20, Y: 20, W: 10, H: 10})
	w.inLayoutPass = false
	if w.boundsChanged {
		t.Fatal("unchanged bounds must not flag a repaint promotion")
	}
}

func TestSubscribeThemeUnsubscribe(t *testing.T) {
	oldSubscribers := themeSubscribers
	defer func() { themeSubscribers = oldSubscribers }()
	themeSubscribers = nil

	calls := 0
	unsubscribe := SubscribeTheme(func() { calls++ })
	SetTheme(currentTheme)
	if calls != 1 {
		t.Fatalf("subscriber calls after SetTheme = %d, want 1", calls)
	}

	unsubscribe()
	SetTheme(currentTheme)
	if calls != 1 {
		t.Fatalf("subscriber should not fire after unsubscribe; calls = %d", calls)
	}
}

func TestWindowInvalidateLayoutAPI(t *testing.T) {
	w := &Window{lastSize: Size{W: 100, H: 100}}
	root := NewContainer(nil)
	w.SetRoot(root)
	root.ClearLayoutDirty()

	w.InvalidateLayout()
	if !root.IsLayoutDirty() {
		t.Error("Window.InvalidateLayout should mark root dirty")
	}
}

func TestWindowInvalidateLayoutNilSafe(t *testing.T) {
	// Should not crash with nil receiver or nil root.
	var w *Window
	w.InvalidateLayout() // no-op

	w = &Window{}
	w.InvalidateLayout() // no root — no-op
}

func TestPaintInvalidateDoesNotMarkLayoutDirty(t *testing.T) {
	// The whole point of the optimization: cursor blink etc. trigger
	// Invalidate (paint-only) but must not trigger re-layout.
	w := &Window{lastSize: Size{W: 100, H: 100}}
	root := NewContainer(nil)
	w.SetRoot(root)
	root.ClearLayoutDirty()

	w.Invalidate() // paint-only invalidation

	if root.IsLayoutDirty() {
		t.Error("Window.Invalidate must not mark layout dirty — that defeats the cache")
	}
	if w.dirtyRegion.IsEmpty() {
		t.Error("Window.Invalidate should set paint dirty region")
	}
}

func TestWindowResizeInvalidatesFullNewSizeAndLayout(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	root := NewContainer(nil)
	w.SetRoot(root)
	root.ClearLayoutDirty()
	w.dirtyRegion = Rect{}

	w.resizeTo(Size{W: 160, H: 120})

	if got := w.dirtyRegion; got != (Rect{W: 160, H: 120}) {
		t.Fatalf("dirty after resize = %+v, want full new window", got)
	}
	if !root.IsLayoutDirty() {
		t.Fatal("resize should invalidate layout")
	}
}

func TestInvalidateRectDoesNotMarkLayoutDirty(t *testing.T) {
	w := &Window{lastSize: Size{W: 100, H: 100}}
	root := NewContainer(nil)
	w.SetRoot(root)
	root.ClearLayoutDirty()

	w.InvalidateRect(Rect{X: 0, Y: 0, W: 50, H: 50})

	if root.IsLayoutDirty() {
		t.Error("InvalidateRect must not mark layout dirty")
	}
}

func TestEventDispatchDoesNotMarkLayoutDirty(t *testing.T) {
	// Event dispatch itself should not force paint or layout; widgets that
	// actually change visual state are responsible for invalidating their
	// own bounds.
	root := &phaseSpyContainer{name: "r", log: &[]phaseLogEntry{}}
	root.BaseWidget = NewBaseWidget()
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	root.SetSelf(root)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.SetRoot(root)
	root.ClearLayoutDirty()
	w.dirtyRegion = Rect{}

	w.dispatch(newPhaseEvent(EventMouseMove, 50, 50))

	if root.IsLayoutDirty() {
		t.Error("dispatch marked layout dirty")
	}
	if !w.dirtyRegion.IsEmpty() {
		t.Error("dispatch without widget visual changes should not invalidate paint")
	}
}
