package qui

// ColumnsLayout balances consecutive, indivisible block boxes across columns.
// The CSS adapter uses this for multicol containers whose children avoid breaks.
// Margins collapse between blocks and are discarded at column breaks.
type ColumnsLayout struct {
	Count int
	Width float32
	Gap   float32
}

func (c ColumnsLayout) run(children []Widget, b Rect, apply bool) Size {
	n := max(c.Count, 1)
	if c.Width > 0 {
		fit := max(int((b.W+c.Gap)/(c.Width+c.Gap)), 1)
		if c.Count > 0 {
			n = min(c.Count, fit)
		} else {
			n = fit
		}
	}
	if n == 1 {
		// With no column break this is normal flow inside a formatting
		// root: retain the final child's margin as part of the height.
		h, _ := (FlowLayout{}).run(children, b, apply)
		return Size{W: b.W, H: h}
	}
	width := max((b.W-float32(n-1)*c.Gap)/float32(n), 0)
	type item struct {
		w             Widget
		width, height float32
		margin        Insets
	}
	var items []item
	for _, child := range children {
		if child == nil {
			continue
		}
		m := child.Style().Margin
		w, h, _ := resolveFlowBlockSize(child, Size{W: width}, max(width-m.Left-m.Right, 0))
		m.Top = edgeMargin(child, false).value()
		m.Bottom = edgeMargin(child, true).value()
		items = append(items, item{child, w, h, m})
	}
	// Find the smallest balanced column height that fits all blocks. A block
	// cannot be split; its trailing margin can be trimmed at a column break.
	heightFor := func(start, end int) float32 {
		var h, pending float32
		for i := start; i < end; i++ {
			it := items[i]
			if i > start {
				h += collapseMargin(pending, it.margin.Top)
			} else if start == 0 {
				h += it.margin.Top
			}
			h += it.height
			pending = it.margin.Bottom
		}
		return h
	}
	var hi float32
	for _, it := range items {
		hi += it.height + max(it.margin.Top, 0) + max(it.margin.Bottom, 0)
	}
	lo := float32(0)
	fits := func(limit float32) bool {
		start, col := 0, 1
		for i := range items {
			if heightFor(start, i+1) > limit+.001 {
				if i == start {
					return false
				}
				col++
				start = i
			}
		}
		return col <= n
	}
	for range 30 {
		mid := (lo + hi) / 2
		if fits(mid) {
			hi = mid
		} else {
			lo = mid
		}
	}
	height := hi
	start, col := 0, 0
	var y, pending float32
	for i, it := range items {
		if i > start && heightFor(start, i+1) > height+.01 {
			col++
			start = i
			y = 0
			pending = 0
		}
		if i > start {
			y += collapseMargin(pending, it.margin.Top)
		} else if start == 0 {
			y += it.margin.Top
		}
		if apply {
			it.w.Layout(Rect{X: b.X + float32(col)*(width+c.Gap) + it.margin.Left, Y: b.Y + y, W: it.width, H: it.height})
		}
		y += it.height
		pending = it.margin.Bottom
	}
	return Size{W: b.W, H: height}
}
func (c ColumnsLayout) Measure(children []Widget, avail Size) Size {
	return c.run(children, Rect{W: avail.W, H: avail.H}, false)
}
func (c ColumnsLayout) Apply(children []Widget, b Rect) { c.run(children, b, true) }
func collapseMargin(a, b float32) float32               { return max(max(a, b), 0) + min(min(a, b), 0) }
