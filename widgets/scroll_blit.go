package widgets

import (
	"image"
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
	// Re-placing the boxes moves them over unchanged content.
	if win := s.Window(); win != nil {
		win.CompositeOnly(s.placeViewportBoxes)
	} else {
		s.placeViewportBoxes()
	}
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
	s.shiftCache(mx, my)
	// What follows repaints the same content in new composition: the
	// scrollbars over it and the viewport boxes' old place, the place
	// their stale copy moved to and their new place. Composite-only, so
	// the content cache (scroll_cache.go) supplies the content there.
	//
	// The scrollbars: both strips along the inner edges (a hovered overlay
	// bar is wider than its resting width, so take a generous strip).
	const barStrip = 2 * scrollBarThickness
	in := s.innerBounds()
	win.InvalidateCompositeRect(RectInWindow(s, Rect{X: in.X + in.W - barStrip, Y: in.Y, W: barStrip, H: in.H}))
	win.InvalidateCompositeRect(RectInWindow(s, Rect{X: in.X, Y: in.Y + in.H - barStrip, W: in.W, H: barStrip}))
	for i, w := range s.vpBoxes {
		if i < len(before) {
			old := before[i]
			win.InvalidateCompositeRect(old)
			win.InvalidateCompositeRect(Rect{X: old.X + mx, Y: old.Y + my, W: old.W, H: old.H}.Intersect(vpWin))
		}
		win.InvalidateCompositeRect(PaintBoundsInWindow(w))
	}
	return true
}

// shiftCache moves the content cache's pixels with a pixel scroll of
// (mx, my) logical units, the way the window moved its own. The strip the
// move exposes arrives as content damage and is re-rendered on paint; a
// move that does not land on whole device pixels drops the cache.
func (s *ScrollView) shiftCache(mx, my float32) {
	c := s.cache
	if c == nil || !c.valid || c.img == nil {
		return
	}
	px, py := float64(mx*c.scaleX), float64(my*c.scaleY)
	dx, dy := int(math.Round(px)), int(math.Round(py))
	if math.Abs(px-float64(dx)) > 0.01 || math.Abs(py-float64(dy)) > 0.01 {
		c.valid = false
		return
	}
	if !shiftRGBA(c.img, dx, dy) {
		c.valid = false
	}
}

// shiftRGBA moves img's pixels by (dx, dy) in place; what moves out is
// lost, what is exposed keeps stale pixels (the caller re-renders it).
func shiftRGBA(img *image.RGBA, dx, dy int) bool {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if dx <= -w || dx >= w || dy <= -h || dy >= h {
		return false
	}
	rowBytes := (w - iabs(dx)) * 4
	srcX, dstX := 0, dx
	if dx < 0 {
		srcX, dstX = -dx, 0
	}
	if dy <= 0 { // moving up: copy rows top to bottom
		for y := 0; y < h+dy; y++ {
			s := (y-dy)*img.Stride + srcX*4
			d := y*img.Stride + dstX*4
			copy(img.Pix[d:d+rowBytes], img.Pix[s:s+rowBytes])
		}
	} else {
		for y := h - 1; y >= dy; y-- {
			s := (y-dy)*img.Stride + srcX*4
			d := y*img.Stride + dstX*4
			copy(img.Pix[d:d+rowBytes], img.Pix[s:s+rowBytes])
		}
	}
	return true
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }
