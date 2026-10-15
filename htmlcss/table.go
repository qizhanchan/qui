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
// Column widths come from tableLayout (below), which runs the CSS 2.1
// table width algorithms at layout time over the cells' measured content:
// automatic layout (content-sized columns, a cell's width as its column's
// width, colspan excess spread over the spanned columns, a wider table's
// extra width shared in proportion to content) and table-layout: fixed
// (widths from <col> and the first row, the rest shared equally).
//
// Also: colspan and rowspan placement; <caption> as a full-width spanning
// grid row (caption-side top/bottom); <colgroup>/<col> widths (from the
// `width` attr or inline style); border-spacing between the cells and
// around them; border-collapse single-line merging; rowspan row-height
// contribution; per-cell padding/border (separate model); th
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

	// Column widths are resolved per layout pass by tableLayout; here we only
	// record the <col> widths. Rows are always auto (tallest cell).
	tl := &tableLayout{
		cols:  make([]colWidth, nCols),
		fill:  cs.HasWidth,
		fixed: strings.EqualFold(strings.TrimSpace(cs.raw["table-layout"]), "fixed"),
	}
	copy(tl.cols, gatherColWidths(e, cs))

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
	tl.collapse = e.borderCollapse
	tl.grid = qui.GridLayout{Columns: make([]qui.GridTrack, nCols), Rows: rowTracks, ColGap: gap, RowGap: gap}
	e.LayoutEngine = tl

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
		tl.caption = caption
	}
	for _, p := range places {
		p.cell.SetGridItem(qui.GridItem{
			Col: p.col, Row: p.row + dataOffset, ColSpan: p.cspan, RowSpan: p.rspan, Explicit: true,
		})
		cells = append(cells, p.cell)
	}

	e.flowKids = nil
	applyBox(&e.Box, cs, e.isStyleRoot()) // the table's own background / border / etc.
	if gap > 0 {
		// border-spacing also separates the outer cells from the table's
		// edge (the separated borders model): inset the grid by it.
		qui.UpdateStyle(&e.Box, func(st *qui.Style) {
			st.Padding.Top += gap
			st.Padding.Right += gap
			st.Padding.Bottom += gap
			st.Padding.Left += gap
		})
	}
	e.setContainerChildren(cells)
}

// tableLayout lays a table's cells out on its grid after resolving the
// column widths from the cells themselves (CSS 2.1 §17.5.2):
//
//   - Automatic layout: each column is as wide as its widest single-span
//     cell's max-content width, or the cell's explicit width; a spanning
//     cell wider than its columns spreads the excess over them in
//     proportion to their widths. A table with a width (fill) that is wider
//     than that shares the extra in proportion to the auto columns' widths;
//     a table narrower than its content shrinks the columns toward their
//     min-content widths.
//   - table-layout: fixed: columns take <col> widths, else the first row's
//     explicit cell widths; the remaining width is shared equally by the
//     rest, whatever their content.
type tableLayout struct {
	grid    qui.GridLayout
	cols    []colWidth // <col> widths; len = column count
	fill    bool       // the table has a width: columns fill it
	fixed   bool       // table-layout: fixed
	// collapse: border-collapse: collapse. applyTableCollapse gives a
	// last-column cell its right edge on top of its left one, so its
	// measured width is one border wider than its column's share.
	collapse bool
	caption qui.Widget // spans all columns; plays no part in column widths
}

// MinContentWidth implements qui.MinContentLayout: the columns at their
// min-content widths, plus the gaps.
func (t *tableLayout) MinContentWidth(children []qui.Widget) float32 {
	g := t.resolved(children, 0)
	var sum float32
	for _, c := range g.Columns {
		sum += c.Size
	}
	return sum + g.ColGap*float32(max(len(g.Columns)-1, 0))
}

func (t *tableLayout) Measure(children []qui.Widget, avail qui.Size) qui.Size {
	return t.resolved(children, avail.W).Measure(children, avail)
}

func (t *tableLayout) Apply(children []qui.Widget, bounds qui.Rect) {
	t.resolved(children, bounds.W).Apply(children, bounds)
}

// tableMaxContent is the measuring width for a cell's max-content size.
const tableMaxContent = 1 << 20

