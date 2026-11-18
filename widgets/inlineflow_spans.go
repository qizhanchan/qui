package widgets

import . "github.com/qizhanchan/qui"

// Inline elements inside an InlineBox (CSS 2.1 § 9.2.2 inline boxes): a
// span groups the runs and atomic boxes added between OpenSpan and
// CloseSpan. Its horizontal margin + border + padding take inline space at
// its first and last fragment (glued to the content, never a break of
// their own); its background and border paint behind every line fragment
// it covers, extending vertically past the text's content area by the
// vertical padding + border without affecting line height; and it can
// restyle its text while the pointer is over it (CSS :hover on a link).

// InlineSpanStyle is an inline element's box decoration.
type InlineSpanStyle struct {
	Background  Color
	Border      Insets // widths
	BorderColor Color
	Radius      float32
	Padding     Insets
	Margin      Insets // only Left / Right take effect
	// LineHeightScale is the span's CSS line-height (a multiple of the
	// font size) for its runs; 0 inherits the enclosing one, < 0 is
	// normal.
	LineHeightScale float32
	// BaselineShift raises (+) or lowers (-) the span's content (CSS
	// vertical-align: super / sub / <length>); nested shifts add up.
	BaselineShift float32
	// Source identifies the element (comparable; a pointer).
	Source any
	// Hover, when set, restyles the span's text while the pointer is over
	// any of its fragments.
	Hover *InlineSpanHover
}

// InlineSpanHover is the text restyle of a hovered span.
type InlineSpanHover struct {
	HasColor   bool
	Color      Color
	HasDeco    bool
	Decoration TextDecoration
	DecoPaint  DecorationPaint
	Background Color // replaces the span background when A > 0
}

type inlineSpanRec struct {
	style  InlineSpanStyle
	parent int32
	shift  float32 // cumulative baseline shift
	lh     float32 // effective line-height scale (0 = the box's)
}

// OpenSpan starts an inline element; content added until the matching
// CloseSpan belongs to it. It returns the span's id (> 0).
func (b *InlineBox) OpenSpan(st InlineSpanStyle) int32 {
	parent := b.curSpan()
	shift, lh := st.BaselineShift, st.LineHeightScale
	if parent > 0 {
		shift += b.spans[parent-1].shift
		if lh == 0 {
			lh = b.spans[parent-1].lh
		}
	}
	b.spans = append(b.spans, inlineSpanRec{style: st, parent: parent, shift: shift, lh: lh})
	id := int32(len(b.spans))
	b.spanStack = append(b.spanStack, id)
	if w := st.Margin.Left + st.Border.Left + st.Padding.Left; w > 0 {
		b.parts = append(b.parts, inlinePart{inset: w, span: id})
	}
	b.haveLay, b.haveFit = false, false
	b.InvalidateLayout()
	return id
}

// CloseSpan ends the innermost open span.
func (b *InlineBox) CloseSpan() {
	n := len(b.spanStack)
	if n == 0 {
		return
	}
	id := b.spanStack[n-1]
	b.spanStack = b.spanStack[:n-1]
	st := b.spans[id-1].style
	if w := st.Margin.Right + st.Border.Right + st.Padding.Right; w > 0 {
		b.parts = append(b.parts, inlinePart{inset: w, insetEnd: true, span: id})
	}
	b.haveLay, b.haveFit = false, false
	b.InvalidateLayout()
}

func (b *InlineBox) curSpan() int32 {
	if n := len(b.spanStack); n > 0 {
		return b.spanStack[n-1]
	}
	return 0
}

func (b *InlineBox) curLineHeight() float32 {
	if id := b.curSpan(); id > 0 {
		return b.spans[id-1].lh
	}
	return 0
}

func (b *InlineBox) curShift() float32 {
	if id := b.curSpan(); id > 0 {
		return b.spans[id-1].shift
	}
	return 0
}

