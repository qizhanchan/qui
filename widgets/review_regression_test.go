package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// Placeholder args may be slices / maps; SetTextKey must not panic comparing
// them (interface == on an uncomparable dynamic type panics).
func TestMessageKeySameArgsHandlesUncomparableValues(t *testing.T) {
	b := NewButton("", nil)
	b.SetTextKey("k", "items", []string{"a", "b"})
	b.SetTextKey("k", "items", []string{"a", "b"}) // must not panic
}

// Text is exported, so a shorter direct assignment can leave selection
// indices past the end. A clipboard shortcut must clamp, not slice-panic.
func TestTextAreaStaleSelectionAfterDirectTextWriteDoesNotPanic(t *testing.T) {
	ta := NewTextArea("")
	ta.Text = "hello"
	ta.ed.selStart, ta.ed.selEnd, ta.ed.cursor = 0, 5, 5
	ta.Text = "x"
	ta.SetFocused(true)
	ta.Handle(NewKeyEvent(EventKeyDown, KeyC, ModSuper|ModControl))
	ta.Handle(NewKeyEvent(EventKeyDown, KeyX, ModSuper|ModControl))
	// The clamped selection covered the single remaining rune, so cut
	// removed it. The point is that neither shortcut panicked.
	if ta.Text != "" {
		t.Fatalf("text = %q, want the clamped selection cut", ta.Text)
	}
}

// SetText is silent (TextChangeNotifier); a user-equivalent publish is
// explicit.
func TestInputSetTextIsSilent(t *testing.T) {
	in := NewInput("")
	n := 0
	in.OnChange = func(string) { n++ }
	in.SetText("hello")
	if n != 0 {
		t.Fatalf("SetText fired OnChange %d times", n)
	}
	in.NotifyTextChanged()
	if n != 1 {
		t.Fatalf("NotifyTextChanged fired OnChange %d times, want 1", n)
	}
}

// A dialog removed through the window (not its own Close) must report
// DialogCloseRemoved and become showable again.
func TestDialogExternalRemovalFiresOnClose(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	d := NewDialog("Title", NewLabel("body"))
	got := DialogCloseProgrammatic
	d.OnClose = func(r DialogCloseReason) { got = r }
	d.Show(w)
	if !d.IsShown() {
		t.Fatal("dialog did not show")
	}
	if !w.RemoveOverlay(d) {
		t.Fatal("RemoveOverlay did not find the dialog")
	}
	if d.IsShown() {
		t.Fatal("dialog still reports shown after external removal")
	}
	if got != DialogCloseRemoved {
		t.Fatalf("close reason = %v, want removed", got)
	}
}

// A viewport that grows after the list was scrolled must clamp scrollY, or
// the list paints blank.
func TestListViewClampsScrollAfterViewportGrows(t *testing.T) {
	items := make([]string, 100)
	lv := NewListView(NewListModel(items))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.ScrollTo(1000)
	if lv.scrollY == 0 {
		t.Fatal("setup: expected a non-zero scroll offset")
	}
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 5000})
	if lv.scrollY != 0 {
		t.Fatalf("scrollY = %v after the viewport grew past the content, want 0", lv.scrollY)
	}
}

// An explicit cross-axis size on auto-main content survives ensureContentSize.
func TestScrollViewKeepsExplicitCrossAxisSize(t *testing.T) {
	content := &fixedSizeWidget{BaseWidget: NewBaseWidget(), size: Size{W: 40, H: 300}}
	content.SetSelf(content)
	sv := NewScrollView()
	sv.SetContent(content, Size{W: 100, H: 0})
	sv.Layout(Rect{X: 0, Y: 0, W: 300, H: 100})
	if sv.ContentSize.W != 100 {
		t.Fatalf("ContentSize.W = %v, want the explicit 100", sv.ContentSize.W)
	}
}

type fixedSizeWidget struct {
	BaseWidget
	size Size
}

func (f *fixedSizeWidget) Measure(Size) Size { return f.size }

