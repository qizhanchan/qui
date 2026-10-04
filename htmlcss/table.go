package htmlcss

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// Table layout. A <table> flattens into ONE qui.GridLayout: every
// <td>/<th> becomes a grid item placed at its (row, col) with col/row spans,
// so columns ALIGN across rows (the defining table property) and reuse the
// grid track solver for content-based auto column widths. Structural row /
// row-group elements (tr, thead/tbody/tfoot) produce no box of their own —
// their cells are hoisted onto the table's grid, while the cells keep their
// DOM parent (tr) for the cascade (the widget-parent / element-parent split
// the engine already uses for inline folding + scroll hosts).
//
// Cell placement is computed here with an occupancy grid (honoring colspan +
// rowspan) rather than relying on grid auto-placement, so a rowspan above
// correctly pushes later cells past the occupied columns.
//
// Scope: auto column widths (or equal 1fr when the table has a width);
// colspan and rowspan placement; <caption> as a full-width spanning grid
// row (caption-side top/bottom); <colgroup>/<col> widths (from the `width`
// attr or inline style → GridFixedTrack); table-layout:fixed (equal columns
// ignoring content); border-collapse single-line merging; rowspan
// row-height contribution; per-cell padding/border (separate model); th
// bold+centered; row striping (a cell adopts its <tr>'s background when it
// has none).

// buildTable assembles the table El as a grid of its cells. cs is the
// table's own computed style (its border/background paint normally).
func (e *El) buildTable(cs *ComputedStyle) {
	rows := gatherTableRows(e)
	caption, capBottom := gatherCaption(e)

	// A caption occupies its own full-width grid row (top or bottom); data
	// rows shift down by one when the caption sits on top.
	dataOffset := 0
	if caption != nil && !capBottom {
		dataOffset = 1
	}
	e.captionRowOffset = dataOffset

	// Occupancy placement: walk rows top-to-bottom, cells left-to-right,
	// skipping columns still covered by a rowspan from an earlier row.
	type placed struct {
		cell                   *El
		col, row, cspan, rspan int
	}
	var places []placed
	occupied := map[[2]int]bool{}
	nCols := 0
	for r, tr := range rows {
		col := 0
		for _, k := range tr.elementKids {
			ke, ok := k.(*El)
			if !ok || (ke.tag != "td" && ke.tag != "th") {
				continue
			}
			for occupied[[2]int{r, col}] {
				col++
			}
			cspan := spanAttr(ke, "colspan")
			rspan := spanAttr(ke, "rowspan")
			places = append(places, placed{ke, col, r, cspan, rspan})
			for rr := r; rr < r+rspan; rr++ {
				for cc := col; cc < col+cspan; cc++ {
					occupied[[2]int{rr, cc}] = true
				}
			}
			col += cspan
			if col > nCols {
				nCols = col
			}
		}
	}
	if nCols == 0 {
		nCols = 1
	}

	// Column tracks: explicit <col> widths win (fixed px); otherwise equal
	// fractions when the table has a width or table-layout:fixed forces equal
	// columns; else content-auto. Rows are always auto (tallest cell).
	fill := cs.HasWidth
	tableFixed := strings.EqualFold(strings.TrimSpace(cs.raw["table-layout"]), "fixed")
	colW := gatherColWidths(e, cs)
	cols := make([]qui.GridTrack, nCols)
	for i := range cols {
		switch {
		case i < len(colW) && colW[i].has:
			cols[i] = qui.GridFixedTrack(colW[i].px)
		case fill || tableFixed:
			cols[i] = qui.GridFractionTrack(1)
		default:
			cols[i] = qui.GridAutoTrack()
		}
	}

	nDataRows := len(rows)
	if nDataRows < 1 {
		nDataRows = 1
	}
	nRows := nDataRows
	if caption != nil {
		nRows++
	}
	rowTracks := make([]qui.GridTrack, nRows)
	for i := range rowTracks {
		rowTracks[i] = qui.GridAutoTrack()
	}

	// border-collapse:collapse merges adjacent cell borders into single lines:
	// spacing is forced to 0 and each cell later paints only its top+left edge
	// (plus the table's outer right/bottom on edge cells) — see applyTableCollapse.
	e.borderCollapse = strings.EqualFold(strings.TrimSpace(cs.raw["border-collapse"]), "collapse")
	e.tableCols = nCols
	e.tableDataRows = nDataRows
	gap := tableSpacing(cs)
	if e.borderCollapse {
		gap = 0
	}
	e.LayoutEngine = qui.GridLayout{Columns: cols, Rows: rowTracks, ColGap: gap, RowGap: gap}

	// Assign explicit grid placement to each cell and collect them as this
	// element's (grid) children. The cells' DOM/elParent stays the <tr>.
	cells := make([]qui.Widget, 0, len(places)+1)
	if caption != nil {
		captionRow := 0
		if capBottom {
			captionRow = nRows - 1
		}
		caption.SetGridItem(qui.GridItem{
			Col: 0, Row: captionRow, ColSpan: nCols, RowSpan: 1, Explicit: true,
		})
		cells = append(cells, caption)
	}
	for _, p := range places {
		p.cell.SetGridItem(qui.GridItem{
			Col: p.col, Row: p.row + dataOffset, ColSpan: p.cspan, RowSpan: p.rspan, Explicit: true,
		})
		cells = append(cells, p.cell)
	}

	e.flowKids = nil
	applyBox(&e.Box, cs, e.isStyleRoot()) // the table's own background / border / etc.
	e.setContainerChildren(cells)
}

