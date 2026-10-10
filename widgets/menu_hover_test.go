package widgets

import (
	"testing"
	"time"

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

func withMenuHoverDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := MenuHoverDelay
	MenuHoverDelay = d
	t.Cleanup(func() { MenuHoverDelay = old })
}

// settle waits d, then runs the jobs the hover timers posted.
func settle(w *Window, d time.Duration) {
	time.Sleep(d)
	w.DrainJobsForTest()
}

func pressKey(w *Window, k Key) {
	w.DispatchTestEvent(NewKeyEvent(EventKeyDown, k, 0))
}

// Keyboard: Right on a submenu row opens it (focus moves in), Left closes
// just that submenu and hands focus back to the parent menu; Right on a
// plain row opens nothing.
func TestSubmenuKeyboardRightLeft(t *testing.T) {
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{{Label: "100%"}, {Label: "150%"}}},
		{Label: "Plain", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 10, 10, items)
	list := popup.Content.(*menuListView)

	pressKey(w, KeyRight)
	if n := len(w.Overlays()); n != 2 {
		t.Fatalf("Right on Zoom: overlays=%d, want 2", n)
	}
	if got := topMenuLabels(w); got != "100%;150%;" {
		t.Fatalf("Right opened %q", got)
	}
	sub := w.Overlays()[1].(*Popup)
	if w.Focused() != Widget(sub.Content.(*menuListView)) {
		t.Fatalf("focus after Right is %T, want the submenu list", w.Focused())
	}
	// Down inside the submenu moves its own highlight, not the parent's.
	pressKey(w, KeyDown)
	if list.focused != 0 {
		t.Fatalf("parent highlight moved to %d while the submenu had focus", list.focused)
	}

	pressKey(w, KeyLeft)
	if n := len(w.Overlays()); n != 1 {
		t.Fatalf("Left in the submenu: overlays=%d, want 1", n)
	}
	if w.Focused() != Widget(list) {
		t.Fatalf("focus after Left is %T, want the parent list", w.Focused())
	}

	pressKey(w, KeyDown) // onto Plain
	pressKey(w, KeyRight)
	if n := len(w.Overlays()); n != 1 {
		t.Fatalf("Right on a plain row opened something: overlays=%d", n)
	}
}

// With the real delay a submenu waits for the pointer to rest: a pass-by
// opens nothing, resting opens it, resting on a sibling replaces it, and
// resting on a plain row closes it.
func TestSubmenuHoverWaitsForDelay(t *testing.T) {
	withMenuHoverDelay(t, 40*time.Millisecond)
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{{Label: "100%"}}},
		{Label: "Text", Submenu: []*MenuItem{{Label: "Bold"}}},
		{Label: "Plain", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 10, 10, items)
	list := popup.Content.(*menuListView)

	moveTo(w, rowCenter(list, 0))
	settle(w, 0)
	if n := len(w.Overlays()); n != 1 {
		t.Fatalf("submenu opened before the delay: overlays=%d", n)
	}
	moveTo(w, rowCenter(list, 2)) // passes on before Zoom's timer fires
	settle(w, 60*time.Millisecond)
	if n := len(w.Overlays()); n != 1 {
		t.Fatalf("a pass-by over Zoom opened its submenu: overlays=%d", n)
	}

	moveTo(w, rowCenter(list, 0))
	settle(w, 70*time.Millisecond)
	if got := topMenuLabels(w); len(w.Overlays()) != 2 || got != "100%;" {
		t.Fatalf("resting on Zoom: overlays=%d top=%q", len(w.Overlays()), got)
	}
	moveTo(w, rowCenter(list, 1))
	settle(w, 5*time.Millisecond)
	if got := topMenuLabels(w); got != "100%;" {
		t.Fatalf("sibling replaced the submenu before the delay: %q", got)
	}
	settle(w, 70*time.Millisecond)
	if got := topMenuLabels(w); len(w.Overlays()) != 2 || got != "Bold;" {
		t.Fatalf("resting on Text: overlays=%d top=%q", len(w.Overlays()), got)
	}
	moveTo(w, rowCenter(list, 2))
	settle(w, 70*time.Millisecond)
	if n := len(w.Overlays()); n != 1 {
		t.Fatalf("resting on a plain row left the submenu: overlays=%d", n)
	}
}

// Aim tolerance: a pointer still travelling toward the open submenu across
// a sibling row doesn't switch submenus; once it comes to rest there the
// sibling wins.
func TestSubmenuAimKeepsSubmenuWhileTravelling(t *testing.T) {
	withMenuHoverDelay(t, 80*time.Millisecond)
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{{Label: "100%"}, {Label: "150%"}}},
		{Label: "Text", Submenu: []*MenuItem{{Label: "Bold"}}},
	}
	popup := ShowContextMenu(w, 10, 10, items)
	list := popup.Content.(*menuListView)

	r0, r1 := list.content.ChildAt(0).Bounds(), list.content.ChildAt(1).Bounds()
	moveTo(w, rowCenter(list, 0))
	settle(w, 110*time.Millisecond)
	if got := topMenuLabels(w); got != "100%;150%;" {
		t.Fatalf("Zoom submenu did not open: %q", got)
	}
	// Leave Zoom near its right edge, then cut across Text toward the
	// submenu.
	right := r0.X + r0.W - 6
	moveTo(w, Point{X: right, Y: r0.Y + r0.H/2})
	moveTo(w, Point{X: right + 3, Y: r1.Y + 6})
	time.Sleep(50 * time.Millisecond)
	moveTo(w, Point{X: right + 4, Y: r1.Y + 10}) // still moving
	settle(w, 40*time.Millisecond)               // Text's timer has fired by now
	if got := topMenuLabels(w); got != "100%;150%;" {
		t.Fatalf("crossing Text toward the submenu switched it: %q", got)
	}
	// Come to rest on Text: it takes over.
	settle(w, 150*time.Millisecond)
	if got := topMenuLabels(w); len(w.Overlays()) != 2 || got != "Bold;" {
		t.Fatalf("resting on Text: overlays=%d top=%q", len(w.Overlays()), got)
	}
}

