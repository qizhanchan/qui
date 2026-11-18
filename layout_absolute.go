package qui

// AnchorSide is a bitmask for AbsolutePosition.Anchor.
type AnchorSide int

const (
	AnchorLeft AnchorSide = 1 << iota
	AnchorTop
	AnchorRight
	AnchorBottom
)

// AbsolutePosition stores per-child anchor constraints.
type AbsolutePosition struct {
	Left, Top, Right, Bottom float32
	// Explicit size used when the corresponding axis isn't pinned at
	// both ends. Ignored otherwise (axis derives from parent edges).
	Width, Height float32
	Anchor        AnchorSide
	// *Pct are fractions of the containing block (CSS `left: 50%`,
	// `width: 20%`), used in place of the px field on their side / axis
	// when non-zero. A percentage size also gets the child's
	// Style.PctExtra (content-box padding + border).
	LeftPct, TopPct, RightPct, BottomPct float32
	WidthPct, HeightPct                  float32
	// InlineStatic marks a box whose display before blockification was
	// inline-level: its static position is on the current line (after the
	// inline content before it) rather than below it.
	InlineStatic bool
}

// resolvedIn returns pos with its percentages resolved against the
// containing block cb, for child w.
func (pos AbsolutePosition) resolvedIn(cb Rect, w Widget) AbsolutePosition {
	pct := func(px *float32, frac, base float32) {
		if frac != 0 {
			*px = frac * base
		}
	}
	pct(&pos.Left, pos.LeftPct, cb.W)
	pct(&pos.Right, pos.RightPct, cb.W)
	pct(&pos.Top, pos.TopPct, cb.H)
	pct(&pos.Bottom, pos.BottomPct, cb.H)
	var extra Size
	if s := w.Style(); s != nil {
		extra = s.PctExtra
	}
	if pos.WidthPct != 0 {
		pos.Width = pos.WidthPct*cb.W + extra.W
	}
	if pos.HeightPct != 0 {
		pos.Height = pos.HeightPct*cb.H + extra.H
	}
	return pos
}

// AbsoluteLayout positions children by their AbsolutePosition anchors.
// Children without an Anchor bitmask set fall back to Measure at the
// parent origin — useful for mixing a few unconstrained widgets with
// anchored peers in the same container.
type AbsoluteLayout struct{}

func (AbsoluteLayout) Apply(children []Widget, bounds Rect) {
	for _, ch := range children {
		placeAbsolute(ch, bounds)
	}
}

// placeAbsolute lays out one out-of-flow child against containing block
// cb: insets anchor it, an axis without insets goes to the child's static
// position when one was recorded.
func placeAbsolute(ch Widget, cb Rect) {
	pos := AbsolutePositionOf(ch).resolvedIn(cb, ch)
	sx, sy := cb.X, cb.Y
	if sp, ok := staticPositionOf(ch); ok {
		sx, sy = sp.X, sp.Y
	}
	x, w := resolveAnchorAxisStatic(pos.Anchor, AnchorLeft, AnchorRight,
		pos.Left, pos.Right, pos.Width, cb.X, cb.W, ch, true, sx, 0)
	y, h := resolveAnchorAxisStatic(pos.Anchor, AnchorTop, AnchorBottom,
		pos.Top, pos.Bottom, pos.Height, cb.Y, cb.H, ch, false, sy,
		applyConstraints(Size{W: w}, widgetMinSize(ch), widgetMaxSize(ch)).W)
	clamped := applyConstraints(Size{W: w, H: h}, widgetMinSize(ch), widgetMaxSize(ch))
	ch.Layout(Rect{X: x, Y: y, W: clamped.W, H: clamped.H})
}

// resolveAnchorAxis computes position + size for one axis. When both
// anchors are pinned, the widget stretches between them. When only
// one is pinned, the explicit size (or Measure) is placed flush with
// that edge. When neither is pinned, the widget sits at the parent
// origin at its natural size.
func resolveAnchorAxis(anchor, startBit, endBit AnchorSide,
	startDist, endDist, explicit, parentStart, parentExtent float32,
	child Widget, widthAxis bool) (pos, size float32) {
	return resolveAnchorAxisStatic(anchor, startBit, endBit, startDist, endDist, explicit,
		parentStart, parentExtent, child, widthAxis, parentStart, 0)
}

// resolveAnchorAxisStatic is resolveAnchorAxis with the axis's static
// position: where the box goes when neither inset is set. usedW is the
// box's resolved width when sizing the height axis (its content is laid
// out at that width); 0 measures at the containing block's extent.
func resolveAnchorAxisStatic(anchor, startBit, endBit AnchorSide,
	startDist, endDist, explicit, parentStart, parentExtent float32,
	child Widget, widthAxis bool, static, usedW float32) (pos, size float32) {

	start := anchor&startBit != 0
	end := anchor&endBit != 0
	min := widgetMinSize(child)
	minAxis := min.H
	if widthAxis {
		minAxis = min.W
	}
	// max-width / max-height bound a single-anchored or unanchored box's
	// size BEFORE an end anchor places it: right: 0 with width: 85% and
	// max-width: 27rem sits flush right at 27rem, not at 85%'s left edge.
	if mx := widgetMaxSize(child); !(start && end) {
		maxAxis := mx.H
		if widthAxis {
			maxAxis = mx.W
		}
		if maxAxis > 0 && explicit > maxAxis {
			explicit = maxAxis
		}
	}

	switch {
	case start && end:
		pos = parentStart + startDist
		size = parentExtent - startDist - endDist
		if size < 0 {
			size = 0
		}
		if size < minAxis {
			size = minAxis
		}
	case start:
		pos = parentStart + startDist
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis, usedW)
		}
		if size < minAxis {
			size = minAxis
		}
	case end:
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis, usedW)
		}
		if size < minAxis {
			size = minAxis
		}
		pos = parentStart + parentExtent - endDist - size
	default:
		// Neither anchor — the static position (the parent origin when
		// none was recorded) at the natural size.
		pos = static
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis, usedW)
		}
		if size < minAxis {
			size = minAxis
		}
	}
	return pos, size
}

func anchorMeasure(child Widget, parentExtent float32, widthAxis bool, usedW float32) float32 {
	avail := Size{W: parentExtent, H: parentExtent}
	if !widthAxis && usedW > 0 {
		// The height of content laid out at the box's own width — not at
		// the containing block's HEIGHT taken as a width (a menu hung off
		// a 56px header item would wrap its text into a 56px column).
		avail.W = usedW
	}
	sz := MeasureChild(child, avail)
	if widthAxis {
		return sz.W
	}
	return sz.H
}
