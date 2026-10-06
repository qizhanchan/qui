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

// resolveFlowBlockSize resolves a block child's laid-out size inside a
// content area availW wide. CSS normal flow: a block with an explicit
// `width` (surfaced as PreferredSize / Style.Width) is that wide; otherwise
// it fills the available content width. Height is the explicit `height` when
// set, else the natural measured height at the resolved width. Both axes are
// clamped by min/max. Returns the border-box width to lay the block out at,
// its height, and its intrinsic (max-content) band width for Measure — which
// is the explicit width when set, else the natural content width (so a
// full-width block still reports its shrink-to-fit width, not availW).
func resolveFlowBlockSize(child Widget, availW, availH float32) (layoutW, height, intrinsicW float32) {
	cb := Size{W: availW, H: availH}
	min := resolvedMinSize(child, cb)
	max := resolvedMaxSize(child, cb)
	pref := widgetPreferredSize(child)

	// Percentage sizes resolve here, against the band's available content
	// area — the flow-layout containing block (CSS `width: 50%`).
	if s := child.Style(); s != nil {
		if pref.W == 0 && s.WidthPct > 0 {
			pref.W = availW * s.WidthPct
		}
		if pref.H == 0 && s.HeightPct > 0 && availH > 0 {
			pref.H = availH * s.HeightPct
		}
	}

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

// Measure returns the natural content size of a flow of children given
// the available space. Width fills the available width (block boxes are
// full-width in CSS normal flow); height is the total stacked height of
// block bands and wrapped inline lines. Container.Measure calls this so
// a FlowLayout container reports its true content height to its parent.
func (f FlowLayout) Measure(children []Widget, avail Size) Size {
	var y float32
	var lineW, lineH float32
	var maxW float32 // widest block band / inline line = intrinsic width
	lineOpen := false

	flushLine := func() {
		if lineOpen {
			if lineW > maxW {
				maxW = lineW
			}
			y += lineH + f.LineGap
			lineW, lineH = 0, 0
			lineOpen = false
		}
	}

	for _, child := range children {
		m := child.Style().Margin
		switch flowLevelOf(child) {
		case FlowInline:
			sz := MeasureChild(child, Size{W: avail.W - m.Left - m.Right, H: avail.H})
			boxW := m.Left + sz.W + m.Right
			if lineOpen && lineW+boxW > avail.W {
				if lineW > maxW {
					maxW = lineW
				}
				y += lineH + f.LineGap
				lineW, lineH = 0, 0
			}
			lineW += boxW + f.InlineGap
			if h := m.Top + sz.H + m.Bottom; h > lineH {
				lineH = h
			}
			lineOpen = true
		default:
			flushLine()
			w := avail.W - m.Left - m.Right
			if w < 0 {
				w = 0
			}
			_, h, intrinsicW := resolveFlowBlockSize(child, w, avail.H)
			if bw := m.Left + intrinsicW + m.Right; bw > maxW {
				maxW = bw
			}
			y += m.Top + h + m.Bottom + f.LineGap
		}
	}
	flushLine()
	if y > 0 {
		// Trim the trailing LineGap added after the last band.
		y -= f.LineGap
	}
	if y < 0 {
		y = 0
	}
	// Width is the intrinsic content width (widest band/line), not the
	// full available width — a FlowLayout box used as a flex item must
	// report its content size for flex-basis. Block children still fill
	// the box's given width at Apply time (that's layout, not measure).
	if maxW > avail.W {
		maxW = avail.W
	}
	return Size{W: maxW, H: y}
}

func (f FlowLayout) Apply(children []Widget, bounds Rect) {
	x := bounds.X       // current inline pen position
	y := bounds.Y       // top of the current band / line
	lineH := float32(0) // tallest inline box on the current line
	lineOpen := false   // whether an inline line is in progress

	// flushLine advances y past the current inline line (if any).
	flushLine := func() {
		if lineOpen {
			y += lineH + f.LineGap
			x = bounds.X
			lineH = 0
			lineOpen = false
		}
	}

	for _, child := range children {
		m := child.Style().Margin

		switch flowLevelOf(child) {
		case FlowInline:
			avail := Size{W: bounds.W - m.Left - m.Right, H: bounds.H}
			sz := MeasureChild(child, avail)
			// Wrap when this box would overflow the content width and
			// the line already has something on it.
			if lineOpen && x+m.Left+sz.W > bounds.X+bounds.W {
				y += lineH + f.LineGap
				x = bounds.X
				lineH = 0
			}
			child.Layout(Rect{X: x + m.Left, Y: y + m.Top, W: sz.W, H: sz.H})
			x += m.Left + sz.W + m.Right + f.InlineGap
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
			bw, h, _ := resolveFlowBlockSize(child, w, bounds.H)
			// CSS auto margins absorb the leftover band space: both auto
			// centers the block (`margin: 0 auto`), left-only pushes it to
			// the right edge. Auto sides carry no numeric margin.
			x := bounds.X + m.Left
			if free := w - bw; free > 0 {
				s := child.Style()
				switch {
				case s.MarginLeftAuto && s.MarginRightAuto:
					x += free / 2
				case s.MarginLeftAuto:
					x += free
				}
			}
			child.Layout(Rect{X: x, Y: y + m.Top, W: bw, H: h})
			y += m.Top + h + m.Bottom + f.LineGap
		}
	}
}
