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
	// Floats, when set, is the host's float state: floats among the
	// children are placed and flowed around (layout_float.go). Nil: no
	// child floats, and none from outside reach this flow.
	Floats *FloatState
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
		if pref.W == 0 && s.WidthPct > 0 && widthPctDefinite() {
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
		w = ShrinkToFit(child, availW)
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
	// Floats sit outside margin collapsing: the edge child is the first
	// (last) in-flow child that is not one.
	kids := w.(flowHost).inFlowChildren()
	for i := range kids {
		c := kids[i]
		if bottom {
			c = kids[len(kids)-1-i]
		}
		if fs, _, _ := floatStyle(c); fs != FloatNone {
			continue
		}
		if flowLevelOf(c) != FlowBlock {
			break
		}
		r.merge(edgeMargin(c, bottom))
		// An empty trailing/leading block does not stop adjoining margins.
		// In particular an empty clearfix after a section must carry the
		// section's bottom margin through to its containing block.
		ct, cb := collapseEdges(c)
		if !ct || !cb || MeasureChild(c, Size{W: max(1, w.Bounds().W)}).H != 0 {
			break
		}
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

	// Floats: this flow's own plus those inherited from an ancestor flow,
	// in this run's coordinates.
	var band *floatBand
	if f.Floats != nil {
		band = &floatBand{x0: bounds.X, x1: bounds.X + bounds.W}
		if !f.Floats.ContainsFloats {
			for _, e := range f.Floats.Inherited {
				r := e.Rect
				r.X += bounds.X
				r.Y += bounds.Y
				band.floats = append(band.floats, floatBox{r: r, side: e.Side})
			}
		}
	}
	inheritedN := 0
	if band != nil {
		inheritedN = len(band.floats)
	}

	seenFlow := false // a non-float child has been placed
	for _, child := range children {
		m := child.Style().Margin
		fside, clear, newFC := floatStyle(child)
		if band != nil && fside != FloatNone {
			// A float: out of flow, against the edge at the current position.
			flushLine()
			mw := bounds.W - m.Left - m.Right
			pref := flowPreferredSize(child, cb)
			w := pref.W
			if w == 0 {
				w = ShrinkToFit(child, mw)
			}
			minS, maxS := resolvedMinSize(child, cb), resolvedMaxSize(child, cb)
			w = clampAxis(w, minS.W, maxS.W)
			h := pref.H
			if h == 0 {
				h = MeasureChild(child, Size{W: w, H: cb.H}).H
			}
			h = clampAxis(h, minS.H, maxS.H)
			if clear != ClearNone {
				y = max(y, band.clearY(clear))
			}
			r := band.place(fside, m.Left+w+m.Right, m.Top+h+m.Bottom, y+pending.value())
			if apply {
				child.Layout(Rect{X: r.X + m.Left, Y: r.Y + m.Top, W: w, H: h})
			}
			if r.X+r.W-bounds.X > maxW {
				maxW = r.X + r.W - bounds.X
			}
			continue
		}
		switch flowLevelOf(child) {
		case FlowInline:
			if im, ok := child.(interface{ InlineMargin() Insets }); ok {
				m = im.InlineMargin()
			}
			seenFlow = true
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
			// Clearance moves the border edge below the float. The block's
			// top margin already participates in that hypothetical position;
			// it must not be added a second time after clearing the float.
			clearing := band != nil && clear != ClearNone
			if clearing {
				if !(top && !seenFlow) {
					pending.merge(edgeMargin(child, false))
				}
				if cy := band.clearY(clear); cy > y+pending.value() {
					y = cy - pending.value()
				}
			}
			left := bounds.X
			w := bounds.W - m.Left - m.Right
			if w < 0 {
				w = 0
			}
			if band != nil && len(band.floats) > 0 {
				top := y + pending.value()
				if newFC {
					// A new formatting context sits beside the floats: in
					// the free band at its top, lower down if it can't fit
					// its content there.
					need := MinContentWidth(child)
					for range 100 {
						l, r := band.edges(top, top+1)
						if r-l-m.Left-m.Right >= need || (l == band.x0 && r == band.x1) {
							left, w = l, max(r-l-m.Left-m.Right, 0)
							break
						}
						next := band.nextBottom(top)
						if next < 0 {
							break
						}
						top = next
						y, pending = next, marginRun{}
					}
				} else if rcv, ok := child.(FloatExclusionReceiver); ok {
					// A normal block spans the floats; its content flows
					// around them.
					rcv.SetFloatExclusions(band.exclusionsFor(top, bounds.X+m.Left, top))
				}
			} else if band != nil {
				if rcv, ok := child.(FloatExclusionReceiver); ok {
					rcv.SetFloatExclusions(nil)
				}
			}
			// A block fills the content width unless it declares an explicit
			// width (then it's that wide, left-aligned); height is explicit or
			// natural. Both clamped by min/max. See resolveFlowBlockSize.
			bw, h, intrinsicW := resolveFlowBlockSize(child, cb, w)
			if bwm := m.Left + intrinsicW + m.Right; bwm > maxW {
				maxW = bwm
			}
			if !clearing && !(top && !seenFlow) {
				// The first child's top margin of a collapsing-through
				// container belongs to the container's own top margin.
				pending.merge(edgeMargin(child, false))
			}
			seenFlow = true
			through := h == 0 // an empty block: its margins adjoin each other
			if through {
				pending.merge(edgeMargin(child, true))
			} else {
				gap()
			}
			// CSS auto margins absorb the leftover band space: both auto
			// centers the block (`margin: 0 auto`), left-only pushes it to
			// the right edge. Auto sides carry no numeric margin.
			cx := left + m.Left
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
			if band != nil && !newFC {
				if src, ok := child.(FloatExclusionSource); ok {
					for _, ex := range src.FlowFloatExclusions(Size{W: bw, H: cb.H}) {
						ex.Rect.X += cx
						ex.Rect.Y += y + pending.value()
						band.floats = append(band.floats, floatBox{r: ex.Rect, side: ex.Side})
					}
				}
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
	if band != nil && f.Floats.ContainsFloats && len(band.floats) > inheritedN {
		// A formatting-context root grows to contain its floats.
		y = max(y, band.bottom())
	}
	if band != nil {
		f.Floats.escapedSize = Size{W: bounds.W, H: bounds.H}
		f.Floats.Escaped = f.Floats.Escaped[:0]
		for _, placed := range band.floats[inheritedN:] {
			r := placed.r
			r.X -= bounds.X
			r.Y -= bounds.Y
			f.Floats.Escaped = append(f.Floats.Escaped, FloatExclusion{Rect: r, Side: placed.side})
		}
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