// applyTableCollapse rewrites a cell's borders for border-collapse:collapse.
// Each cell keeps only its TOP and LEFT edges; the RIGHT edge is kept only for
// last-column cells and the BOTTOM edge only for last-row cells. Interior edges
// are thus each drawn exactly once (by the lower/right neighbour's top/left),
// and the outer frame by the edge cells — single hairlines with no doubling
// and no dependence on a separate table border. No-op unless this El is a cell
// of a collapsed table with a border to collapse.
func (e *El) applyTableCollapse(cs *ComputedStyle) {
	if e.tag != "td" && e.tag != "th" {
		return
	}
	t := e.enclosingTable()
	if t == nil || !t.borderCollapse {
		return
	}
	w, col, ok := cellBorderSource(cs)
	if !ok {
		return
	}
	gi := e.GridItemValue()
	cspan, rspan := gi.ColSpan, gi.RowSpan
	if cspan < 1 {
		cspan = 1
	}
	if rspan < 1 {
		rspan = 1
	}
	lastCol := gi.Col+cspan >= t.tableCols
	dataRow := gi.Row - t.captionRowOffset
	lastRow := dataRow+rspan >= t.tableDataRows

	right, bottom := float32(0), float32(0)
	if lastCol {
		right = w
	}
	if lastRow {
		bottom = w
	}
	st := e.Style()
	st.BorderSize = 0
	st.Border = qui.Color{}
	st.BorderWidths = qui.Insets{Top: w, Left: w, Right: right, Bottom: bottom}
	st.BorderColors = qui.SideColors{Top: col, Right: col, Bottom: col, Left: col}
	st.BorderStyle = cs.BorderStyle
}

// cellBorderSource returns a representative (width, color) for a cell's border:
// the uniform border pair, or the widest non-zero side for per-side borders.
// ok is false when the cell declares no border.
func cellBorderSource(cs *ComputedStyle) (float32, qui.Color, bool) {
	if cs.BorderWidth > 0 {
		return cs.BorderWidth, cs.BorderColor, true
	}
	if cs.HasSideBorders {
		sides := []struct {
			w float32
			c qui.Color
		}{
			{cs.SideWidths.Top, cs.SideColors.Top},
			{cs.SideWidths.Right, cs.SideColors.Right},
			{cs.SideWidths.Bottom, cs.SideColors.Bottom},
			{cs.SideWidths.Left, cs.SideColors.Left},
		}
		best := float32(0)
		var bc qui.Color
		for _, s := range sides {
			if s.w > best {
				best, bc = s.w, s.c
			}
		}
		if best > 0 {
			if bc.A == 0 {
				bc = cs.BorderColor
			}
			return best, bc, true
		}
	}
	return 0, qui.Color{}, false
}

// gatherCaption returns the table's first <caption> child (or nil) and
// whether caption-side:bottom was requested (default top).
func gatherCaption(table *El) (*El, bool) {
	for _, k := range table.elementKids {
		if ke, ok := k.(*El); ok && ke.tag == "caption" {
			side := ""
			if v, has := ke.attrs["style"]; has {
				side = inlineDeclValue(v, "caption-side")
			}
			return ke, strings.EqualFold(strings.TrimSpace(side), "bottom")
		}
	}
	return nil, false
}

