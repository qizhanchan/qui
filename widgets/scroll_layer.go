package widgets

import (
	"image"
	"log"
	"math"
	"os"

	. "github.com/qizhanchan/qui"
)

// Scroll layers (RFC 0005 P3): a ScrollView in a page window (PageHost)
// commits its content cache's tiles with every frame, so the host's UI
// thread can scroll it while the page thread is busy. See
// qui/page_scroll_layer.go.

var _ ScrollLayerSource = (*ScrollView)(nil)

// nestedLayerWanted reports whether a nested scroller gets a content cache:
// in a page window, opted in, with a solid background to fill under moved
// tiles.
func (s *ScrollView) nestedLayerWanted() bool {
	if !s.NestedLayer || s.LayerBackground == nil {
		return false
	}
	w := s.Window()
	if w == nil || !w.WantsScrollLayers() {
		return false
	}
	_, ok := s.LayerBackground()
	return ok
}

// enclosingLayer is the nearest ancestor ScrollView whose cache the last
// frame composited: the space a nested layer's tiles were recorded in.
func (s *ScrollView) enclosingLayer() *ScrollView {
	for p := s.Parent(); p != nil; p = p.Parent() {
		if sv, ok := p.(*ScrollView); ok {
			if sv.cache != nil && sv.cache.committed {
				return sv
			}
			return nil
		}
	}
	return nil
}

// registerScrollLayer enrols s with a page window. Layout calls it.
func (s *ScrollView) registerScrollLayer() {
	if w := s.Window(); w != nil && w.WantsScrollLayers() {
		w.RegisterScrollLayer(s)
	}
}

// noteCommit records where the cache composited this frame: the frame a
// page commits shows the tiles at exactly this origin.
func (c *scrollCache) noteCommit(phys image.Rectangle, off image.Point, scroll Point) {
	c.commitPhys, c.commitOff, c.commitScroll, c.committed = phys, off, scroll, true
}

func (s *ScrollView) layerOffsets() (off, max Point) {
	if s.Orientation == ScrollHorizontal {
		return Point{X: s.scrollOff}, Point{X: s.maxScroll()}
	}
	off = Point{X: s.scrollX, Y: s.scrollOff}
	max = Point{Y: s.maxScroll()}
	if s.crossScrolls() {
		max.X = s.maxScrollX()
	}
	return off, max
}

// ScrollLayer implements ScrollLayerSource.
func (s *ScrollView) ScrollLayer() (ScrollLayerFrame, bool) {
	win := s.Window()
	if win == nil || s.Content == nil {
		return ScrollLayerFrame{}, false
	}
	scale := win.DevicePixelRatio()
	f := ScrollLayerFrame{Scale: scale, TileSize: contentTileSize}
	f.Offset, f.Max = s.layerOffsets()
	if f.Max.X <= 0 && f.Max.Y <= 0 {
		return f, false
	}
	f.Viewport = deviceRectOf(RectInWindow(s, s.viewportRect()), scale)
	if f.Viewport.Empty() {
		return f, false
	}
	z := s.zoomFactor()
	f.WheelStep = s.wheelStep() / z
	f.PreciseScale = preciseDeltaScale / z
	if bg, ok := s.viewportFill(); ok {
		f.Background = bg
	}
	c := s.cache
	// Composited only when the frame being committed showed the tiles at
	// the current offset. Drawn straight to the window, the tiles are in
	// window device space; drawn into an enclosing scroller's recording (a
	// nested layer), they are in that scroller's content device space and
	// map to the window through its origin.
	phys, origin := image.Rectangle{}, image.Point{}
	if c != nil {
		phys, origin = c.commitPhys, c.commitOff
		if !nearRect(phys, f.Viewport, 1) && s.NestedLayer {
			if p := s.enclosingLayer(); p != nil {
				phys = phys.Sub(p.cache.commitOff)
				origin = origin.Add(p.cache.commitOff)
				if bg, ok := s.LayerBackground(); ok {
					f.Background = bg
				}
			}
		}
	}
	if c == nil || !c.committed || c.disabled || c.content != s.Content || c.commitScroll != f.Offset ||
		!nearRect(phys, f.Viewport, 1) {
		if debugScrollLayers {
			if c == nil {
				log.Printf("qui/scroll-layer: %p not composited: no cache", s)
			} else {
				log.Printf("qui/scroll-layer: %p not composited: committed=%v disabled=%v content=%v scroll=%v/%v phys=%v/%v",
					s, c.committed, c.disabled, c.content == s.Content, c.commitScroll, f.Offset, c.commitPhys, f.Viewport)
			}
		}
		return f, true
	}
	f.Composited = true
	f.Viewport = phys
	f.Origin = origin
	f.Tiles = make(map[image.Point]*image.RGBA, len(c.tiles))
	for k, t := range c.tiles {
		if t.img == nil || t.stale {
			continue
		}
		// Shared with the host from here on: an in-place repaint copies.
		t.private = false
		f.Tiles[k] = t.img
	}
	cover := func(r Rect) {
		if d := deviceRectOf(r, scale).Intersect(f.Viewport); !d.Empty() {
			f.Covers = append(f.Covers, d)
		}
	}
	for _, w := range s.coverBoxes() {
		if !isWidgetHiddenOrCollapsed(w) {
			cover(PaintBoundsInWindow(w))
		}
	}
	for _, l := range s.cacheLayers() {
		cover(PaintBoundsInWindow(l.box))
	}
	if s.hasBar() {
		cover(RectInWindow(s, s.barTrack()))
	}
	if s.hasXBar() {
		cover(RectInWindow(s, s.barX().Track))
	}
	return f, true
}

// ScrollLayerTo implements ScrollLayerSource: the UI thread already shows
// these offsets.
func (s *ScrollView) ScrollLayerTo(x, y float32) {
	if s.Orientation == ScrollHorizontal {
		s.ScrollTo(x)
		return
	}
	if s.crossScrolls() {
		s.ScrollToX(x)
	}
	s.ScrollTo(y)
}

// deviceRectOf is r (logical) in whole device pixels, outward.
func deviceRectOf(r Rect, scale float32) image.Rectangle {
	return image.Rect(
		int(math.Floor(float64(r.X*scale))), int(math.Floor(float64(r.Y*scale))),
		int(math.Ceil(float64((r.X+r.W)*scale))), int(math.Ceil(float64((r.Y+r.H)*scale))))
}

func nearRect(a, b image.Rectangle, tol int) bool {
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	return abs(a.Min.X-b.Min.X) <= tol && abs(a.Min.Y-b.Min.Y) <= tol &&
		abs(a.Max.X-b.Max.X) <= tol && abs(a.Max.Y-b.Max.Y) <= tol
}

// debugScrollLayers (QUI_DEBUG_PAGEVIEW=1) logs why a scroller is not
// composited.
var debugScrollLayers = os.Getenv("QUI_DEBUG_PAGEVIEW") != ""
