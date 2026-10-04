package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A class change on a live element must recompute + re-apply its CSS in
// place — the core of the retained-DOM restyle path.
func TestElRestyleOnClassChange(t *testing.T) {
	eng := NewStyleEngine(`.a { background: #ff0000 } .b { background: #00ff00 }`)
	root := eng.NewEl("div")
	root.SetClass("a")
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(root)
	eng.Restyle()

	if got := root.Style().Background; got.R < 0.9 || got.G > 0.1 {
		t.Fatalf("class .a background = %+v, want red", got)
	}

	root.Layout(qui.Rect{W: 200, H: 200})
	root.ClearLayoutDirty()
	win.ClearDirtyRegion()
	root.SetClass("b") // markDirty → inline restyle in test mode
	if got := root.Style().Background; got.G < 0.9 || got.R > 0.1 {
		t.Fatalf("after class change to .b, background = %+v, want green", got)
	}
	if root.IsLayoutDirty() {
		t.Fatal("paint-only class restyle dirtied layout")
	}
	if win.DirtyRegion().IsEmpty() {
		t.Fatal("paint-only class restyle did not invalidate the window")
	}
}

func TestElGeometryRestyleInvalidatesLayout(t *testing.T) {
	eng := NewStyleEngine(`.a { padding: 4px } .b { padding: 20px }`)
	root := eng.NewEl("div")
	root.SetClass("a")
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(root)
	eng.Restyle()
	root.Layout(qui.Rect{W: 200, H: 200})
	root.ClearLayoutDirty()
	win.ClearDirtyRegion()

	root.SetClass("b")
	if !root.IsLayoutDirty() {
		t.Fatal("geometry class restyle did not dirty layout")
	}
	if win.DirtyRegion().IsEmpty() {
		t.Fatal("geometry class restyle did not invalidate the window")
	}
}

func TestElDroppingAbsolutePositionClearsAnchors(t *testing.T) {
	eng := NewStyleEngine(`.absolute { position: absolute; left: 12px; top: 8px; width: 40px; height: 20px }`)
	root := eng.NewEl("div")
	child := eng.NewEl("div")
	child.SetClass("absolute")
	root.SetElementChildren([]qui.Widget{child})
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	win.SetRoot(root)
	eng.Restyle()
	if !child.OutOfFlow() || child.AbsolutePositionValue().Anchor == 0 {
		t.Fatalf("absolute style was not applied: outOfFlow=%v position=%+v", child.OutOfFlow(), child.AbsolutePositionValue())
	}

	child.SetClass("")
	if child.OutOfFlow() {
		t.Fatal("dropping position:absolute left the element out of flow")
	}
	if got := child.AbsolutePositionValue(); got != (qui.AbsolutePosition{}) {
		t.Fatalf("dropping position:absolute retained anchors: %+v", got)
	}
}

func TestElRelativeOffsetRestyleInvalidatesLayout(t *testing.T) {
	eng := NewStyleEngine(`.a { position: relative; left: 4px } .b { position: relative; left: 20px }`)
	root := eng.NewEl("div")
	root.SetClass("a")
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	win.SetRoot(root)
	eng.Restyle()
	root.ClearLayoutDirty()
	win.ClearDirtyRegion()

	root.SetClass("b")
	if !root.IsLayoutDirty() {
		t.Fatal("relative-position offset change did not dirty layout")
	}
	if root.PosOffset.X != 20 {
		t.Fatalf("relative-position offset = %+v, want X=20", root.PosOffset)
	}
}

// A text leaf renders its text through an internal label child and updates
// in place when SetText is called.
func TestElTextLeaf(t *testing.T) {
	eng := NewStyleEngine(``)
	root := eng.NewEl("span")
	root.SetTextContent("hello")
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(root)
	eng.Restyle()

	if len(root.ChildList()) != 1 {
		t.Fatalf("text leaf should have 1 (label) child, got %d", len(root.ChildList()))
	}
	if got := root.AccessibleName(); got != "hello" {
		t.Fatalf("AccessibleName = %q, want hello", got)
	}
	root.SetTextContent("world")
	if got := root.AccessibleName(); got != "world" {
		t.Fatalf("after SetText, AccessibleName = %q, want world", got)
	}
}

