package qui

import "testing"

// drawSpy records whether its Draw method was invoked, and its bounds.
type drawSpy struct {
	BaseWidget
	drawn bool
}

func newDrawSpy(r Rect) *drawSpy {
	s := &drawSpy{BaseWidget: NewBaseWidget()}
	s.rect = r
	return s
}

func (s *drawSpy) Draw(_ Canvas) { s.drawn = true }

type zHitSpy struct {
	drawSpy
	z int
}

func newZHitSpy(r Rect, z int) *zHitSpy {
	s := &zHitSpy{drawSpy: *newDrawSpy(r), z: z}
	s.SetSelf(s)
	return s
}

func (s *zHitSpy) ZIndex() int { return s.z }

func TestContainerDrawSkipsSubtreeOutsideClip(t *testing.T) {
	// Two children at distinct positions. Clip intersects only the first.
	a := newDrawSpy(Rect{X: 0, Y: 0, W: 50, H: 50})
	b := newDrawSpy(Rect{X: 200, Y: 200, W: 50, H: 50})
	c := NewContainer(nil, a, b)
	c.rect = Rect{X: 0, Y: 0, W: 300, H: 300}

	clip := Rect{X: 10, Y: 10, W: 20, H: 20} // overlaps a, not b
	canvas := &RecordingCanvas{canvasState: newCanvasState(clip)}

	c.Draw(canvas)

	if !a.drawn {
		t.Error("child A intersecting clip should have Draw invoked")
	}
	if b.drawn {
		t.Error("child B outside clip should have Draw skipped")
	}
}

func TestContainerRemoveChild(t *testing.T) {
	a := newDrawSpy(Rect{X: 0, Y: 0, W: 10, H: 10})
	b := newDrawSpy(Rect{X: 0, Y: 0, W: 10, H: 10})
	c := NewContainer(nil, a, b)
	c.ClearLayoutDirty()

	c.RemoveChild(a)

	children := c.Children()
	if len(children) != 1 || children[0] != b {
		t.Fatalf("expected [b] after removal, got %v", children)
	}
	if a.Parent() != nil {
		t.Errorf("removed child should have nil parent, got %v", a.Parent())
	}
	if !c.IsLayoutDirty() {
		t.Error("RemoveChild should mark layout dirty")
	}

	// Idempotent / unknown-child no-op.
	c.ClearLayoutDirty()
	c.RemoveChild(a)
	if c.IsLayoutDirty() {
		t.Error("removing a non-member should not dirty layout")
	}
	if c.ChildCount() != 1 {
		t.Errorf("removing a non-member should not mutate children, got len=%d", c.ChildCount())
	}

	// nil is a no-op.
	c.RemoveChild(nil)
	if c.ChildCount() != 1 {
		t.Errorf("RemoveChild(nil) should be a no-op, got len=%d", c.ChildCount())
	}
}

func TestContainerDrawWithoutClipHitsAll(t *testing.T) {
	// Without a clipAware canvas, Container should not skip any child.
	a := newDrawSpy(Rect{X: 0, Y: 0, W: 50, H: 50})
	b := newDrawSpy(Rect{X: 200, Y: 200, W: 50, H: 50})
	c := NewContainer(nil, a, b)
	c.rect = Rect{X: 0, Y: 0, W: 300, H: 300}

	c.Draw(&RecordingCanvas{})

	if !a.drawn || !b.drawn {
		t.Errorf("without clip, both children should draw; a=%v b=%v", a.drawn, b.drawn)
	}
}

func TestContainerHitTestUsesReversePaintOrder(t *testing.T) {
	front := newZHitSpy(Rect{X: 0, Y: 0, W: 50, H: 50}, 10)
	back := newZHitSpy(Rect{X: 0, Y: 0, W: 50, H: 50}, 0)
	c := NewContainer(nil, front, back)
	c.rect = Rect{X: 0, Y: 0, W: 50, H: 50}

	if got := c.HitTest(Point{X: 10, Y: 10}); got != front {
		t.Fatalf("HitTest = %T %p, want highest-z child %p", got, got, front)
	}
}

func TestNewContainerSetsSelfByDefault(t *testing.T) {
	c := NewContainer(nil)
	if c.Self() != c {
		t.Fatalf("NewContainer Self = %v, want container", c.Self())
	}
}

type containerWrapper struct{ *Container }

func TestContainerSetSelfRepairsExistingChildParent(t *testing.T) {
	child := newDrawSpy(Rect{})
	inner := NewContainer(nil, child)
	wrapper := &containerWrapper{Container: inner}
	wrapper.SetSelf(wrapper)

	if child.Parent() != wrapper {
		t.Fatalf("child.Parent = %v, want wrapper", child.Parent())
	}
}

func TestContainerChildrenReturnsImmutableSnapshot(t *testing.T) {
	a := newDrawSpy(Rect{})
	b := newDrawSpy(Rect{})
	c := NewContainer(nil, a, b)

	snapshot := c.Children()
	snapshot[0] = b
	snapshot = snapshot[:1]

	if c.ChildCount() != 2 || c.ChildAt(0) != a || c.ChildAt(1) != b {
		t.Fatalf("mutating snapshot changed container children: %v", c.Children())
	}
}

func TestContainerSetChildrenDetachesOnlyRemovedSubtrees(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	c := newFocusableSpy("c")
	root := NewContainer(nil, a, b)
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)
	w.SetFocus(a)

	root.SetChildren(a, c)

	if w.Focused() != a || !a.focused {
		t.Fatal("retained focused child lost focus during SetChildren")
	}
	if a.Parent() != root || a.Window() != w {
		t.Fatalf("retained child wiring changed: parent=%v window=%v", a.Parent(), a.Window())
	}
	if b.Parent() != nil || b.Window() != nil {
		t.Fatalf("removed child remained attached: parent=%v window=%v", b.Parent(), b.Window())
	}
	if c.Parent() != root || c.Window() != w {
		t.Fatalf("new child not attached: parent=%v window=%v", c.Parent(), c.Window())
	}
}

func TestContainerAddChildReparentsFromOldContainer(t *testing.T) {
	child := newDrawSpy(Rect{})
	oldParent := NewContainer(nil, child)
	newParent := NewContainer(nil)

	newParent.AddChild(child)

	if oldParent.ChildCount() != 0 {
		t.Fatalf("old parent still contains child: %v", oldParent.Children())
	}
	if newParent.ChildCount() != 1 || newParent.ChildAt(0) != child || child.Parent() != newParent {
		t.Fatalf("reparent failed: children=%v parent=%v", newParent.Children(), child.Parent())
	}
}
