package widgets

import (
	"fmt"
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

func manyRows(n int) *SliceListModel {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf("Item %d", i)
	}
	return NewListModel(items)
}

// ---- Basic model / selection ----

func TestSliceListModelCountAndText(t *testing.T) {
	m := NewListModel([]string{"a", "b", "c"})
	if m.RowCount() != 3 {
		t.Errorf("RowCount = %d, want 3", m.RowCount())
	}
	if m.RowText(1) != "b" {
		t.Errorf("RowText(1) = %q, want b", m.RowText(1))
	}
}

func TestListViewDefaultNoSelection(t *testing.T) {
	lv := NewListView(NewListModel([]string{"a", "b"}))
	if lv.SelectedIdx != -1 {
		t.Errorf("default SelectedIdx = %d, want -1", lv.SelectedIdx)
	}
}

func TestListViewSelectClampsAndFires(t *testing.T) {
	called := -1
	lv := NewListView(NewListModel([]string{"a", "b", "c"}))
	lv.OnSelect = func(i int) { called = i }
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})

	lv.Select(1)
	if lv.SelectedIdx != 1 || called != 1 {
		t.Errorf("Select(1): idx=%d called=%d", lv.SelectedIdx, called)
	}
	lv.Select(-5)
	if lv.SelectedIdx != 0 {
		t.Errorf("Select(-5) should clamp to 0, got %d", lv.SelectedIdx)
	}
	lv.Select(99)
	if lv.SelectedIdx != 2 {
		t.Errorf("Select(99) should clamp to last (2), got %d", lv.SelectedIdx)
	}
}

func TestListViewSelectSyncsPublicSelectedIdx(t *testing.T) {
	lv := NewListView(NewListModel([]string{"a", "b", "c"}))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.SelectedIdx = 99

	lv.Select(1)

	if lv.SelectedIdx != 1 {
		t.Fatalf("Select should sync from public SelectedIdx then select; got %d, want 1", lv.SelectedIdx)
	}
}

func TestListViewSelectedTextReturnsRow(t *testing.T) {
	lv := NewListView(NewListModel([]string{"alpha", "beta"}))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.Select(1)
	if got := lv.SelectedText(); got != "beta" {
		t.Errorf("SelectedText = %q, want beta", got)
	}
}

// ---- Virtualization: visibleRange ----

func TestListViewVisibleRangeAtTop(t *testing.T) {
	lv := NewListView(manyRows(1000))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	// RowHeight=26, viewport H=100 → visible = 100/26 + 1 = ~4.8 → 5 rows
	start, end := lv.visibleRange()
	if start != 0 {
		t.Errorf("start at scrollY=0 should be 0, got %d", start)
	}
	// End should be small (~5), not 1000.
	if end < 4 || end > 6 {
		t.Errorf("end at top should be ~5, got %d", end)
	}
}

func TestListViewVisibleRangeScrolled(t *testing.T) {
	lv := NewListView(manyRows(1000))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.scrollY = 26 * 50 // scroll past row 50
	start, end := lv.visibleRange()
	if start != 50 {
		t.Errorf("start after scrolling to row 50: got %d, want 50", start)
	}
	if end-start < 3 || end-start > 6 {
		t.Errorf("visible span = %d, want ~5", end-start)
	}
}

func TestListViewVisibleRangeClampedToRowCount(t *testing.T) {
	lv := NewListView(manyRows(5))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500}) // viewport taller than content
	start, end := lv.visibleRange()
	if start != 0 || end != 5 {
		t.Errorf("range for 5-row tall viewport: got (%d,%d), want (0,5)", start, end)
	}
}

// ---- Scrolling ----

func TestListViewWheelScrolls(t *testing.T) {
	lv := NewListView(manyRows(100))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.Handle(newScrollEvent(-1, 100, 50))
	if lv.scrollY == 0 {
		t.Error("wheel did not scroll")
	}
}

