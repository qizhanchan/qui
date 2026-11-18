package widgets

import . "github.com/qizhanchan/qui"

// Content zoom — browser page zoom for one scroll view.
//
// ContentZoom z lays the content out in a viewport of (viewport / z) units
// and draws it scaled by z about the viewport origin, so the content
// REFLOWS at the narrower width (text re-wraps, percentages resolve
// against it) and is painted through the canvas transform, which
// rasterizes text at the scaled size — crisp, not magnified pixels. It is
// Window.SetZoom confined to one subtree: the chrome around the view
// stays at 100%.
//
// Units: the content tree, ContentSize and the scroll offsets (ScrollY /
// ScrollX / ScrollTo) are in CONTENT units (CSS px for a page); the view's
// own Bounds, its scrollbars and the window are in view units. The
// interaction / paint transforms carry the scale, so hit testing, event
// coordinates, LocalPointToWindow and invalidation all map through it.

// SetContentZoom sets the content zoom (<= 0 or 1: none). The scroll
// position is kept in content units.
func (s *ScrollView) SetContentZoom(z float32) {
	if z <= 0 {
		z = 1
	}
	if z == s.zoomFactor() {
		return
	}
	s.contentZoom = z
	s.InvalidateLayout()
	s.Invalidate()
}

// ContentZoom reports the content zoom (1 when none).
func (s *ScrollView) ContentZoom() float32 { return s.zoomFactor() }

func (s *ScrollView) zoomFactor() float32 {
	if s.contentZoom <= 0 {
		return 1
	}
	return s.contentZoom
}

// unzoom expresses a view-space rect's size in content units (the origin
// is the zoom's fixed point, so it stays).
func (s *ScrollView) unzoom(r Rect) Rect {
	z := s.zoomFactor()
	if z == 1 {
		return r
	}
	r.W /= z
	r.H /= z
	return r
}

// contentMatrix maps content coordinates into the view's space: scroll,
// then scale about the viewport origin.
func (s *ScrollView) contentMatrix() Matrix {
	dx, dy := s.contentTranslation()
	z := s.zoomFactor()
	if z == 1 {
		return TranslateMatrix(dx, dy)
	}
	o := s.innerBounds()
	return TranslateMatrix(o.X, o.Y).Concat(ScaleMatrix(z, z)).Concat(TranslateMatrix(dx-o.X, dy-o.Y))
}

// toContent maps a view-space point into content coordinates.
func (s *ScrollView) toContent(p Point) Point {
	inv, ok := s.contentMatrix().Invert()
	if !ok {
		return p
	}
	return inv.TransformPoint(p)
}

// applyContentTransform concatenates contentMatrix onto the canvas.
func (s *ScrollView) applyContentTransform(canvas Canvas) {
	dx, dy := s.contentTranslation()
	z := s.zoomFactor()
	if z == 1 {
		canvas.Translate(dx, dy)
		return
	}
	o := s.innerBounds()
	canvas.Translate(o.X, o.Y)
	canvas.Scale(z, z)
	canvas.Translate(dx-o.X, dy-o.Y)
}