// SpanSourceAt returns the Source of the innermost span under window
// point p (nil, false when p is on no span).
func (b *InlineBox) SpanSourceAt(p Point) (any, bool) {
	id := b.spanAt(p)
	if id == 0 {
		return nil, false
	}
	return b.spans[id-1].style.Source, true
}

// SpanRects returns the border boxes of the span with this Source, one per
// line fragment, in the same coordinate space as Bounds (an element's
// client rects).
func (b *InlineBox) SpanRects(source any) []Rect {
	for i := range b.spans {
		if b.spans[i].style.Source == source {
			rects := b.spanLineRects()
			origin := b.Bounds()
			var out []Rect
			for _, lr := range rects {
				if r, ok := lr[int32(i+1)]; ok {
					r.X += origin.X
					r.Y += origin.Y
					out = append(out, r)
				}
			}
			return out
		}
	}
	return nil
}

func (b *InlineBox) spanAt(p Point) int32 {
	if len(b.spans) == 0 {
		return 0
	}
	origin := b.Bounds()
	lp := Point{X: p.X - origin.X, Y: p.Y - origin.Y}
	for _, line := range b.layout.Lines {
		if lp.Y < line.Y || lp.Y >= line.Y+line.Height {
			continue
		}
		for _, f := range line.Frags {
			if f.Span != 0 && lp.X >= f.Rect.X && lp.X < f.Rect.X+f.Rect.W {
				return f.Span
			}
		}
	}
	return 0
}

// inSpan reports whether span id lies inside (or is) span anc.
func (b *InlineBox) inSpan(id, anc int32) bool {
	for id > 0 {
		if id == anc {
			return true
		}
		id = b.spans[id-1].parent
	}
	return false
}

// hoverSpanFor returns the outermost span with a Hover style containing
// id (CSS: every hovered ancestor matches :hover; the outermost link is
// the one that restyles).
func (b *InlineBox) hoverSpanFor(id int32) int32 {
	var found int32
	for id > 0 {
		if b.spans[id-1].style.Hover != nil {
			found = id
		}
		id = b.spans[id-1].parent
	}
	return found
}

// Handle tracks the hovered span (for InlineSpanHover); it never consumes
// the event.
func (b *InlineBox) Handle(event Event) bool {
	if me, ok := event.(MouseEvent); ok && len(b.spans) > 0 {
		var h int32
		switch me.Type() {
		case EventMouseMove, EventMouseEnter:
			h = b.hoverSpanFor(b.spanAt(Point{X: me.X, Y: me.Y}))
		}
		if h != b.hoverSpan {
			b.hoverSpan = h
			b.Invalidate()
		}
	}
	return b.Container.Handle(event)
}

// spanLineRects computes, per line, the border box of every span present
// on it (local coordinates relative to the box's content origin).
func (b *InlineBox) spanLineRects() []map[int32]Rect {
	n := int32(len(b.spans))
	first := make([]int, n+1)
	last := make([]int, n+1)
	for i := range first {
		first[i] = -1
	}
	out := make([]map[int32]Rect, len(b.layout.Lines))
	for li, line := range b.layout.Lines {
		var m map[int32]Rect
		for _, f := range line.Frags {
			if f.Span == 0 {
				continue
			}
			r := f.Rect
			if r.H <= 0 {
				r.Y = line.Baseline - line.InkAscent
				r.H = line.InkAscent + line.InkDescent
			}
			for id := f.Span; id > 0; id = b.spans[id-1].parent {
				if m == nil {
					m = map[int32]Rect{}
				}
				if cur, ok := m[id]; ok {
					m[id] = cur.Union(r)
				} else {
					m[id] = r
				}
				if first[id] < 0 {
					first[id] = li
				}
				last[id] = li
			}
		}
		out[li] = m
	}
	// Extend each span by its padding + border; children (higher ids)
	// first so a parent's box covers its children's.
	for li := range out {
		m := out[li]
		for id := n; id >= 1; id-- {
			r, ok := m[id]
			if !ok {
				continue
			}
			st := b.spans[id-1].style
			r.Y -= st.Padding.Top + st.Border.Top
			r.H += st.Padding.Vertical() + st.Border.Vertical()
			if first[id] == li {
				d := st.Padding.Left + st.Border.Left
				r.X -= d
				r.W += d
			}
			if last[id] == li {
				r.W += st.Padding.Right + st.Border.Right
			}
			m[id] = r
			if p := b.spans[id-1].parent; p > 0 {
				if pr, ok := m[p]; ok {
					m[p] = pr.Union(r)
				}
			}
		}
	}
	// Remember which edges each line fragment owns, for borders.
	b.spanFirst, b.spanLast = first, last
	return out
}