// Text mixed with a foldable inline child folds into ONE inline formatting
// context: the parent hosts a single InlineBox and the child's text flows
// as a styled span (word-level wrap), not as a separate block widget.
func TestElInlineFlowFoldsSpans(t *testing.T) {
	eng := NewStyleEngine(``)
	root := eng.NewEl("p")
	root.SetTextContent("Hello ")
	kid := eng.NewEl("b")
	kid.SetTextContent("world")
	root.SetElementChildren([]qui.Widget{kid})
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(root)
	eng.Restyle()

	kids := root.ChildList()
	if len(kids) != 1 {
		t.Fatalf("inline flow should host exactly 1 InlineBox, got %d children", len(kids))
	}
	ib, ok := kids[0].(*widgets.InlineBox)
	if !ok {
		t.Fatalf("child is %T, want *widgets.InlineBox", kids[0])
	}
	if len(ib.ChildList()) != 0 {
		t.Fatalf("pure-text span must fold to a text run, got %d atomic boxes", len(ib.ChildList()))
	}
	if got := root.AccessibleName(); got != "Hello world" {
		t.Fatalf("AccessibleName = %q, want %q", got, "Hello world")
	}

	// A folded child's SetText re-flows the parent (markDirty → inline
	// restyle in test mode) — the signal-binding path.
	kid.SetTextContent("qui")
	if got := root.AccessibleName(); got != "Hello qui" {
		t.Fatalf("after child SetText, AccessibleName = %q, want %q", got, "Hello qui")
	}
}

// An inline child that needs real widget behavior (a click handler here)
// stays an atomic box INSIDE the inline flow, so hit-testing still works.
func TestElInlineFlowKeepsInteractiveChildrenAtomic(t *testing.T) {
	eng := NewStyleEngine(``)
	root := eng.NewEl("p")
	root.SetTextContent("Click ")
	kid := eng.NewEl("span")
	kid.SetTextContent("here")
	kid.SetOnClick(func() {})
	root.SetElementChildren([]qui.Widget{kid})
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(root)
	eng.Restyle()

	ib, ok := root.ChildList()[0].(*widgets.InlineBox)
	if !ok {
		t.Fatalf("root child is %T, want *widgets.InlineBox", root.ChildList()[0])
	}
	boxes := ib.ChildList()
	if len(boxes) != 1 || boxes[0] != qui.Widget(kid) {
		t.Fatalf("interactive span must ride as an atomic box (got %d boxes)", len(boxes))
	}
}

// Mixed block + inline content splits into anonymous inline runs around
// the block child (CSS anonymous-block behavior): [run][block][run],
// with each run its own InlineBox.
func TestElMixedBlockAndInlineRuns(t *testing.T) {
	eng := NewStyleEngine(``)
	root := eng.NewEl("div")
	root.SetTextContent("lead ")
	b := eng.NewEl("b")
	b.SetTextContent("bold")
	block := eng.NewEl("div")
	block.SetTextContent("block")
	tail := eng.NewEl("span")
	tail.SetTextContent("tail")
	root.SetElementChildren([]qui.Widget{b, block, tail})
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	win.SetRoot(root)
	eng.Restyle()

	kids := root.ChildList()
	if len(kids) != 3 {
		t.Fatalf("want [run, block, run], got %d children", len(kids))
	}
	run1, ok1 := kids[0].(*widgets.InlineBox)
	run2, ok2 := kids[2].(*widgets.InlineBox)
	if !ok1 || !ok2 {
		t.Fatalf("outer children are %T / %T, want InlineBoxes", kids[0], kids[2])
	}
	if kids[1] != qui.Widget(block) {
		t.Fatalf("middle child is %T, want the block element", kids[1])
	}
	if got := run1.Text(); got != "lead bold" {
		t.Errorf("run1 text = %q, want %q", got, "lead bold")
	}
	if got := run2.Text(); got != "tail" {
		t.Errorf("run2 text = %q, want %q", got, "tail")
	}
	if got := root.AccessibleName(); got != "lead boldtail" {
		t.Errorf("AccessibleName = %q", got)
	}
}

