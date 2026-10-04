package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A <table> aligns its columns across rows: cells in the same column share
// the same X and width, regardless of which <tr> they belong to.
func TestTableColumnsAlignAcrossRows(t *testing.T) {
	res := RenderDoc(`<body><table>
		<tr><td id="a">Ann</td><td id="b">Engineer</td></tr>
		<tr><td id="c">B</td><td id="d">X</td></tr>
	</table></body>`,
		`td{padding:4px 8px}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 500, H: 300})

	a := res.ByID["a"].(*El).Bounds()
	c := res.ByID["c"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	d := res.ByID["d"].(*El).Bounds()

	if a.X != c.X || a.W != c.W {
		t.Errorf("column 0 not aligned: a=%+v c=%+v", a, c)
	}
	if b.X != d.X || b.W != d.W {
		t.Errorf("column 1 not aligned: b=%+v d=%+v", b, d)
	}
	// Column widths are content-sized: "Engineer" (col 1) is wider than the
	// short col-0 cells.
	if b.W <= a.W {
		t.Errorf("expected content-auto columns: col1 W=%v should exceed col0 W=%v", b.W, a.W)
	}
	// Second row sits below the first.
	if c.Y <= a.Y {
		t.Errorf("row 2 (Y=%v) should be below row 1 (Y=%v)", c.Y, a.Y)
	}
}

// colspan makes a cell span multiple columns' combined width.
func TestTableColspan(t *testing.T) {
	res := RenderDoc(`<body><table>
		<tr><td id="x">a</td><td id="y">b</td></tr>
		<tr><td id="wide" colspan="2">wiiiiide</td></tr>
	</table></body>`, `td{padding:4px}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 500, H: 300})

	x := res.ByID["x"].(*El).Bounds()
	y := res.ByID["y"].(*El).Bounds()
	wide := res.ByID["wide"].(*El).Bounds()

	// The spanning cell starts at column 0 and covers both columns.
	if wide.X != x.X {
		t.Errorf("colspan cell X=%v, want %v (column 0)", wide.X, x.X)
	}
	want := x.W + y.W // no gap in this table
	if diff := wide.W - want; diff < -0.5 || diff > 0.5 {
		t.Errorf("colspan cell W=%v, want ~%v (col0+col1)", wide.W, want)
	}
}

// A striped row: `tr:nth-child(even)` background is adopted by that row's
// cells (which have no background of their own).
func TestTableRowStriping(t *testing.T) {
	res := RenderDoc(`<body><table>
		<tr><td id="r1">1</td></tr>
		<tr><td id="r2">2</td></tr>
	</table></body>`,
		`tr:nth-child(even){background:#ff0000}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 200})

	red := qui.Color{R: 1, A: 1}
	if bg := res.ByID["r2"].(*El).Style().Background; bg != red {
		t.Errorf("even-row cell background = %+v, want red (striping via tr:nth-child)", bg)
	}
	if bg := res.ByID["r1"].(*El).Style().Background; bg == red {
		t.Error("odd-row cell should not be striped red")
	}
}

// Structural check: the table's children are its cells (grid items) — the
// <tr> is hoisted away as a layout box.
func TestTableFlattensRowsToCells(t *testing.T) {
	res := RenderDoc(`<body><table id="t">
		<tr><td>a</td><td>b</td></tr>
		<tr><td>c</td><td>d</td></tr>
	</table></body>`, ``, Options{})
	tbl := res.ByID["t"].(*El)
	kids := tbl.ChildList()
	if len(kids) != 4 {
		t.Fatalf("table has %d direct children, want 4 cells (rows flattened)", len(kids))
	}
	for i, k := range kids {
		if e, ok := k.(*El); !ok || e.tag != "td" {
			t.Errorf("child %d = %T (%v), want a <td> El", i, k, tagOf(k))
		}
	}
	// The layout engine is a grid.
	if _, ok := tbl.LayoutEngine.(qui.GridLayout); !ok {
		t.Errorf("table LayoutEngine = %T, want qui.GridLayout", tbl.LayoutEngine)
	}
}

// A <caption> is placed as a full-width grid row above the data cells and
// spans every column.
func TestTableCaptionTop(t *testing.T) {
	res := RenderDoc(`<body><table>
		<caption id="cap">My table</caption>
		<tr><td id="a">a</td><td id="b">b</td></tr>
	</table></body>`, `td{padding:4px}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	cap := res.ByID["cap"].(*El).Bounds()
	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()

	if cap.Y >= a.Y {
		t.Errorf("top caption Y=%v should be above first row Y=%v", cap.Y, a.Y)
	}
	// Caption spans both columns: its right edge reaches past column 1's start.
	if cap.X > a.X+0.5 || cap.W < (b.X+b.W)-a.X-0.5 {
		t.Errorf("caption (X=%v W=%v) should span both columns (a.X=%v .. b.end=%v)",
			cap.X, cap.W, a.X, b.X+b.W)
	}
}

// caption-side:bottom places the caption below the data rows.
func TestTableCaptionBottom(t *testing.T) {
	res := RenderDoc(`<body><table>
		<caption id="cap" style="caption-side:bottom">Below</caption>
		<tr><td id="a">a</td></tr>
	</table></body>`, `td{padding:4px}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 300})

	if cap, a := res.ByID["cap"].(*El).Bounds(), res.ByID["a"].(*El).Bounds(); cap.Y <= a.Y {
		t.Errorf("bottom caption Y=%v should be below the row Y=%v", cap.Y, a.Y)
	}
}

// A top caption must not corrupt clipboard cell row coordinates — cells stay
// data-relative (row 0 for the first data row) despite the grid shift.
func TestTableCaptionClipboardCoords(t *testing.T) {
	res := RenderDoc(`<body><table>
		<caption>Cap</caption>
		<tr><td id="a">a</td></tr>
		<tr><td id="c">c</td></tr>
	</table></body>`, ``, Options{})

	if _, r, _, _, _, _ := res.ByID["a"].(*El).ClipboardTableCell(); r != 0 {
		t.Errorf("first data cell row = %d, want 0 (caption offset removed)", r)
	}
	if _, r, _, _, _, _ := res.ByID["c"].(*El).ClipboardTableCell(); r != 1 {
		t.Errorf("second data cell row = %d, want 1", r)
	}
}

// <colgroup>/<col> widths pin exact column widths (fixed px tracks).
func TestTableColgroupWidths(t *testing.T) {
	res := RenderDoc(`<body><table>
		<colgroup>
			<col width="80">
			<col style="width:200px">
		</colgroup>
		<tr><td id="a">a</td><td id="b">bbbbbbbbbbbbbb</td></tr>
	</table></body>`, `td{padding:0}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 600, H: 200})

	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	if diff := a.W - 80; diff < -0.5 || diff > 0.5 {
		t.Errorf("col 0 width = %v, want 80 (from col width attr)", a.W)
	}
	if diff := b.W - 200; diff < -0.5 || diff > 0.5 {
		t.Errorf("col 1 width = %v, want 200 (from inline col style)", b.W)
	}
}

