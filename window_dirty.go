package qui

import (
	"image"
	"math"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/render"
)

// Dirty rectangles and pixel scrolling.
//
// dirtyRegion (window.go) stays the one bounding rectangle every caller
// reads. Alongside it the window keeps up to maxDirtyRects of the
// rectangles that made it up, so a frame whose damage is a few separate
// strips — a scrolled editor's newly exposed rows, its sticky header and
// its scrollbar — repaints those strips instead of their bounding box.
// The list is a hint: it is used only when it still accounts for exactly
// dirtyRegion (a full Invalidate, or any path that sets dirtyRegion
// directly, makes it stale and the frame paints the bounding box).
//
// ScrollPixels moves pixels the previous frame left in the renderer's
// buffer, so content that only moved (a scrolled list or editor) is not
// rasterized again: the frame repaints just what the move exposed.

// maxDirtyRects bounds the rectangle list; past it the frame paints the
// bounding box.
const maxDirtyRects = 8

// unionDirty adds r to the dirty region and its rectangle list.
func (w *Window) unionDirty(r Rect) {
	if r.IsEmpty() {
		return
	}
	w.dirtyRegion = w.dirtyRegion.Union(r)
	if w.dirtyRectsOverflow {
		return
	}
	kept := w.dirtyRects[:0]
	for _, d := range w.dirtyRects {
		if rectContains(d, r) {
			return // already covered (and nothing was removed yet)
		}
		if !rectContains(r, d) {
			kept = append(kept, d)
		}
	}
	w.dirtyRects = append(kept, r)
	if len(w.dirtyRects) > maxDirtyRects {
		w.dirtyRects = w.dirtyRects[:0]
		w.dirtyRectsOverflow = true
	}
}

// resetDirty clears the dirty region and its rectangles.
func (w *Window) resetDirty() {
	w.dirtyRegion = Rect{}
	w.dirtyRects = w.dirtyRects[:0]
	w.dirtyRectsOverflow = false
}

func rectContains(outer, inner Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.W <= outer.X+outer.W && inner.Y+inner.H <= outer.Y+outer.H
}

// paintClips is the list of clip rectangles a frame paints: the dirty
// rectangles, clamped to the window and snapped outward to physical
// pixels, when they are a valid and worthwhile split of bounds (the
// clamped, snapped dirty region); otherwise just bounds.
func (w *Window) paintClips(bounds Rect, win Rect, scaleX, scaleY float32) []Rect {
	if w.dirtyRectsOverflow || len(w.dirtyRects) < 2 {
		return []Rect{bounds}
	}
	var union Rect
	for _, d := range w.dirtyRects {
		union = union.Union(d)
	}
	if union != w.dirtyRegion {
		return []Rect{bounds} // something set dirtyRegion directly
	}
	clips := make([]Rect, 0, len(w.dirtyRects))
	var area float32
	for _, d := range w.dirtyRects {
		c := d.Intersect(win)
		if c.IsEmpty() {
			continue
		}
		c = gfx.SnapRectOutward(c, scaleX, scaleY)
		clips = append(clips, c)
		area += c.W * c.H
	}
	// Overlapping or nearly-covering rectangles: one pass is cheaper than
	// several traversals of the tree.
	if len(clips) < 2 || area > 0.7*bounds.W*bounds.H {
		return []Rect{bounds}
	}
	return clips
}

// pixelScroll is a pending ScrollPixels move, in physical pixels.
type pixelScroll struct {
	rect   image.Rectangle
	dx, dy int
}