// An li directly inside a ul renders a [marker | content] row; the
// bullet honors list-style-type (none suppresses it).
func TestElListMarkers(t *testing.T) {
	eng := NewStyleEngine(``)
	ul := eng.NewEl("ol")
	li1 := eng.NewEl("li")
	li1.SetTextContent("first")
	li2 := eng.NewEl("li")
	li2.SetTextContent("second")
	ul.SetElementChildren([]qui.Widget{li1, li2})
	eng.SetRoot(ul)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	win.SetRoot(ul)
	eng.Restyle()

	for i, li := range []*El{li1, li2} {
		kids := li.ChildList()
		if len(kids) != 2 {
			t.Fatalf("li%d has %d children, want [marker, content]", i+1, len(kids))
		}
		mk, ok := kids[0].(*widgets.Label)
		if !ok {
			t.Fatalf("li%d marker is %T, want *widgets.Label", i+1, kids[0])
		}
		want := []string{"1.", "2."}[i]
		if mk.Text() != want {
			t.Errorf("li%d marker = %q, want %q", i+1, mk.Text(), want)
		}
	}
}

// A container whose element children are all block-level keeps the plain
// separate-children path — no inline box is introduced.
func TestElBlockChildrenSkipInlineFlow(t *testing.T) {
	eng := NewStyleEngine(``)
	root := eng.NewEl("div")
	a := eng.NewEl("div")
	a.SetTextContent("a")
	b := eng.NewEl("div")
	b.SetTextContent("b")
	root.SetElementChildren([]qui.Widget{a, b})
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(root)
	eng.Restyle()

	kids := root.ChildList()
	if len(kids) != 2 || kids[0] != qui.Widget(a) || kids[1] != qui.Widget(b) {
		t.Fatalf("block children must stay separate widgets, got %d", len(kids))
	}
}

// A draggable element shows live feedback: the window cursor changes and it
// follows the pointer via a paint-only Transform (no opacity dim — that would
// SaveLayer-clip the translated row); drag end restores the pre-drag values.
func TestElDragFeedback(t *testing.T) {
	eng := NewStyleEngine(`.row { background: #ffffff }`)
	row := eng.NewEl("div")
	row.SetClass("row")
	row.SetDraggable(true)
	row.SetDragKey("7")
	row.SetOnDrop(func(string) {})
	eng.SetRoot(row)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 300})
	win.SetRoot(row)
	eng.Restyle()
	row.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 40})

	if row.VisualTransform != nil {
		t.Fatalf("pre-drag: transform=%v, want nil", row.VisualTransform)
	}

	row.Handle(qui.NewDragEvent(qui.EventDragStart, 10, 20, row))
	if win.Cursor() != qui.CursorHand {
		t.Fatalf("drag start: cursor=%v, want CursorHand", win.Cursor())
	}

	row.Handle(qui.NewDragEvent(qui.EventDragMove, 10, 55, row))
	// Paint-only Transform (not PosOffset) so hit-test bounds stay put.
	if row.VisualTransform == nil || row.VisualTransform.TY != 35 { // 55 - 20
		t.Fatalf("drag move: Transform=%+v, want TY=35", row.VisualTransform)
	}

	row.Handle(qui.NewDragEvent(qui.EventDragEnd, 10, 55, row))
	if row.VisualTransform != nil {
		t.Fatalf("drag end: transform=%v, want restored to nil", row.VisualTransform)
	}
	if win.Cursor() != qui.CursorDefault {
		t.Fatalf("drag end: cursor=%v, want CursorDefault", win.Cursor())
	}
}

func TestElDragFeedbackPreservesCSSTransform(t *testing.T) {
	eng := NewStyleEngine(`.row { transform: translateX(12px) }`)
	row := eng.NewEl("div")
	row.SetClass("row")
	row.SetDraggable(true)
	eng.SetRoot(row)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	win.SetRoot(row)
	eng.Restyle()
	row.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 30})
	if row.Transform == nil || row.Transform.TX != 12 {
		t.Fatalf("CSS transform before drag = %+v, want TX=12", row.Transform)
	}

	row.Handle(qui.NewDragEvent(qui.EventDragStart, 10, 10, row))
	row.Handle(qui.NewDragEvent(qui.EventDragMove, 10, 40, row))
	if row.Transform == nil || row.Transform.TX != 12 {
		t.Fatalf("drag overwrote CSS transform: %+v", row.Transform)
	}
	if row.VisualTransform == nil || row.VisualTransform.TY != 30 {
		t.Fatalf("drag visual transform = %+v, want TY=30", row.VisualTransform)
	}

	row.Handle(qui.NewDragEvent(qui.EventDragEnd, 10, 40, row))
	if row.Transform == nil || row.Transform.TX != 12 {
		t.Fatalf("CSS transform after drag = %+v, want TX=12", row.Transform)
	}
	if row.VisualTransform != nil {
		t.Fatalf("visual transform after drag = %+v, want nil", row.VisualTransform)
	}
}