func TestListViewScrollClamps(t *testing.T) {
	lv := NewListView(manyRows(100))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.ScrollTo(-100)
	if lv.scrollY != 0 {
		t.Errorf("negative scroll should clamp to 0; got %v", lv.scrollY)
	}
	lv.ScrollTo(99999)
	if lv.scrollY != lv.maxScroll() {
		t.Errorf("over-scroll should clamp to maxScroll %v; got %v", lv.maxScroll(), lv.scrollY)
	}
}

func TestListViewNoScrollbarWhenContentFits(t *testing.T) {
	lv := NewListView(manyRows(3)) // 3 * 26 = 78 < 200
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	if lv.hasScrollbar() {
		t.Error("scrollbar should hide when content fits")
	}
}

// ---- Interaction ----

func TestListViewClickSelectsRow(t *testing.T) {
	lv := NewListView(manyRows(10))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})
	// Row 3 sits at y = 3*26 = 78..104, pick mid = 90.
	lv.Handle(newMouseEventForHandle(EventMouseDown, 50, 90))
	if lv.SelectedIdx != 3 {
		t.Errorf("click on row 3: SelectedIdx=%d, want 3", lv.SelectedIdx)
	}
}

func TestListViewDispatchClickInvalidatesSelectionBounds(t *testing.T) {
	w := NewTestWindow(Size{W: 220, H: 160})
	lv := NewListView(manyRows(10))
	w.SetRoot(lv)
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 120})
	lv.ClearLayoutDirty()
	w.ClearDirtyRegion()

	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 50, 90, MouseButtonLeft, 0))

	if lv.SelectedIdx != 3 {
		t.Fatalf("click selected row %d, want 3", lv.SelectedIdx)
	}
	if !w.DirtyRegion().Intersects(lv.Bounds()) {
		t.Fatalf("selection should invalidate list bounds; dirty=%v bounds=%v", w.DirtyRegion(), lv.Bounds())
	}
}

func TestListViewClickOnScrollbarDoesNotSelectRow(t *testing.T) {
	lv := NewListView(manyRows(100)) // enough rows to show scrollbar
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	// Scrollbar track area: x ∈ [190, 200]. Click on thumb.
	thumb := lv.barThumb()
	lv.Handle(newMouseEventForHandle(EventMouseDown, thumb.X+thumb.W/2, thumb.Y+thumb.H/2))
	if lv.SelectedIdx != -1 {
		t.Errorf("clicking scrollbar should not select a row; got %d", lv.SelectedIdx)
	}
	if !lv.barDragging {
		t.Error("scrollbar click should start drag")
	}
}

func TestListViewArrowKeysNavigate(t *testing.T) {
	lv := NewListView(manyRows(10))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})
	lv.SetFocused(true)

	lv.Handle(newKeyDown(KeyDown))
	if lv.SelectedIdx != 0 {
		t.Errorf("KeyDown from -1: got %d, want 0", lv.SelectedIdx)
	}
	lv.Handle(newKeyDown(KeyDown))
	lv.Handle(newKeyDown(KeyDown))
	if lv.SelectedIdx != 2 {
		t.Errorf("after 3 KeyDowns: got %d, want 2", lv.SelectedIdx)
	}
	lv.Handle(newKeyDown(KeyUp))
	if lv.SelectedIdx != 1 {
		t.Errorf("after KeyUp: got %d, want 1", lv.SelectedIdx)
	}
}

func TestListViewEnterFiresOnActivate(t *testing.T) {
	activated := -1
	lv := NewListView(manyRows(5))
	lv.OnActivate = func(i int) { activated = i }
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	lv.SetFocused(true)
	lv.Select(2)
	lv.Handle(newKeyDown(KeyEnter))
	if activated != 2 {
		t.Errorf("Enter did not fire OnActivate; got %d", activated)
	}
}

// ---- Auto-scroll to keep selection visible ----

