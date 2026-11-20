package qui

import (
	"image"
	"log"
	"math"
	"os"
)

// UI-thread scrolling (RFC 0005 §3.3, P3). A wheel over a composited scroll
// layer moves its committed tiles right away — no page thread involved —
// and tells the page the new offset; until a commit shows the page caught
// up, PageView draws the tiles moved by the difference. A wheel over a slow
// region (a scroller the page did not composite) goes to the page.

// viewLayer is one committed scroll layer and the offset the UI shows.
type viewLayer struct {
	f    ScrollLayerFrame
	ui   Point  // logical offset on screen
	sent uint64 // last SetScrollLayerOffset sequence number sent
}

// delta is how far, in whole device pixels, the screen is ahead of the
// committed frame.
func (l *viewLayer) delta() image.Point {
	s := l.f.Scale
	if s <= 0 {
		s = 1
	}
	return image.Pt(int(math.Round(float64((l.ui.X-l.f.Offset.X)*s))), int(math.Round(float64((l.ui.Y-l.f.Offset.Y)*s))))
}

// adoptLayers takes a commit's layers. A layer the page has caught up on
// shows the page's offset (a script may have scrolled since); one with
// UI scrolls still in flight keeps the UI offset. It returns the device
// areas whose picture changed because of that.
func (v *PageView) adoptLayers(fs []ScrollLayerFrame) (dirty []image.Rectangle) {
	old := map[any]*viewLayer{}
	for _, l := range v.layers {
		old[l.f.Key] = l
	}
	v.layers = v.layers[:0]
	for _, f := range fs {
		l := &viewLayer{f: f, ui: f.Offset}
		if o := old[f.Key]; o != nil {
			wasMoved := o.delta() != image.Point{}
			if o.sent > f.Applied {
				l.ui, l.sent = clampOffset(o.ui, f.Max), o.sent
			}
			if wasMoved || l.delta() != (image.Point{}) {
				dirty = append(dirty, f.Viewport, o.f.Viewport)
			}
		}
		v.layers = append(v.layers, l)
	}
	return dirty
}

func clampOffset(p, max Point) Point {
	return Point{X: min(max.X, p.X*boolf(p.X > 0)), Y: min(max.Y, p.Y*boolf(p.Y > 0))}
}

func boolf(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// containerShift is how far the screen has moved layer i's committed
// viewport: the delta of the innermost composited layer around it.
func (v *PageView) containerShift(i int) image.Point {
	l := v.layers[i]
	for j := i - 1; j >= 0; j-- {
		p := v.layers[j]
		if p.f.Composited && p.f.Depth < l.f.Depth && l.f.Viewport.In(p.f.Viewport) {
			return p.delta().Add(v.containerShift(j))
		}
	}
	return image.Point{}
}

// scrollLayer handles a wheel at page-logical (x, y): it scrolls the
// innermost layer under the pointer that can move in that direction and
// reports true, or false when the page must handle the wheel (a slow
// region, or nothing composited can scroll).
func (v *PageView) scrollLayer(e MouseEvent, x, y float64) bool {
	if debugPageView {
		for _, l := range v.layers {
			log.Printf("qui/pageview: wheel at %.0f,%.0f layer vp=%v off=%v max=%v comp=%v tiles=%d", x, y, l.f.Viewport, l.ui, l.f.Max, l.f.Composited, len(l.f.Tiles))
		}
	}
	if len(v.layers) == 0 {
		return false
	}
	s := v.pageScale()
	p := image.Pt(int(x*float64(s)), int(y*float64(s)))
	precise := e.PreciseScroll || e.ScrollPhase != GesturePhaseNone
	for i := len(v.layers) - 1; i >= 0; i-- {
		l := v.layers[i]
		vp := l.f.Viewport.Sub(v.containerShift(i))
		if !p.In(vp) {
			continue
		}
		step := l.f.WheelStep
		if precise {
			step = l.f.PreciseScale
		}
		d := Point{X: -e.DeltaX * step, Y: -e.DeltaY * step}
		if l.f.Max.X <= 0 {
			d.X = 0
		}
		if l.f.Max.Y <= 0 {
			d.Y = 0
		}
		next := clampOffset(Point{X: l.ui.X + d.X, Y: l.ui.Y + d.Y}, l.f.Max)
		if next == l.ui {
			continue // at its end on this axis: the wheel chains outward
		}
		if !l.f.Composited {
			return false // only the page can scroll it
		}
		l.ui = next
		v.layerSeq++
		l.sent = v.layerSeq
		v.host.SetScrollLayerOffset(l.f.Key, next.X, next.Y, l.sent)
		v.invalidateDevice(vp)
		return true
	}
	return false
}

// drawMovedLayers draws the composited layers the UI has scrolled ahead of
// the committed frame: background, tiles at the UI offset, then the covers
// (fixed / sticky boxes, scrollbars) copied from the frame where they were.
func (v *PageView) drawMovedLayers(c Canvas) {
	if v.front == nil {
		return
	}
	s := v.pageScale()
	b := v.Bounds()
	toLogical := func(r image.Rectangle) Rect {
		return Rect{X: b.X + float32(r.Min.X)/s, Y: b.Y + float32(r.Min.Y)/s, W: float32(r.Dx()) / s, H: float32(r.Dy()) / s}
	}
	for i, l := range v.layers {
		if !l.f.Composited {
			continue
		}
		d := l.delta()
		shift := v.containerShift(i)
		if d == (image.Point{}) && shift == (image.Point{}) {
			continue
		}
		vp := l.f.Viewport.Sub(shift)
		depth := c.Save()
		c.ClipRect(toLogical(vp))
		bg := l.f.Background
		if bg.A == 0 {
			bg = ColorWhite
		}
		c.FillRect(toLogical(vp), bg)
		ts := l.f.TileSize
		for k, img := range l.f.Tiles {
			ib := img.Bounds()
			tr := image.Rectangle{Min: image.Pt(k.X*ts, k.Y*ts)}
			tr.Max = tr.Min.Add(ib.Size())
			dst := tr.Sub(l.f.Origin).Sub(d).Sub(shift)
			vis := dst.Intersect(vp)
			if vis.Empty() {
				continue
			}
			sub := img.SubImage(vis.Add(d).Add(shift).Add(l.f.Origin).Sub(tr.Min).Add(ib.Min)).(*image.RGBA)
			c.DrawImage(sub, toLogical(vis))
		}
		for _, cv := range l.f.Covers {
			cv = cv.Intersect(v.front.Bounds())
			if cv.Empty() {
				continue
			}
			c.DrawImage(v.front.SubImage(cv).(*image.RGBA), toLogical(cv.Sub(shift)))
		}
		c.RestoreTo(depth)
	}
}

// debugPageView (QUI_DEBUG_PAGEVIEW=1) logs PageView's UI-thread scrolling.
var debugPageView = os.Getenv("QUI_DEBUG_PAGEVIEW") != ""
