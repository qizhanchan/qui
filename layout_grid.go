package qui

// GridTrackKind labels how a track's size is computed.
type GridTrackKind int

const (
	GridFixed    GridTrackKind = iota // Size is literal pixels
	GridFraction                      // Size is the weight for fraction distribution
	GridAuto                          // size = max child Measure on this track
)

// GridTrack specifies a column or row. Construct via GridFixedTrack /
// GridFractionTrack / GridAutoTrack helpers for readability.
type GridTrack struct {
	Kind GridTrackKind
	Size float32 // pixels for Fixed, weight for Fraction, unused for Auto
	Min  float32 // optional floor after fraction / auto resolution
}

// Convenience constructors.
func GridFixedTrack(pixels float32) GridTrack { return GridTrack{Kind: GridFixed, Size: pixels} }

func GridFractionTrack(weight float32) GridTrack { return GridTrack{Kind: GridFraction, Size: weight} }

func GridAutoTrack() GridTrack { return GridTrack{Kind: GridAuto} }

// GridItem is per-child grid placement data. Zero value = auto-place
// in row-major order with span 1×1. Set Col/Row via SetCell for
// explicit placement (the Explicit flag tells auto-placement to skip
// this entry); zero Col/Row with Explicit=false auto-places.
type GridItem struct {
	Col      int
	Row      int
	ColSpan  int // 0 or 1 = single-cell
	RowSpan  int // 0 or 1 = single-cell
	Explicit bool
}

// SetCell is the canonical way to place a widget at an explicit grid
// cell. Sets Col, Row, and the Explicit flag together so zero-value
// (0, 0) placements don't get confused with unplaced widgets.
func (g *GridItem) SetCell(col, row int) {
	g.Col = col
	g.Row = row
	g.Explicit = true
}

// GridLayout arranges widgets in a grid with explicitly-sized tracks.
//
// Columns / Rows are the explicit grid. As in CSS, items placed beyond it
// — explicitly, or by auto-placement running out of rows — extend it with
// implicit tracks sized by AutoColumns / AutoRows (grid-auto-columns /
// grid-auto-rows), repeated in order; empty means auto-sized.
type GridLayout struct {
	Columns     []GridTrack
	Rows        []GridTrack
	AutoColumns []GridTrack
	AutoRows    []GridTrack
	ColGap      float32
	RowGap      float32
}

// gridMaxTracks caps each axis of the implicit grid (CSS lets a UA clamp
// it): an item placed at line 1e6 is dropped rather than allocating a
// million tracks.
const gridMaxTracks = 10000

// implicitTrack is the i-th implicit track after the explicit ones.
func implicitTrack(pattern []GridTrack, i int) GridTrack {
	if len(pattern) == 0 {
		return GridAutoTrack()
	}
	return pattern[i%len(pattern)]
}

// withImplicit normalizes the children's placements, auto-places the rest
// and returns the grid extended by whatever implicit tracks they need.
func (l GridLayout) withImplicit(children []Widget) (GridLayout, []GridItem) {
	placements := make([]GridItem, len(children))
	nCols, rowEnd, autoRows := len(l.Columns), len(l.Rows), 0
	for i, ch := range children {
		p := GridItemOf(ch)
		p.ColSpan, p.RowSpan = max(p.ColSpan, 1), max(p.RowSpan, 1)
		placements[i] = p
		if p.Explicit {
			nCols = max(nCols, p.Col+p.ColSpan)
			rowEnd = max(rowEnd, p.Row+p.RowSpan)
		} else {
			nCols = max(nCols, p.ColSpan)
			autoRows += p.RowSpan
		}
	}
	g := l
	extend := func(tracks, pattern []GridTrack, n int) []GridTrack {
		if n <= len(tracks) {
			return tracks
		}
		out := append(make([]GridTrack, 0, n), tracks...)
		for i := len(tracks); i < n; i++ {
			out = append(out, implicitTrack(pattern, i-len(tracks)))
		}
		return out
	}
	g.Columns = extend(l.Columns, l.AutoColumns, min(nCols, max(gridMaxTracks, len(l.Columns))))
	// Room for every auto item on a row of its own; trimmed below.
	g.Rows = extend(l.Rows, l.AutoRows, min(rowEnd+autoRows, max(gridMaxTracks, len(l.Rows))))
	g.autoPlace(placements)
	used := len(l.Rows)
	for _, p := range placements {
		if p.Explicit {
			used = max(used, p.Row+p.RowSpan)
		}
	}
	g.Rows = g.Rows[:min(used, len(g.Rows))]
	return g, placements
}

