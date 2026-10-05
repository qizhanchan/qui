package widgets

import (
	"fmt"
	"testing"

	. "github.com/qizhanchan/qui"
)

// ---- ContextMenu ----

func TestShowContextMenuPushesOverlay(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{
		{Label: "Copy", OnClick: func() {}},
		{Label: "Paste", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 100, 100, items)
	if popup == nil {
		t.Fatal("ShowContextMenu returned nil")
	}
	if len(w.Overlays()) != 1 {
		t.Errorf("overlays = %d, want 1", len(w.Overlays()))
	}
}

func TestContextMenuItemSelectionClosesPopup(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	clicked := false
	items := []*MenuItem{
		{Label: "Copy", OnClick: func() { clicked = true }},
	}
	popup := ShowContextMenu(w, 50, 50, items)

	// Find the first menu-item widget inside the popup's content and
	// trigger activation through the MenuActivatable interface — the
	// concrete type (now *menuItemView) is intentionally unexported.
	container := popup.Content.(*menuListView).content
	container.ChildAt(0).(MenuActivatable).ActivateMenuRow()

	if !clicked {
		t.Error("menu item OnClick did not fire")
	}
	if len(w.Overlays()) != 0 {
		t.Errorf("menu item selection should close popup; overlays = %d", len(w.Overlays()))
	}
}

func TestContextMenuDismissesOnWindowResize(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{{Label: "Sheet 1", OnClick: func() {}}}
	if ShowContextMenu(w, 100, 100, items) == nil {
		t.Fatal("ShowContextMenu returned nil")
	}
	if len(w.Overlays()) != 1 {
		t.Fatalf("overlays = %d, want 1 before resize", len(w.Overlays()))
	}
	w.ResizeForTest(Size{W: 500, H: 700})
	if len(w.Overlays()) != 0 {
		t.Errorf("point-anchored context menu should dismiss on resize; overlays = %d", len(w.Overlays()))
	}
}

func TestAnchoredMenuFollowsTriggerOnResize(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	// Trigger pinned to the bottom edge — the menu opens upward. On a taller
	// window the trigger moves down; the menu must follow, not vanish.
	anchor := Rect{X: 20, Y: 480, W: 80, H: 20}
	items := []*MenuItem{{Label: "Sheet 1", OnClick: func() {}}, {Label: "Sheet 2", OnClick: func() {}}}
	popup := ShowContextMenuForAnchorFunc(w, func() Rect { return anchor }, items)
	if popup == nil {
		t.Fatal("ShowContextMenuForAnchorFunc returned nil")
	}
	beforeY := popup.Bounds().Y

	// Grow the window and move the trigger down with it, then resize.
	anchor.Y = 680
	w.ResizeForTest(Size{W: 500, H: 700})

	if len(w.Overlays()) != 1 {
		t.Fatalf("anchored menu should stay open on resize; overlays = %d", len(w.Overlays()))
	}
	if popup.Bounds().Y <= beforeY {
		t.Errorf("menu should follow trigger downward; Y %v -> %v", beforeY, popup.Bounds().Y)
	}
}

// ---- MenuBar ----

func TestMenuBarAddMenuAppendsTrigger(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	mb := NewMenuBar(w)
	mb.AddMenu("File", []*MenuItem{{Label: "New"}})
	mb.AddMenu("Edit", []*MenuItem{{Label: "Copy"}})

	if mb.ChildCount() != 2 {
		t.Errorf("expected 2 trigger buttons, got %d", mb.ChildCount())
	}
	if b, ok := mb.ChildAt(0).(*Button); !ok || b.Text != "File" {
		t.Errorf("first trigger wrong: %+v", mb.ChildAt(0))
	}
}

func TestMenuBarShowDropdownOpensPopup(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	mb := NewMenuBar(w)
	mb.AddMenu("File", []*MenuItem{{Label: "New"}})
	// Lay out so the trigger has real bounds.
	mb.Layout(Rect{X: 0, Y: 0, W: 300, H: 30})
	mb.menus[0].trigger.Layout(Rect{X: 0, Y: 0, W: 60, H: 26})

	mb.OpenMenu(0)
	// Dropdown uses ShowContextMenu which pushes an overlay.
	if len(w.Overlays()) != 1 {
		t.Errorf("dropdown did not push overlay; overlays=%d", len(w.Overlays()))
	}
}

func TestShowContextMenuAutoCloseChain(t *testing.T) {
	// Verify ShowContextMenu returns a popup whose onClose chain clears
	// the overlay stack — this is the standard dropdown lifecycle.
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{{Label: "A"}}
	popup := ShowContextMenu(w, 0, 0, items)
	popup.Close()
	if len(w.Overlays()) != 0 {
		t.Errorf("popup.Close should remove overlay; overlays=%d", len(w.Overlays()))
	}
}

// TestContextMenuFlipsUpNearBottomEdge is the regression for a menu
// being clipped when right-clicked near the window's bottom edge: it
// must flip above the cursor and stay fully inside the window.
func TestContextMenuFlipsUpNearBottomEdge(t *testing.T) {
	const winH float32 = 500
	w := NewTestWindow(Size{W: 500, H: winH})
	items := []*MenuItem{
		{Label: "Cut", OnClick: func() {}},
		{Label: "Copy", OnClick: func() {}},
		{Label: "Paste", OnClick: func() {}},
		{Label: "Format Document", OnClick: func() {}},
	}
	// Right-click 10px from the bottom — the menu can't fit downward.
	popup := ShowContextMenu(w, 50, winH-10, items)
	if popup == nil {
		t.Fatal("ShowContextMenu returned nil")
	}
	b := popup.Content.Bounds()
	if b.Y+b.H > winH+0.01 {
		t.Errorf("menu overflows bottom: y=%.1f h=%.1f winH=%.0f", b.Y, b.H, winH)
	}
	if b.Y < 0 {
		t.Errorf("menu overflows top: y=%.1f", b.Y)
	}
	// It should have flipped above the cursor, not stayed at y≈490.
	if b.Y >= winH-10 {
		t.Errorf("menu did not flip up near the bottom edge: y=%.1f", b.Y)
	}
}

// TestContextMenuScrollsWhenTallerThanWindow confirms a menu with more items
// than fit the window caps its height and scrolls (rather than overflowing off
// both edges or squishing its items to slivers) — the many-sheets case.
func TestContextMenuScrollsWhenTallerThanWindow(t *testing.T) {
	const winH float32 = 300
	w := NewTestWindow(Size{W: 400, H: winH})
	items := make([]*MenuItem, 40)
	for i := range items {
		items[i] = &MenuItem{Label: fmt.Sprintf("Sheet%d", i+1), OnClick: func() {}}
	}
	popup := ShowContextMenu(w, 20, 20, items)
	if popup == nil {
		t.Fatal("ShowContextMenu returned nil")
	}
	lv := popup.Content.(*menuListView)
	if lv.scroll == nil {
		t.Fatal("tall menu did not wrap its content in a ScrollView")
	}
	if b := lv.Bounds(); b.H > winH+0.01 {
		t.Errorf("menu height %.1f exceeds window %.0f", b.H, winH)
	}
	if lv.scroll.MaxScroll() <= 0 {
		t.Errorf("expected the list to be scrollable (MaxScroll > 0), got %.1f", lv.scroll.MaxScroll())
	}
	before := lv.scroll.ScrollOffset()
	lv.scroll.ScrollTo(before + 500)
	if lv.scroll.ScrollOffset() <= before {
		t.Errorf("scroll offset did not advance: %.1f -> %.1f", before, lv.scroll.ScrollOffset())
	}
}

// TestContextMenuTallNearBottomStaysAboveTrigger guards the bug where a long
// menu opened from a trigger near the bottom edge (the all-sheets ☰ button)
// filled the whole window and covered the trigger / bottom bar. The menu must
// open UPWARD and its bottom edge must not pass the trigger point.
func TestContextMenuTallNearBottomStaysAboveTrigger(t *testing.T) {
	const winH float32 = 400
	w := NewTestWindow(Size{W: 400, H: winH})
	items := make([]*MenuItem, 40)
	for i := range items {
		items[i] = &MenuItem{Label: fmt.Sprintf("Sheet%d", i+1), OnClick: func() {}}
	}
	triggerY := winH - 12 // a hair above the bottom, like the ☰ button
	popup := ShowContextMenu(w, 20, triggerY, items)
	lv := popup.Content.(*menuListView)
	if lv.scroll == nil {
		t.Fatal("tall menu should scroll")
	}
	b := lv.Bounds()
	if b.Y+b.H > triggerY+0.5 {
		t.Errorf("menu covers its trigger: bottom=%.1f, trigger=%.1f", b.Y+b.H, triggerY)
	}
	if b.Y < 0 {
		t.Errorf("menu top off-screen: %.1f", b.Y)
	}
}

// TestContextMenuNoScrollWhenItFits confirms a short menu is NOT wrapped in a
// ScrollView (no scrollbar, natural size).
func TestContextMenuNoScrollWhenItFits(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 600})
	items := []*MenuItem{
		{Label: "Cut", OnClick: func() {}},
		{Label: "Copy", OnClick: func() {}},
		{Label: "Paste", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 20, 20, items)
	lv := popup.Content.(*menuListView)
	if lv.scroll != nil {
		t.Error("short menu should not create a ScrollView")
	}
}

