package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

func keyDown(k Key) KeyEvent { return NewKeyEvent(EventKeyDown, k, 0) }

func testCanvas(w, h int) Canvas { return NewImageCanvas(image.NewRGBA(image.Rect(0, 0, w, h))) }

func TestSelectOptionsValueModel(t *testing.T) {
	cb := NewSelect(nil, nil, nil)
	var got SelectOption
	cb.OnOptionChange = func(_ int, o SelectOption) { got = o }
	cb.SetOptions([]SelectOption{
		{Value: "de", Label: "German", Group: "Europe"},
		{Value: "fr", Label: "French", Group: "Europe"},
		{Value: "ja", Label: "Japanese", Group: "Asia", Disabled: true},
	})
	if len(cb.Items) != 3 || cb.Items[1] != "French" || cb.itemEnabled(2) {
		t.Fatalf("Items/ItemDisabled not synced: %v %v", cb.Items, cb.ItemDisabled)
	}
	if !cb.SetValue("fr") || got.Value != "fr" || cb.SelectedOptionValue() != "fr" {
		t.Fatalf("SetValue: got %+v, selected %q", got, cb.SelectedOptionValue())
	}
	// Type action by value works whatever the label language is.
	cb.SetText("de")
	if cb.SelectedOptionValue() != "de" {
		t.Fatalf("SetText by value selected %q", cb.SelectedOptionValue())
	}
	// Re-setting options keeps the selection by value.
	cb.SetOptions([]SelectOption{{Value: "en", Label: "English"}, {Value: "de", Label: "Deutsch"}})
	if cb.SelectedIdx != 1 {
		t.Fatalf("selection by value lost: idx %d", cb.SelectedIdx)
	}
}

func TestSelectUsesAttachedWindowAndGroups(t *testing.T) {
	cb := NewSelect(nil, nil, nil)
	cb.SetOptions([]SelectOption{{Label: "A", Group: "G1"}, {Label: "B", Group: "G1"}, {Label: "C", Group: "G2"}})
	opened, closed := 0, 0
	cb.OnOpen = func() { opened++ }
	cb.OnClose = func() { closed++ }
	w := windowWithRoot(Size{W: 500, H: 500}, cb)
	cb.Layout(Rect{X: 10, Y: 10, W: 120, H: 30})
	cb.openDropdown()
	if !cb.IsOpen() || len(w.Overlays()) != 1 || opened != 1 {
		t.Fatalf("a Select built with a nil window should open in its attached window")
	}
	list := cb.popup.Content.(*menuListView)
	// Two headings + three options.
	if len(list.items) != 5 || list.items[0].Label != "G1" || !list.items[0].Disabled {
		t.Fatalf("group headings not inserted: %d items", len(list.items))
	}
	cb.closeDropdown()
	if closed != 1 {
		t.Fatal("OnClose not fired")
	}
}

func TestSubmenuLeftClosesOnlyItself(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	items := []*MenuItem{{Label: "Format", Submenu: []*MenuItem{{Label: "Bold"}}}}
	ShowContextMenu(w, 10, 10, items)
	root := w.Overlays()[0].(*Popup).Content.(*menuListView)
	root.layoutRowsForTest()
	root.Handle(keyDown(KeyRight))
	if len(w.Overlays()) != 2 {
		t.Fatalf("Right should open the submenu, overlays=%d", len(w.Overlays()))
	}
	sub := w.Overlays()[1].(*Popup).Content.(*menuListView)
	sub.Handle(keyDown(KeyLeft))
	if len(w.Overlays()) != 1 {
		t.Fatalf("Left in a submenu should close only it, overlays=%d", len(w.Overlays()))
	}
}

func (lv *menuListView) layoutRowsForTest() {
	lv.Measure(Size{W: 400, H: 400})
	lv.Layout(Rect{X: 10, Y: 10, W: 200, H: 200})
}

func TestMenuBarArrowSwitchesMenus(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 600})
	mb := NewMenuBar(nil)
	mb.AddMenu("File", []*MenuItem{{Label: "New"}})
	mb.AddMenu("Edit", []*MenuItem{{Label: "Copy"}})
	w.SetRoot(mb)
	mb.Layout(Rect{W: 300, H: 30})
	mb.OpenMenu(0)
	list := w.Overlays()[0].(*Popup).Content.(*menuListView)
	list.Handle(keyDown(KeyRight))
	if mb.OpenIndex() != 1 || len(w.Overlays()) != 1 {
		t.Fatalf("Right should move to Edit: open=%d overlays=%d", mb.OpenIndex(), len(w.Overlays()))
	}
	list = w.Overlays()[0].(*Popup).Content.(*menuListView)
	list.Handle(keyDown(KeyLeft))
	if mb.OpenIndex() != 0 {
		t.Fatalf("Left should move back to File: open=%d", mb.OpenIndex())
	}
}

type activatablePanel struct {
	BaseWidget
	activated   int
	highlighted bool
}

func (p *activatablePanel) ActivateMenuRow()           { p.activated++ }
func (p *activatablePanel) SetMenuHighlighted(on bool) { p.highlighted = on }
func (p *activatablePanel) Measure(Size) Size          { return Size{W: 100, H: 30} }

func TestPanelRowJoinsKeyboardNav(t *testing.T) {
	panel := &activatablePanel{BaseWidget: NewBaseWidget()}
	items := []*MenuItem{{Label: "A"}, {Panel: func(func()) Widget { return panel }}}
	_, list := buildMenuPopup(nil, nil, items)
	list.Handle(keyDown(KeyDown))
	if list.focused != 1 || !panel.highlighted {
		t.Fatalf("panel row should be reachable: focused=%d highlighted=%v", list.focused, panel.highlighted)
	}
	list.Handle(keyDown(KeyEnter))
	if panel.activated != 1 {
		t.Fatal("Enter should activate the panel row")
	}
}

func TestPopupVetoAndClickThrough(t *testing.T) {
	under := newPhaseSpy("under", Rect{W: 400, H: 400}, nil)
	w := windowWithRoot(Size{W: 400, H: 400}, under)
	content := newPhaseSpy("content", Rect{W: 50, H: 50}, nil)
	p := NewPopup(content)
	allow := false
	var reasons []PopupCloseReason
	p.CanClose = func(PopupCloseReason) bool { return allow }
	p.OnCloseReason = func(r PopupCloseReason) { reasons = append(reasons, r) }
	p.ShowAt(w, 380, 380) // clamps into the window
	if b := p.Bounds(); b.X+b.W > 400 || b.Y+b.H > 400 {
		t.Fatalf("ShowAt should clamp to the window, got %v", b)
	}
	p.Handle(keyDown(KeyEscape))
	if !p.IsShown() {
		t.Fatal("CanClose=false should veto Escape")
	}
	allow = true
	p.Handle(keyDown(KeyEscape))
	if p.IsShown() || len(reasons) != 1 || reasons[0] != PopupCloseEscape {
		t.Fatalf("close reasons %v", reasons)
	}

	p2 := NewPopup(newPhaseSpy("c2", Rect{W: 50, H: 50}, nil))
	p2.ClickThrough = true
	p2.ShowAt(w, 0, 0)
	if hit := w.WidgetAt(Point{X: 200, Y: 200}); hit != under {
		t.Fatalf("a click-through popup must not claim outside hits, got %T", hit)
	}
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 200, 200, MouseButtonLeft, 0))
	if p2.IsShown() {
		t.Fatal("outside press should still dismiss a click-through popup")
	}
}