// ScrollPixels moves the pixels the window last painted inside r (logical
// window coordinates) by (dx, dy) logical pixels, so content that merely
// moved does not have to be rasterized again. It reports whether the move
// will happen; when it returns false nothing changed and the caller must
// repaint r as usual.
//
// On success the window repaints what the move exposed (the part of r the
// moved pixels no longer cover) and carries already-pending damage inside
// r along with the move. Everything else inside r that does not simply
// move with the content — a sticky header, a scrollbar, a caret drawn
// relative to the viewport — the caller must invalidate itself.
//
// A move needs a renderer that keeps its frame in memory (CPU, Metal and
// the GL renderer's CPU path do) and a delta that is a whole number of
// physical pixels; anything else is declined.
func (w *Window) ScrollPixels(r Rect, dx, dy float32) bool {
	if w == nil || (dx == 0 && dy == 0) || r.IsEmpty() {
		return false
	}
	w.assertUIThread("Window.ScrollPixels")
	ps, ok := w.renderer.(render.PixelScroller)
	if !ok || !ps.CanScrollPixels() || w.plat == nil {
		return false
	}
	fbW, fbH := w.plat.framebufferSize()
	if w.lastSize.W <= 0 || w.lastSize.H <= 0 || fbW <= 0 || fbH <= 0 {
		return false
	}
	sx, sy := float32(fbW)/w.lastSize.W, float32(fbH)/w.lastSize.H
	pdx, okx := wholePixels(dx * sx)
	pdy, oky := wholePixels(dy * sy)
	if !okx || !oky {
		return false
	}
	r = r.Intersect(Rect{W: w.lastSize.W, H: w.lastSize.H})
	// Pixels painted over r by something above the caller (an in-window
	// overlay, a drag image, the debug layers) would move with it.
	if w.debugOverlayEnabled || w.highlight != nil || w.coveredByOverlay(r) {
		return false
	}
	if r.IsEmpty() || math.Abs(float64(dx)) >= float64(r.W) || math.Abs(float64(dy)) >= float64(r.H) {
		return false
	}
	// The physical rectangle must be whole pixels too, or the move would
	// smear edge pixels; r is snapped inward so nothing outside it moves.
	x0, y0 := int(math.Ceil(float64(r.X*sx))), int(math.Ceil(float64(r.Y*sy)))
	x1, y1 := int(math.Floor(float64((r.X+r.W)*sx))), int(math.Floor(float64((r.Y+r.H)*sy)))
	if x1-x0 <= intAbs(pdx) || y1-y0 <= intAbs(pdy) {
		return false
	}
	phys := image.Rect(x0, y0, x1, y1)
	inner := Rect{X: float32(x0) / sx, Y: float32(y0) / sy, W: float32(x1-x0) / sx, H: float32(y1-y0) / sy}

	// Damage already pending inside the moved area moves with it: those
	// pixels are stale wherever they end up.
	pending := append([]Rect(nil), w.dirtyRects...)
	if w.dirtyRectsOverflow || len(pending) == 0 {
		pending = []Rect{w.dirtyRegion}
	}
	for _, d := range pending {
		if m := d.Intersect(inner); !m.IsEmpty() {
			w.unionDirty(Rect{X: m.X + dx, Y: m.Y + dy, W: m.W, H: m.H}.Intersect(inner))
		}
	}
	// The partly covered pixels at r's edges did not move but hold part
	// of r's old content: repaint them (the dirty clip snaps outward to
	// whole pixels, so the neighbor's share is repainted too).
	if inner.X > r.X {
		w.unionDirty(Rect{X: r.X, Y: r.Y, W: inner.X - r.X, H: r.H})
	}
	if e, ie := r.X+r.W, inner.X+inner.W; e > ie {
		w.unionDirty(Rect{X: ie, Y: r.Y, W: e - ie, H: r.H})
	}
	if inner.Y > r.Y {
		w.unionDirty(Rect{X: r.X, Y: r.Y, W: r.W, H: inner.Y - r.Y})
	}
	if e, ie := r.Y+r.H, inner.Y+inner.H; e > ie {
		w.unionDirty(Rect{X: r.X, Y: ie, W: r.W, H: e - ie})
	}
	// What the move exposes: the strips of inner the shifted pixels leave.
	ldx, ldy := float32(pdx)/sx, float32(pdy)/sy
	if ldy > 0 {
		w.unionDirty(Rect{X: inner.X, Y: inner.Y, W: inner.W, H: ldy})
	} else if ldy < 0 {
		w.unionDirty(Rect{X: inner.X, Y: inner.Y + inner.H + ldy, W: inner.W, H: -ldy})
	}
	if ldx > 0 {
		w.unionDirty(Rect{X: inner.X, Y: inner.Y, W: ldx, H: inner.H})
	} else if ldx < 0 {
		w.unionDirty(Rect{X: inner.X + inner.W + ldx, Y: inner.Y, W: -ldx, H: inner.H})
	}
	w.pixelScrolls = append(w.pixelScrolls, pixelScroll{rect: phys, dx: pdx, dy: pdy})
	return true
}