func TestListViewSelectionAutoScrolls(t *testing.T) {
	lv := NewListView(manyRows(100))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.SetFocused(true)

	// Jump to row 50 — it's well past the initial viewport.
	lv.Select(50)
	if lv.scrollY == 0 {
		t.Errorf("selecting row 50 should auto-scroll; scrollY=%v", lv.scrollY)
	}
	// The row should now be within the visible range.
	start, end := lv.visibleRange()
	if 50 < start || 50 >= end {
		t.Errorf("row 50 not visible after select; range=[%d,%d)", start, end)
	}
}

func TestListViewSelectedRowNoScrollWhenAlreadyVisible(t *testing.T) {
	lv := NewListView(manyRows(100))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500}) // huge viewport
	lv.Select(3)
	// Row 3 was already visible from y=0; no scroll needed.
	if lv.scrollY != 0 {
		t.Errorf("selecting visible row should not scroll; got scrollY=%v", lv.scrollY)
	}
}

// ---- Model swap ----

func TestSetModelResetsSelectionAndScroll(t *testing.T) {
	lv := NewListView(manyRows(100))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.Select(10)
	lv.scrollY = 50

	lv.SetModel(NewListModel([]string{"new"}))
	if lv.SelectedIdx != -1 {
		t.Errorf("SetModel should reset selection; got %d", lv.SelectedIdx)
	}
	if lv.scrollY != 0 {
		t.Errorf("SetModel should reset scroll; got %v", lv.scrollY)
	}
}

// ---- rowAt edge cases ----

func TestRowAtOutsideBoundsReturnsMinusOne(t *testing.T) {
	lv := NewListView(manyRows(10))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	if lv.rowAt(500, 500) != -1 {
		t.Error("rowAt out-of-bounds should return -1")
	}
}

func TestRowAtIgnoresScrollbarColumn(t *testing.T) {
	lv := NewListView(manyRows(100)) // forces scrollbar
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	// Click in scrollbar column x=195 should not identify a row.
	if lv.rowAt(195, 50) != -1 {
		t.Error("rowAt in scrollbar column should return -1")
	}
}

func TestRowAtCorrectIndexAfterScroll(t *testing.T) {
	lv := NewListView(manyRows(100))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	lv.ScrollTo(26 * 10) // row 10 at top of viewport
	// Click at y=5 → still inside top row → row 10.
	idx := lv.rowAt(50, 5)
	if idx != 10 {
		t.Errorf("after scroll to row 10, rowAt(y=5) = %d, want 10", idx)
	}
}

// ---- Empty model ----

func TestListViewHandlesEmptyModel(t *testing.T) {
	lv := NewListView(NewListModel(nil))
	lv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})

	start, end := lv.visibleRange()
	if start != 0 || end != 0 {
		t.Errorf("empty model visibleRange: got (%d,%d), want (0,0)", start, end)
	}
	lv.Select(0) // should not crash, should not select
	if lv.SelectedIdx != -1 {
		t.Errorf("empty model Select(0) should stay -1; got %d", lv.SelectedIdx)
	}
}

type delegateRowWidget struct {
	BaseWidget
	boundIndex int
	boundText  string
	selected   bool
	hovered    bool
	focused    bool
}

func (w *delegateRowWidget) Draw(Canvas) {}
func (w *delegateRowWidget) HitTest(p Point) Widget {
	if w.Bounds().Contains(p) {
		return w
	}
	return nil
}

type listNoopCanvas struct{}

