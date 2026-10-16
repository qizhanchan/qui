package qui

// FlowLayout is CSS normal flow — the layout engine behind the html-css
// engine's `display: block` (and, degraded, `display: inline` / inline-
// block). It is the document-flow counterpart to FlexLayout (flexbox)
// and GridLayout (grid).
//
// Two participation levels, chosen per child via the FlowLeveled
// interface (children that don't implement it are treated as block):
//
//   - FlowBlock  — the child occupies its own horizontal band: full
//     content width (minus its horizontal margin), natural height,
//     stacked top-to-bottom. This is CSS block-box behavior.
//   - FlowInline — the child is an atomic inline box: laid left-to-
//     right on the current line, wrapping to the next line when it
//     would overflow the content width. This is CSS inline-block
//     behavior. (True word-level inline flow across mixed text + inline
//     elements is handled one level up by folding a run of inline
//     content into a single rich-text Label; FlowLayout only packs
//     atomic inline boxes.)
//
// Vertical spacing between successive block bands / wrapped inline lines
// is LineGap plus each block's own top/bottom margin. Horizontal margins
// are honored on the main axis for both levels; InlineGap separates
// inline boxes on a line.
type FlowLayout struct {
	// LineGap is extra vertical space inserted between block bands and
	// between wrapped inline lines (on top of per-child margins).
	LineGap float32
	// InlineGap is horizontal space between inline boxes on one line.
	InlineGap float32
	// Align places each line of inline boxes within the content width
	// (CSS text-align on the container): start, center or end. Block
	// children are unaffected.
	Align TextAlign
}

// FlowLevel selects how a child participates in a FlowLayout.
type FlowLevel uint8

const (
	FlowBlock  FlowLevel = iota // full-width band, stacked vertically
	FlowInline                  // atomic inline box, packed onto lines
)

// FlowLeveled is implemented by widgets that want to declare their flow
// participation (typically driven by computed CSS `display`). Widgets
// that don't implement it default to FlowBlock.
type FlowLeveled interface {
	FlowLevel() FlowLevel
}

func flowLevelOf(w Widget) FlowLevel {
	if fl, ok := w.(FlowLeveled); ok {
		return fl.FlowLevel()
	}
	return FlowBlock
}

// ShrinkToFitWidth is implemented by block widgets that should be only as
// wide as their content (CSS shrink-to-fit — e.g. an auto-width table)
// rather than filling the available inline width like a normal block.
type ShrinkToFitWidth interface {
	ShrinkToFitWidth() bool
}

func shrinkToFitWidth(w Widget) bool {
	s, ok := w.(ShrinkToFitWidth)
	return ok && s.ShrinkToFitWidth()
}

// flowPreferredSize is a child's explicit size in a flow whose containing
// block (the FlowLayout's content box) is cb: PreferredSize / Style
// Width-Height, else a percentage of cb (CSS `width: 50%` resolves against
// the containing block, not the space left after the child's own margins).
// A zero axis means auto.
func flowPreferredSize(child Widget, cb Size) Size {
	pref := widgetPreferredSize(child)
	if s := child.Style(); s != nil {
		if pref.W == 0 && s.WidthPct > 0 {
			pref.W = s.pctWidth(s.WidthPct, cb.W)
		}
		if pref.H == 0 && s.HeightPct > 0 && cb.H > 0 {
			pref.H = s.pctHeight(s.HeightPct, cb.H)
		}
	}
	return pref
}

// clampAxis applies a min / max pair (0 = none) to one axis.
func clampAxis(v, lo, hi float32) float32 {
	if hi > 0 && v > hi {
		v = hi
	}
	if lo > 0 && v < lo {
		v = lo
	}
	return v
}

// resolveFlowInlineSize resolves an atomic inline box's (inline-block's)
// size on a line in a containing block cb: an explicit width / height wins
// over the shrink-to-fit content size, and both are min/max-clamped — CSS
// sizes an inline-block like a block, it just doesn't fill the line.
func resolveFlowInlineSize(child Widget, cb Size, availW float32) Size {
	min := resolvedMinSize(child, cb)
	max := resolvedMaxSize(child, cb)
	pref := flowPreferredSize(child, cb)
	w := pref.W
	if w == 0 {
		w = MeasureChild(child, Size{W: availW, H: cb.H}).W
	}
	w = clampAxis(w, min.W, max.W)
	h := pref.H
	if h == 0 {
		h = MeasureChild(child, Size{W: w, H: cb.H}).H
	}
	return Size{W: w, H: clampAxis(h, min.H, max.H)}
}

