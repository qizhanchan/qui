package widgets

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// Scroll blitting (ScrollView.PixelScroll).
//
// A scroll that only moves the content does not need the content painted
// again: the window shifts the pixels it painted last frame
// (Window.ScrollPixels) and the frame repaints the strip the move exposed.
// What does NOT move with the content is repainted on top: the scrollbars
// (they sit in the view's own space) and the viewport-positioned boxes —
// a fixed header stays where it was on screen, a sticky one may stay or
// move — at their old place, at the place their stale copy was moved to,
// and at their new place.

// scrolled re-places the viewport boxes after the offset moved by (dx, dy)
// content units and invalidates what the move changed on screen.
func (s *ScrollView) scrolled(dx, dy float32) {
	var before []Rect
	blit := s.PixelScroll && s.Window() != nil
	if blit {
		for _, w := range s.vpBoxes {
			before = append(before, PaintBoundsInWindow(w))
		}
	}
	s.placeViewportBoxes()
	if !blit || !s.blitScroll(dx, dy, before) {
		s.Invalidate()
	}
}

// blitScroll asks the window to move the viewport's pixels for an offset
// change of (dx, dy) content units; false when it can't (the caller then
// repaints the whole view).
func (s *ScrollView) blitScroll(dx, dy float32, before []Rect) bool {
	win := s.Window()
	vp := s.viewportRect()
	vpWin := RectInWindow(s, vp)
	// Only a plain translation up to the window: a scaled, rotated or
	// clipped viewport does not map pixel for pixel.
	if !near(vpWin.W, vp.W) || !near(vpWin.H, vp.H) {
		return false
	}
	z := s.zoomFactor()
	mx, my := -dx*z, -dy*z
	if !win.ScrollPixels(vpWin, mx, my) {
		return false
	}
	// The scrollbars: both strips along the inner edges (a hovered overlay
	// bar is wider than its resting width, so take a generous strip).
	const barStrip = 2 * scrollBarThickness
	in := s.innerBounds()
	win.InvalidateRect(RectInWindow(s, Rect{X: in.X + in.W - barStrip, Y: in.Y, W: barStrip, H: in.H}))
	win.InvalidateRect(RectInWindow(s, Rect{X: in.X, Y: in.Y + in.H - barStrip, W: in.W, H: barStrip}))
	for i, w := range s.vpBoxes {
		if i < len(before) {
			old := before[i]
			win.InvalidateRect(old)
			win.InvalidateRect(Rect{X: old.X + mx, Y: old.Y + my, W: old.W, H: old.H}.Intersect(vpWin))
		}
		win.InvalidateRect(PaintBoundsInWindow(w))
	}
	return true
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }
