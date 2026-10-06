package widgets_test

// Regression tests for gaps an app hits when it builds on the native
// widgets from its own module: each one drives only the public API, the
// way a downstream app would.

import (
	"encoding/json"
	"image"
	"reflect"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// fixedBox is a downstream leaf widget with a fixed natural size that can
// act as a drag source or drop target.
type fixedBox struct {
	qui.BaseWidget
	w, h      float32
	draggable bool
	droppable bool
	drops     int
	enters    int
}

func newFixedBox(w, h float32) *fixedBox {
	b := &fixedBox{BaseWidget: qui.NewBaseWidget(), w: w, h: h}
	b.SetSelf(b)
	return b
}
func (b *fixedBox) Measure(qui.Size) qui.Size { return qui.Size{W: b.w, H: b.h} }
func (b *fixedBox) Draggable() bool           { return b.draggable }
func (b *fixedBox) Droppable() bool           { return b.droppable }
func (b *fixedBox) Handle(e qui.Event) bool {
	if de, ok := e.(qui.DragEvent); ok {
		switch de.Type() {
		case qui.EventDrop:
			b.drops++
			return true
		case qui.EventDragEnter:
			b.enters++
		}
	}
	return false
}

func docWindow() *qui.Window {
	w := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, widgets.NewLabel("doc")))
	w.LayoutForTest()
	return w
}

func cmdKey(k qui.Key) qui.KeyEvent { return qui.NewKeyEvent(qui.EventKeyDown, k, qui.ModSuper) }

// A menu's shortcuts join the window's registry instead of replacing it:
// bindings other code made there (a scoped editor Cmd+F) keep working, a
// menu added later is picked up, and the remover takes only the menu's.
func TestMenuBarBindAcceleratorsKeepsOtherBindings(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	editor := widgets.NewInput("editor")
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, editor))
	w.LayoutForTest()
	w.SetFocus(editor)

	finds, opens, quits := 0, 0, 0
	if _, err := w.Accelerators().RegisterScoped("Cmd+F", editor, func() { finds++ }); err != nil {
		t.Fatal(err)
	}
	mb := widgets.NewMenuBar(w)
	mb.AddMenu("File", []*widgets.MenuItem{{Label: "Open", Shortcut: "Cmd+O", OnClick: func() { opens++ }}})
	remove := mb.BindAccelerators(w.Accelerators())
	mb.AddMenu("App", []*widgets.MenuItem{{Label: "Quit", Shortcut: "Cmd+Q", OnClick: func() { quits++ }}})

	for _, k := range []qui.Key{qui.KeyF, qui.KeyO, qui.KeyQ} {
		w.DispatchTestEvent(cmdKey(k))
	}
	if finds != 1 || opens != 1 || quits != 1 {
		t.Fatalf("after binding the menu: find=%d open=%d quit=%d, want 1 each (shortcuts %v)",
			finds, opens, quits, w.AcceleratorShortcuts())
	}
	remove()
	for _, k := range []qui.Key{qui.KeyF, qui.KeyO, qui.KeyQ} {
		w.DispatchTestEvent(cmdKey(k))
	}
	if finds != 2 || opens != 1 || quits != 1 {
		t.Fatalf("after removing the menu's bindings: find=%d open=%d quit=%d, want 2/1/1", finds, opens, quits)
	}
}