// Apply resolves track sizes, determines each child's cell placement,
// and calls child.Layout with the computed rect.
//
// The child's final cell rect spans [colStart..colStart+colSpan-1]
// columns and [rowStart..rowStart+rowSpan-1] rows, including any
// col/row gaps internal to that span.
func (l GridLayout) Apply(children []Widget, bounds Rect) {
	if len(children) == 0 {
		return
	}
	// Place every child (auto-placing the rest), growing implicit tracks.
	l, placements := l.withImplicit(children)
	if len(l.Columns) == 0 || len(l.Rows) == 0 {
		return
	}

	// Resolve track sizes — Fixed/Auto first, then Fraction. Columns first,
	// so auto ROW heights can measure each cell at its resolved column width
	// (wrapping content reports its true multi-line height, matching Measure).
	colWidths := l.resolveTracks(l.Columns, bounds.W, l.ColGap,
		children, placements, true, nil, 0)
	rowHeights := l.resolveTracks(l.Rows, bounds.H, l.RowGap,
		children, placements, false, colWidths, l.ColGap)
	// Grow auto rows so multi-row-spanning children (e.g. table rowspan) fit.
	growAutoRowsForRowSpans(rowHeights, l.Rows, l.RowGap, colWidths, l.ColGap,
		children, placements)

	// Walk children and lay each out into its spanning cell rect.
	for i, ch := range children {
		p := placements[i]
		if !p.Explicit || p.Col < 0 || p.Row < 0 || p.Col >= len(l.Columns) || p.Row >= len(l.Rows) {
			continue
		}
		cellX := bounds.X + trackOffset(colWidths, l.ColGap, p.Col)
		cellY := bounds.Y + trackOffset(rowHeights, l.RowGap, p.Row)
		cellW := trackSpan(colWidths, l.ColGap, p.Col, p.ColSpan)
		cellH := trackSpan(rowHeights, l.RowGap, p.Row, p.RowSpan)
		clamped := applyConstraints(Size{W: cellW, H: cellH}, widgetMinSize(ch), widgetMaxSize(ch))
		ch.Layout(Rect{X: cellX, Y: cellY, W: clamped.W, H: clamped.H})
	}
}

