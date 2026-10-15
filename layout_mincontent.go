package qui

// Min-content width: the narrowest a widget can be laid out without its
// content overflowing — every soft wrap opportunity taken, no word broken
// (CSS min-content inline size). Table column sizing needs it to shrink a
// table narrower than its content the way a browser does: columns keep
// their longest word, the slack goes to the columns that can wrap.

// MinContentWidther is implemented by widgets that know their min-content
// width (text: the widest unbreakable segment, plus padding).
type MinContentWidther interface {
	MinContentWidth() float32
}

// MinContentLayout is implemented by layout engines that can derive a
// container's min-content content width from its in-flow children.
type MinContentLayout interface {
	MinContentWidth(children []Widget) float32
}

type layoutHost interface {
	inFlowChildren() []Widget
	layoutEngine() Layout
}

func (c *Container) layoutEngine() Layout { return c.LayoutEngine }

// MinContentWidth returns w's min-content border-box width: its explicit
// width when it has one, else what the widget (MinContentWidther) or its
// layout engine (MinContentLayout) reports, else its max-content width —
// never narrower than it can really get. Min / max widths clamp it.
func MinContentWidth(w Widget) float32 {
	s := w.Style()
	if p := widgetPreferredSize(w); p.W > 0 {
		return p.W
	}
	var v float32
	switch {
	case func() bool { _, ok := w.(MinContentWidther); return ok }():
		v = w.(MinContentWidther).MinContentWidth()
	case func() bool {
		h, ok := w.(layoutHost)
		if !ok {
			return false
		}
		_, ok = h.layoutEngine().(MinContentLayout)
		return ok
	}():
		h := w.(layoutHost)
		v = h.layoutEngine().(MinContentLayout).MinContentWidth(h.inFlowChildren())
		if s != nil {
			v += s.ContentInsets().Horizontal()
		}
	default:
		v = MeasureChild(w, Size{}).W
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

// minContentMargined is a child's min-content width plus its horizontal
// margins.
func minContentMargined(w Widget) float32 {
	m := w.Style().Margin
	return MinContentWidth(w) + m.Left + m.Right
}

// MinContentWidth: every flow child, block or inline, can sit on a band
// of its own, so the flow is as narrow as its widest child.
func (f FlowLayout) MinContentWidth(children []Widget) float32 {
	var v float32
	for _, c := range children {
		v = max(v, minContentMargined(c))
	}
	return v
}

// MinContentWidth: a single-line row needs every item side by side; a
// wrapping row or a column only its widest item.
func (l FlexLayout) MinContentWidth(children []Widget) float32 {
	var sum, widest float32
	for _, c := range children {
		w := minContentMargined(c)
		sum += w
		widest = max(widest, w)
	}
	if l.Direction == Horizontal && !l.Wrap {
		if n := len(children); n > 1 {
			sum += l.Gap * float32(n-1)
		}
		return sum
	}
	return widest
}

// MinContentWidth: fixed columns count as their size; other columns as
// their widest single-span item.
func (l GridLayout) MinContentWidth(children []Widget) float32 {
	n := len(l.Columns)
	if n == 0 {
		return 0
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
		cols[gi.Col] = max(cols[gi.Col], minContentMargined(c))
	}
	sum := l.ColGap * float32(n-1)
	for _, w := range cols {
		sum += w
	}
	return sum
}
