package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestTableRowLaysMixedColumnsToMatchHeader(t *testing.T) {
	cols := []TableColumn{
		{Title: "ID", Width: 60},
		{Title: "Name"},  // flex
		{Title: "Email"}, // flex
	}
	cells := []Widget{
		NewLabel("1"),
		NewInput(""),
		NewLabel("a@b"),
	}
	row := NewTableRow(cols, cells)

	rect := Rect{X: 10, Y: 20, W: 260, H: 28}
	row.Layout(rect)

	// 260 total - 60 fixed = 200, split 2 ways = 100 each.
	want := []Rect{
		{X: 10, Y: 20, W: 60, H: 28},
		{X: 70, Y: 20, W: 100, H: 28},
		{X: 170, Y: 20, W: 100, H: 28},
	}
	for i, child := range row.Children() {
		got := child.Bounds()
		if got != want[i] {
			t.Errorf("cell %d: got %+v, want %+v", i, got, want[i])
		}
	}
}

func TestTableRowExtraCellsIgnored(t *testing.T) {
	cols := []TableColumn{{Title: "A", Width: 40}, {Title: "B", Width: 40}}
	cells := []Widget{NewLabel("a"), NewLabel("b"), NewLabel("c")}
	row := NewTableRow(cols, cells)
	row.Layout(Rect{X: 0, Y: 0, W: 80, H: 20})

	// Third cell was added as a child (AddChild took it) but Layout
	// must not place it past the last column — its bounds should
	// stay at the zero rect it had before Layout.
	if got := row.ChildAt(2).Bounds(); got != (Rect{}) {
		t.Errorf("extra cell should be left unlaid; got %+v", got)
	}
}

func TestTableRowNilCellsSkipped(t *testing.T) {
	cols := []TableColumn{{Width: 30}, {Width: 30}}
	row := NewTableRow(cols, []Widget{nil, NewLabel("x"), nil})
	if row.ChildCount() != 1 {
		t.Fatalf("nil cells should be skipped, got %d children", row.ChildCount())
	}
}

func TestTableRowReLayoutOnNewWidth(t *testing.T) {
	cols := []TableColumn{{Width: 50}, {}} // one fixed, one flex
	row := NewTableRow(cols, []Widget{NewLabel("x"), NewLabel("y")})

	row.Layout(Rect{X: 0, Y: 0, W: 150, H: 20})
	if row.ChildAt(1).Bounds().W != 100 {
		t.Errorf("flex column should take remaining 100, got %v", row.ChildAt(1).Bounds().W)
	}
	row.Layout(Rect{X: 0, Y: 0, W: 250, H: 20})
	if row.ChildAt(1).Bounds().W != 200 {
		t.Errorf("after re-Layout, flex column should take 200, got %v", row.ChildAt(1).Bounds().W)
	}
}