// Measure returns the grid's intrinsic size for `avail`: columns are
// resolved against avail.W, then each Auto/Fraction row's height is the max
// natural height of the single-row-span children in it (measured at their
// resolved column width so wrapping content reports its true height), and
// Fixed rows keep their size. Without this, a grid measured with H<=0 (the
// ScrollView / FlowLayout content-measure path) reported `available` verbatim
// and every sibling laid out after the grid overflowed the measured content
// and became unreachable.
func (l GridLayout) Measure(children []Widget, avail Size) Size {
	if len(children) == 0 {
		return Size{}
	}
	gl, placements := l.withImplicit(children)
	if len(gl.Columns) == 0 || len(gl.Rows) == 0 {
		return Size{}
	}

	colWidths := gl.resolveTracks(gl.Columns, avail.W, gl.ColGap, children, placements, true, nil, 0)

	rowHeights := make([]float32, len(gl.Rows))
	for i, tr := range gl.Rows {
		if tr.Kind == GridFixed {
			rowHeights[i] = tr.Size
		}
	}
	// Content rows: measure single-row children at their real cell width.
	for ci, ch := range children {
		p := placements[ci]
		if !p.Explicit || p.Row < 0 || p.Row >= len(gl.Rows) || p.RowSpan != 1 {
			continue
		}
		if gl.Rows[p.Row].Kind == GridFixed {
			continue
		}
		cw := trackSpan(colWidths, gl.ColGap, p.Col, p.ColSpan)
		nat := measureWithConstraints(ch, Size{W: cw, H: 0})
		if nat.H > rowHeights[p.Row] {
			rowHeights[p.Row] = nat.H
		}
	}
	// Grow auto rows so multi-row-spanning children fit (mirrors Apply).
	growAutoRowsForRowSpans(rowHeights, gl.Rows, gl.RowGap, colWidths, gl.ColGap,
		children, placements)
	for i, tr := range gl.Rows {
		if tr.Min > 0 && rowHeights[i] < tr.Min {
			rowHeights[i] = tr.Min
		}
	}

	var w float32
	for _, cw := range colWidths {
		w += cw
	}
	if len(colWidths) > 1 {
		w += gl.ColGap * float32(len(colWidths)-1)
	}
	var h float32
	for _, rh := range rowHeights {
		h += rh
	}
	if len(rowHeights) > 1 {
		h += gl.RowGap * float32(len(rowHeights)-1)
	}
	return Size{W: w, H: h}
}

// autoPlace fills in cells for non-Explicit placements via row-major
// scanning, respecting span sizes and already-occupied cells.
func (l GridLayout) autoPlace(placements []GridItem) {
	nCols, nRows := len(l.Columns), len(l.Rows)
	occupied := make([]bool, nCols*nRows)
	mark := func(col, row, colSpan, rowSpan int) {
		for r := row; r < row+rowSpan && r < nRows; r++ {
			for c := col; c < col+colSpan && c < nCols; c++ {
				if c >= 0 && r >= 0 {
					occupied[r*nCols+c] = true
				}
			}
		}
	}
	// First pass: mark all explicit placements.
	for _, p := range placements {
		if p.Explicit {
			mark(p.Col, p.Row, p.ColSpan, p.RowSpan)
		}
	}
	// Second pass: place autos into first free slot fitting their span.
	cursor := 0
	for i := range placements {
		if placements[i].Explicit {
			continue
		}
		p := placements[i]
		for ; cursor < nCols*nRows; cursor++ {
			c := cursor % nCols
			r := cursor / nCols
			if c+p.ColSpan > nCols || r+p.RowSpan > nRows {
				continue
			}
			ok := true
			for rr := r; rr < r+p.RowSpan && ok; rr++ {
				for cc := c; cc < c+p.ColSpan && ok; cc++ {
					if occupied[rr*nCols+cc] {
						ok = false
					}
				}
			}
			if ok {
				placements[i].Col = c
				placements[i].Row = r
				placements[i].Explicit = true
				mark(c, r, p.ColSpan, p.RowSpan)
				cursor++
				break
			}
		}
	}
}