func (listNoopCanvas) Clear(Color)                                     {}
func (listNoopCanvas) FillRect(Rect, Color)                            {}
func (listNoopCanvas) FillRoundedRect(Rect, float32, Color)            {}
func (listNoopCanvas) StrokeRect(Rect, Color, float32)                 {}
func (listNoopCanvas) StrokeRoundedRect(Rect, float32, Color, float32) {}
func (listNoopCanvas) DrawText(string, Rect, Color, Font)              {}
func (listNoopCanvas) DrawImage(image.Image, Rect)                     {}
func (listNoopCanvas) DrawLine(Point, Point, Color, float32)           {}
func (listNoopCanvas) DrawPolyline([]Point, Color, float32)            {}
func (listNoopCanvas) DrawShadow(Rect, float32, ElevationSpec, Color)  {}
func (listNoopCanvas) DrawVector(VectorSource, Rect, Color)            {}
func (listNoopCanvas) DrawShape(Shape, Paint)                          {}

// Canvas (Phase B) state stack — no-op stubs. ClipBounds returns a
// large sentinel so ListView's "intersect outer clip with my body"
// arithmetic produces a non-empty rect and row drawing actually runs.
func (listNoopCanvas) Save() int                  { return 0 }
func (listNoopCanvas) SaveLayer(Rect, Paint) int  { return 0 }
func (listNoopCanvas) ClipPath(*Path)             {}
func (listNoopCanvas) Restore()                   {}
func (listNoopCanvas) RestoreTo(int)              {}
func (listNoopCanvas) ClipRect(Rect)              {}
func (listNoopCanvas) Scale(float32, float32)     {}
func (listNoopCanvas) Translate(float32, float32) {}
func (listNoopCanvas) Rotate(float32)             {}
func (listNoopCanvas) Concat(Matrix)              {}
func (listNoopCanvas) CurrentMatrix() Matrix      { return IdentityMatrix() }
func (listNoopCanvas) ClipBounds() Rect           { return Rect{X: -1e9, Y: -1e9, W: 2e9, H: 2e9} }

func TestListViewSetRowWidgetsSwitchesRowSource(t *testing.T) {
	lv := NewListView(NewListModel([]string{"a", "b"}))
	rowA := &delegateRowWidget{BaseWidget: NewBaseWidget()}
	rowB := &delegateRowWidget{BaseWidget: NewBaseWidget()}
	lv.SetRowWidgets([]Widget{rowA, rowB})

	if lv.RowCount() != 2 {
		t.Fatalf("widget-row mode RowCount=%d, want 2", lv.RowCount())
	}
	if lv.Model != nil {
		t.Fatal("SetRowWidgets should clear text model")
	}
	if got := lv.SelectedText(); got != "" {
		t.Fatalf("SelectedText in widget-row mode=%q, want empty", got)
	}
}

func TestListViewSetRowWidgetsWiresAndDetachesParent(t *testing.T) {
	lv := NewListView(nil)
	rowA := &delegateRowWidget{BaseWidget: NewBaseWidget()}
	rowB := &delegateRowWidget{BaseWidget: NewBaseWidget()}

	lv.SetRowWidgets([]Widget{rowA})
	if rowA.Parent() != lv {
		t.Fatalf("rowA parent=%T, want *ListView", rowA.Parent())
	}

	lv.SetRowWidgets([]Widget{rowB})
	if rowA.Parent() != nil {
		t.Fatal("old row should be detached when row set is replaced")
	}
	if rowB.Parent() != lv {
		t.Fatalf("rowB parent=%T, want *ListView", rowB.Parent())
	}
}

func TestListViewHitTestReturnsRowWidget(t *testing.T) {
	lv := NewListView(nil)
	row0 := &delegateRowWidget{BaseWidget: NewBaseWidget()}
	row1 := &delegateRowWidget{BaseWidget: NewBaseWidget()}
	lv.SetRowWidgets([]Widget{row0, row1})
	lv.Layout(Rect{X: 0, Y: 0, W: 220, H: 80})

	hit := lv.HitTest(Point{X: 30, Y: 10})
	if hit != row0 {
		t.Fatalf("first-row hit=%T, want row0 widget", hit)
	}
	hit = lv.HitTest(Point{X: 30, Y: 35})
	if hit != row1 {
		t.Fatalf("second-row hit=%T, want row1 widget", hit)
	}
}

