package qui

import "testing"

// cursorProbe is a leaf widget that can play either tier of the resolution
// rule: a declared shape (via SetCursorShape) and/or a built-in one.
type cursorProbe struct {
	BaseWidget
	builtIn    CursorShape
	hasBuiltIn bool
	perPoint   bool // publish CursorAt instead of WidgetCursor
}

func (c *cursorProbe) Measure(Size) Size { return Size{W: 100, H: 100} }
func (c *cursorProbe) Draw(Canvas)       {}
func (c *cursorProbe) HitTest(p Point) Widget {
	if c.Bounds().Contains(p) {
		return c
	}
	return nil
}

func (c *cursorProbe) WidgetCursor() (CursorShape, bool) {
	if c.perPoint {
		return CursorDefault, false
	}
	return c.builtIn, c.hasBuiltIn
}

func (c *cursorProbe) CursorAt(p Point) (CursorShape, bool) {
	if !c.perPoint {
		return CursorDefault, false
	}
	// Left half claims a hand (stands in for a hyperlink run), right half
	// declines — exercises "same widget, different answer per point".
	if p.X < c.Bounds().X+c.Bounds().W/2 {
		return CursorHand, true
	}
	return CursorDefault, false
}

// hoverAt drives the real hover pipeline so the test exercises
// syncHoverPath → updateCursorFromHover, not just resolveCursor.
func hoverAt(w *Window, x, y float32) {
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, x, y, MouseButtonLeft, 0))
}

// TestCursorDeclaredBeatsBuiltIn is the rule CSS `cursor` depends on: a
// declared shape on an ancestor outranks a descendant's built-in shape, no
// matter how much deeper the descendant sits. Without this, a
// `cursor: pointer` card would show the I-beam of the label inside it.
func TestCursorDeclaredBeatsBuiltIn(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	leaf := &cursorProbe{builtIn: CursorText, hasBuiltIn: true}
	leaf.SetSelf(leaf)
	outer := NewContainer(&FlexLayout{Direction: Vertical}, leaf)
	outer.SetCursorShape(CursorHand)
	win.SetRoot(outer)
	outer.Layout(Rect{W: 200, H: 200})

	hoverAt(win, 10, 10)
	if got := win.Cursor(); got != CursorHand {
		t.Fatalf("declared ancestor cursor should win: got %v want CursorHand", got)
	}

	// Dropping the declaration hands the decision back to the leaf.
	outer.ClearCursorShape()
	win.SetCursor(CursorDefault)
	hoverAt(win, 11, 11)
	if got := win.Cursor(); got != CursorText {
		t.Fatalf("after ClearCursorShape the built-in should answer: got %v want CursorText", got)
	}
}

// TestCursorInnermostDeclarationWins covers the nesting case: a child that
// declares its own cursor overrides the declaring ancestor.
func TestCursorInnermostDeclarationWins(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	leaf := &cursorProbe{}
	leaf.SetSelf(leaf)
	leaf.SetCursorShape(CursorText)
	outer := NewContainer(&FlexLayout{Direction: Vertical}, leaf)
	outer.SetCursorShape(CursorHand)
	win.SetRoot(outer)
	outer.Layout(Rect{W: 200, H: 200})

	hoverAt(win, 10, 10)
	if got := win.Cursor(); got != CursorText {
		t.Fatalf("innermost declaration should win: got %v want CursorText", got)
	}
}

// TestCursorPerPointBeatsWholeWidget: CursorAt is more specific than
// WidgetCursor, and declining per point falls through to the default.
func TestCursorPerPointBeatsWholeWidget(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	leaf := &cursorProbe{perPoint: true}
	leaf.SetSelf(leaf)
	root := NewContainer(&FlexLayout{Direction: Vertical}, leaf)
	win.SetRoot(root)
	root.Layout(Rect{W: 200, H: 200})

	b := leaf.Bounds()
	hoverAt(win, b.X+b.W*0.25, b.Y+5) // left half → hand
	if got := win.Cursor(); got != CursorHand {
		t.Fatalf("per-point hand: got %v want CursorHand", got)
	}
	hoverAt(win, b.X+b.W*0.75, b.Y+5) // right half → declines → default
	if got := win.Cursor(); got != CursorDefault {
		t.Fatalf("per-point decline should reset: got %v want CursorDefault", got)
	}
}

// TestCursorResetsWhenNothingClaims guards the regression the old
// imperative model kept hitting: an I-beam (or hand) set over one widget
// sticking window-wide after the pointer moved off it.
func TestCursorResetsWhenNothingClaims(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	claim := &cursorProbe{builtIn: CursorHand, hasBuiltIn: true}
	claim.SetSelf(claim)
	bare := &cursorProbe{}
	bare.SetSelf(bare)
	root := NewContainer(&FlexLayout{Direction: Horizontal}, claim, bare)
	win.SetRoot(root)
	root.Layout(Rect{W: 300, H: 100})

	hoverAt(win, claim.Bounds().X+5, 10)
	if got := win.Cursor(); got != CursorHand {
		t.Fatalf("over the claiming widget: got %v want CursorHand", got)
	}
	hoverAt(win, bare.Bounds().X+5, 10)
	if got := win.Cursor(); got != CursorDefault {
		t.Fatalf("cursor stuck after leaving the claiming widget: got %v", got)
	}
}