// Heading away from the submenu is not aiming: the sibling row wins after
// the plain delay even though the pointer keeps moving.
func TestSubmenuAwayFromSubmenuSwitchesPromptly(t *testing.T) {
	withMenuHoverDelay(t, 60*time.Millisecond)
	w := NewTestWindow(Size{W: 600, H: 500})
	w.SetRoot(NewContainer(FlexLayout{}))
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{{Label: "100%"}}},
		{Label: "Text", Submenu: []*MenuItem{{Label: "Bold"}}},
	}
	popup := ShowContextMenu(w, 10, 10, items)
	list := popup.Content.(*menuListView)
	r0, r1 := list.content.ChildAt(0).Bounds(), list.content.ChildAt(1).Bounds()
	moveTo(w, rowCenter(list, 0))
	settle(w, 90*time.Millisecond)
	if got := topMenuLabels(w); got != "100%;" {
		t.Fatalf("Zoom submenu did not open: %q", got)
	}
	// Drop straight down the left side of the panel, away from the submenu.
	moveTo(w, Point{X: r0.X + 20, Y: r0.Y + r0.H - 2})
	moveTo(w, Point{X: r0.X + 20, Y: r1.Y + 6})
	time.Sleep(30 * time.Millisecond)
	moveTo(w, Point{X: r0.X + 22, Y: r1.Y + 10})
	settle(w, 45*time.Millisecond)
	if got := topMenuLabels(w); got != "Bold;" {
		t.Fatalf("moving away from the submenu kept it: %q", got)
	}
}

func TestMenuAiming(t *testing.T) {
	sub := Rect{X: 100, Y: 0, W: 120, H: 80} // submenu panel right of the parent
	from := Point{X: 90, Y: 16}              // left the opening row near its right edge
	tests := []struct {
		name string
		cur  Point
		want bool
	}{
		{"just below the opener, inside the triangle", Point{X: 96, Y: 40}, true},
		{"straight down the panel, away", Point{X: 40, Y: 48}, false},
		{"above the triangle", Point{X: 96, Y: -20}, false},
		{"far below the submenu's bottom", Point{X: 96, Y: 140}, false},
		{"on the submenu's near edge", Point{X: 100, Y: 40}, true},
	}
	for _, tc := range tests {
		if got := menuAiming(from, tc.cur, sub); got != tc.want {
			t.Errorf("%s: menuAiming = %v, want %v", tc.name, got, tc.want)
		}
	}
	// A submenu that opened to the left is aimed at through its right edge.
	left := Rect{X: -140, Y: 0, W: 120, H: 80}
	if !menuAiming(Point{X: 10, Y: 16}, Point{X: 4, Y: 25}, left) {
		t.Error("left-opening submenu: pointer travelling toward it was not aiming")
	}
}

// With a dropdown AND a submenu open, hovering another menu-bar trigger
// switches the whole menu, submenu included.
func TestMenuBarHoverSwitchClosesSubmenu(t *testing.T) {
	withMenuHoverDelay0(t)
	w := NewTestWindow(Size{W: 600, H: 500})
	mb := NewMenuBar(w)
	mb.AddMenu("File", []*MenuItem{{Label: "Open recent", Submenu: []*MenuItem{{Label: "a.docx"}}}})
	mb.AddMenu("Edit", []*MenuItem{{Label: "Copy"}})
	w.SetRoot(mb)
	mb.Layout(Rect{X: 0, Y: 0, W: 600, H: 30})
	mb.menus[0].trigger.Layout(Rect{X: 0, Y: 0, W: 60, H: 26})
	mb.menus[1].trigger.Layout(Rect{X: 60, Y: 0, W: 60, H: 26})

	mb.OpenMenu(0)
	list := w.Overlays()[0].(*Popup).Content.(*menuListView)
	moveTo(w, rowCenter(list, 0))
	if n := len(w.Overlays()); n != 2 {
		t.Fatalf("submenu did not open: overlays=%d", n)
	}
	moveTo(w, Point{X: 90, Y: 10})
	if mb.OpenIndex() != 1 || len(w.Overlays()) != 1 {
		t.Fatalf("after hovering Edit: open=%d overlays=%d, want 1/1", mb.OpenIndex(), len(w.Overlays()))
	}
	if got := topMenuLabels(w); got != "Copy;" {
		t.Fatalf("Edit dropdown shows %q", got)
	}
}