func TestListViewHitTestRowWidgetAfterScroll(t *testing.T) {
	lv := NewListView(nil)
	rows := make([]Widget, 0, 50)
	for i := 0; i < 50; i++ {
		rows = append(rows, &delegateRowWidget{BaseWidget: NewBaseWidget()})
	}
	lv.SetRowWidgets(rows)
	lv.Layout(Rect{X: 0, Y: 0, W: 220, H: 100})
	lv.ScrollTo(26 * 10)

	hit := lv.HitTest(Point{X: 20, Y: 5})
	if hit != rows[10] {
		t.Fatalf("scrolled top hit=%T, want row widget index 10", hit)
	}
}

func TestListViewWidgetRowsParticipateInFocusCollection(t *testing.T) {
	lv := NewListView(nil)
	row := newFocusableSpy("row-focus")
	lv.SetRowWidgets([]Widget{row})

	root := NewContainer(FlexLayout{Direction: Vertical}, lv)
	win := windowWithRoot(Size{W: 300, H: 200}, root)

	focusables := win.CollectFocusables()
	found := false
	for _, w := range focusables {
		if w == row {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected focusable row widget to be discovered by focus walk")
	}
}

func TestListViewDrawWithRowWidgets(t *testing.T) {
	lv := NewListView(nil)
	row := &delegateRowWidget{BaseWidget: NewBaseWidget()}
	lv.SetRowWidgets([]Widget{row})
	lv.Layout(Rect{X: 0, Y: 0, W: 220, H: 100})

	lv.Draw(listNoopCanvas{})
	if row.Bounds().H != lv.RowHeight {
		t.Fatalf("row height after draw=%v, want %v", row.Bounds().H, lv.RowHeight)
	}
}

type rowWithActionButton struct {
	BaseWidget
	btn      *Button
	children []Widget
}

func newRowWithActionButton(onClick func()) *rowWithActionButton {
	r := &rowWithActionButton{BaseWidget: NewBaseWidget()}
	r.SetSelf(r)
	r.btn = NewButton("Do", onClick)
	r.btn.SetParent(r)
	r.children = []Widget{r.btn}
	return r
}

func (r *rowWithActionButton) ChildList() []Widget { return r.children }
func (r *rowWithActionButton) Layout(rect Rect) {
	r.BaseWidget.Layout(rect)
	r.btn.Layout(Rect{X: rect.X + rect.W - 68, Y: rect.Y + 4, W: 60, H: rect.H - 8})
}
func (r *rowWithActionButton) Draw(canvas Canvas) {
	r.btn.Draw(canvas)
}
func (r *rowWithActionButton) Handle(Event) bool { return false }
func (r *rowWithActionButton) HitTest(p Point) Widget {
	if !r.Bounds().Contains(p) {
		return nil
	}
	if hit := r.btn.HitTest(p); hit != nil {
		return hit
	}
	return r
}

func TestListViewWidgetRowButtonReceivesClickThroughDispatch(t *testing.T) {
	clicks := 0
	row := newRowWithActionButton(func() { clicks++ })
	lv := NewListView(nil)
	lv.SetRowWidgets([]Widget{row})
	lv.Layout(Rect{X: 0, Y: 0, W: 260, H: 80})
	lv.Draw(listNoopCanvas{})

	btn := row.btn.Bounds()
	x := btn.X + btn.W/2
	y := btn.Y + btn.H/2

	win := NewTestWindow(Size{W: 260, H: 80})
	win.SetRoot(lv)
	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, x, y, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, x, y, MouseButtonLeft, 0))

	if clicks != 1 {
		t.Fatalf("button clicks=%d, want 1", clicks)
	}
	if lv.SelectedIdx != 0 {
		t.Fatalf("row should still become selected on click, got %d", lv.SelectedIdx)
	}
}