// drawSpans paints span backgrounds and borders (outer spans first).
func (b *InlineBox) drawSpans(canvas Canvas, origin Rect) {
	if len(b.spans) == 0 {
		return
	}
	decorated := false
	for i := range b.spans {
		st := &b.spans[i].style
		if st.Background.A > 0 || (st.BorderColor.A > 0 && st.Border != (Insets{})) ||
			(st.Hover != nil && st.Hover.Background.A > 0) {
			decorated = true
			break
		}
	}
	if !decorated {
		return
	}
	lines := b.spanLineRects()
	for li, m := range lines {
		for id := int32(1); id <= int32(len(b.spans)); id++ {
			r, ok := m[id]
			if !ok {
				continue
			}
			st := b.spans[id-1].style
			r.X += origin.X
			r.Y += origin.Y
			bg := st.Background
			if b.hoverSpan == id && st.Hover != nil && st.Hover.Background.A > 0 {
				bg = st.Hover.Background
			}
			if bg.A > 0 {
				if st.Radius > 0 {
					canvas.FillRoundedRect(r, st.Radius, bg)
				} else {
					canvas.FillRect(r, bg)
				}
			}
			if st.BorderColor.A == 0 || st.Border == (Insets{}) {
				continue
			}
			bc := st.BorderColor
			if w := st.Border.Top; w > 0 {
				canvas.FillRect(Rect{X: r.X, Y: r.Y, W: r.W, H: w}, bc)
			}
			if w := st.Border.Bottom; w > 0 {
				canvas.FillRect(Rect{X: r.X, Y: r.Y + r.H - w, W: r.W, H: w}, bc)
			}
			if w := st.Border.Left; w > 0 && b.spanFirst[id] == li {
				canvas.FillRect(Rect{X: r.X, Y: r.Y, W: w, H: r.H}, bc)
			}
			if w := st.Border.Right; w > 0 && b.spanLast[id] == li {
				canvas.FillRect(Rect{X: r.X + r.W - w, Y: r.Y, W: w, H: r.H}, bc)
			}
		}
	}
}

// hoverLayout returns the layout with the hovered span's text restyled.
func (b *InlineBox) hoverLayout() InlineLayout {
	if b.hoverSpan == 0 {
		return b.layout
	}
	hv := b.spans[b.hoverSpan-1].style.Hover
	if hv == nil || (!hv.HasColor && !hv.HasDeco) {
		return b.layout
	}
	l := b.layout
	l.Lines = append([]InlineLine(nil), b.layout.Lines...)
	for li := range l.Lines {
		touched := false
		for _, f := range l.Lines[li].Frags {
			if f.Span != 0 && b.inSpan(f.Span, b.hoverSpan) {
				touched = true
				break
			}
		}
		if !touched {
			continue
		}
		frags := append([]InlineFrag(nil), l.Lines[li].Frags...)
		for i := range frags {
			if frags[i].Span == 0 || !b.inSpan(frags[i].Span, b.hoverSpan) {
				continue
			}
			if hv.HasColor {
				frags[i].Color = hv.Color
			}
			if hv.HasDeco {
				frags[i].Decoration = hv.Decoration
				frags[i].DecorationPaint = hv.DecoPaint
			}
		}
		l.Lines[li].Frags = frags
	}
	return l
}