// A <col span="2"> width applies to both spanned columns.
func TestTableColSpanAttrWidth(t *testing.T) {
	res := RenderDoc(`<body><table>
		<colgroup><col span="2" width="60"></colgroup>
		<tr><td id="a">a</td><td id="b">b</td></tr>
	</table></body>`, `td{padding:0}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 600, H: 200})
	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	if diff := a.W - 60; diff < -0.5 || diff > 0.5 {
		t.Errorf("col 0 width = %v, want 60", a.W)
	}
	if diff := b.W - 60; diff < -0.5 || diff > 0.5 {
		t.Errorf("col 1 width = %v, want 60", b.W)
	}
}

// table-layout:fixed with a table width gives equal columns regardless of
// content width.
func TestTableLayoutFixedEqualColumns(t *testing.T) {
	res := RenderDoc(`<body><table style="width:300px;table-layout:fixed">
		<tr><td id="a">a</td><td id="b">bbbbbbbbbbbbbbbbbbbb</td></tr>
	</table></body>`, `td{padding:0}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 600, H: 200})
	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	if diff := a.W - b.W; diff < -0.5 || diff > 0.5 {
		t.Errorf("table-layout:fixed columns should be equal: a.W=%v b.W=%v", a.W, b.W)
	}
}

// A rowspan cell taller than its spanned rows grows those rows so it fits
// (root GridLayout span distribution).
func TestTableRowspanRowHeight(t *testing.T) {
	res := RenderDoc(`<body><table>
		<tr><td rowspan="2" id="tall" style="height:120px">X</td><td id="a">a</td></tr>
		<tr><td id="b">b</td></tr>
	</table></body>`, `td{padding:0}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 400})

	tall := res.ByID["tall"].(*El).Bounds()
	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()

	if tall.H < 119 {
		t.Errorf("rowspan cell height = %v, want >=120 (rows grew to fit its content)", tall.H)
	}
	if b.Y <= a.Y {
		t.Errorf("row 2 (Y=%v) should sit below row 1 (Y=%v)", b.Y, a.Y)
	}
	if b.Y >= tall.Y+tall.H {
		t.Errorf("row 2 (Y=%v) should start inside the rowspan cell extent (%v..%v)",
			b.Y, tall.Y, tall.Y+tall.H)
	}
}

// A cell whose text wraps to multiple lines gets a row tall enough to hold it:
// the auto row is sized at the cell's real column width, not the full available
// width (else wrapping text overflows the row and paints outside the table).
func TestTableWrappingCellRowHeight(t *testing.T) {
	res := RenderDoc(`<body><table style="width:300px;table-layout:fixed">
		<tr><td id="long">one two three four five six seven eight nine ten eleven twelve thirteen</td></tr>
	</table></body>`, `td{padding:0}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 900, H: 400})

	cell := res.ByID["long"].(*El)
	b := cell.Bounds()
	nat := cell.Measure(qui.Size{W: b.W, H: 0})
	if b.H+0.5 < nat.H {
		t.Errorf("wrapping cell laid-out H=%v shorter than its natural H=%v at width %v (row underfit → text overflows)",
			b.H, nat.H, b.W)
	}
}