func TestListViewDoubleClickAndFactoryRows(t *testing.T) {
	lv := NewListView(NewListModel([]string{"a", "b", "c"}))
	w := windowWithRoot(Size{W: 300, H: 200}, lv)
	lv.Layout(Rect{W: 300, H: 200})
	activated := -1
	lv.OnActivate = func(i int) { activated = i }
	for i := 0; i < 2; i++ {
		w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 30, MouseButtonLeft, 0))
		w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 20, 30, MouseButtonLeft, 0))
	}
	if activated != 1 {
		t.Fatalf("double-click should activate row 1, got %d", activated)
	}

	built := 0
	lv.SetRowFactory(10000, func(i int) Widget {
		built++
		return NewLabel("row")
	})
	lv.Draw(testCanvas(300, 200))
	if built == 0 || built > 40 {
		t.Fatalf("factory built %d rows for a 200px viewport", built)
	}
	if n := len(lv.ChildList()); n != built {
		t.Fatalf("ChildList has %d rows, built %d", n, built)
	}
	lv.ScrollTo(26 * 5000)
	lv.Draw(testCanvas(300, 200))
	if n := len(lv.ChildList()); n > 40 {
		t.Fatalf("rows scrolled far away weren't released: %d live", n)
	}
}

type sortModel struct {
	*SliceTableModel
	col  int
	desc bool
}

func (m *sortModel) SortBy(col int, desc bool) { m.col, m.desc = col, desc }

func TestTableHeaderSortAndCellRenderer(t *testing.T) {
	m := &sortModel{SliceTableModel: NewTableModel([][]string{{"1", "x"}, {"2", "y"}}), col: -1}
	drawn := 0
	cols := []TableColumn{
		{Title: "ID", Width: 100, Sortable: true, Align: TextAlignEnd},
		{Title: "Name", DrawCell: func(Canvas, TableCell) { drawn++ }},
	}
	tv := NewTableView(cols, m)
	w := windowWithRoot(Size{W: 300, H: 200}, tv)
	tv.Layout(Rect{W: 300, H: 200})
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 10, MouseButtonLeft, 0))
	if m.col != 0 || m.desc || tv.SortColumn != 0 {
		t.Fatalf("first header click: col=%d desc=%v", m.col, m.desc)
	}
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 20, 10, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 20, 10, MouseButtonLeft, 0))
	if !m.desc {
		t.Fatal("second click should flip to descending")
	}
	tv.Draw(testCanvas(300, 200))
	if drawn != 2 {
		t.Fatalf("DrawCell ran %d times, want 2", drawn)
	}
	ax := tv.AccessibleChildren()
	if len(ax) != 2+4 || ax[0].Role != RoleColumnHeader {
		t.Fatalf("AX children %d", len(ax))
	}
}

func TestTabViewCloseVetoDisabled(t *testing.T) {
	tv := NewTabView(
		Tab{Title: "One", Content: NewLabel("1")},
		Tab{Title: "Two", Content: NewLabel("2"), Disabled: true},
		Tab{Title: "Three", Content: NewLabel("3"), Closable: true},
	)
	windowWithRoot(Size{W: 600, H: 300}, tv)
	tv.Layout(Rect{W: 600, H: 300})
	tv.Select(1)
	if tv.SelectedIdx != 0 {
		t.Fatal("a disabled tab must not be selectable")
	}
	tv.CanSelect = func(from, to int) bool { return false }
	tv.Select(2)
	if tv.SelectedIdx != 0 {
		t.Fatal("CanSelect=false should veto")
	}
	tv.CanSelect = nil
	closed := -1
	tv.OnCloseTab = func(i int) { closed = i; tv.RemoveTab(i) }
	r := closeRect(tv.tabRects()[2])
	tv.Handle(NewMouseEvent(EventMouseDown, r.X+r.W/2, r.Y+r.H/2, MouseButtonLeft, 0))
	if closed != 2 || len(tv.Tabs) != 2 {
		t.Fatalf("close button: closed=%d tabs=%d", closed, len(tv.Tabs))
	}
	if ax := tv.AccessibleChildren(); len(ax) != 2 || ax[1].State&AXStateDisabled == 0 {
		t.Fatalf("tab AX children wrong: %+v", ax)
	}
}