// A drag handle initiates the drag but its ghost (dim + follow) applies to
// its PARENT row, so grabbing a small handle lifts the whole row.
func TestElDragHandleGhostsParent(t *testing.T) {
	eng := NewStyleEngine(``)
	row := eng.NewEl("div")
	grip := eng.NewEl("span")
	grip.SetTextContent("::")
	grip.SetDraggable(true)
	grip.SetDragHandle(true)
	row.SetElementChildren([]qui.Widget{grip})
	eng.SetRoot(row)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 300})
	win.SetRoot(row)
	eng.Restyle()
	row.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 40})

	grip.Handle(qui.NewDragEvent(qui.EventDragStart, 5, 10, grip))
	grip.Handle(qui.NewDragEvent(qui.EventDragMove, 5, 40, grip))

	// The parent row is lifted (transformed), not the handle.
	if row.VisualTransform == nil || row.VisualTransform.TY != 30 {
		t.Fatalf("parent row Transform=%+v, want TY=30", row.VisualTransform)
	}
	if grip.VisualTransform != nil {
		t.Fatalf("handle itself should not be transformed, got %+v", grip.VisualTransform)
	}

	grip.Handle(qui.NewDragEvent(qui.EventDragEnd, 5, 40, grip))
	if row.VisualTransform != nil {
		t.Fatalf("after drag end parent row not restored: transform=%v", row.VisualTransform)
	}
}

// A horizontal-axis drag (tabs) lifts the ghost along X, not Y, and its
// z-index rises so it paints over the siblings it slides across.
func TestElHorizontalDragFollowsX(t *testing.T) {
	eng := NewStyleEngine(``)
	tab := eng.NewEl("div")
	tab.SetDraggable(true)
	tab.SetDragAxisHorizontal(true)
	eng.SetRoot(tab)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	win.SetRoot(tab)
	eng.Restyle()
	tab.Layout(qui.Rect{X: 0, Y: 0, W: 80, H: 30})

	tab.Handle(qui.NewDragEvent(qui.EventDragStart, 20, 15, tab))
	if tab.ZIndex() != dragZIndex {
		t.Fatalf("dragging z-index = %d, want %d", tab.ZIndex(), dragZIndex)
	}
	tab.Handle(qui.NewDragEvent(qui.EventDragMove, 55, 40, tab))
	if tab.VisualTransform == nil || tab.VisualTransform.TX != 35 || tab.VisualTransform.TY != 0 {
		t.Fatalf("horizontal drag Transform=%+v, want TX=35 TY=0", tab.VisualTransform)
	}
	tab.Handle(qui.NewDragEvent(qui.EventDragEnd, 55, 40, tab))
	if tab.ZIndex() != 0 {
		t.Fatalf("post-drag z-index = %d, want 0", tab.ZIndex())
	}
}

// OnDragOver on a horizontal element reports `after` from the HORIZONTAL
// midpoint (right half), for left-to-right strips like tabs.
func TestElHorizontalDragOverUsesXMidpoint(t *testing.T) {
	eng := NewStyleEngine(``)
	tab := eng.NewEl("div")
	tab.SetDragAxisHorizontal(true)
	var got []bool
	tab.SetOnDragOver(func(_ string, after bool) { got = append(got, after) })
	eng.SetRoot(tab)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	win.SetRoot(tab)
	eng.Restyle()
	tab.Layout(qui.Rect{X: 100, Y: 0, W: 80, H: 30}) // midpoint x = 140

	src := eng.NewEl("div")
	tab.Handle(qui.NewDragEvent(qui.EventDragOver, 120, 15, src)) // left half
	tab.Handle(qui.NewDragEvent(qui.EventDragOver, 160, 15, src)) // right half
	if len(got) != 2 || got[0] != false || got[1] != true {
		t.Fatalf("after flags = %v, want [false true]", got)
	}
}