// A disabled item's shortcut — directly or through a disabled submenu —
// doesn't run, and a lower binding of the same key gets it instead.
func TestDisabledMenuItemShortcutPassesThrough(t *testing.T) {
	w := docWindow()
	saves, fallback, exports := 0, 0, 0
	if _, err := w.Accelerators().Bind("Cmd+S", func() { fallback++ }); err != nil {
		t.Fatal(err)
	}
	save := &widgets.MenuItem{Label: "Save", Shortcut: "Cmd+S", OnClick: func() { saves++ }, Disabled: true}
	export := &widgets.MenuItem{Label: "Export", Disabled: true, Submenu: []*widgets.MenuItem{
		{Label: "PDF", Shortcut: "Cmd+E", OnClick: func() { exports++ }},
	}}
	mb := widgets.NewMenuBar(w)
	mb.AddMenu("File", []*widgets.MenuItem{save, export})
	mb.BindAccelerators(w.Accelerators())

	w.DispatchTestEvent(cmdKey(qui.KeyS))
	w.DispatchTestEvent(cmdKey(qui.KeyE))
	if saves != 0 || exports != 0 || fallback != 1 {
		t.Fatalf("disabled: save=%d export=%d fallback=%d, want 0/0/1", saves, exports, fallback)
	}
	save.Disabled, export.Disabled = false, false
	w.DispatchTestEvent(cmdKey(qui.KeyS))
	w.DispatchTestEvent(cmdKey(qui.KeyE))
	if saves != 1 || exports != 1 || fallback != 1 {
		t.Fatalf("enabled: save=%d export=%d fallback=%d, want 1/1/1", saves, exports, fallback)
	}
}

func doubleClick(w *qui.Window, x, y float32) {
	for i := 0; i < 2; i++ {
		w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
		w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	}
}

// With widget rows the list selects (and activates) in the capture pass; a
// press the row widget doesn't consume bubbles back and must not activate
// a second time. One double-click opens the document once.
func TestWidgetRowsDoubleClickActivatesOnce(t *testing.T) {
	t.Run("ListView", func(t *testing.T) {
		w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
		lv := widgets.NewListView(nil)
		lv.SetRowFactory(5, func(int) qui.Widget { return newFixedBox(200, 24) })
		n := 0
		lv.OnActivate = func(int) { n++ }
		w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, lv))
		w.LayoutForTest()
		lv.Draw(&qui.RecordingCanvas{}) // build the visible rows
		b := lv.Bounds()
		doubleClick(w, b.X+20, b.Y+10)
		if n != 1 {
			t.Fatalf("OnActivate fired %d times for one double-click, want 1", n)
		}
	})
	t.Run("TableView", func(t *testing.T) {
		w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
		tv := widgets.NewTableView([]widgets.TableColumn{{Title: "Name"}}, nil)
		tv.SetRowFactory(5, func(int) qui.Widget { return newFixedBox(200, 24) })
		n := 0
		tv.OnActivate = func(int) { n++ }
		w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, tv))
		w.LayoutForTest()
		tv.Draw(&qui.RecordingCanvas{})
		b := tv.Bounds()
		doubleClick(w, b.X+20, b.Y+tv.HeaderHeight+10)
		if n != 1 {
			t.Fatalf("OnActivate fired %d times for one double-click, want 1", n)
		}
	})
}

// sortableModel is a downstream TableModel that sorts itself; keyed adds
// RowKey so the table can keep the selection on its record.
type sortableModel struct{ rows [][]string }

func (m *sortableModel) RowCount() int { return len(m.rows) }
func (m *sortableModel) CellText(r, c int) string {
	if r < 0 || r >= len(m.rows) || c < 0 || c >= len(m.rows[r]) {
		return ""
	}
	return m.rows[r][c]
}
func (m *sortableModel) SortBy(col int, desc bool) {
	less := func(i, j int) bool { return m.rows[i][col] < m.rows[j][col] }
	if desc {
		less = func(i, j int) bool { return m.rows[i][col] > m.rows[j][col] }
	}
	for i := 1; i < len(m.rows); i++ { // insertion sort: stable, tiny
		for j := i; j > 0 && less(j, j-1); j-- {
			m.rows[j], m.rows[j-1] = m.rows[j-1], m.rows[j]
		}
	}
}

type keyedModel struct{ sortableModel }

func (m *keyedModel) RowKey(r int) any { return m.rows[r][0] }

