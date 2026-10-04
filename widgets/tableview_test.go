package widgets

import (
	"fmt"
	"testing"

	. "github.com/qizhanchan/qui"
)

func manyTableRows(n, cols int) *SliceTableModel {
	rows := make([][]string, n)
	for i := range rows {
		r := make([]string, cols)
		for j := range r {
			r[j] = fmt.Sprintf("r%d c%d", i, j)
		}
		rows[i] = r
	}
	return NewTableModel(rows)
}

func newTestTable(rows, cols int) *TableView {
	columns := make([]TableColumn, cols)
	for i := range columns {
		columns[i] = TableColumn{Title: fmt.Sprintf("Col%d", i), Width: 80}
	}
	return NewTableView(columns, manyTableRows(rows, cols))
}

// ---- Model ----

func TestSliceTableModelDimensions(t *testing.T) {
	m := NewTableModel([][]string{{"a", "b"}, {"c", "d"}})
	if m.RowCount() != 2 {
		t.Errorf("RowCount = %d, want 2", m.RowCount())
	}
	if m.CellText(1, 0) != "c" {
		t.Errorf("CellText(1,0) = %q, want c", m.CellText(1, 0))
	}
	// Out of range returns empty string.
	if m.CellText(99, 99) != "" {
		t.Error("out-of-range CellText should return empty string")
	}
}

// ---- Virtualization ----

func TestTableViewVisibleRangeSmallForLargeModel(t *testing.T) {
	tv := newTestTable(10000, 3)
	tv.Layout(Rect{X: 0, Y: 0, W: 400, H: 200})

	start, end := tv.visibleRange()
	if end-start > 10 {
		t.Errorf("visible span for 10k-row model should be small; got %d", end-start)
	}
	// Start should be 0 with no scroll.
	if start != 0 {
		t.Errorf("start = %d, want 0", start)
	}
}

func TestTableViewVisibleRangeAfterScroll(t *testing.T) {
	tv := newTestTable(1000, 3)
	tv.Layout(Rect{X: 0, Y: 0, W: 400, H: 200})
	tv.scrollY = tv.RowHeight * 100
	start, end := tv.visibleRange()
	if start != 100 {
		t.Errorf("start after scroll: got %d, want 100", start)
	}
	if end-start > 8 {
		t.Errorf("visible span too big: %d", end-start)
	}
}

// ---- Column widths ----

func TestTableViewColumnWidthsFixedKept(t *testing.T) {
	tv := NewTableView(
		[]TableColumn{{Title: "A", Width: 100}, {Title: "B", Width: 50}},
		manyTableRows(10, 2),
	)
	tv.Layout(Rect{X: 0, Y: 0, W: 400, H: 200})
	widths := tv.columnWidths()
	if widths[0] != 100 || widths[1] != 50 {
		t.Errorf("widths = %+v, want [100, 50]", widths)
	}
}

func TestTableViewColumnWidthsFlexSplit(t *testing.T) {
	tv := NewTableView(
		[]TableColumn{{Title: "ID", Width: 80}, {Title: "A"}, {Title: "B"}},
		manyTableRows(10, 3),
	)
	tv.Layout(Rect{X: 0, Y: 0, W: 400, H: 200})
	// No scrollbar (content 10*26 = 260 < 200 - header 28 = 172 — wait 260 > 172, scrollbar!
	// Actually widths depend on avail which subtracts scrollbar.
	widths := tv.columnWidths()
	// Fixed: 80. Remaining: (400 - VBarWidth - 80) / 2 = (400-10-80)/2 = 155
	if widths[0] != 80 {
		t.Errorf("fixed col width: got %v, want 80", widths[0])
	}
	if widths[1] != widths[2] {
		t.Errorf("flex cols should be equal; got %v, %v", widths[1], widths[2])
	}
}

// ---- Interaction ----

func TestTableViewClickSelectsRow(t *testing.T) {
	tv := newTestTable(10, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})

	// Row 3 is at body.Y (28) + 3*26 = 106, mid = 119.
	tv.Handle(newMouseEventForHandle(EventMouseDown, 50, 119))
	if tv.SelectedIdx != 3 {
		t.Errorf("click row 3: SelectedIdx = %d, want 3", tv.SelectedIdx)
	}
}

func TestTableViewClickOnHeaderIgnored(t *testing.T) {
	tv := newTestTable(10, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})

	// Header strip is y ∈ [0, 28). Click in there.
	tv.Handle(newMouseEventForHandle(EventMouseDown, 50, 10))
	if tv.SelectedIdx != -1 {
		t.Errorf("header click should not select; got %d", tv.SelectedIdx)
	}
}

