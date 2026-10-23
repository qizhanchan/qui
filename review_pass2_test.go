package qui

import "testing"

// CSS flex grow: a grow-factor sum below 1 hands out only that fraction of
// the free space, and an item clamped at its max size gives its surplus
// to its siblings.
func TestFlexGrowFractionAndMaxFreeze(t *testing.T) {
	a := NewBaseWidget()
	a.SetFlexItem(FlexItem{Grow: 0.5})
	row := NewContainer(FlexLayout{Direction: Horizontal}, &a)
	row.Layout(Rect{W: 400, H: 20})
	full := NewBaseWidget()
	full.SetFlexItem(FlexItem{Grow: 1})
	NewContainer(FlexLayout{Direction: Horizontal}, &full).Layout(Rect{W: 400, H: 20})
	if got, want := a.Bounds().W, full.Bounds().W/2; got != want {
		t.Fatalf("grow 0.5: width %v, want %v (half the free space)", got, want)
	}

	b, c := NewBaseWidget(), NewBaseWidget()
	b.SetFlexItem(FlexItem{Grow: 1})
	b.Style().MaxWidth = 50
	c.SetFlexItem(FlexItem{Grow: 1})
	row2 := NewContainer(FlexLayout{Direction: Horizontal}, &b, &c)
	row2.Layout(Rect{W: 400, H: 20})
	// MaxWidth is UI-scaled; whatever it resolves to, the sibling gets
	// the rest.
	if bw, cw := b.Bounds().W, c.Bounds().W; bw > 50 || bw+cw != full.Bounds().W {
		t.Fatalf("max-clamped sibling: widths %v / %v, want ≤50 and the rest", bw, cw)
	}
}

// A blur hook that removes the focused widget blurs it once, not twice.
func TestBlurHookRemovingWidgetBlursOnce(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 100})
	field := newFocusLeaf()
	other := newFocusLeaf()
	root := NewContainer(AbsoluteLayout{}, field, other)
	win.SetRoot(root)
	blurs := 0
	field.OnBlur(func() {
		blurs++
		root.RemoveChild(field) // inline edit: commit and remove on blur
	})
	win.SetFocus(field)
	win.SetFocus(other)
	if blurs != 1 {
		t.Fatalf("OnBlur ran %d times, want 1", blurs)
	}
	if win.Focused() != Widget(other) {
		t.Fatalf("focus = %v, want the other field", win.Focused())
	}
}

type focusLeaf struct{ BaseWidget }

func newFocusLeaf() *focusLeaf {
	f := &focusLeaf{BaseWidget: NewBaseWidget()}
	f.SetSelf(f)
	return f
}
func (f *focusLeaf) Focusable() bool   { return true }
func (f *focusLeaf) SetFocused(bool)   {}
func (f *focusLeaf) Handle(Event) bool { return false }

// A drop handler that moves the drag source ("remove the card here, add it
// there") ends the drag normally instead of panicking.
func TestDropHandlerMovingSourceEndsDrag(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	src := newDnd(Rect{X: 0, Y: 0, W: 50, H: 50})
	src.data = &DragData{}
	src.data.Set("application/x-card", 1)
	from := NewContainer(AbsoluteLayout{}, src)
	to := NewContainer(AbsoluteLayout{})
	target := &movingDrop{dndWidget: newDnd(Rect{X: 200, Y: 0, W: 50, H: 50})}
	target.SetSelf(target)
	target.accept = func(*DragData) bool { return true }
	target.move = func() { from.RemoveChild(src); to.AddChild(src) }
	root := NewContainer(AbsoluteLayout{}, from, to, target)
	win.SetRoot(root)
	root.Layout(Rect{W: 300, H: 100})
	from.Layout(Rect{W: 100, H: 100})
	to.Layout(Rect{X: 100, W: 100, H: 100})
	src.Layout(Rect{W: 50, H: 50})
	target.Layout(Rect{X: 200, Y: 0, W: 50, H: 50})

	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, 10, 10, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseMove, 220, 10, 0, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, 220, 10, MouseButtonLeft, 0))
	if !src.ended.Accepted {
		t.Fatal("DragEnd should report the accepted drop")
	}
	if _, dragging := win.DragInProgress(); dragging {
		t.Fatal("drag still in progress")
	}
}

type movingDrop struct {
	*dndWidget
	move func()
}

func (m *movingDrop) Handle(e Event) bool {
	if de, ok := e.(DragEvent); ok && de.Type() == EventDrop {
		m.move()
	}
	return m.dndWidget.Handle(e)
}