// Dropping a dragged element must not ALSO click it: the mouse-up that ends
// a drag carries AfterDrag, and a click handler on the dragged element (the
// grid overview's "click a slide to open it") has to stay silent.
func TestElDropDoesNotClick(t *testing.T) {
	eng := NewStyleEngine(``)
	cellEl := eng.NewEl("div")
	cellEl.SetDraggable(true)
	clicks := 0
	cellEl.SetOnClick(func() { clicks++ })
	eng.SetRoot(cellEl)

	win := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	win.SetRoot(cellEl)
	eng.Restyle()
	cellEl.Layout(qui.Rect{X: 0, Y: 0, W: 120, H: 90})

	// A plain press / release IS a click.
	cellEl.Handle(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
	cellEl.Handle(qui.NewMouseEvent(qui.EventMouseUp, 10, 10, qui.MouseButtonLeft, 0))
	if clicks != 1 {
		t.Fatalf("plain click count = %d, want 1", clicks)
	}

	// A release that ended a drag is not.
	cellEl.Handle(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
	cellEl.Handle(qui.NewDragEvent(qui.EventDragStart, 10, 10, cellEl))
	cellEl.Handle(qui.NewDragEvent(qui.EventDragMove, 90, 10, cellEl))
	cellEl.Handle(qui.NewDragEvent(qui.EventDragEnd, 90, 10, cellEl))
	up := qui.NewMouseEvent(qui.EventMouseUp, 90, 10, qui.MouseButtonLeft, 0)
	up.AfterDrag = true
	cellEl.Handle(up)
	if clicks != 1 {
		t.Fatalf("click count after a drag = %d, want it unchanged at 1", clicks)
	}
}

// A ghost nested inside a wrapper (the usual "row + drop indicator lines"
// box) must lift its ANCESTORS too: z-index only orders siblings, so a
// wrapper left at 0 lets the NEXT wrapper paint over the row being dragged.
func TestElDragLiftsAncestors(t *testing.T) {
	eng := NewStyleEngine(``)
	list := eng.NewEl("div")
	item := eng.NewEl("div")
	row := eng.NewEl("div")
	row.SetDraggable(true)
	item.SetElementChildren([]qui.Widget{row})
	list.SetElementChildren([]qui.Widget{item})
	eng.SetRoot(list)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 400})
	win.SetRoot(list)
	eng.Restyle()
	list.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 400})

	row.Handle(qui.NewDragEvent(qui.EventDragStart, 10, 10, row))
	if item.ZIndex() != dragZIndex {
		t.Fatalf("wrapper z-index while dragging = %d, want %d", item.ZIndex(), dragZIndex)
	}
	if list.ZIndex() != dragZIndex {
		t.Fatalf("list z-index while dragging = %d, want %d", list.ZIndex(), dragZIndex)
	}
	row.Handle(qui.NewDragEvent(qui.EventDragEnd, 10, 40, row))
	if item.ZIndex() != 0 || list.ZIndex() != 0 {
		t.Fatalf("post-drag z-index: wrapper=%d list=%d, want 0 0", item.ZIndex(), list.ZIndex())
	}
}

// A free-axis drag (a wrapped grid of thumbnails) follows the cursor on BOTH
// axes — a grid drag moves in two dimensions, so a ghost pinned to one axis
// lags behind the pointer as soon as the drag leaves its starting row.
func TestElFreeDragFollowsBothAxes(t *testing.T) {
	eng := NewStyleEngine(``)
	cell := eng.NewEl("div")
	cell.SetDraggable(true)
	cell.SetDragAxis(DragAxisFree)
	eng.SetRoot(cell)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(cell)
	eng.Restyle()
	cell.Layout(qui.Rect{X: 0, Y: 0, W: 120, H: 90})

	cell.Handle(qui.NewDragEvent(qui.EventDragStart, 20, 15, cell))
	cell.Handle(qui.NewDragEvent(qui.EventDragMove, 260, 140, cell))
	if cell.VisualTransform == nil || cell.VisualTransform.TX != 240 || cell.VisualTransform.TY != 125 {
		t.Fatalf("free drag Transform=%+v, want TX=240 TY=125", cell.VisualTransform)
	}
	cell.Handle(qui.NewDragEvent(qui.EventDragEnd, 260, 140, cell))
	if cell.VisualTransform != nil {
		t.Fatalf("after drag end transform not restored: %v", cell.VisualTransform)
	}
}