// Disabling a control mid-gesture must drop its transient state; otherwise
// re-enabling it resumes the drag on the next mouse move.
func TestDisableCancelsSliderDrag(t *testing.T) {
	s := NewSlider(0, 10, 5, nil)
	s.Layout(Rect{X: 0, Y: 0, W: 200, H: 40})
	s.Handle(NewMouseEvent(EventMouseDown, 100, 20, MouseButtonLeft, 0))
	if !s.dragging {
		t.Fatal("setup: slider did not start dragging")
	}
	s.SetEnabled(false)
	if s.dragging {
		t.Fatal("SetEnabled(false) left the slider dragging")
	}
}

// A disabled textarea advertises itself as focusable, unlike every other
// control.
func TestTextAreaFocusableHonorsEnabled(t *testing.T) {
	ta := NewTextArea("")
	if !ta.Focusable() {
		t.Fatal("enabled textarea should be focusable")
	}
	ta.SetEnabled(false)
	if ta.Focusable() {
		t.Fatal("disabled textarea should not be focusable")
	}
}

// Cursor blink must run for a focused textarea even when the pointer is not
// over it (Tab focus / programmatic focus).
func TestTextAreaBlinksWithoutHover(t *testing.T) {
	ta := NewTextArea("")
	ta.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	ta.SetFocused(true)
	ta.ed.blinkMS = time.Now().Add(-time.Second).UnixMilli()
	if dirty := ta.Tick(time.Now()); dirty.IsEmpty() {
		t.Fatal("focused textarea did not tick its caret")
	}
}

// Radio AX state follows what Draw shows, not only group.selected.
func TestRadioAccessibleStateFollowsChecked(t *testing.T) {
	r := NewRadioButton(NewRadioGroup(), "x")
	r.Checked = true
	if r.AccessibleState()&AXStateChecked == 0 {
		t.Fatal("AX reports unchecked while Checked is true")
	}
}

// Select reports the stable option Value (not the label) to OnChange when
// the rich option model is in use.
func TestSelectOnChangeReportsOptionValue(t *testing.T) {
	sel := NewSelect(nil, nil, nil)
	var got string
	sel.OnChange = func(_ int, v string) { got = v }
	sel.SetOptions([]SelectOption{{Label: "United States", Value: "us"}})
	sel.selectIndex(0)
	if got != "us" {
		t.Fatalf("OnChange value = %q, want the option value %q", got, "us")
	}
}

// A display:none / visibility:hidden leaf must not intercept clicks.
func TestHiddenLeafIsNotHit(t *testing.T) {
	btn := NewButton("x", nil)
	root := NewContainer(FlexLayout{}, btn)
	root.Layout(Rect{X: 0, Y: 0, W: 100, H: 100})
	btn.Layout(Rect{X: 0, Y: 0, W: 100, H: 100})
	btn.SetCollapsed(true)
	if hit := root.HitTest(Point{X: 10, Y: 10}); hit == Widget(btn) {
		t.Fatal("hidden button still intercepted the click")
	}
}

// ChildList must expose every widget row: window attach/detach walks it, so
// an off-screen row hidden from it kept a nil window after mounting.
func TestListViewOffscreenRowsGetWindow(t *testing.T) {
	rows := make([]Widget, 100)
	for i := range rows {
		rows[i] = NewButton("r", nil)
	}
	lv := NewListView(nil)
	lv.SetRowWidgets(rows)
	lv.Layout(Rect{W: 200, H: 100})
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(lv)
	if got := rows[90].(*Button).Window(); got != w {
		t.Fatalf("off-screen row window = %v after mounting, want the list's window", got)
	}
}

// Tab onto a row that is off screen scrolls the list to it and lays it out,
// so the focused control is visible and hit-testable.
func TestListViewTabScrollsOffscreenRowIntoView(t *testing.T) {
	rows := make([]Widget, 50)
	for i := range rows {
		rows[i] = NewBox(FlowLayout{}, NewButton("r", nil))
	}
	lv := NewListView(nil)
	lv.SetRowWidgets(rows)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(lv)
	lv.Layout(Rect{W: 200, H: 100})
	target := rows[40].(*Box).ChildList()[0]
	w.SetFocus(rows[39].(*Box).ChildList()[0])
	w.FocusNext()
	if w.Focused() != target {
		t.Fatalf("focused %T, want the button in row 40", w.Focused())
	}
	if b := target.Bounds(); b.IsEmpty() || !lv.Bounds().Intersects(b) {
		t.Fatalf("row 40 button bounds %v not inside the list viewport %v", b, lv.Bounds())
	}
}

