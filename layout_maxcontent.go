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

type MaxContentOverrider interface {
	MaxContentOverride() (float32, bool)
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
	if p := widgetPreferredSize(w); p.W > 0 || explicitZeroWidth(w) {
		return p.W
	}
	v := cachedIntrinsic(w, true, func() float32 { return contentMaxWidth(w) })
	v = max(v, aspectWidth(s))
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

// contentMaxWidth is w's max-content width from its content, before the
// aspect ratio and min / max widths apply.
func contentMaxWidth(w Widget) float32 {
	if o, ok := w.(MaxContentOverrider); ok {
		if v, ok := o.MaxContentOverride(); ok {
			return v
		}
	}
	if mc, ok := w.(MaxContentWidther); ok {
		return mc.MaxContentWidth()
	}
	if h, ok := w.(layoutHost); ok {
		if ml, ok := h.layoutEngine().(MaxContentLayout); ok {
			v := ml.MaxContentWidth(h.inFlowChildren())
			if s := w.Style(); s != nil {
				v += s.ContentInsets().Horizontal()
			}
			return v
		}
	}
	return maxContentByMeasure(w)
}

func maxContentByMeasure(w Widget) float32 {
	v := MeasureChild(w, Size{W: maxContentMeasure}).W
	if v > maxContentMeasure/2 {
		return MinContentWidth(w)
	}
	return v
}

// MinContentContributor is implemented by widgets whose contribution to
// their container's max-content width is only their min-content width —
// a table caption (CSS 2.1 § 17.4: the caption never widens the table
// beyond its own minimum; it wraps to the table's width).
type MinContentContributor interface {
	ContributesMinContent() bool
}

// maxContentMargined is a child's max-content width plus its horizontal
// margins.
func maxContentMargined(w Widget) float32 {
	m := w.Style().Margin
	if c, ok := w.(MinContentContributor); ok && c.ContributesMinContent() {
		return MinContentWidth(w) + m.Left + m.Right
	}
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
	l = l.expandRepeat(children, 0)
	var placements []GridItem
	l, placements = l.withImplicit(children)
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
	for ci, c := range children {
		gi := placements[ci]
		if gi.ColSpan > 1 || gi.Col < 0 || gi.Col >= n || l.Columns[gi.Col].Kind == GridFixed {
			continue
		}
		cols[gi.Col] = max(cols[gi.Col], maxContentMargined(c))
	}
	// Flexible tracks share one fr size, including during max-content
	// sizing. Auto-placed items contribute to their actual column.
	var fr float32
	for i, tr := range l.Columns {
		cols[i] = max(cols[i], tr.Min)
		if tr.Kind == GridFraction && tr.Size > 0 {
			fr = max(fr, cols[i]/tr.Size)
		}
	}
	for i, tr := range l.Columns {
		if tr.Kind == GridFraction {
			cols[i] = fr * tr.Size
		}
	}
	sum := l.ColGap * float32(n-1)
	for _, w := range cols {
		sum += w
	}
	return sum
}

// ShrinkToFit is CSS's shrink-to-fit width (CSS 2.1 § 10.3.5) of w in
// availW: min(max(min-content, available), max-content) — what an
// inline-block, a float or an absolutely positioned box of auto width
// takes. Leaves that know their size are simply measured.
func ShrinkToFit(w Widget, availW float32) float32 {
	if _, ok := w.(layoutHost); !ok {
		if _, ok := w.(MaxContentWidther); !ok {
			return MeasureChild(w, Size{W: availW}).W
		}
	}
	maxW := MaxContentWidth(w)
	if availW <= 0 || maxW <= availW {
		return maxW
	}
	return max(MinContentWidth(w), availW)
}

// aspectWidth is the width an aspect-ratio gives a box with a definite
// height and an auto width (0 when it does not apply).
func aspectWidth(s *Style) float32 {
	if s == nil || s.AspectRatio <= 0 || s.Height <= 0 || s.Width > 0 || s.WidthPct > 0 {
		return 0
	}
	if s.AspectBorderBox {
		return s.Height * s.AspectRatio
	}
	ins := s.ContentInsets()
	return max(s.Height-ins.Vertical(), 0)*s.AspectRatio + ins.Horizontal()
}
