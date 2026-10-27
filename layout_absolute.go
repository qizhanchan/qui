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
		pos := AbsolutePositionOf(ch).resolvedIn(bounds, ch)
		x, w := resolveAnchorAxis(pos.Anchor, AnchorLeft, AnchorRight,
			pos.Left, pos.Right, pos.Width, bounds.X, bounds.W, ch, true)
		y, h := resolveAnchorAxis(pos.Anchor, AnchorTop, AnchorBottom,
			pos.Top, pos.Bottom, pos.Height, bounds.Y, bounds.H, ch, false)
		clamped := applyConstraints(Size{W: w, H: h}, widgetMinSize(ch), widgetMaxSize(ch))
		ch.Layout(Rect{X: x, Y: y, W: clamped.W, H: clamped.H})
	}
}

// resolveAnchorAxis computes position + size for one axis. When both
// anchors are pinned, the widget stretches between them. When only
// one is pinned, the explicit size (or Measure) is placed flush with
// that edge. When neither is pinned, the widget sits at the parent
// origin at its natural size.
func resolveAnchorAxis(anchor, startBit, endBit AnchorSide,
	startDist, endDist, explicit, parentStart, parentExtent float32,
	child Widget, widthAxis bool) (pos, size float32) {

	start := anchor&startBit != 0
	end := anchor&endBit != 0
	min := widgetMinSize(child)
	minAxis := min.H
	if widthAxis {
		minAxis = min.W
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
			size = anchorMeasure(child, parentExtent, widthAxis)
		}
		if size < minAxis {
			size = minAxis
		}
	case end:
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis)
		}
		if size < minAxis {
			size = minAxis
		}
		pos = parentStart + parentExtent - endDist - size
	default:
		// Neither anchor — place at parent origin with natural size.
		pos = parentStart
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis)
		}
		if size < minAxis {
			size = minAxis
		}
	}
	return pos, size
}

func anchorMeasure(child Widget, parentExtent float32, widthAxis bool) float32 {
	sz := MeasureChild(child, Size{W: parentExtent, H: parentExtent})
	if widthAxis {
		return sz.W
	}
	return sz.H
}