// resolveTracks returns resolved pixel sizes for each track on one axis.
// Fixed tracks keep their Size. Auto tracks measure single-cell (span=1)
// children that land in them and take the max. Fraction tracks then
// split whatever remains by weight.
// crossSizes/crossGap carry the already-resolved cross-axis track sizes so
// that auto ROW heights (columnAxis == false) measure each cell at its true
// column-span width — wrapping content then reports its real multi-line
// height. They are nil/0 for the column pass (max-content width is measured
// against avail).
func (l GridLayout) resolveTracks(tracks []GridTrack, avail, gap float32,
	children []Widget, placements []GridItem, columnAxis bool,
	crossSizes []float32, crossGap float32) []float32 {
	sizes := make([]float32, len(tracks))
	used := gap * float32(len(tracks)-1) // total inter-track gaps
	var fractionSum float32

	// Pass 1: Fixed and Auto.
	for i, tr := range tracks {
		switch tr.Kind {
		case GridFixed:
			sizes[i] = tr.Size
			used += tr.Size
		case GridAuto:
			var max float32
			for ci, ch := range children {
				p := placements[ci]
				if columnAxis && (p.Col != i || p.ColSpan != 1) {
					continue
				}
				if !columnAxis && (p.Row != i || p.RowSpan != 1) {
					continue
				}
				if columnAxis {
					nat := measureWithConstraints(ch, Size{W: avail, H: avail})
					if nat.W > max {
						max = nat.W
					}
				} else {
					// Measure at the cell's resolved column-span width so
					// wrapping text contributes its true height.
					cw := trackSpan(crossSizes, crossGap, p.Col, p.ColSpan)
					nat := measureWithConstraints(ch, Size{W: cw, H: 0})
					if nat.H > max {
						max = nat.H
					}
				}
			}
			sizes[i] = max
			used += max
		case GridFraction:
			fractionSum += tr.Size
		}
	}

	// Pass 2: Fraction.
	if fractionSum > 0 {
		remaining := avail - used
		if remaining < 0 {
			remaining = 0
		}
		for i, tr := range tracks {
			if tr.Kind == GridFraction {
				sizes[i] = remaining * tr.Size / fractionSum
			}
		}
	}

	// Apply per-track Min after resolution.
	for i, tr := range tracks {
		if tr.Min > 0 && sizes[i] < tr.Min {
			sizes[i] = tr.Min
		}
	}
	return sizes
}

// growAutoRowsForRowSpans grows Auto rows so a multi-row-spanning child fits
// within its spanned rows (CSS grid span distribution). resolveTracks sizes
// tracks only from single-span children, so a rowspan cell taller than the sum
// of its spanned rows would otherwise overflow. For each such cell, measured at
// its resolved column-span width, any deficit over the current spanned-row
// extent (+ internal gaps) is split equally across the spanned Auto rows.
// Fixed/Fraction rows are left untouched.
func growAutoRowsForRowSpans(rowHeights []float32, rowTracks []GridTrack, rowGap float32,
	colWidths []float32, colGap float32, children []Widget, placements []GridItem) {
	for ci, ch := range children {
		p := placements[ci]
		if p.RowSpan < 2 || p.Row < 0 || p.Row >= len(rowTracks) {
			continue
		}
		end := p.Row + p.RowSpan
		if end > len(rowTracks) {
			end = len(rowTracks)
		}
		cur := rowGap * float32(end-p.Row-1)
		var autoRows []int
		for i := p.Row; i < end; i++ {
			cur += rowHeights[i]
			if rowTracks[i].Kind == GridAuto {
				autoRows = append(autoRows, i)
			}
		}
		if len(autoRows) == 0 {
			continue // no flexible row to absorb the overflow
		}
		cw := trackSpan(colWidths, colGap, p.Col, p.ColSpan)
		nat := measureWithConstraints(ch, Size{W: cw, H: 0})
		if nat.H <= cur {
			continue
		}
		share := (nat.H - cur) / float32(len(autoRows))
		for _, i := range autoRows {
			rowHeights[i] += share
		}
	}
}

// trackOffset sums track sizes + gaps up to (but not including) idx.
func trackOffset(sizes []float32, gap float32, idx int) float32 {
	var off float32
	for i := 0; i < idx; i++ {
		off += sizes[i] + gap
	}
	return off
}

// trackSpan returns the total extent of `span` tracks starting at idx,
// including internal gaps between those tracks.
func trackSpan(sizes []float32, gap float32, idx, span int) float32 {
	if idx < 0 || idx >= len(sizes) {
		return 0
	}
	end := idx + span
	if end > len(sizes) {
		end = len(sizes)
	}
	var total float32
	for i := idx; i < end; i++ {
		total += sizes[i]
	}
	total += gap * float32(end-idx-1)
	return total
}