// TestContextMenuNoFlipWithRoom confirms a menu with room below opens at
// the cursor without repositioning.
func TestContextMenuNoFlipWithRoom(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{{Label: "Copy", OnClick: func() {}}}
	popup := ShowContextMenu(w, 80, 60, items)
	b := popup.Content.Bounds()
	if b.Y != 60 || b.X != 80 {
		t.Errorf("menu should open at the cursor (80,60); got (%.1f,%.1f)", b.X, b.Y)
	}
}

// TestContextMenuClampsRightEdge confirms a menu near the right edge is
// shifted left to stay inside the window.
func TestContextMenuClampsRightEdge(t *testing.T) {
	const winW float32 = 500
	w := NewTestWindow(Size{W: winW, H: 500})
	items := []*MenuItem{{Label: "Format Document", OnClick: func() {}}}
	popup := ShowContextMenu(w, winW-5, 50, items)
	b := popup.Content.Bounds()
	if b.X+b.W > winW+0.01 {
		t.Errorf("menu overflows right: x=%.1f w=%.1f winW=%.0f", b.X, b.W, winW)
	}
	if b.X < 0 {
		t.Errorf("menu overflows left: x=%.1f", b.X)
	}
}

// ---- New features ----

func TestContextMenuSeparatorRendersDivider(t *testing.T) {
	// Separator items must not be rendered as Buttons — they show as
	// thin divider Containers instead.
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{
		{Label: "Copy", OnClick: func() {}},
		{Separator: true},
		{Label: "Paste", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 0, 0, items)
	list := popup.Content.(*menuListView)
	if list.content.ChildCount() != 3 {
		t.Fatalf("got %d children, want 3", list.content.ChildCount())
	}
	if _, isItem := list.content.ChildAt(1).(MenuActivatable); isItem {
		t.Error("separator rendered as a menu item, expected a divider widget")
	}
}

func TestContextMenuCheckableTogglesChecked(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	item := &MenuItem{Label: "Show gridlines", Checkable: true, Checked: false, OnClick: func() {}}
	popup := ShowContextMenu(w, 0, 0, []*MenuItem{item})
	list := popup.Content.(*menuListView)
	list.content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
	if !item.Checked {
		t.Error("expected Checked=true after first click")
	}
	// ShowContextMenu closes the popup on select; reopen for a second click.
	popup = ShowContextMenu(w, 0, 0, []*MenuItem{item})
	list = popup.Content.(*menuListView)
	list.content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
	if item.Checked {
		t.Error("expected Checked=false after second click")
	}
}

func TestContextMenuSubmenuOpensChildPopup(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{
		{Label: "Zoom", Submenu: []*MenuItem{
			{Label: "100%", OnClick: func() {}},
			{Label: "150%", OnClick: func() {}},
		}},
	}
	popup := ShowContextMenu(w, 0, 0, items)
	if len(w.Overlays()) != 1 {
		t.Fatalf("parent popup overlay count = %d, want 1", len(w.Overlays()))
	}
	list := popup.Content.(*menuListView)
	list.content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
	if len(w.Overlays()) != 2 {
		t.Errorf("submenu should push a second overlay; got %d", len(w.Overlays()))
	}
}

func TestMenuListKeyboardNav(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	fired := ""
	items := []*MenuItem{
		{Label: "A", OnClick: func() { fired = "A" }},
		{Label: "B", OnClick: func() { fired = "B" }},
		{Label: "C", OnClick: func() { fired = "C" }},
	}
	popup := ShowContextMenu(w, 0, 0, items)
	list := popup.Content.(*menuListView)
	if list.focused != 0 {
		t.Fatalf("initial focused = %d, want 0", list.focused)
	}
	list.Handle(NewKeyEvent(EventKeyDown, KeyDown, 0))
	if list.focused != 1 {
		t.Errorf("after KeyDown: focused = %d, want 1", list.focused)
	}
	list.Handle(NewKeyEvent(EventKeyDown, KeyDown, 0))
	list.Handle(NewKeyEvent(EventKeyDown, KeyEnter, 0))
	if fired != "C" {
		t.Errorf("Enter should fire item[2], got %q", fired)
	}
}

func TestMenuListKeyboardNavSkipsSeparator(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{
		{Label: "A", OnClick: func() {}},
		{Separator: true},
		{Label: "B", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 0, 0, items)
	list := popup.Content.(*menuListView)
	list.Handle(NewKeyEvent(EventKeyDown, KeyDown, 0))
	if list.focused != 2 {
		t.Errorf("focused = %d, want 2 (separator skipped)", list.focused)
	}
}

func TestContextMenuCloseRestoresPreviousFocus(t *testing.T) {
	main := newFocusableSpy("main")
	root := NewContainer(nil, main)
	w := windowWithRoot(Size{W: 500, H: 500}, root)
	w.SetFocus(main)

	popup := ShowContextMenu(w, 20, 20, []*MenuItem{{Label: "Save", OnClick: func() {}}})
	if popup == nil {
		t.Fatal("ShowContextMenu returned nil")
	}
	if w.Focused() == main {
		t.Fatal("context menu did not take focus")
	}

	popup.Close()

	if w.Focused() != main || !main.focused {
		t.Fatalf("focus after context menu close = %v, want main", w.Focused())
	}
}

// TestContextMenuCloseInvalidatesShadowHalo regression-guards the fix
// for "menu opened upward leaves residual shadow pixels on the trigger
// button after close". The framework's default overlay invalidation
// covers only widget.Bounds(); the menu's level-2 elevation extends
// ~12 px past Bounds via DrawShadow. menuListView declares the inflated
// rect via OverlayHaloRect; Popup.Close consumes it and unions it into
// the window's dirty region before RemoveOverlay's bounds-only call.
//
// Verifies by checking that the post-close dirty rect strictly contains
// the inflated halo, not just the menu surface.
func TestContextMenuCloseInvalidatesShadowHalo(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	items := []*MenuItem{{Label: "Save", OnClick: func() {}}}
	popup := ShowContextMenu(w, 100, 100, items)
	if popup == nil {
		t.Fatal("ShowContextMenu returned nil")
	}
	list := popup.Content.(*menuListView)
	menuBounds := list.Bounds()

	// Clear any prior dirty state from the show frame so we measure
	// only what Close contributes.
	w.ClearDirtyRegion()

	popup.Close()

	dirty := w.DirtyRegion()
	// The halo extends menuShadowMargin (12) past the menu surface
	// on every side. Verify the post-close dirty rect covers ALL of it,
	// not just the menu bounds.
	want := Rect{
		X: menuBounds.X - menuShadowMargin,
		Y: menuBounds.Y - menuShadowMargin,
		W: menuBounds.W + 2*menuShadowMargin,
		H: menuBounds.H + 2*menuShadowMargin,
	}
	if dirty.X > want.X || dirty.Y > want.Y ||
		dirty.X+dirty.W < want.X+want.W ||
		dirty.Y+dirty.H < want.Y+want.H {
		t.Errorf("close dirty rect %+v does not cover halo %+v (menu bounds were %+v)", dirty, want, menuBounds)
	}
}

func TestMenuBarAcceleratorRegistryHarvestsShortcuts(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	saveFired := 0
	mb := NewMenuBar(w)
	mb.AddMenu("File", []*MenuItem{
		{Label: "Save", Shortcut: "Cmd+S", OnClick: func() { saveFired++ }},
		{Separator: true},
		{Label: "Zoom", Submenu: []*MenuItem{
			{Label: "Zoom in", Shortcut: "Cmd+=", OnClick: func() {}},
		}},
	})
	reg := mb.AcceleratorRegistry()
	if reg.Len() != 2 {
		t.Errorf("registry Len = %d, want 2 (Save + Zoom in)", reg.Len())
	}
	if !reg.Match(NewKeyEvent(EventKeyDown, KeyS, ModSuper)) {
		t.Error("Cmd+S should match")
	}
	if saveFired != 1 {
		t.Errorf("saveFired = %d, want 1", saveFired)
	}
}

// A leaf click has to dismiss the WHOLE chain, not one level of it.
//
// The old closeChain closed its own popup and called parent.Close() once,
// relying on a comment that said the parent would close its own parent —
// which nothing implemented. Two-level menus therefore looked fine and
// three-level menus left the grandparent on screen, covering the document
// the command had just changed.
func TestSubmenuLeafClickClosesTheWholeChain(t *testing.T) {
	for _, depth := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("depth%d", depth), func(t *testing.T) {
			w := NewTestWindow(Size{W: 600, H: 600})
			fired := false
			leaf := &MenuItem{Label: "Leaf", OnClick: func() { fired = true }}
			items := []*MenuItem{leaf}
			for i := 1; i < depth; i++ {
				items = []*MenuItem{{Label: fmt.Sprintf("Level%d", i), Submenu: items}}
			}
			popup := ShowContextMenu(w, 0, 0, items)

			list := popup.Content.(*menuListView)
			for i := 0; i < depth; i++ {
				list.content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
				if i == depth-1 {
					break
				}
				overlays := w.Overlays()
				if len(overlays) != i+2 {
					t.Fatalf("after opening level %d: %d overlays, want %d", i+1, len(overlays), i+2)
				}
				sub, ok := overlays[len(overlays)-1].(*Popup)
				if !ok {
					t.Fatalf("overlay %d is not a Popup", len(overlays)-1)
				}
				list = sub.Content.(*menuListView)
			}
			if !fired {
				t.Fatal("the leaf's OnClick did not run")
			}
			if n := len(w.Overlays()); n != 0 {
				t.Fatalf("%d menu overlay(s) still open after the leaf ran; the whole chain must close", n)
			}
		})
	}
}

