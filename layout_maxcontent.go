package qui

// Max-content width: how wide a widget lays out when it is given all the
// room it wants — no soft wrap taken (CSS max-content inline size). It is
// the "preferred width" of CSS shrink-to-fit (CSS 2.1 § 10.3.5): table
// columns, inline-blocks, floats and absolutely positioned boxes size from
// it. Measuring with a huge available width is NOT the same thing — a
// block, or a right-aligned line, fills whatever it is offered.

// MaxContentWidther is implemented by widgets that know their max-content
// width (an inline run: its unbroken line width).
type MaxContentWidther interface {
	MaxContentWidth() float32
}

// MaxContentLayout is implemented by layout engines that derive a
// container's max-content content width from its in-flow children.
type MaxContentLayout interface {
	MaxContentWidth(children []Widget) float32
}

// maxContentMeasure is the available width the fallback path measures
// leaves with: wide enough that nothing wraps.
const maxContentMeasure = 1 << 20

// MaxContentWidth returns w's max-content border-box width: its explicit
// width when it has one, else what the widget (MaxContentWidther) or its
// layout engine (MaxContentLayout) reports, else its measured width at an
// unbounded width (a leaf that reports more than half of that unbounded
// width fills its slot and has no max-content size of its own: it gets
// its min-content width instead). Min / max widths clamp it.
func MaxContentWidth(w Widget) float32 {
	s := w.Style()
	if p := widgetPreferredSize(w); p.W > 0 {
		return p.W
	}
	var v float32
	if mc, ok := w.(MaxContentWidther); ok {
		v = mc.MaxContentWidth()
	} else if h, ok := w.(layoutHost); ok {
		if ml, ok := h.layoutEngine().(MaxContentLayout); ok {
			v = ml.MaxContentWidth(h.inFlowChildren())
			if s != nil {
				v += s.ContentInsets().Horizontal()
			}
		} else {
			v = maxContentByMeasure(w)
		}
	} else {
		v = maxContentByMeasure(w)
	}
	if s != nil {
		if s.MaxWidth > 0 && v > s.MaxWidth {
			v = s.MaxWidth
		}
		if s.MinWidth > 0 && v < s.MinWidth {
			v = s.MinWidth
		}
	}
	return v
}

func maxContentByMeasure(w Widget) float32 {
	v := MeasureChild(w, Size{W: maxContentMeasure}).W
	if v > maxContentMeasure/2 {
		return MinContentWidth(w)
	}
	return v
}

// maxContentMargined is a child's max-content width plus its horizontal
// margins.
func maxContentMargined(w Widget) float32 {
	m := w.Style().Margin
	return MaxContentWidth(w) + m.Left + m.Right
}

// MaxContentWidth: block children each take a band of their own; a run of
// consecutive inline boxes shares one line.
func (f FlowLayout) MaxContentWidth(children []Widget) float32 {
	var widest, line float32
	inLine := 0
	flush := func() {
		widest = max(widest, line)
		line, inLine = 0, 0
	}
	for _, c := range children {
		if flowLevelOf(c) == FlowInline {
			if inLine > 0 {
				line += f.InlineGap
			}
			line += maxContentMargined(c)
			inLine++
			continue
		}
		flush()
		widest = max(widest, maxContentMargined(c))
	}
	flush()
	return widest
}

// MaxContentWidth: a row lays every item side by side (a wrapping row too:
// at max-content nothing wraps); a column is as wide as its widest item.
func (l FlexLayout) MaxContentWidth(children []Widget) float32 {
	var sum, widest float32
	for _, c := range children {
		w := maxContentMargined(c)
		sum += w
		widest = max(widest, w)
	}
	if l.Direction == Horizontal {
		if n := len(children); n > 1 {
			sum += l.Gap * float32(n-1)
		}
		return sum
	}
	return widest
}

// MaxContentWidth: fixed columns count as their size; other columns as
// their widest single-span item.
func (l GridLayout) MaxContentWidth(children []Widget) float32 {
	n := len(l.Columns)
	if n == 0 {
		var widest float32
		for _, c := range children {
			widest = max(widest, maxContentMargined(c))
		}
		return widest
	}
	cols := make([]float32, n)
	for i, t := range l.Columns {
		if t.Kind == GridFixed {
			cols[i] = t.Size
		}
	}
	for _, c := range children {
		gi := GridItemOf(c)
		if gi.ColSpan > 1 || gi.Col < 0 || gi.Col >= n || l.Columns[gi.Col].Kind == GridFixed {
			continue
		}
		cols[gi.Col] = max(cols[gi.Col], maxContentMargined(c))
	}
	sum := l.ColGap * float32(n-1)
	for _, w := range cols {
		sum += w
	}
	return sum
}