// colWidth is a resolved <col> width: px is meaningful only when has is true.
type colWidth struct {
	px  float32
	has bool
}

// gatherColWidths flattens <colgroup>/<col> (and bare <col>) into a per-column
// width list, honoring the `span` attribute. It reads the DOM Node mirror (not
// elementKids): <col> has display:none, so the static Render path prunes its
// El, but the parsed Node tree always retains it. Widths come from the `width`
// attribute or an inline `style` width — class-based CSS on <col> is not
// consulted (columns are resolved before their own cascade runs).
func gatherColWidths(table *El, tableCS *ComputedStyle) []colWidth {
	var out []colWidth
	add := func(n *Node) {
		span := spanAttrNode(n, "span")
		px, has := colWidthOfNode(n, tableCS)
		for i := 0; i < span; i++ {
			out = append(out, colWidth{px, has})
		}
	}
	if table.node == nil {
		return out
	}
	for _, c := range table.node.Children {
		if c.Type != ElementNode {
			continue
		}
		switch c.Tag {
		case "col":
			add(c)
		case "colgroup":
			hadCol := false
			for _, cc := range c.Children {
				if cc.Type == ElementNode && cc.Tag == "col" {
					add(cc)
					hadCol = true
				}
			}
			if !hadCol {
				add(c) // colgroup carrying its own span + width
			}
		}
	}
	return out
}

// spanAttrNode reads a span attribute off a DOM node, clamped to >= 1.
func spanAttrNode(n *Node, name string) int {
	if v, err := strconv.Atoi(strings.TrimSpace(n.AttrOr(name, ""))); err == nil && v > 1 {
		return v
	}
	return 1
}

// colWidthOfNode resolves a column's width from its `width` attribute or inline
// style. Percentages resolve against the table's declared width.
func colWidthOfNode(n *Node, tableCS *ComputedStyle) (float32, bool) {
	if w := strings.TrimSpace(n.AttrOr("width", "")); w != "" {
		if px, ok := resolveColLen(w, tableCS); ok {
			return px, true
		}
	}
	if st := n.AttrOr("style", ""); st != "" {
		if w := inlineDeclValue(st, "width"); w != "" {
			if px, ok := resolveColLen(w, tableCS); ok {
				return px, true
			}
		}
	}
	return 0, false
}

// resolveColLen parses a column length (px/em/%/bare number). Percentages use
// the table's declared content width as the reference.
func resolveColLen(s string, tableCS *ComputedStyle) (float32, bool) {
	fs, ref := float32(16), float32(0)
	if tableCS != nil {
		if tableCS.FontSize > 0 {
			fs = tableCS.FontSize
		}
		if tableCS.HasWidth {
			ref = tableCS.Width
		}
	}
	return parseLength(s, fs, ref)
}

// inlineDeclValue returns the value of prop in an inline style string, or "".
func inlineDeclValue(style, prop string) string {
	for _, d := range parseDeclarations(style) {
		if strings.EqualFold(d.Property, prop) {
			return d.Value
		}
	}
	return ""
}

// gatherTableRows returns the <tr> elements of a table in document order,
// descending through thead/tbody/tfoot row groups.
func gatherTableRows(table *El) []*El {
	var rows []*El
	var walk func(*El)
	walk = func(el *El) {
		for _, k := range el.elementKids {
			ke, ok := k.(*El)
			if !ok {
				continue
			}
			switch ke.tag {
			case "tr":
				rows = append(rows, ke)
			case "thead", "tbody", "tfoot":
				walk(ke)
			}
		}
	}
	walk(table)
	return rows
}

// spanAttr reads a colspan/rowspan attribute, clamped to a minimum of 1.
func spanAttr(e *El, name string) int {
	if v, err := strconv.Atoi(strings.TrimSpace(e.attrs[name])); err == nil && v > 1 {
		return v
	}
	return 1
}

// tableSpacing maps CSS border-spacing (single length) to the grid gap used
// between cells. Defaults to 0 (author borders provide separation).
func tableSpacing(cs *ComputedStyle) float32 {
	if v, ok := cs.raw["border-spacing"]; ok {
		if px, ok := parseLength(firstField(v), cs.FontSize, 0); ok {
			return px
		}
	}
	return 0
}
