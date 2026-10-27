package qui

import "testing"

// canvasWidget stands in for a widget that paints its own interactive
// content instead of composing child widgets (a document view, a sheet
// grid, a chart). It publishes two boxes and records where it was clicked.
type canvasWidget struct {
	BaseWidget
	boxes    []AXChild
	clicked  []Point
	hitTests int
}

func newCanvasWidget(boxes []AXChild) *canvasWidget {
	c := &canvasWidget{BaseWidget: NewBaseWidget(), boxes: boxes}
	c.SetSelf(c)
	return c
}

func (c *canvasWidget) AccessibleChildren() []AXChild { return c.boxes }
func (c *canvasWidget) Role() string                  { return RoleGeneric }
func (c *canvasWidget) AccessibleName() string        { return "canvas" }

func (c *canvasWidget) Handle(e Event) bool {
	if me, ok := e.(MouseEvent); ok && me.Type() == EventMouseDown {
		c.clicked = append(c.clicked, Point{X: me.X, Y: me.Y})
		return true
	}
	return false
}

func newCanvasWindow(t *testing.T, boxes []AXChild) (*Window, *canvasWidget) {
	t.Helper()
	win := NewTestWindow(Size{W: 400, H: 300})
	c := newCanvasWidget(boxes)
	c.Layout(Rect{X: 0, Y: 0, W: 400, H: 300})
	win.SetRoot(c)
	return win, c
}

// Self-drawn content shows up as addressable nodes, with its own bounds,
// role and state — not as one opaque rectangle.
func TestAccessibleChildrenAppearInTree(t *testing.T) {
	win, _ := newCanvasWindow(t, []AXChild{
		{Role: RoleCheckbox, Name: "buy milk", Bounds: Rect{X: 10, Y: 10, W: 12, H: 12}, State: AXStateChecked},
		{Role: RoleCheckbox, Name: "walk dog", Bounds: Rect{X: 10, Y: 40, W: 12, H: 12}},
	})

	nodes, err := win.FindNodes(`[role=checkbox]`)
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("matched %d checkbox nodes, want 2", len(nodes))
	}
	if nodes[0].Name != "buy milk" || nodes[0].State != "checked" {
		t.Errorf("node 0 = %+v, want name=buy milk state=checked", nodes[0])
	}
	if got := nodes[1].Bounds; got.X != 10 || got.Y != 40 {
		t.Errorf("node 1 bounds = %+v, want the child's own rect", got)
	}
	// The host stays the owner, so actions have a widget to dispatch through.
	if nodes[0].Widget() == nil {
		t.Error("published child lost its host widget")
	}
}

// An action on a published child must land on the CHILD's rect. Aiming at
// the host widget's center instead would put every click in the middle of
// the document — the bug this contract exists to prevent.
func TestClickOnAccessibleChildHitsItsOwnRect(t *testing.T) {
	win, c := newCanvasWindow(t, []AXChild{
		{Role: RoleCheckbox, Name: "walk dog", Bounds: Rect{X: 100, Y: 40, W: 12, H: 12}},
	})

	if err := win.Click(`[role=checkbox][name="walk dog"]`, ClickOptions{}); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if len(c.clicked) == 0 {
		t.Fatal("no mouse event reached the host widget")
	}
	got := c.clicked[0]
	if got.X != 106 || got.Y != 46 {
		t.Errorf("clicked at %+v, want the child's center (106,46) — host center is (200,150)", got)
	}
}

// A child that straddles the host's edge is clipped by the host when it
// paints, so an action has to aim at the part that exists: a spreadsheet's
// right-most column is half off the viewport nearly always, and the whole
// child's center lands outside the host — where the click finds no widget or,
// worse, a neighbour.
func TestClickOnClippedAccessibleChildAimsAtItsVisiblePart(t *testing.T) {
	win := NewTestWindow(Size{W: 400, H: 300})
	c := newCanvasWidget([]AXChild{
		{Role: RoleTableCell, Name: "Z9", Bounds: Rect{X: 190, Y: 40, W: 80, H: 20}},
	})
	win.SetRoot(c)
	// A host narrower than the window, so the child really is cut off.
	c.Layout(Rect{X: 0, Y: 0, W: 200, H: 300})

	if err := win.Click(`[role=cell][name="Z9"]`, ClickOptions{}); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if len(c.clicked) == 0 {
		t.Fatal("no mouse event reached the host widget")
	}
	// Visible part is x∈[190,200), so the aim point is 195 — not 230, which
	// is past the host entirely.
	if got := c.clicked[0]; got.X != 195 || got.Y != 50 {
		t.Errorf("clicked at %+v, want the visible part's center (195,50)", got)
	}
}

// Published children participate in the tree hash, so an agent waiting for
// the tree to settle notices a checkbox toggling.
func TestAccessibleChildStateChangesHash(t *testing.T) {
	win, c := newCanvasWindow(t, []AXChild{
		{Role: RoleCheckbox, Name: "buy milk", Bounds: Rect{X: 10, Y: 10, W: 12, H: 12}},
	})
	before := win.AccessibilityTree().Hash()
	c.boxes[0].State = AXStateChecked
	if after := win.AccessibilityTree().Hash(); after == before {
		t.Error("toggling a published child did not change the tree hash")
	}
}