// applyPixelScrolls performs the pending moves in the renderer's buffer,
// before the frame paints, and returns the physical area they touched.
func (w *Window) applyPixelScrolls() image.Rectangle {
	if len(w.pixelScrolls) == 0 {
		return image.Rectangle{}
	}
	var moved image.Rectangle
	if ps, ok := w.renderer.(render.PixelScroller); ok {
		for _, s := range w.pixelScrolls {
			if ps.ScrollPixels(s.rect, s.dx, s.dy) {
				moved = moved.Union(s.rect)
			} else {
				w.Invalidate()
			}
		}
	} else {
		w.Invalidate()
	}
	w.pixelScrolls = w.pixelScrolls[:0]
	return moved
}

func wholePixels(v float32) (int, bool) {
	r := math.Round(float64(v))
	return int(r), math.Abs(float64(v)-r) < 0.01
}

func intAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// paintClip paints the window's content inside clip (logical, already
// snapped to physical pixels): background, tree, overlays and the debug
// layers, all scoped to clip so untouched pixels survive.
func (w *Window) paintClip(canvas Canvas, clip Rect) {
	id := canvas.Save()
	defer canvas.RestoreTo(id)
	canvas.ClipRect(clip)
	// An overlay panel has no background of its own: it draws a rounded
	// card and lets the corners composite through to whatever is behind
	// the window. Clearing to Surface there would paint an opaque
	// rectangle over exactly the pixels the transparency exists for.
	//
	// A window with a GPU pre-render hook (SetPreRender) has its background
	// drawn by the hook, under the 2D layer: clear transparent there too.
	if w.kind == WindowOverlayPanel || w.kind == WindowPopup || w.gpuPre != nil {
		canvas.Clear(ColorTransparent)
	} else {
		canvas.Clear(CurrentTheme().Surface)
	}
	if w.root != nil {
		w.root.Draw(canvas)
	}
	// Overlays paint after the main tree, bottom-of-stack first so a
	// later-pushed overlay sits on top.
	for _, ov := range w.overlays {
		if PaintBoundsOf(ov).Intersects(clip) {
			ov.Draw(canvas)
		}
	}
	for _, ov := range w.exitingOverlays {
		if PaintBoundsOf(ov).Intersects(clip) {
			ov.Draw(canvas)
		}
	}
	if img := w.drag.image; img != nil && PaintBoundsOf(img).Intersects(clip) {
		img.Draw(canvas)
	}
	if w.onRender != nil {
		w.onRender(canvas)
	}
	if w.debugOverlayEnabled {
		w.drawDebugOverlay(canvas)
	}
	w.drawHighlight(canvas)
}

// dirtyArea is the physical area the frame's clips cover (bounds when the
// frame painted one clip).
func dirtyArea(clips []Rect, bounds Rect, sx, sy float32) int {
	if len(clips) == 0 {
		clips = []Rect{bounds}
	}
	var a float32
	for _, c := range clips {
		a += c.W * sx * c.H * sy
	}
	return int(a)
}

// coveredByOverlay reports whether an overlay or the drag image paints
// inside r.
func (w *Window) coveredByOverlay(r Rect) bool {
	for _, ov := range w.overlays {
		if PaintBoundsOf(ov).Intersects(r) {
			return true
		}
	}
	for _, ov := range w.exitingOverlays {
		if PaintBoundsOf(ov).Intersects(r) {
			return true
		}
	}
	if img := w.drag.image; img != nil && PaintBoundsOf(img).Intersects(r) {
		return true
	}
	return false
}