func TestFitTabsScrollSelectedIntoView(t *testing.T) {
	var tabs []Tab
	for i := 0; i < 20; i++ {
		tabs = append(tabs, Tab{Title: "A fairly long tab title"})
	}
	tv := NewTabView(tabs...)
	tv.FitTabs = true
	tv.StripHeight = 32
	windowWithRoot(Size{W: 400, H: 300}, tv)
	tv.Layout(Rect{W: 400, H: 300})
	tv.Select(19)
	r := tv.tabRects()[19]
	if r.X+r.W > 400.5 || r.X < 0 || r.H != 32 {
		t.Fatalf("selected tab not scrolled into view: %v", r)
	}
}

func TestOnKeyDownHookRunsBeforeWidget(t *testing.T) {
	in := NewInput("")
	w := windowWithRoot(Size{W: 300, H: 100}, in)
	in.Layout(Rect{W: 200, H: 30})
	w.SetFocus(in)
	var seen []Key
	in.OnKeyDown(func(e KeyEvent) bool {
		seen = append(seen, e.Key)
		return e.Key == KeyDown
	})
	focused, blurred := 0, 0
	in.OnFocus(func() { focused++ })
	in.OnBlur(func() { blurred++ })
	w.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyDown, 0))
	w.SetFocus(nil)
	w.SetFocus(in)
	if len(seen) != 1 || blurred != 1 || focused != 1 {
		t.Fatalf("seen=%v focus=%d blur=%d", seen, focused, blurred)
	}
}

func TestScrollViewAutoContentSize(t *testing.T) {
	box := NewContainer(FlexLayout{Direction: Vertical})
	box.AddChild(NewLabel("one"))
	sv := NewScrollView()
	sv.SetContentAuto(box)
	windowWithRoot(Size{W: 200, H: 50}, sv)
	sv.Layout(Rect{W: 200, H: 50})
	h1 := sv.ContentSize.H
	for i := 0; i < 10; i++ {
		box.AddChild(NewLabel("more"))
	}
	sv.Layout(Rect{W: 200, H: 50})
	if sv.ContentSize.H <= h1 {
		t.Fatalf("auto content size didn't follow growth: %v → %v", h1, sv.ContentSize.H)
	}
}

func TestNativeControlsFollowTheme(t *testing.T) {
	saved := *CurrentTheme()
	defer SetTheme(saved)
	if themed(htmlControlBorder) != htmlControlBorder {
		t.Fatal("baseline theme must keep baseline pixels")
	}
	dark := saved
	dark.BorderStrong = Color{R: 0.5, G: 0.1, B: 0.1, A: 1}
	dark.Accent = Color{R: 0.1, G: 0.7, B: 0.2, A: 1}
	SetTheme(dark)
	if themed(htmlControlBorder) != dark.BorderStrong || themed(htmlAccent) != dark.Accent {
		t.Fatal("baseline colors should map to theme tokens after SetTheme")
	}
	custom := Color{R: 0.2, G: 0.3, B: 0.4, A: 1}
	if themed(custom) != custom {
		t.Fatal("app colors must pass through")
	}
}

func TestI18nKeysAndNameOverride(t *testing.T) {
	r := NewRadioButton(NewRadioGroup(), "Fallback")
	r.SetLabelKey("missing.key")
	if r.DisplayLabel() != "Fallback" || r.AccessibleNameKey() != "missing.key" {
		t.Fatalf("radio label key: %q", r.DisplayLabel())
	}
	in := NewInput("")
	in.Label = "Email"
	in.Placeholder = "you@example.com"
	if WidgetName(in) != "Email" {
		t.Fatalf("Input name should be its label, got %q", WidgetName(in))
	}
	in.SetAccessibleName("Work email")
	if WidgetName(in) != "Work email" || WidgetNameKey(in) != "" {
		t.Fatal("SetAccessibleName should override")
	}
	b := NewButton("x", nil)
	b.SetTooltipKey("missing.tip", "Delete row")
	if b.TooltipText() != "Delete row" {
		t.Fatalf("tooltip key fallback: %q", b.TooltipText())
	}
}