// A free-axis drop target reads `after` from the HORIZONTAL midpoint: a
// wrapped grid is one linear sequence in reading order, so the vertical
// position inside a cell must not flip the insertion side.
func TestElFreeDragOverUsesXMidpoint(t *testing.T) {
	eng := NewStyleEngine(``)
	cell := eng.NewEl("div")
	cell.SetDragAxis(DragAxisFree)
	var got []bool
	cell.SetOnDragOver(func(_ string, after bool) { got = append(got, after) })
	eng.SetRoot(cell)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(cell)
	eng.Restyle()
	cell.Layout(qui.Rect{X: 100, Y: 40, W: 120, H: 90}) // mid x = 160, mid y = 85

	src := eng.NewEl("div")
	cell.Handle(qui.NewDragEvent(qui.EventDragOver, 120, 120, src)) // left half, low
	cell.Handle(qui.NewDragEvent(qui.EventDragOver, 200, 50, src))  // right half, high
	if len(got) != 2 || got[0] != false || got[1] != true {
		t.Fatalf("after flags = %v, want [false true]", got)
	}
}

// OnDragOver reports whether the cursor is past the element's vertical
// midpoint (after), so a list can insert below a row — the "reach the last
// slot" case.
func TestElDragOverAfterHalf(t *testing.T) {
	eng := NewStyleEngine(``)
	row := eng.NewEl("div")
	var got []bool
	row.SetOnDragOver(func(_ string, after bool) { got = append(got, after) })
	eng.SetRoot(row)

	win := qui.NewTestWindow(qui.Size{W: 200, H: 200})
	win.SetRoot(row)
	eng.Restyle()
	row.Layout(qui.Rect{X: 0, Y: 100, W: 200, H: 40}) // midpoint y = 120

	src := eng.NewEl("div")
	row.Handle(qui.NewDragEvent(qui.EventDragOver, 10, 108, src)) // upper half
	row.Handle(qui.NewDragEvent(qui.EventDragOver, 10, 132, src)) // lower half

	if len(got) != 2 || got[0] != false || got[1] != true {
		t.Fatalf("after flags = %v, want [false true]", got)
	}
}

// SetOnClickMods delivers the modifiers held at click time, which is what a
// multi-selectable list row needs: plain click selects, Shift extends, Cmd
// toggles. A plain OnClick handler on the same element still fires.
func TestOnClickModsReceivesModifiers(t *testing.T) {
	res := RenderDoc(`<body><div id="row">Row</div></body>`,
		`#row { width: 100px; height: 20px; }`, Options{})
	row := res.ByID["row"].(*El)

	var got []qui.Modifiers
	plain := 0
	row.SetOnClick(func() { plain++ })
	row.SetOnClickMods(func(mods qui.Modifiers) { got = append(got, mods) })

	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 200, H: 100})

	center := qui.Point{X: row.Bounds().X + 10, Y: row.Bounds().Y + 10}
	for _, mods := range []qui.Modifiers{0, qui.ModShift, qui.ModSuper} {
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, center.X, center.Y, qui.MouseButtonLeft, mods))
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, center.X, center.Y, qui.MouseButtonLeft, mods))
	}
	if len(got) != 3 {
		t.Fatalf("handler fired %d times, want 3", len(got))
	}
	if got[0] != 0 || got[1] != qui.ModShift || got[2] != qui.ModSuper {
		t.Errorf("modifiers = %v", got)
	}
	if plain != 3 {
		t.Errorf("the plain OnClick fired %d times, want 3", plain)
	}
}

// An element that only has a modifier-aware handler must stay a real widget —
// folding it into an inline run would drop the handler, so the fold decision
// has to know about OnClickMods just as it knows about OnClick.
func TestOnClickModsKeepsElementUnfolded(t *testing.T) {
	span := newEl("span", NewStyleEngine(""))
	if !foldableInline(span, nil) {
		t.Fatal("a plain span should be foldable to begin with")
	}
	span.SetOnClickMods(func(qui.Modifiers) {})
	if foldableInline(span, nil) {
		t.Error("an element with a modifier click handler must not fold into a text run")
	}
}