// Sorting moves records between row indexes: with RowKey the selection
// follows its record (and OnSelect reports the new index); without one the
// table clears it rather than leave it on a different record.
func TestTableSortKeepsSelectedRecord(t *testing.T) {
	header := func(tv *widgets.TableView) {
		tv.Handle(qui.NewMouseEvent(qui.EventMouseDown, 20, 5, qui.MouseButtonLeft, 0))
	}
	keyed := &keyedModel{sortableModel{rows: [][]string{{"Carol"}, {"Bob"}, {"Alice"}}}}
	tv := widgets.NewTableView([]widgets.TableColumn{{Title: "Name", Sortable: true}}, keyed)
	tv.Layout(qui.Rect{W: 200, H: 200})
	tv.Select(0) // Carol
	reported := -1
	tv.OnSelect = func(r int) { reported = r }
	header(tv)
	if got := keyed.CellText(tv.SelectedRow(), 0); got != "Carol" || reported != 2 {
		t.Fatalf("keyed: selection after sort = %q (OnSelect %d), want Carol at row 2", got, reported)
	}

	plain := &sortableModel{rows: [][]string{{"Carol"}, {"Bob"}, {"Alice"}}}
	tv = widgets.NewTableView([]widgets.TableColumn{{Title: "Name", Sortable: true}}, plain)
	tv.Layout(qui.Rect{W: 200, H: 200})
	tv.Select(0)
	header(tv)
	if tv.SelectedRow() != -1 {
		t.Fatalf("unkeyed: selection after sort = row %d (%q), want cleared",
			tv.SelectedRow(), plain.CellText(tv.SelectedRow(), 0))
	}
}

// Zero colors mean "theme", so switching zebra stripes / hover off takes
// NoStripe / NoHover.
func TestRowColorsNoStripe(t *testing.T) {
	stripedOddRow := func(colors widgets.RowColors) bool {
		tv := widgets.NewTableView([]widgets.TableColumn{{Title: "Name"}},
			widgets.NewTableModel([][]string{{"a"}, {"b"}, {"c"}}))
		tv.Colors = colors
		tv.Layout(qui.Rect{W: 200, H: 200})
		rc := &qui.RecordingCanvas{}
		tv.Draw(rc)
		rowY := tv.HeaderHeight + tv.RowHeight
		for _, f := range rc.Fills {
			if f.Y == rowY && f.H == tv.RowHeight {
				return true
			}
		}
		return false
	}
	if !stripedOddRow(widgets.RowColors{}) {
		t.Fatal("control: the theme stripe should paint the odd row")
	}
	if stripedOddRow(widgets.RowColors{NoStripe: true}) {
		t.Fatal("NoStripe still paints a zebra stripe")
	}
}

// LabelKey options re-resolve on a language switch while the Select is
// closed — the shown value, SelectedValue, AX value and type-ahead all
// speak the new language without the dropdown being opened.
func TestSelectFollowsLocaleWhileClosed(t *testing.T) {
	install(t, mapTranslator{
		"en": {"color.red": "Red", "color.blue": "Blue", "color.yellow": "Yellow"},
		"de": {"color.red": "Rot", "color.blue": "Blau", "color.yellow": "Gelb"},
	})
	qui.SetDefaultLocale("en")
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	sel := widgets.NewSelect(w, nil, nil)
	sel.SetOptions([]widgets.SelectOption{
		{Value: "red", LabelKey: "color.red", Label: "red"},
		{Value: "blue", LabelKey: "color.blue", Label: "blue"},
		{Value: "yellow", LabelKey: "color.yellow", Label: "yellow"},
	})
	sel.SetValue("blue")
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, sel))
	w.LayoutForTest()

	qui.SetDefaultLocale("de")
	if sel.SelectedValue() != "Blau" || sel.AccessibleValue() != "Blau" {
		t.Fatalf("after switching to de: SelectedValue=%q AccessibleValue=%q, want Blau",
			sel.SelectedValue(), sel.AccessibleValue())
	}
	w.SetFocus(sel)
	w.DispatchTestEvent(qui.NewCharEvent('g', 0)) // Gelb
	if sel.SelectedOptionValue() != "yellow" {
		t.Fatalf("type-ahead 'g' in de picked %q, want yellow", sel.SelectedOptionValue())
	}
}

