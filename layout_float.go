package qui

// CSS floats in normal flow (CSS 2.1 § 9.5), as FlowLayout implements them:
//
//   - A child whose Style().Float is FloatLeft / FloatRight leaves the
//     flow: it is sized shrink-to-fit (or by its width), placed against the
//     left / right edge of the flow at the current position — beside any
//     earlier float on that side, lower down when the band is too narrow —
//     and the flow does not advance past it.
//   - Content after a float flows around it: inline runs (widgets that
//     implement FloatExclusionReceiver, such as an InlineBox) narrow their
//     lines beside it, and a normal block child passes the floats down to
//     its own content (the block itself still spans the full width, as in
//     CSS). A child that establishes a new formatting context
//     (Style().NewFormattingContext: overflow other than visible, flex /
//     grid / table / flow-root, …) is instead placed in the space beside
//     the floats.
//   - Style().Clear moves a block below the floats on the cleared side(s).
//   - A flow whose host establishes a formatting context grows to contain
//     its floats; any other flow's height ignores them.
//
// Floats share their formatting context across ordinary block boundaries;
// only a new formatting context contains them.

// FloatSide is CSS float.
type FloatSide uint8

const (
	FloatNone FloatSide = iota
	FloatLeft
	FloatRight
)

// ClearSide is CSS clear.
type ClearSide uint8

const (
	ClearNone ClearSide = iota
	ClearLeft
	ClearRight
	ClearBoth
)

// FloatExclusion is a float's margin box in a receiver's coordinates.
type FloatExclusion struct {
	Rect Rect
	Side FloatSide
}

// FloatExclusionReceiver is implemented by widgets whose content flows
// around floats placed by an ancestor flow: inline runs, and containers
// that pass the floats on to their own flow. The rects are in the
// receiver's own border-box coordinates (origin at its top-left corner).
// Setting different exclusions must invalidate the receiver's layout.
type FloatExclusionReceiver interface {
	SetFloatExclusions(ex []FloatExclusion)
}

// FloatExclusionSource exposes floats escaping an ordinary block, in its
// border-box coordinates, for following content in the same formatting context.
// available is the resolved border-box width and the height offered to Measure.
type FloatExclusionSource interface {
	FlowFloatExclusions(available Size) []FloatExclusion
}

// floatBox is one placed float in a flow's coordinates.
type floatBox struct {
	r    Rect // margin box
	side FloatSide
}

// floatBand answers "how much width is free between the floats at
// [y0, y1)" for a flow whose content spans [x0, x1).
type floatBand struct {
	x0, x1 float32
	floats []floatBox
}

// edges returns the free [left, right) span at the vertical range.
func (b *floatBand) edges(y0, y1 float32) (l, r float32) {
	l, r = b.x0, b.x1
	if y1 <= y0 {
		y1 = y0 + 0.01
	}
	for _, f := range b.floats {
		if f.r.Y >= y1 || f.r.Y+f.r.H <= y0 {
			continue
		}
		if f.side == FloatLeft {
			l = max(l, f.r.X+f.r.W)
		} else {
			r = min(r, f.r.X)
		}
	}
	return l, r
}

// nextBottom returns the smallest float bottom below y (the next y where
// the free band may widen), or -1.
func (b *floatBand) nextBottom(y float32) float32 {
	best := float32(-1)
	for _, f := range b.floats {
		if bot := f.r.Y + f.r.H; bot > y && (best < 0 || bot < best) {
			best = bot
		}
	}
	return best
}

// clearY is the y below every float on the cleared side(s).
func (b *floatBand) clearY(side ClearSide) float32 {
	var y float32
	for _, f := range b.floats {
		if side == ClearBoth || (side == ClearLeft && f.side == FloatLeft) || (side == ClearRight && f.side == FloatRight) {
			y = max(y, f.r.Y+f.r.H)
		}
	}
	return y
}

// bottom is the lowest float bottom (0 when none).
func (b *floatBand) bottom() float32 { return b.clearY(ClearBoth) }