// Escape from a submenu closes the chain too — the same walk.
func TestSubmenuEscapeClosesTheWholeChain(t *testing.T) {
	w := NewTestWindow(Size{W: 600, H: 600})
	items := []*MenuItem{{Label: "A", Submenu: []*MenuItem{
		{Label: "B", Submenu: []*MenuItem{{Label: "C", OnClick: func() {}}}},
	}}}
	popup := ShowContextMenu(w, 0, 0, items)
	list := popup.Content.(*menuListView)
	list.content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
	sub := w.Overlays()[1].(*Popup)
	sub.Content.(*menuListView).content.ChildAt(0).(MenuActivatable).ActivateMenuRow()
	deep := w.Overlays()[2].(*Popup).Content.(*menuListView)
	deep.Handle(NewKeyEvent(EventKeyDown, KeyEscape, 0))
	if n := len(w.Overlays()); n != 0 {
		t.Fatalf("%d overlay(s) still open after Escape in a third-level submenu", n)
	}
}

// A menu longer than the window scrolls — and its items still have to BE
// there. The scroll wrapper adopts the content, and a parent never has a
// child taken from it without agreeing (AdoptWidgetTree), so the view has to
// release it: without that the ScrollView came up empty and the whole menu
// drew as a blank panel with a scrollbar.
func TestLongMenuScrollsWithItsItemsIntact(t *testing.T) {
	const winH float32 = 400
	w := NewTestWindow(Size{W: 500, H: winH})
	items := make([]*MenuItem, 40)
	for i := range items {
		items[i] = &MenuItem{Label: "Item " + string(rune('A'+i%26)), OnClick: func() {}}
	}
	popup := ShowContextMenu(w, 10, 10, items)
	defer popup.Close()

	lv := findMenuList(popup)
	if lv == nil {
		t.Fatal("no menu list view in the popup")
	}
	if lv.scroll == nil {
		t.Fatalf("a %d-item menu in a %.0fpx window did not scroll", len(items), winH)
	}
	if lv.scroll.Content == nil {
		t.Fatal("the scroll wrapper has no content: the menu would draw empty")
	}
	if got := lv.content.Parent(); got != Widget(lv.scroll) {
		t.Fatalf("content parent = %T, want the scroll wrapper", got)
	}
	// The items are reachable from the widget tree, which is what the
	// accessibility walk (and every agent-driven click) follows.
	found := 0
	var walk func(Widget)
	walk = func(node Widget) {
		if _, ok := node.(*menuItemView); ok {
			found++
		}
		if cl, ok := node.(interface{ ChildList() []Widget }); ok {
			for _, c := range cl.ChildList() {
				walk(c)
			}
		}
	}
	walk(lv)
	if found != len(items) {
		t.Fatalf("%d items reachable through ChildList, want %d", found, len(items))
	}
}

