package qui

import (
	"strings"
	"testing"
)

// richClip captures both clipboard flavors for assertions.
type richClip struct{ plain, html string }

func (c *richClip) Get() string         { return c.plain }
func (c *richClip) Set(s string)        { c.plain, c.html = s, "" }
func (c *richClip) SetRich(p, h string) { c.plain, c.html = p, h }

// stubCell is a selectable that also reports table-cell membership.
type stubCell struct {
	*stubSel
	tbl      interface{}
	row, col int
	spanCols int
	header   bool
}

func (c *stubCell) ClipboardTableCell() (interface{}, int, int, int, int, bool) {
	cs := c.spanCols
	if cs < 1 {
		cs = 1
	}
	return c.tbl, c.row, c.col, cs, 1, c.header
}

// Copying a selection that spans table cells rebuilds a <table> (HTML
// flavor, for Google Docs / Word) and tab-separated rows (plain flavor).
func TestCopyRebuildsTable(t *testing.T) {
	fc := &richClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	tblID := new(int) // opaque shared table identity
	mk := func(text string, r, c int, hdr bool) *stubCell {
		sc := &stubCell{stubSel: newStubSel(text), tbl: tblID, row: r, col: c, header: hdr}
		sc.SetSelf(sc)
		return sc
	}
	cells := []*stubCell{
		mk("Name", 0, 0, true), mk("Role", 0, 1, true),
		mk("Ann", 1, 0, false), mk("Eng", 1, 1, false),
	}
	root := NewContainer(FlowLayout{})
	for _, c := range cells {
		root.AddChild(c)
	}
	w := NewTestWindow(Size{W: 400, H: 300})
	w.SetRoot(root)
	// Give each cell positive bounds (collector skips zero-size widgets) and
	// select it fully.
	for i, c := range cells {
		c.Layout(Rect{X: 0, Y: float32(i * 20), W: 100, H: 20})
		c.SetSelectionRange(0, c.SelectableLength())
	}

	if !w.copyTextSelection() {
		t.Fatal("copyTextSelection should handle a table selection")
	}

	// HTML flavor: a real table with header + body cells.
	for _, want := range []string{"<table>", "<th>Name</th>", "<th>Role</th>", "<td>Ann</td>", "<td>Eng</td>"} {
		if !strings.Contains(fc.html, want) {
			t.Errorf("html flavor missing %q:\n%s", want, fc.html)
		}
	}
	if n := strings.Count(fc.html, "<tr>"); n != 2 {
		t.Errorf("want 2 <tr>, got %d:\n%s", n, fc.html)
	}
	// Plain flavor: tab-separated rows.
	if fc.plain != "Name\tRole\nAnn\tEng" {
		t.Errorf("plain flavor = %q, want TSV rows", fc.plain)
	}
}

// Selecting a SINGLE cell copies just its content — not a one-cell <table>
// padded with empty rows/columns up to the cell's absolute grid position.
func TestCopySingleCellNoTable(t *testing.T) {
	fc := &richClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	tblID := new(int)
	// A cell deep in the grid (row 2, col 2), like the "$99" cell.
	cell := &stubCell{stubSel: newStubSel("$99"), tbl: tblID, row: 2, col: 2}
	cell.SetSelf(cell)
	root := NewContainer(FlowLayout{})
	root.AddChild(cell)
	w := NewTestWindow(Size{W: 400, H: 300})
	w.SetRoot(root)
	cell.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})
	cell.SetSelectionRange(0, cell.SelectableLength())

	if !w.copyTextSelection() {
		t.Fatal("copyTextSelection should handle a single-cell selection")
	}
	if strings.Contains(fc.html, "<table>") {
		t.Errorf("single-cell copy should not emit a <table>:\n%s", fc.html)
	}
	if strings.Contains(fc.html, "<td></td>") {
		t.Errorf("single-cell copy should not pad empty cells:\n%s", fc.html)
	}
	if !strings.Contains(fc.html, "$99") {
		t.Errorf("single-cell copy lost its content:\n%s", fc.html)
	}
	// Plain flavor: just the text, no leading tabs/newlines from padding.
	if fc.plain != "$99" {
		t.Errorf("plain flavor = %q, want %q (no TSV padding)", fc.plain, "$99")
	}
}

// A colspan cell carries the colspan attribute into the rebuilt HTML.
func TestCopyTableColspanAttr(t *testing.T) {
	fc := &richClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	tblID := new(int)
	mkSpan := func(text string, r, c, span int) *stubCell {
		sc := &stubCell{stubSel: newStubSel(text), tbl: tblID, row: r, col: c}
		sc.SetSelf(sc)
		sc.spanCols = span
		return sc
	}
	a := mkSpan("a", 0, 0, 1)
	b := mkSpan("b", 0, 1, 1)
	wide := mkSpan("wide", 1, 0, 2)
	cells := []*stubCell{a, b, wide}
	root := NewContainer(FlowLayout{})
	for _, c := range cells {
		root.AddChild(c)
	}
	w := NewTestWindow(Size{W: 400, H: 300})
	w.SetRoot(root)
	for i, c := range cells {
		c.Layout(Rect{X: 0, Y: float32(i * 20), W: 100, H: 20})
		c.SetSelectionRange(0, c.SelectableLength())
	}
	if !w.copyTextSelection() {
		t.Fatal("copy should handle table")
	}
	if !strings.Contains(fc.html, `<td colspan="2">wide</td>`) {
		t.Errorf("colspan not emitted:\n%s", fc.html)
	}
}