// AX options say which choices can't be picked, their app-side value and
// their group, so an agent can address options language-independently.
func TestSelectAXOptionsCarryValueDisabledGroup(t *testing.T) {
	sel := widgets.NewSelect(nil, nil, nil)
	sel.SetOptions([]widgets.SelectOption{
		{Value: "fr", Label: "French", Group: "Europe"},
		{Value: "de", Label: "German", Group: "Europe", Disabled: true},
		{Label: "Other"},
	})
	raw, _ := json.Marshal(sel.AccessibleOptions())
	want := `[{"label":"French","value":"fr","group":"Europe"},` +
		`{"label":"German","value":"de","disabled":true,"group":"Europe"},{"label":"Other"}]`
	if string(raw) != want {
		t.Fatalf("AccessibleOptions\n got %s\nwant %s", raw, want)
	}
}

func darkTheme() qui.Theme {
	t := qui.LightTheme
	t.Surface = qui.Color{R: 0.10, G: 0.10, B: 0.11, A: 1}
	t.SurfaceSunken = qui.Color{R: 0.07, G: 0.07, B: 0.08, A: 1}
	t.SurfaceRaised = qui.Color{R: 0.14, G: 0.14, B: 0.15, A: 1}
	t.SurfaceStrong = qui.Color{R: 0.18, G: 0.18, B: 0.19, A: 1}
	t.SurfaceOverlay = qui.Color{R: 0.16, G: 0.16, B: 0.17, A: 1}
	t.Text = qui.Color{R: 0.92, G: 0.92, B: 0.92, A: 1}
	t.TextMuted = qui.Color{R: 0.60, G: 0.60, B: 0.60, A: 1}
	t.Border = qui.Color{R: 0.25, G: 0.25, B: 0.27, A: 1}
	t.BorderStrong = qui.Color{R: 0.35, G: 0.35, B: 0.37, A: 1}
	return t
}

// luminanceAt paints w at rect onto a magenta canvas and reads the
// luminance at (x, y).
func luminanceAt(w qui.Widget, rect qui.Rect, x, y int) float64 {
	img := image.NewRGBA(image.Rect(0, 0, int(rect.W)+4, int(rect.H)+4))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 0, 255, 255
	}
	w.Layout(rect)
	w.Draw(qui.NewImageCanvas(img))
	c := img.RGBAAt(x, y)
	return (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
}

// Built with the zero-config constructors, TextArea, TabView and MenuBar
// follow SetTheme like Input does.
func TestNativeChromeFollowsSetTheme(t *testing.T) {
	prev := *qui.CurrentTheme()
	qui.SetTheme(darkTheme())
	t.Cleanup(func() { qui.SetTheme(prev) })

	if l := luminanceAt(widgets.NewInput(""), qui.Rect{W: 200, H: 40}, 100, 20); l > 0.5 {
		t.Fatalf("control: Input background luminance %.2f under a dark theme", l)
	}
	if l := luminanceAt(widgets.NewTextArea(""), qui.Rect{W: 200, H: 80}, 100, 40); l > 0.5 {
		t.Errorf("TextArea background luminance %.2f under a dark theme", l)
	}
	tv := widgets.NewTabView(widgets.Tab{Title: "One", Content: widgets.NewLabel("")})
	if l := luminanceAt(tv, qui.Rect{W: 300, H: 160}, 290, 10); l > 0.5 {
		t.Errorf("TabView strip luminance %.2f under a dark theme", l)
	}
	mb := widgets.NewMenuBar(qui.NewTestWindow(qui.Size{W: 400, H: 300}))
	mb.AddMenu("File", nil)
	if l := luminanceAt(mb, qui.Rect{W: 300, H: 28}, 280, 14); l > 0.5 {
		t.Errorf("MenuBar background luminance %.2f under a dark theme", l)
	}
	if bg := mb.Style().Background; bg != (qui.Color{R: 0.95, G: 0.95, B: 0.95, A: 1}) {
		t.Errorf("MenuBar.Style().Background read back as %+v; drawing must not rewrite it", bg)
	}
}