// resolved returns the grid with fixed column tracks for a table whose
// content box is availW wide.
func (t *tableLayout) resolved(children []qui.Widget, availW float32) qui.GridLayout {
	n := len(t.cols)
	g := t.grid
	g.Columns = make([]qui.GridTrack, n)
	gaps := g.ColGap * float32(n-1)
	space := availW - gaps
	if space < 0 {
		space = 0
	}
	maxc := make([]float32, n)
	minc := make([]float32, n)
	explicit := make([]bool, n)
	for i, c := range t.cols {
		if c.has {
			maxc[i], minc[i], explicit[i] = c.px, c.px, true
		}
	}
	type span struct {
		col, n   int
		max, min float32
	}
	var spans []span
	first := t.firstRow(children)
	for _, ch := range children {
		if ch == t.caption {
			continue
		}
		gi := qui.GridItemOf(ch)
		cs := max(gi.ColSpan, 1)
		if gi.Col < 0 || gi.Col+cs > n {
			continue
		}
		if t.fixed && gi.Row != first {
			continue // fixed layout reads the first row only
		}
		w := ch.Style().Width // explicit cell width, border-box
		if t.fixed {
			if w > 0 && cs == 1 && !explicit[gi.Col] {
				maxc[gi.Col], explicit[gi.Col] = w, true
			}
			continue
		}
		mx, mn := w, w
		if w == 0 {
			mx = qui.MeasureChild(ch, qui.Size{W: tableMaxContent}).W
			mn = qui.MinContentWidth(ch)
			if t.collapse {
				r := ch.Style().BorderWidths.Right
				mx, mn = mx-r, mn-r
			}
		}
		if cs > 1 {
			spans = append(spans, span{gi.Col, cs, mx, mn})
			continue
		}
		if explicit[gi.Col] && w == 0 {
			continue // a <col> or cell width already fixed this column
		}
		if w > 0 && !explicit[gi.Col] {
			maxc[gi.Col], minc[gi.Col], explicit[gi.Col] = w, w, true
			continue
		}
		maxc[gi.Col] = max(maxc[gi.Col], mx)
		minc[gi.Col] = max(minc[gi.Col], mn)
	}

	if t.fixed {
		var used float32
		rest := 0
		for i := range maxc {
			if explicit[i] {
				used += maxc[i]
			} else {
				rest++
			}
		}
		share := float32(0)
		if rest > 0 && space > used {
			share = (space - used) / float32(rest)
		}
		for i := range g.Columns {
			w := share
			if explicit[i] {
				w = maxc[i]
			}
			g.Columns[i] = qui.GridFixedTrack(w)
		}
		return g
	}

	// A spanning cell wider than its columns (plus the gaps between them)
	// spreads the excess over them, in proportion to their widths.
	for _, s := range spans {
		inner := g.ColGap * float32(s.n-1)
		spread := func(widths []float32, need float32) {
			var sum float32
			for i := s.col; i < s.col+s.n; i++ {
				sum += widths[i]
			}
			if excess := need - inner - sum; excess > 0 {
				for i := s.col; i < s.col+s.n; i++ {
					if sum > 0 {
						widths[i] += excess * widths[i] / sum
					} else {
						widths[i] += excess / float32(s.n)
					}
				}
			}
		}
		spread(maxc, s.max)
		spread(minc, s.min)
	}

	widths := append([]float32(nil), maxc...)
	var total float32
	for _, w := range maxc {
		total += w
	}
	switch {
	case total > space:
		// Too narrow for max-content: start from min-content and share what
		// is left in proportion to how much more each column wants.
		var minTotal, wantTotal float32
		for i := range minc {
			minTotal += minc[i]
			wantTotal += maxc[i] - minc[i]
		}
		left := max(space-minTotal, 0)
		for i := range widths {
			widths[i] = minc[i]
			if wantTotal > 0 {
				widths[i] += left * (maxc[i] - minc[i]) / wantTotal
			}
		}
	case t.fill && total < space:
		// A table with a width wider than its content: the extra goes to the
		// auto columns in proportion to their widths (all columns when every
		// one has an explicit width).
		var autoSum float32
		autoN := 0
		for i := range maxc {
			if !explicit[i] {
				autoSum += maxc[i]
				autoN++
			}
		}
		extra := space - total
		for i := range widths {
			switch {
			case autoN == 0:
				widths[i] += extra * maxc[i] / total
			case explicit[i]:
			case autoSum > 0:
				widths[i] += extra * maxc[i] / autoSum
			default:
				widths[i] += extra / float32(autoN)
			}
		}
	}
	for i, w := range widths {
		g.Columns[i] = qui.GridFixedTrack(w)
	}
	return g
}

// firstRow is the grid row of the table's first data row (below a top
// caption).
func (t *tableLayout) firstRow(children []qui.Widget) int {
	first := -1
	for _, ch := range children {
		if ch == t.caption {
			continue
		}
		if r := qui.GridItemOf(ch).Row; first < 0 || r < first {
			first = r
		}
	}
	return first
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