// Tab between two fields inside one popup keeps the popup open; Tab out of
// it closes it.
func TestPopupTabBetweenFieldsKeepsPopupOpen(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	outside := NewInput("outside")
	w.SetRoot(NewBox(FlowLayout{}, outside))
	a, b := NewInput("a"), NewInput("b")
	p := NewPopup(NewBox(FlexLayout{Direction: Vertical}, a, b))
	p.ShowAt(w, 10, 10)
	w.SetFocus(a)
	w.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyTab, 0))
	if !p.IsShown() {
		t.Fatal("Tab from field a closed the popup")
	}
	if w.Focused() != Widget(b) {
		t.Fatalf("focused %T, want field b", w.Focused())
	}
	w.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyTab, 0))
	if p.IsShown() {
		t.Fatal("Tab out of the popup left it open")
	}
}

// A FieldSet scrolled inside a ScrollView still paints its own surface: the
// cull test must compare the clip in the canvas's local space.
func TestFieldSetPaintsInScrolledView(t *testing.T) {
	spacer := NewBox(FlowLayout{})
	spacer.Style().Height = 600
	spacer.Style().Width = 300
	fs := NewFieldSet("T", FlowLayout{})
	fs.Style().Height = 100
	fs.Style().Width = 300
	sv := NewScrollView()
	sv.SetContent(NewBox(FlexLayout{Direction: Vertical}, spacer, fs), Size{})
	sv.Layout(Rect{W: 300, H: 300})
	sv.ScrollTo(500)
	sv.Layout(Rect{W: 300, H: 300})
	rc := &RecordingCanvas{}
	rc.ClipRect(Rect{W: 300, H: 300})
	sv.Draw(rc)
	for _, r := range rc.Rounds {
		if r.Rect.H > 90 && r.Rect.H < 110 {
			return
		}
	}
	t.Fatalf("FieldSet surface not painted after scroll; rounds=%v", rc.Rounds)
}

// A placeholder struct that is Comparable() by type can still hold a slice
// in an interface field; == on it panics at runtime.
func TestMessageKeySameArgsHandlesStructWithSliceInInterface(t *testing.T) {
	type wrap struct{ V any }
	b := NewButton("", nil)
	b.SetTextKey("k", "items", wrap{[]string{"a"}})
	b.SetTextKey("k", "items", wrap{[]string{"a"}}) // must not panic
}

// The lazy row holding keyboard focus survives a scroll far past the
// overscan window; releasing it would silently drop focus.
func TestLazyListKeepsFocusedRowWhenScrolledAway(t *testing.T) {
	lv := NewListView(nil)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(lv)
	lv.Layout(Rect{W: 200, H: 100})
	lv.SetRowFactory(1000, func(int) Widget { return NewButton("r", nil) })
	row := lv.builtRow(0)
	if row == nil {
		t.Fatal("setup: row 0 not built")
	}
	w.SetFocus(row)
	lv.ScrollTo(lv.RowHeight * 500)
	if w.Focused() != row {
		t.Fatalf("focus moved to %v after scrolling the focused row away", w.Focused())
	}
	if lv.builtRow(0) != row {
		t.Fatal("focused lazy row was released by prune")
	}
	w.SetFocus(nil)
	lv.ScrollTo(lv.RowHeight * 600)
	if lv.builtRow(0) != nil {
		t.Fatal("row 0 still built after it lost focus and scrolled away")
	}
}

// Draw never runs the row factory or releases rows; rows the scroll offset
// exposed outside event handling are prepared by the next Tick instead.
func TestListViewDrawDoesNotBuildRows(t *testing.T) {
	lv := NewListView(nil)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(lv)
	lv.Layout(Rect{W: 200, H: 100})
	built := 0
	lv.SetRowFactory(1000, func(int) Widget { built++; return NewLabel("r") })
	before := built
	lv.scrollY = lv.RowHeight * 300 // moved behind the list's back
	lv.Draw(&RecordingCanvas{})
	if built != before {
		t.Fatalf("Draw ran the row factory %d times", built-before)
	}
	if dirty := lv.Tick(time.Now()); dirty.IsEmpty() {
		t.Fatal("Tick prepared rows but did not repaint the list")
	}
	if built == before {
		t.Fatal("Tick did not build the newly visible rows")
	}
}