// OnKeyDown sees Tab before focus navigation, so an autocomplete on a
// plain Input can take Tab to accept; a hook that passes leaves Tab moving
// focus as usual.
func TestOnKeyDownSeesTab(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	in, next := widgets.NewInput("search"), widgets.NewInput("next")
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, in, next))
	w.LayoutForTest()
	accept, seen := true, 0
	in.OnKeyDown(func(e qui.KeyEvent) bool {
		if e.Key == qui.KeyTab {
			seen++
			return accept
		}
		return false
	})
	w.SetFocus(in)
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyTab, 0))
	if seen != 1 || w.Focused() != qui.Widget(in) {
		t.Fatalf("consuming hook: saw Tab %d times, focus on %T; want 1 and the input kept", seen, w.Focused())
	}
	accept = false
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyTab, 0))
	if seen != 2 || w.Focused() != qui.Widget(next) {
		t.Fatalf("passing hook: saw Tab %d times, focus on %p; want 2 and the next field", seen, w.Focused())
	}
}

// Every On* registrar on widgets and windows returns a remover, so a
// component that hooks a long-lived widget on mount can let go on unmount.
func TestHookRegistrarsReturnRemovers(t *testing.T) {
	in := widgets.NewInput("")
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	registrars := map[string]any{
		"BaseWidget.OnFocus": in.OnFocus, "BaseWidget.OnBlur": in.OnBlur, "BaseWidget.OnKeyDown": in.OnKeyDown,
		"Window.OnActivate": w.OnActivate, "Window.OnMove": w.OnMove, "Window.OnMinimize": w.OnMinimize,
		"Window.OnFullscreenChange": w.OnFullscreenChange, "Window.OnClose": w.OnClose,
	}
	remover := reflect.TypeOf(func() {})
	for name, fn := range registrars {
		if ft := reflect.TypeOf(fn); ft.NumOut() != 1 || ft.Out(0) != remover {
			t.Errorf("%s returns %v, want a func() remover", name, ft)
		}
	}

	calls, focuses := 0, 0
	var removers []func()
	for mount := 0; mount < 3; mount++ {
		removers = append(removers, in.OnKeyDown(func(qui.KeyEvent) bool { calls++; return false }))
		removers = append(removers, in.OnFocus(func() { focuses++ }))
	}
	for _, rm := range removers {
		rm()
	}
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, in))
	w.SetFocus(in)
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyLeft, 0))
	if calls != 0 || focuses != 0 {
		t.Fatalf("removed hooks still ran: key=%d focus=%d", calls, focuses)
	}
}

// A click-through dropdown opened inside a dialog closes on a press
// elsewhere in that dialog, and stays open for a press on itself.
func TestClickThroughPopupDismissedInsideDialog(t *testing.T) {
	w := docWindow()
	field := newFixedBox(300, 200)
	widgets.NewDialog("Settings", field).Show(w)
	w.LayoutForTest()

	pop := widgets.NewPopup(newFixedBox(80, 40))
	pop.DismissOnOutsideClick, pop.ClickThrough = true, true
	fb := field.Bounds()
	pop.ShowAt(w, fb.X, fb.Y)
	press := func(x, y float32) {
		w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
		w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	}
	press(fb.X+10, fb.Y+10) // on the popup
	if !pop.IsShown() {
		t.Fatal("a press on the popup itself closed it")
	}
	press(fb.X+fb.W-10, fb.Y+fb.H-10) // elsewhere in the dialog
	if pop.IsShown() {
		t.Fatal("a press elsewhere in the dialog left the click-through popup open")
	}
}