// place positions a float of margin-box size (w, h) at or below y.
func (b *floatBand) place(side FloatSide, w, h, y float32) Rect {
	// A float's top is never above an earlier float's top.
	for _, f := range b.floats {
		y = max(y, f.r.Y)
	}
	for range 1000 {
		l, r := b.edges(y, y+h)
		if r-l >= w || (l == b.x0 && r == b.x1) {
			x := l
			if side == FloatRight {
				x = r - w
			}
			rect := Rect{X: x, Y: y, W: w, H: h}
			b.floats = append(b.floats, floatBox{r: rect, side: side})
			return rect
		}
		next := b.nextBottom(y)
		if next < 0 {
			break
		}
		y = next
	}
	rect := Rect{X: b.x0, Y: y, W: w, H: h}
	if side == FloatRight {
		rect.X = b.x1 - w
	}
	b.floats = append(b.floats, floatBox{r: rect, side: side})
	return rect
}

// exclusionsFor returns the floats that reach below top, translated into
// the coordinates of a child whose border box starts at (ox, oy).
func (b *floatBand) exclusionsFor(top, ox, oy float32) []FloatExclusion {
	var out []FloatExclusion
	for _, f := range b.floats {
		if f.r.Y+f.r.H <= top {
			continue
		}
		r := f.r
		r.X -= ox
		r.Y -= oy
		out = append(out, FloatExclusion{Rect: r, Side: f.side})
	}
	return out
}

// FloatState is a flow container's float bookkeeping, shared by the
// FlowLayout values its host installs (FlowLayout.Floats). The host owns
// it; the parent flow fills Inherited before measuring the host.
type FloatState struct {
	// ContainsFloats: the host establishes a formatting context, so its
	// height includes its floats and it ignores the floats around it.
	ContainsFloats bool
	// Inherited are the floats of an ancestor flow, in the host's content
	// box coordinates.
	Inherited []FloatExclusion
	// Escaped contains locally placed floats, including descendants, in content
	// coordinates. Ancestor exclusions are excluded to avoid duplicating them.
	Escaped []FloatExclusion
	// escapedSize identifies the measurement that produced Escaped. An intrinsic
	// margin probe can otherwise replace it while MeasureChild retains a size
	// computed at the actual flow width.
	escapedSize Size
}

// floatStyle reads a child's float / clear / new-formatting-context.
func floatStyle(w Widget) (FloatSide, ClearSide, bool) {
	s := w.Style()
	if s == nil {
		return FloatNone, ClearNone, false
	}
	return s.Float, s.Clear, s.NewFormattingContext
}

func exclusionsEqual(a, b []FloatExclusion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SetFloatExclusions implements FloatExclusionReceiver for a container
// laid out by a FlowLayout with a FloatState: the floats (in its border
// box coordinates) move into its content box and reach its own flow.
func (c *Container) SetFloatExclusions(ex []FloatExclusion) {
	fl, ok := c.LayoutEngine.(FlowLayout)
	if !ok || fl.Floats == nil || fl.Floats.ContainsFloats {
		return
	}
	in := c.Style().ContentInsets()
	var moved []FloatExclusion
	for _, e := range ex {
		e.Rect.X -= in.Left
		e.Rect.Y -= in.Top
		moved = append(moved, e)
	}
	if exclusionsEqual(fl.Floats.Inherited, moved) {
		return
	}
	fl.Floats.Inherited = moved
	c.InvalidateLayout()
}

// FlowFloatExclusions implements FloatExclusionSource for ordinary flow blocks.
func (c *Container) FlowFloatExclusions(available Size) []FloatExclusion {
	fl, ok := c.LayoutEngine.(FlowLayout)
	if !ok || fl.Floats == nil || fl.Floats.ContainsFloats {
		return nil
	}
	in := c.Style().ContentInsets()
	content := Size{W: max(0, available.W-in.Horizontal()), H: max(0, available.H-in.Vertical())}
	if fl.Floats.escapedSize != content {
		// Bypass the size memo to restore the float geometry for this offer.
		// Descendant float sources perform the same check recursively.
		c.selfOrContainer().Measure(available)
	}
	out := make([]FloatExclusion, len(fl.Floats.Escaped))
	for i, ex := range fl.Floats.Escaped {
		ex.Rect.X += in.Left
		ex.Rect.Y += in.Top
		out[i] = ex
	}
	return out
}
