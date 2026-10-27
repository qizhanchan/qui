package qui

import "testing"

// hitProbe is a leaf that reports itself when hit, so the test observes
// which widget the window would target.
type hitProbe struct {
	BaseWidget
	name string
}

func (h *hitProbe) Measure(Size) Size { return Size{W: 100, H: 100} }
func (h *hitProbe) Draw(Canvas)       {}
func (h *hitProbe) HitTest(p Point) Widget {
	if h.Bounds().Contains(p) {
		return h
	}
	return nil
}

// A pointer-transparent widget on top must not become the target; the widget
// painted beneath it gets the hit instead.
func TestPointerTransparentFallsThrough(t *testing.T) {
	win := NewTestWindow(Size{W: 100, H: 100})
	under := &hitProbe{name: "under"}
	under.SetSelf(under)
	over := &hitProbe{name: "over"}
	over.SetSelf(over)
	over.SetPointerTransparent(true)
	// AbsoluteLayout stacks both at the same spot, `over` last (on top).
	root := NewContainer(&AbsoluteLayout{}, under, over)
	win.SetRoot(root)
	root.Layout(Rect{W: 100, H: 100})

	hit := win.HitTestForTest(Point{X: 50, Y: 50})
	if hit == Widget(over) {
		t.Fatal("transparent widget became the hit target")
	}
	if hit != Widget(under) {
		t.Fatalf("hit = %T(%v), want the widget underneath", hit, hit)
	}
}

// A transparent CONTAINER still lets its opaque children be hit (that is
// what makes `pointer-events: auto` on a descendant work), while the
// container itself declines to be the target.
func TestPointerTransparentContainerKeepsChildrenHittable(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 100})
	child := &hitProbe{name: "child"}
	child.SetSelf(child)
	outer := NewContainer(&FlexLayout{Direction: Horizontal}, child)
	outer.SetPointerTransparent(true)
	win.SetRoot(outer)
	outer.Layout(Rect{W: 200, H: 100})

	b := child.Bounds()
	if hit := win.HitTestForTest(Point{X: b.X + 5, Y: b.Y + 5}); hit != Widget(child) {
		t.Fatalf("child of a transparent container not hittable: got %T", hit)
	}
	// A point inside the container but outside the child hits nothing.
	if hit := win.HitTestForTest(Point{X: 195, Y: 95}); hit != nil {
		t.Fatalf("transparent container answered for its own area: %T", hit)
	}
}