// resolveFlowBlockSize resolves a block child's laid-out size in a band
// availW wide (the containing block cb minus the child's horizontal
// margins). CSS normal flow: a block with an explicit `width` (surfaced as
// PreferredSize / Style.Width, or a percentage of cb) is that wide;
// otherwise it fills the band. Height is the explicit `height` when set,
// else the natural measured height at the resolved width. Both axes are
// clamped by min/max. Returns the border-box width to lay the block out at,
// its height, and its intrinsic (max-content) band width for Measure — which
// is the explicit width when set, else the natural content width (so a
// full-width block still reports its shrink-to-fit width, not availW).
func resolveFlowBlockSize(child Widget, cb Size, availW float32) (layoutW, height, intrinsicW float32) {
	availH := cb.H
	min := resolvedMinSize(child, cb)
	max := resolvedMaxSize(child, cb)
	pref := flowPreferredSize(child, cb)

	layoutW = availW
	explicitW := pref.W > 0
	switch {
	case explicitW:
		layoutW = pref.W
	case shrinkToFitWidth(child):
		// A shrink-to-fit block (CSS display:table without a width) is only
		// as wide as its content, not the full available width.
		if n := MeasureChild(child, Size{W: availW, H: availH}); n.W < layoutW {
			layoutW = n.W
		}
	}
	if min.W > 0 && layoutW < min.W {
		layoutW = min.W
	}
	if max.W > 0 && layoutW > max.W {
		layoutW = max.W
	}

	sz := MeasureChild(child, Size{W: layoutW, H: availH})
	height = sz.H
	if pref.H > 0 {
		height = pref.H
	}
	if min.H > 0 && height < min.H {
		height = min.H
	}
	if max.H > 0 && height > max.H {
		height = max.H
	}

	intrinsicW = sz.W
	if explicitW {
		intrinsicW = layoutW
	}
	return layoutW, height, intrinsicW
}

// MarginCollapser is implemented by flow containers whose own vertical
// margins may collapse with their first / last in-flow block child's (CSS
// margin collapsing through a parent). It answers for the container's
// formatting context only — false for a block formatting context root
// (overflow other than visible, flow-root, a table cell, a control); the
// layout additionally requires no padding / border on that edge, an auto
// height for the bottom edge, and FlowLayout on both the container and its
// parent. Widgets that don't implement it never collapse through.
type MarginCollapser interface {
	MarginsCollapseWithChildren() bool
}

// flowHost is a container laid out by a FlowLayout (Container satisfies it,
// and so does anything embedding one).
type flowHost interface {
	inFlowChildren() []Widget
	flowLayout() (FlowLayout, bool)
}

// collapseEdges reports whether w's top / bottom margin collapses with its
// first / last in-flow child's.
func collapseEdges(w Widget) (top, bottom bool) {
	mc, ok := w.(MarginCollapser)
	if !ok || !mc.MarginsCollapseWithChildren() || flowLevelOf(w) != FlowBlock {
		return false, false
	}
	if h, ok := w.(flowHost); !ok {
		return false, false
	} else if _, ok := h.flowLayout(); !ok {
		return false, false
	}
	if p, ok := w.Parent().(flowHost); !ok {
		return false, false
	} else if _, ok := p.flowLayout(); !ok {
		return false, false
	}
	s := w.Style()
	in := s.ContentInsets()
	top = in.Top == 0
	bottom = in.Bottom == 0 && s.Height == 0 && s.HeightPct == 0 &&
		s.MinHeight == 0 && s.MinHeightPct == 0 && widgetPreferredSize(w).H == 0
	return top, bottom
}

// marginRun accumulates adjoining vertical margins: CSS collapses them to
// the largest positive plus the most negative.
type marginRun struct{ pos, neg float32 }

func (r *marginRun) add(m float32) {
	if m > r.pos {
		r.pos = m
	}
	if m < r.neg {
		r.neg = m
	}
}

func (r *marginRun) merge(o marginRun) { r.add(o.pos); r.add(o.neg) }

func (r marginRun) value() float32 { return r.pos + r.neg }

// edgeMargin is the margin w presents on its top (or bottom) edge: its own,
// collapsed with its first (last) in-flow block child's when the margins
// collapse through w.
func edgeMargin(w Widget, bottom bool) marginRun {
	var r marginRun
	m := w.Style().Margin
	if bottom {
		r.add(m.Bottom)
	} else {
		r.add(m.Top)
	}
	ct, cb := collapseEdges(w)
	if (bottom && !cb) || (!bottom && !ct) {
		return r
	}
	kids := w.(flowHost).inFlowChildren()
	if len(kids) == 0 {
		return r
	}
	k := kids[0]
	if bottom {
		k = kids[len(kids)-1]
	}
	if flowLevelOf(k) == FlowBlock {
		r.merge(edgeMargin(k, bottom))
	}
	return r
}

