package qui

// Viewport-relative positioning: CSS position: fixed and position: sticky.
//
// Both depend on the scroll position of the document's viewport, which
// layout alone does not know: the containers lay a fixed box out like an
// absolute one and a sticky box in flow, and a scroll view that hosts the
// document viewport (widgets.ScrollView.FixedViewport) re-places them
// after its content's layout and on every scroll — a fixed box against the
// visible viewport rect, a sticky box shifted from its in-flow rect so its
// sticky insets hold, without leaving its parent's content box. The boxes
// then have real geometry: painting, hit testing and element geometry need
// no special case.

// ViewportPositioning selects fixed / sticky behavior for a widget.
type ViewportPositioning uint8

const (
	ViewportNone   ViewportPositioning = iota
	ViewportFixed                      // CSS position: fixed
	ViewportSticky                     // CSS position: sticky (insets in AbsolutePosition)
)

// SetViewportPositioning marks the widget fixed or sticky (or neither).
func (b *BaseWidget) SetViewportPositioning(v ViewportPositioning) {
	if b.viewportPos == v {
		return
	}
	b.viewportPos = v
	b.InvalidateLayout()
}

// ViewportPositioning reports the widget's fixed / sticky mode.
func (b *BaseWidget) ViewportPositioning() ViewportPositioning { return b.viewportPos }

// ViewportPositioningOf reads a widget's mode (ViewportNone for widgets
// without one).
func ViewportPositioningOf(w Widget) ViewportPositioning {
	if v, ok := w.(interface{ ViewportPositioning() ViewportPositioning }); ok {
		return v.ViewportPositioning()
	}
	return ViewportNone
}

// ViewportPositioned lists the fixed and sticky widgets in root's subtree
// (root excluded), in tree order. Collapsed subtrees are skipped. The walk
// does not enter widgets that clip their children's coordinates (another
// scroll view): their sticky descendants stick to that scroller.
func ViewportPositioned(root Widget) []Widget {
	var out []Widget
	var walk func(w Widget, top bool)
	walk = func(w Widget, top bool) {
		if !top {
			if isCollapsed(w) {
				return
			}
			if ViewportPositioningOf(w) != ViewportNone {
				out = append(out, w)
			}
			if _, ok := w.(interface{ ChildInteractionTransform() Matrix }); ok {
				return // a nested scroll view: its own viewport
			}
		}
		if cl, ok := w.(ChildLister); ok {
			for _, ch := range cl.ChildList() {
				walk(ch, false)
			}
		}
	}
	walk(root, true)
	return out
}

// PlaceFixed lays out a fixed widget against the viewport rect vp (in the
// coordinates its siblings are laid out in). An axis without insets keeps
// its static position, shifted by scroll — where it sat with the document
// unscrolled.
func PlaceFixed(w Widget, vp Rect, scroll Point) {
	pos := AbsolutePositionOf(w).resolvedIn(vp, w)
	sx, sy := vp.X, vp.Y
	if sp, ok := staticPositionOf(w); ok {
		sx, sy = sp.X+scroll.X, sp.Y+scroll.Y
	}
	x, ww := resolveAnchorAxisStatic(pos.Anchor, AnchorLeft, AnchorRight,
		pos.Left, pos.Right, pos.Width, vp.X, vp.W, w, true, sx)
	y, h := resolveAnchorAxisStatic(pos.Anchor, AnchorTop, AnchorBottom,
		pos.Top, pos.Bottom, pos.Height, vp.Y, vp.H, w, false, sy)
	clamped := applyConstraints(Size{W: ww, H: h}, widgetMinSize(w), widgetMaxSize(w))
	r := Rect{X: x, Y: y, W: clamped.W, H: clamped.H}
	if w.Bounds() != r {
		w.Layout(r)
	}
}

// StickyRect is where a sticky widget whose in-flow rect is base goes for
// the viewport rect vp: shifted so each set inset keeps it that far inside
// the viewport, but never out of its parent's content box.
func StickyRect(w Widget, base, vp Rect) Rect {
	pos := AbsolutePositionOf(w)
	cb := base
	if p := w.Parent(); p != nil {
		cb = p.Bounds()
		if st := p.Style(); st != nil {
			cb = cb.Inset(st.ContentInsets())
		}
	}
	var dx, dy float32
	if pos.Anchor&AnchorTop != 0 {
		if d := vp.Y + pos.Top - base.Y; d > 0 {
			dy = min(d, max(0, cb.Y+cb.H-(base.Y+base.H)))
		}
	}
	if pos.Anchor&AnchorBottom != 0 && dy == 0 {
		if d := vp.Y + vp.H - pos.Bottom - (base.Y + base.H); d < 0 {
			dy = max(d, min(0, cb.Y-base.Y))
		}
	}
	if pos.Anchor&AnchorLeft != 0 {
		if d := vp.X + pos.Left - base.X; d > 0 {
			dx = min(d, max(0, cb.X+cb.W-(base.X+base.W)))
		}
	}
	if pos.Anchor&AnchorRight != 0 && dx == 0 {
		if d := vp.X + vp.W - pos.Right - (base.X + base.W); d < 0 {
			dx = max(d, min(0, cb.X-base.X))
		}
	}
	return Rect{X: base.X + dx, Y: base.Y + dy, W: base.W, H: base.H}
}