// findMenuList digs the list view out of a popup's widget tree.
func findMenuList(popup *Popup) *menuListView {
	var found *menuListView
	var walk func(Widget)
	walk = func(node Widget) {
		if lv, ok := node.(*menuListView); ok && found == nil {
			found = lv
			return
		}
		if cl, ok := node.(interface{ ChildList() []Widget }); ok {
			for _, c := range cl.ChildList() {
				walk(c)
			}
		}
	}
	walk(popup)
	return found
}

// ---- MenuItem.Panel ----

// A Panel row hands the menu over to caller content: the widget is in the
// tree (so it hit-tests, paints and shows up in the accessibility walk), the
// row itself is not activatable, and the panel's `close` unwinds the popup —
// the contract a Panel row provides.
func TestMenuPanelRowHostsCallerContent(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	content := newFocusableSpy("picker")
	var closePanel func()
	items := []*MenuItem{
		{Panel: func(close func()) Widget { closePanel = close; return content }},
		{Separator: true},
		{Label: "Insert table…", OnClick: func() {}},
	}
	popup := ShowContextMenu(w, 0, 0, items)
	lv := findMenuList(popup)
	if lv == nil {
		t.Fatal("no menu list view in the popup")
	}
	// One widget per item, in item order: menuListView.activate reaches a row
	// by its ITEM index, so a panel that appended nothing would misdirect
	// every keyboard activation after it.
	if got := lv.content.ChildCount(); got != len(items) {
		t.Fatalf("row count = %d, want %d", got, len(items))
	}
	if _, isItem := lv.content.ChildAt(0).(MenuActivatable); isItem {
		t.Error("the panel row is activatable; its content owns the pointer")
	}
	found := false
	var walk func(Widget)
	walk = func(node Widget) {
		if node == Widget(content) {
			found = true
			return
		}
		if cl, ok := node.(interface{ ChildList() []Widget }); ok {
			for _, c := range cl.ChildList() {
				walk(c)
			}
		}
	}
	walk(lv)
	if !found {
		t.Error("the panel content is not reachable through ChildList")
	}
	if closePanel == nil {
		t.Fatal("the panel was never given a close callback")
	}
	closePanel()
	if len(w.Overlays()) != 0 {
		t.Errorf("close() left %d overlays up, want 0", len(w.Overlays()))
	}
}

// Keyboard navigation treats a panel row like a separator: there is nothing
// to activate on it, so focus lands on — and steps to — the real commands.
func TestMenuPanelRowSkippedByKeyboardNav(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	fired := ""
	items := []*MenuItem{
		{Panel: func(func()) Widget { return newFocusableSpy("picker") }},
		{Separator: true},
		{Label: "Insert table…", OnClick: func() { fired = "insert" }},
	}
	popup := ShowContextMenu(w, 0, 0, items)
	lv := findMenuList(popup)
	if lv.focused != 2 {
		t.Fatalf("initial focused = %d, want 2 (the panel row is not selectable)", lv.focused)
	}
	lv.Handle(NewKeyEvent(EventKeyDown, KeyDown, 0))
	if lv.focused != 2 {
		t.Errorf("after KeyDown: focused = %d, want 2 (wrapped past the panel)", lv.focused)
	}
	lv.Handle(NewKeyEvent(EventKeyDown, KeyEnter, 0))
	if fired != "insert" {
		t.Errorf("Enter fired %q, want the Insert table… command", fired)
	}
}