// run is the one normal-flow pass behind Measure and Apply: it positions
// (when apply) each child in bounds and returns the content height and the
// intrinsic width (widest band / line).
//
// Vertical margins collapse as in CSS: adjoining block margins (a block's
// bottom and the next block's top, and both margins of an empty block)
// combine into one gap, and when the container collapses through
// (collapseEdges), its first / last block child's margin is not inside it —
// the parent flow applies it as part of the container's own margin.
func (f FlowLayout) run(children []Widget, bounds Rect, apply bool) (contentH, maxW float32) {
	var top, bottom bool
	if len(children) > 0 {
		if p := children[0].Parent(); p != nil {
			top, bottom = collapseEdges(p)
		}
	}
	cb := Size{W: bounds.W, H: bounds.H}
	x, y := bounds.X, bounds.Y
	var lineW, lineH float32
	lineOpen := false
	// The open line's boxes, laid out when the line closes so text-align
	// can shift the whole line.
	type placed struct {
		w Widget
		r Rect
	}
	var line []placed
	layLine := func() {
		if apply && len(line) > 0 {
			var off float32
			if free := bounds.W - (lineW - f.InlineGap); free > 0 {
				switch f.Align {
				case TextAlignCenter:
					off = free / 2
				case TextAlignEnd:
					off = free
				}
			}
			for _, p := range line {
				p.r.X += off
				p.w.Layout(p.r)
			}
		}
		line = line[:0]
	}
	var pending marginRun // margins waiting below the last block band
	placedAny := false    // a band or line exists (LineGap goes between them)

	gap := func() {
		if placedAny {
			y += f.LineGap
		}
		placedAny = true
	}
	flushLine := func() {
		if lineOpen {
			layLine()
			if lineW > maxW {
				maxW = lineW
			}
			y += lineH
			x, lineW, lineH = bounds.X, 0, 0
			lineOpen = false
		}
	}

	for i, child := range children {
		m := child.Style().Margin
		switch flowLevelOf(child) {
		case FlowInline:
			sz := resolveFlowInlineSize(child, cb, bounds.W-m.Left-m.Right)
			boxW := m.Left + sz.W + m.Right
			if !lineOpen {
				y += pending.value()
				pending = marginRun{}
				gap()
			} else if lineW+boxW > bounds.W {
				// Wrap when this box's margin box would overflow the line.
				layLine()
				if lineW > maxW {
					maxW = lineW
				}
				y += lineH + f.LineGap
				x, lineW, lineH = bounds.X, 0, 0
			}
			line = append(line, placed{child, Rect{X: x + m.Left, Y: y + m.Top, W: sz.W, H: sz.H}})
			x += boxW + f.InlineGap
			lineW += boxW + f.InlineGap
			if h := m.Top + sz.H + m.Bottom; h > lineH {
				lineH = h
			}
			lineOpen = true

		default: // FlowBlock
			flushLine()
			w := bounds.W - m.Left - m.Right
			if w < 0 {
				w = 0
			}
			// A block fills the content width unless it declares an explicit
			// width (then it's that wide, left-aligned); height is explicit or
			// natural. Both clamped by min/max. See resolveFlowBlockSize.
			bw, h, intrinsicW := resolveFlowBlockSize(child, cb, w)
			if bwm := m.Left + intrinsicW + m.Right; bwm > maxW {
				maxW = bwm
			}
			if !(top && i == 0) {
				// The first child's top margin of a collapsing-through
				// container belongs to the container's own top margin.
				pending.merge(edgeMargin(child, false))
			}
			through := h == 0 // an empty block: its margins adjoin each other
			if through {
				pending.merge(edgeMargin(child, true))
			} else {
				gap()
			}
			// CSS auto margins absorb the leftover band space: both auto
			// centers the block (`margin: 0 auto`), left-only pushes it to
			// the right edge. Auto sides carry no numeric margin.
			cx := bounds.X + m.Left
			if free := w - bw; free > 0 {
				s := child.Style()
				switch {
				case s.MarginLeftAuto && s.MarginRightAuto:
					cx += free / 2
				case s.MarginLeftAuto:
					cx += free
				}
			}
			if apply {
				child.Layout(Rect{X: cx, Y: y + pending.value(), W: bw, H: h})
			}
			if !through {
				y += pending.value() + h
				pending = edgeMargin(child, true)
			}
		}
	}
	flushLine()
	if !bottom {
		y += pending.value()
	}
	return max(y-bounds.Y, 0), maxW
}

// Measure returns the natural content size of a flow of children given
// the available space: the stacked height of block bands and wrapped inline
// lines (see run), and the intrinsic content width (widest band / line), not
// the full available width — a FlowLayout box used as a flex item must
// report its content size for flex-basis. Block children still fill the
// box's given width at Apply time.
func (f FlowLayout) Measure(children []Widget, avail Size) Size {
	h, w := f.run(children, Rect{W: avail.W, H: avail.H}, false)
	if w > avail.W {
		w = avail.W
	}
	return Size{W: w, H: h}
}

func (f FlowLayout) Apply(children []Widget, bounds Rect) {
	f.run(children, bounds, true)
}