func TestTableViewArrowKeysNavigate(t *testing.T) {
	tv := newTestTable(10, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})
	tv.SetFocused(true)

	tv.Handle(newKeyDown(KeyDown))
	if tv.SelectedIdx != 0 {
		t.Errorf("KeyDown from -1: got %d, want 0", tv.SelectedIdx)
	}
	tv.Handle(newKeyDown(KeyDown))
	tv.Handle(newKeyDown(KeyDown))
	if tv.SelectedIdx != 2 {
		t.Errorf("after 3 KeyDowns: got %d", tv.SelectedIdx)
	}
}

func TestTableViewEnterFiresOnActivate(t *testing.T) {
	activated := -1
	tv := newTestTable(10, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})
	tv.OnActivate = func(i int) { activated = i }
	tv.SetFocused(true)
	tv.Select(3)
	tv.Handle(newKeyDown(KeyEnter))
	if activated != 3 {
		t.Errorf("Enter: activated = %d, want 3", activated)
	}
}

// ---- Scroll ----

func TestTableViewWheelScrolls(t *testing.T) {
	tv := newTestTable(100, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	tv.Handle(newScrollEvent(-1, 50, 100))
	if tv.scrollY == 0 {
		t.Error("wheel did not scroll")
	}
}

func TestTableViewScrollClamps(t *testing.T) {
	tv := newTestTable(100, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	// Scroll past end via internal manipulation; verify clamp.
	tv.scrollY = 99999
	tv.scrollY = ClampScroll(tv.scrollY, tv.contentHeight(), tv.bodyRect().H)
	max := tv.contentHeight() - tv.bodyRect().H
	if tv.scrollY != max {
		t.Errorf("clamp: got %v, want %v", tv.scrollY, max)
	}
}

// ---- Auto-scroll ----

func TestTableViewSelectionAutoScrolls(t *testing.T) {
	tv := newTestTable(100, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})
	tv.Select(60)
	if tv.scrollY == 0 {
		t.Errorf("selecting distant row should auto-scroll; scrollY = %v", tv.scrollY)
	}
	start, end := tv.visibleRange()
	if 60 < start || 60 >= end {
		t.Errorf("row 60 not visible after select; range=[%d,%d)", start, end)
	}
}

// ---- rowAt ----

func TestTableViewRowAtHeaderReturnsMinusOne(t *testing.T) {
	tv := newTestTable(10, 2)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 500})
	if idx := tv.rowAt(50, 10); idx != -1 {
		t.Errorf("header rowAt should be -1; got %d", idx)
	}
}

func TestTableViewEmptyModel(t *testing.T) {
	tv := NewTableView(
		[]TableColumn{{Title: "Col"}},
		NewTableModel(nil),
	)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	start, end := tv.visibleRange()
	if start != 0 || end != 0 {
		t.Errorf("empty model visibleRange: got (%d,%d), want (0,0)", start, end)
	}
	tv.Select(0)
	if tv.SelectedIdx != -1 {
		t.Errorf("Select on empty model: got %d, want -1", tv.SelectedIdx)
	}
}

func TestTableViewSelectSyncsPublicSelectedIdx(t *testing.T) {
	tv := NewTableView(
		[]TableColumn{{Title: "Col"}},
		NewTableModel([][]string{{"a"}, {"b"}, {"c"}}),
	)
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 120})
	tv.SelectedIdx = 99

	tv.Select(1)

	if tv.SelectedIdx != 1 {
		t.Fatalf("Select should sync from public SelectedIdx then select; got %d, want 1", tv.SelectedIdx)
	}
}

func TestTableViewWidgetRowButtonReceivesClickThroughDispatch(t *testing.T) {
	clicks := 0
	row := newRowWithActionButton(func() { clicks++ })
	tv := NewTableView([]TableColumn{{Title: "Col", Width: 120}}, nil)
	tv.SetRowWidgets([]Widget{row})
	tv.Layout(Rect{X: 0, Y: 0, W: 280, H: 120})
	tv.Draw(listNoopCanvas{})

	btn := row.btn.Bounds()
	x := btn.X + btn.W/2
	y := btn.Y + btn.H/2

	win := NewTestWindow(Size{W: 280, H: 120})
	win.SetRoot(tv)
	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, x, y, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, x, y, MouseButtonLeft, 0))

	if clicks != 1 {
		t.Fatalf("button clicks=%d, want 1", clicks)
	}
	if tv.SelectedIdx != 0 {
		t.Fatalf("row should become selected on click, got %d", tv.SelectedIdx)
	}
}
