package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func withMenuHoverDelay0(t *testing.T) {
	t.Helper()
	old := MenuHoverDelay
	MenuHoverDelay = 0
	t.Cleanup(func() { MenuHoverDelay = old })
}

func moveTo(w *Window, p Point) {
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, p.X, p.Y, MouseButtonLeft, 0))
}

func rowCenter(list *menuListView, i int) Point {
	b := list.content.ChildAt(i).Bounds()
	return Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
}

func topMenuLabels(w *Window) string {
	ov := w.Overlays()
	if len(ov) == 0 {
		return ""
	}
	p, ok := ov[len(ov)-1].(*Popup)
	if !ok {
		return ""
	}
	list := p.Content.(*menuListView)
	out := ""
	for _, it := range list.items {
		out += it.Label + ";"
	}
	return out
}

// Google-style menus: resting on a submenu row opens it, resting on a
// sibling switches, resting on a plain row closes it; travelling into the
// submenu keeps it.
func TestSubmenuOpensOnHover(t *testing.T) {
	withMenuHoverDelay0(t)
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{{Label: "100%"}, {Label: "150%"}}},
		{Label: "Text", Submenu: []*MenuItem{{Label: "Bold"}, {Label: "Italic"}}},
		{Label: "Plain", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 10, 10, items)
	list := popup.Content.(*menuListView)

	moveTo(w, rowCenter(list, 0))
	if n := len(w.Overlays()); n != 2 {
		t.Fatalf("hover on Zoom: overlays=%d, want 2", n)
	}
	if got := topMenuLabels(w); got != "100%;150%;" {
		t.Fatalf("hover on Zoom opened %q", got)
	}
	// Into the submenu: stays open.
	sub := w.Overlays()[1].(*Popup)
	sb := sub.Bounds()
	moveTo(w, Point{X: sb.X + sb.W/2, Y: sb.Y + 12})
	if n := len(w.Overlays()); n != 2 {
		t.Fatalf("moving into the submenu closed it: overlays=%d", n)
	}
	// Back over the parent: the sibling row's submenu replaces it.
	moveTo(w, rowCenter(list, 1))
	if n := len(w.Overlays()); n != 2 {
		t.Fatalf("hover on Text: overlays=%d, want 2", n)
	}
	if got := topMenuLabels(w); got != "Bold;Italic;" {
		t.Fatalf("hover on Text shows %q", got)
	}
	// A plain row closes the submenu.
	moveTo(w, rowCenter(list, 2))
	if n := len(w.Overlays()); n != 1 {
		t.Fatalf("hover on Plain: overlays=%d, want 1", n)
	}
	// Closing the menu takes an open submenu with it.
	moveTo(w, rowCenter(list, 0))
	popup.Close()
	if n := len(w.Overlays()); n != 0 {
		t.Fatalf("closing the menu left %d overlays", n)
	}
}

// A click on a parent row while a submenu is up reaches that row.
func TestClickParentRowWhileSubmenuOpen(t *testing.T) {
	withMenuHoverDelay0(t)
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	clicked := false
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{{Label: "100%"}}},
		{Label: "Plain", OnClick: func() { clicked = true }},
	}
	popup := ShowContextMenu(w, 10, 10, items)
	list := popup.Content.(*menuListView)
	list.content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
	if len(w.Overlays()) != 2 {
		t.Fatal("submenu did not open")
	}
	c := rowCenter(list, 1)
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, c.X, c.Y, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, c.X, c.Y, MouseButtonLeft, 0))
	if !clicked {
		t.Error("parent row click did not reach the row")
	}
	if n := len(w.Overlays()); n != 0 {
		t.Errorf("picking a parent row left %d overlays", n)
	}
}

// With a menu-bar dropdown open, hovering another trigger switches to its
// menu.
func TestMenuBarHoverSwitchesMenu(t *testing.T) {
	w := NewTestWindow(Size{W: 600, H: 500})
	mb := NewMenuBar(w)
	mb.AddMenu("File", []*MenuItem{{Label: "New"}})
	mb.AddMenu("Edit", []*MenuItem{{Label: "Copy"}})
	mb.AddMenu("View", []*MenuItem{{Label: "Zoom"}})
	w.SetRoot(mb)
	mb.Layout(Rect{X: 0, Y: 0, W: 600, H: 30})
	mb.menus[0].trigger.Layout(Rect{X: 0, Y: 0, W: 60, H: 26})
	mb.menus[1].trigger.Layout(Rect{X: 60, Y: 0, W: 60, H: 26})
	mb.menus[2].trigger.Layout(Rect{X: 120, Y: 0, W: 60, H: 26})

	moveTo(w, Point{X: 70, Y: 10}) // hover alone opens nothing
	if len(w.Overlays()) != 0 {
		t.Fatal("hover without an open menu opened one")
	}
	mb.OpenMenu(0)
	moveTo(w, Point{X: 30, Y: 10}) // own trigger: stays
	if mb.OpenIndex() != 0 {
		t.Fatalf("open index = %d after hovering own trigger", mb.OpenIndex())
	}
	moveTo(w, Point{X: 150, Y: 10})
	if mb.OpenIndex() != 2 || len(w.Overlays()) != 1 {
		t.Fatalf("after hovering View: open=%d overlays=%d, want 2/1", mb.OpenIndex(), len(w.Overlays()))
	}
	if got := topMenuLabels(w); got != "Zoom;" {
		t.Fatalf("View dropdown shows %q", got)
	}
	mb.CloseMenu()
	moveTo(w, Point{X: 70, Y: 10})
	if len(w.Overlays()) != 0 {
		t.Fatal("hover switch survived CloseMenu")
	}
}