// A popup taken off the overlay stack behind its back (PopOverlay, a
// generic "dismiss the top overlay") reports closed, fires its close
// callbacks once, and can be shown again.
func TestPopupRecoversFromExternalOverlayRemoval(t *testing.T) {
	w := docWindow()
	pop := widgets.NewPopup(newFixedBox(80, 40))
	closed := 0
	var reason widgets.PopupCloseReason = -1
	pop.OnClose = func() { closed++ }
	pop.OnCloseReason = func(r widgets.PopupCloseReason) { reason = r }
	pop.ShowAt(w, 10, 10)

	w.PopOverlay()
	if pop.IsShown() || closed != 1 || reason != widgets.PopupCloseRemoved {
		t.Fatalf("after PopOverlay: IsShown=%v closes=%d reason=%v", pop.IsShown(), closed, reason)
	}
	pop.ShowAt(w, 10, 10)
	if !pop.IsShown() || len(w.Overlays()) != 1 {
		t.Fatalf("re-show after external removal: IsShown=%v overlays=%d", pop.IsShown(), len(w.Overlays()))
	}
	pop.Close()
	if closed != 2 || reason != widgets.PopupCloseProgrammatic || len(w.Overlays()) != 0 {
		t.Fatalf("own Close: closes=%d reason=%v overlays=%d", closed, reason, len(w.Overlays()))
	}
}

// Escape cancels a drag in progress: nothing drops on release.
func TestEscapeCancelsDrag(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	src, dst := newFixedBox(100, 100), newFixedBox(100, 100)
	src.draggable, dst.droppable = true, true
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 50}, src, dst))
	w.LayoutForTest()
	s, d := src.Bounds(), dst.Bounds()

	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, s.X+50, s.Y+50, qui.MouseButtonLeft, 0))
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, s.X+80, s.Y+50, 0, 0))
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, d.X+50, d.Y+50, 0, 0))
	if _, dragging := w.DragInProgress(); !dragging || dst.enters == 0 {
		t.Fatalf("precondition: drag over the target (dragging=%v enters=%d)", dragging, dst.enters)
	}
	w.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEscape, 0))
	_, still := w.DragInProgress()
	w.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, d.X+50, d.Y+50, qui.MouseButtonLeft, 0))
	if still || dst.drops != 0 {
		t.Fatalf("after Escape: dragging=%v drops=%d, want false/0", still, dst.drops)
	}
}

// A focused Input inside a scrolled ScrollView reports its caret-blink
// repaint in WINDOW coordinates. Its Bounds are content coordinates there,
// so returning them repainted a region far off screen and the caret never
// visibly blinked.
func TestCaretBlinkRepaintsOnScreenInsideScrollView(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	in := widgets.NewInput("x")
	sv := widgets.NewScrollView()
	sv.SetContent(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, newFixedBox(300, 800), in), qui.Size{})
	w.SetRoot(sv)
	w.LayoutForTest()
	sv.ScrollTo(700)
	w.LayoutForTest()
	w.SetFocus(in)

	dirty := qui.TickWidget(w.Root(), time.Now().Add(time.Second))
	onScreen := qui.PaintBoundsInWindow(in)
	if onScreen.IsEmpty() || dirty.Intersect(onScreen).IsEmpty() {
		t.Fatalf("blink dirty rect %+v misses the field on screen at %+v (content-space bounds %+v)",
			dirty, onScreen, in.Bounds())
	}
}

// The caret spans the text's line box, not the whole field: in a tall
// field it stays text-sized and vertically centered on the text.
func TestInputCaretIsTextHeight(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	in := widgets.NewInput("")
	in.SetText("Hello")
	qui.UpdateStyle(in, func(s *qui.Style) { s.Height = 60 })
	w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, in))
	w.LayoutForTest()
	w.SetFocus(in)

	caret, field := in.CaretRect(), in.Bounds()
	if caret.H <= 0 || caret.H > field.H/2 {
		t.Fatalf("caret height %.1f in a %.1f-tall field, want text-sized", caret.H, field.H)
	}
	mid, fieldMid := caret.Y+caret.H/2, field.Y+field.H/2
	if d := mid - fieldMid; d < -6 || d > 6 {
		t.Fatalf("caret center %.1f is %.1f px off the field center %.1f", mid, d, fieldMid)
	}
}