// border-collapse:collapse rewrites each cell to single shared lines: interior
// cells keep only top+left; edge cells add the outer right/bottom; spacing is 0.
func TestTableBorderCollapse(t *testing.T) {
	res := RenderDoc(`<body><table style="border-collapse:collapse">
		<tr><td id="a">a</td><td id="b">b</td></tr>
		<tr><td id="c">c</td><td id="d">d</td></tr>
	</table></body>`, `td{border:2px solid #000;padding:4px}`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})

	a := res.ByID["a"].(*El).Style() // interior top-left: top+left only
	if a.BorderWidths.Top != 2 || a.BorderWidths.Left != 2 {
		t.Errorf("cell a top/left = %v/%v, want 2/2", a.BorderWidths.Top, a.BorderWidths.Left)
	}
	if a.BorderWidths.Right != 0 || a.BorderWidths.Bottom != 0 {
		t.Errorf("interior cell a right/bottom = %v/%v, want 0/0 (collapsed)",
			a.BorderWidths.Right, a.BorderWidths.Bottom)
	}
	if b := res.ByID["b"].(*El).Style(); b.BorderWidths.Right != 2 || b.BorderWidths.Bottom != 0 {
		t.Errorf("last-col cell b R/B = %v/%v, want 2/0", b.BorderWidths.Right, b.BorderWidths.Bottom)
	}
	if c := res.ByID["c"].(*El).Style(); c.BorderWidths.Bottom != 2 || c.BorderWidths.Right != 0 {
		t.Errorf("last-row cell c B/R = %v/%v, want 2/0", c.BorderWidths.Bottom, c.BorderWidths.Right)
	}
	if d := res.ByID["d"].(*El).Style(); d.BorderWidths.Right != 2 || d.BorderWidths.Bottom != 2 {
		t.Errorf("corner cell d R/B = %v/%v, want 2/2", d.BorderWidths.Right, d.BorderWidths.Bottom)
	}
	// Cells are adjacent (border-spacing forced to 0).
	ab := res.ByID["a"].(*El).Bounds()
	bb := res.ByID["b"].(*El).Bounds()
	if diff := bb.X - (ab.X + ab.W); diff < -0.5 || diff > 0.5 {
		t.Errorf("collapsed cells should touch (gap 0): a.end=%v b.X=%v", ab.X+ab.W, bb.X)
	}
}

func tagOf(w qui.Widget) string {
	if e, ok := w.(*El); ok {
		return e.tag
	}
	return "?"
}

// A cell reports its table membership + grid coordinates for clipboard
// table reconstruction (qui.ClipboardTableCell).
func TestTableClipboardCellCoords(t *testing.T) {
	res := RenderDoc(`<body><table>
		<tr><th id="h">H</th><td id="d">x</td></tr>
		<tr><td id="c" colspan="2">wide</td></tr>
	</table></body>`, ``, Options{})

	h := res.ByID["h"].(*El)
	tbl, row, col, cs, rs, hdr := h.ClipboardTableCell()
	if tbl == nil {
		t.Fatal("<th> reported nil table identity")
	}
	if row != 0 || col != 0 || cs != 1 || rs != 1 || !hdr {
		t.Errorf("th coords = (r%d c%d cs%d rs%d hdr%v), want (0,0,1,1,true)", row, col, cs, rs, hdr)
	}

	c := res.ByID["c"].(*El)
	tbl2, row2, col2, cs2, _, hdr2 := c.ClipboardTableCell()
	if row2 != 1 || col2 != 0 || cs2 != 2 || hdr2 {
		t.Errorf("colspan cell coords = (r%d c%d cs%d hdr%v), want (1,0,2,false)", row2, col2, cs2, hdr2)
	}
	if tbl != tbl2 {
		t.Error("cells of the same table reported different identities")
	}

	// A non-cell reports no table.
	body := res.Root.(*El)
	if tb, _, _, _, _, _ := body.ClipboardTableCell(); tb != nil {
		t.Error("non-cell element reported a table identity")
	}
}
