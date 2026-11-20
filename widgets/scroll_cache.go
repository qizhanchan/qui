package widgets

import (
	"fmt"
	"image"
	"math"
	"os"
	"slices"
	"time"

	. "github.com/qizhanchan/qui"
)

// Content cache (ScrollView with PixelScroll and fixed / sticky boxes).
//
// A pixel scroll moves what is on screen and repaints the exposed strip,
// but a box that stays on screen while the content moves under it — a
// sticky header, often translucent with a backdrop blur and a shadow —
// must be recomposited every step over the content beneath it. Without a
// copy of that content, the view re-renders the whole content stack in
// the box's band each frame (react.dev: a 160px band of gradients and
// shadows, most of every scroll frame).
//
// The cache is that copy: the viewport's content rendered without the
// fixed / sticky boxes, in device pixels, moved with every pixel scroll.
// A frame re-renders into it only what the window reports as content
// damage (Window.ContentDirty); damage marked composite-only (the boxes'
// old and new places, the scrollbars) is a copy from the cache, after
// which the boxes are drawn on top in the canvas.
//
// It stays off when the boxes cannot be drawn on their own exactly as in
// place (an ancestor clips, transforms or applies an effect) and for
// content that draws through the GPU (retained / GPU draws need the
// window's canvas).

// The cache is tiled (RFC 0004 R3): 256×256 device-pixel tiles in
// CONTENT device space — the content's own coordinates times the device
// scale, independent of the scroll offset — so a scroll moves where the
// tiles land, never what they hold. The UI thread records the content
// over an interest area (the viewport and a viewport's worth around it)
// as a Picture; raster workers play it into tiles. A frame composites the
// visible tiles; those missing are rasterized in parallel and waited for,
// the ones around the viewport are prepared in the background so a
// scroll finds them ready. Content damage repaints the damaged part of
// the tiles that hold it, on this thread (it is usually small).

const contentTileSize = 256

type contentTile struct {
	img     *image.RGBA // nil until its first raster lands
	pending *RasterTask // a raster on the worker pool
	stale   bool        // img predates content damage too large to repaint in place
	private bool        // img was copied for an in-place repaint this frame
	used    uint64      // paint seq it was last needed in
}

type scrollCache struct {
	tiles   map[image.Point]*contentTile // by tile index in content device space
	scaleX  float32                      // content device scale
	scaleY  float32
	phase   Point // sub-pixel phase the tiles are rasterized at
	content Widget
	valid   bool
	// disabled: the content needed the window canvas (GPU draws).
	disabled bool
	seq      uint64 // window paint the damage was last applied for
	seqSet   bool
	layers   []*Box // animated layers kept out of it (anim_layer.go)
	// pic is the content recorded over picRect (content device space);
	// nil once content damage made it stale.
	pic     *Picture
	picRect image.Rectangle
	// Where the last frame composited the tiles (scroll_layer.go): window
	// device viewport, content device origin, and the offsets it showed.
	commitPhys   image.Rectangle
	commitOff    image.Point
	commitScroll Point
	committed    bool
}

// tileRect is tile k's rectangle in content device space.
func tileRect(k image.Point) image.Rectangle {
	return image.Rect(k.X*contentTileSize, k.Y*contentTileSize, (k.X+1)*contentTileSize, (k.Y+1)*contentTileSize)
}

// tilesIn lists the tiles r touches.
func tilesIn(r image.Rectangle) []image.Point {
	if r.Empty() {
		return nil
	}
	x0, y0 := floorDiv(r.Min.X, contentTileSize), floorDiv(r.Min.Y, contentTileSize)
	x1, y1 := floorDiv(r.Max.X-1, contentTileSize), floorDiv(r.Max.Y-1, contentTileSize)
	out := make([]image.Point, 0, (x1-x0+1)*(y1-y0+1))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			out = append(out, image.Pt(x, y))
		}
	}
	return out
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// cacheWanted reports whether the content cache applies to this view now.
func (s *ScrollView) cacheWanted(layers []cacheLayer) bool {
	if s.Content == nil || tiledCacheOff && len(s.vpBoxes) == 0 && len(layers) == 0 {
		return false
	}
	if !(s.PixelScroll && s.FixedViewport) && !s.nestedLayerWanted() {
		return false
	}
	if s.cache != nil && s.cache.disabled && s.cache.content == s.Content {
		return false
	}
	// A box that cannot be drawn on its own exactly as in place (an
	// ancestor transforms, clips or applies an effect — a fixed box under a
	// transform is not even fixed in CSS) is painted into the tiles like
	// the rest of the content; its moves are content damage (blitScroll).
	s.tileBoxes = s.tileBoxes[:0]
	for _, w := range s.vpBoxes {
		if !plainPathToContent(w, s.Content) {
			if debugTiles {
				noteTileBypass(fmt.Sprintf("viewport box %T pos=%v in tiles: %s", w, ViewportPositioningOf(w), whyNotPlain(w, s.Content)))
			}
			s.tileBoxes = append(s.tileBoxes, w)
		}
	}
	return true
}

// inTiles reports whether viewport box w is painted into the tiles.
func (s *ScrollView) inTiles(w Widget) bool { return slices.Contains(s.tileBoxes, w) }

// coverBoxes are the viewport boxes drawn over the tiles.
func (s *ScrollView) coverBoxes() []Widget {
	if len(s.tileBoxes) == 0 {
		return s.vpBoxes
	}
	out := make([]Widget, 0, len(s.vpBoxes))
	for _, w := range s.vpBoxes {
		if !s.inTiles(w) {
			out = append(out, w)
		}
	}
	return out
}

// whyNotPlain names the first ancestor property that makes plainPathToContent
// false (QUI_DEBUG_TILES).
func whyNotPlain(w, content Widget) string {
	for cur := w.Parent(); cur != nil && cur != content; cur = cur.Parent() {
		if t, ok := cur.(ChildPaintTransformer); ok && t.ChildPaintTransform() != IdentityMatrix() {
			return fmt.Sprintf("%T child transform", cur)
		}
		if t, ok := cur.(PaintTransformer); ok && t.PaintTransform() != IdentityMatrix() {
			return fmt.Sprintf("%T transform", cur)
		}
		if c, ok := cur.(PaintClipper); ok && !c.PaintClip().IsEmpty() {
			return fmt.Sprintf("%T clip", cur)
		}
		if b := asStackBox(cur); b != nil {
			op := b.Style().Opacity
			if (op > 0 && op < 1) || b.Filter != nil || b.ClipPath != nil || b.Mask != nil || b.BackdropFilter != nil || b.ClipChildren {
				return fmt.Sprintf("box opacity=%v filter=%v clipPath=%v mask=%v backdrop=%v clipChildren=%v", op, b.Filter != nil, b.ClipPath != nil, b.Mask != nil, b.BackdropFilter != nil, b.ClipChildren)
			}
		}
	}
	return "?"
}

// tiledCacheOff (QUI_CONTENT_TILES=0) keeps the cache to views with
// viewport boxes or animated layers, as before tiling, to bisect.
var tiledCacheOff = os.Getenv("QUI_CONTENT_TILES") == "0"

// plainPathToContent reports whether every ancestor of w up to and
// including content paints w untouched: no clip, transform, opacity or
// effect, so drawing w alone (under the content transform) matches its
// in-place paint.
func plainPathToContent(w, content Widget) bool {
	for cur := w.Parent(); cur != nil; cur = cur.Parent() {
		if t, ok := cur.(ChildPaintTransformer); ok && t.ChildPaintTransform() != IdentityMatrix() {
			return false
		}
		if t, ok := cur.(PaintTransformer); ok && t.PaintTransform() != IdentityMatrix() {
			return false
		}
		if c, ok := cur.(PaintClipper); ok && !c.PaintClip().IsEmpty() {
			return false
		}
		if b := asStackBox(cur); b != nil {
			op := b.Style().Opacity
			if (op > 0 && op < 1) || b.Filter != nil || b.ClipPath != nil || b.Mask != nil ||
				b.BackdropFilter != nil || b.ClipChildren {
				return false
			}
		}
		if cur == content {
			return true
		}
	}
	return false // w is not under content
}

// contentDeviceMatrix is the canvas matrix content draws under, the
// whole-pixel offset from device space to content device space (content
// device = device + off), and the sub-pixel phase the content sits at
// (tiles are rasterized at it: a scroll that changes it re-rasterizes
// them); false when the content is not a plain scale + translation.
func (s *ScrollView) contentDeviceMatrix(m Matrix) (Matrix, image.Point, Point, bool) {
	geo := &RecordingCanvas{}
	geo.Concat(m)
	s.applyContentTransform(geo)
	mc := geo.CurrentMatrix()
	if mc.B != 0 || mc.C != 0 || mc.A <= 0 || mc.D <= 0 {
		return mc, image.Point{}, Point{}, false
	}
	tx, ty := math.Round(float64(mc.TX)), math.Round(float64(mc.TY))
	phase := Point{X: float32(math.Round((float64(mc.TX)-tx)*256) / 256), Y: float32(math.Round((float64(mc.TY)-ty)*256) / 256)}
	return mc, image.Pt(-int(tx), -int(ty)), phase, true
}

// drawCached draws the content through the cache. effClip is the logical
// part of the viewport this frame paints (already clipped on canvas), vp
// the viewport. False means the caller draws the content directly.
func (s *ScrollView) drawCached(canvas Canvas, effClip, vp Rect) bool {
	layers := s.cacheLayers()
	if !s.cacheWanted(layers) {
		noteTileBypass("not wanted")
		s.dropCache()
		s.dropGPULayers()
		return false
	}
	win := s.Window()
	ca, ok := canvas.(ClipAware)
	if win == nil || !ok || !win.InPaintPass() {
		noteTileBypass("outside paint")
		return false
	}
	m := canvas.CurrentMatrix()
	if m.B != 0 || m.C != 0 || m.A <= 0 || m.D <= 0 {
		noteTileBypass("transformed")
		s.dropGPULayers()
		return false
	}
	phys, ok := wholeDeviceRect(m.TransformRect(vp))
	mc, off, phase, ok2 := s.contentDeviceMatrix(m)
	if !ok || !ok2 || phys.Empty() {
		noteTileBypass(fmt.Sprintf("off-pixel vp=%v content=%v", ok, ok2))
		s.dropGPULayers()
		return false
	}
	c := s.cache
	if c == nil || c.content != s.Content || c.scaleX != mc.A || c.scaleY != mc.D {
		s.dropCache()
		c = &scrollCache{content: s.Content, scaleX: mc.A, scaleY: mc.D, phase: phase, tiles: map[image.Point]*contentTile{}}
		s.cache = c
	}
	if c.phase != phase {
		// A scroll by a fraction of a device pixel: the tiles hold the
		// content at another phase.
		c.phase = phase
		c.invalidateAll()
	}
	if sig := layerSignature(layers); !slices.Equal(sig, c.layers) {
		// A box that joined or left the layers is missing from (or still
		// in) the cached pixels.
		c.layers = sig
		c.invalidateAll()
	}
	seq := win.PaintSeq()
	// The damage is the frame's: a frame paints its clip rectangles one by
	// one, so apply it once.
	if !c.seqSet || c.seq != seq {
		c.seq, c.seqSet = seq, true
		rects, all := win.ContentDirty()
		if !c.valid || all {
			c.invalidateAll()
		} else if len(rects) > 0 {
			vpWin := RectInWindow(s, vp)
			woff := Point{X: vpWin.X - vp.X, Y: vpWin.Y - vp.Y}
			var dev []image.Rectangle
			for _, r := range rects {
				// Not clipped to the viewport: tiles are kept ready around it.
				lr := Rect{X: r.X - woff.X, Y: r.Y - woff.Y, W: r.W, H: r.H}
				if lr.IsEmpty() {
					continue
				}
				dr := m.TransformRect(lr)
				d := image.Rect(int(math.Floor(float64(dr.X))), int(math.Floor(float64(dr.Y))),
					int(math.Ceil(float64(dr.X+dr.W))), int(math.Ceil(float64(dr.Y+dr.H)))).Add(off)
				dev = append(dev, d)
			}
			if s.repaintDamage(canvas, m, off, dev, layers) {
				c.disabled = true
				s.dropCache()
				s.dropGPULayers()
				return false
			}
		}
		c.valid = true
	}

	// The tiles this frame shows, and the ones kept ready around them.
	view := phys.Add(off)
	ready := s.readyArea(view, mc, off)
	if s.prepareTiles(canvas, m, off, view, ready, layers, seq) {
		c.disabled = true
		s.dropCache()
		s.dropGPULayers()
		return false
	}

	if win.WantsScrollLayers() {
		so, _ := s.layerOffsets()
		c.noteCommit(phys, off, so)
	}

	// Composite the visible tiles for this frame's clip, 1:1 in device
	// pixels.
	cb := ca.ClipBounds()
	clip := image.Rect(int(math.Floor(float64(cb.X))), int(math.Floor(float64(cb.Y))),
		int(math.Ceil(float64(cb.X+cb.W))), int(math.Ceil(float64(cb.Y+cb.H)))).Intersect(phys)
	for _, k := range tilesIn(clip.Add(off)) {
		t := c.tiles[k]
		if t == nil || t.img == nil {
			continue
		}
		dst := tileRect(k).Sub(off).Intersect(clip)
		if dst.Empty() {
			continue
		}
		t.private = false // shown from here on: shared with whatever records this frame
		sub := t.img.SubImage(dst.Add(off).Sub(tileRect(k).Min)).(*image.RGBA)
		canvas.DrawImage(sub, Rect{
			X: (float32(dst.Min.X) - m.TX) / m.A, Y: (float32(dst.Min.Y) - m.TY) / m.D,
			W: float32(dst.Dx()) / m.A, H: float32(dst.Dy()) / m.D,
		})
	}

	// The animated layers, then the fixed / sticky boxes on top, as the
	// content would paint them.
	if !s.drawGPULayers(canvas, effClip, vp, layers, seq) {
		s.drawLayers(canvas, effClip, layers, seq)
	}
	depth := canvas.Save()
	canvas.ClipRect(effClip)
	s.applyContentTransform(canvas)
	for _, w := range s.coverBoxes() {
		if !isWidgetHiddenOrCollapsed(w) {
			w.Draw(canvas)
		}
	}
	canvas.RestoreTo(depth)
	if debugTiles {
		tileDbg.frames++
		tileDbg.tiles = len(c.tiles)
		flushTileDebug()
	}
	if debugMem {
		s.noteCacheMem()
	}
	return true
}

// noteCacheMem publishes the cache's bytes to the QUI_DEBUG_MEM report.
func (s *ScrollView) noteCacheMem() {
	c := s.cache
	if c == nil {
		memNote("content-tiles", s, 0, 0)
		memNote("content-recording", s, 0, 0)
		return
	}
	n, b := 0, 0
	for _, t := range c.tiles {
		if t.img != nil {
			n++
			b += len(t.img.Pix)
		}
	}
	memNote("content-tiles", s, b, n)
	memNote("content-recording", s, c.pic.Bytes(), c.pic.Len())
}

// readyArea is the content device area whose tiles are kept rasterized:
// the view, a view height above and below, half a width either side,
// within the content — narrowed (sides first) to what fits
// contentTileBudget, as Chromium's tile manager shrinks its interest
// area to its memory budget. The view itself is always kept.
func (s *ScrollView) readyArea(view image.Rectangle, mc Matrix, off image.Point) image.Rectangle {
	within := func(r image.Rectangle) image.Rectangle {
		if ext := s.contentExtent(mc, off); !ext.Empty() {
			return r.Intersect(ext.Union(view))
		}
		return r
	}
	mx, my := view.Dx()/2, view.Dy()
	r := within(image.Rect(view.Min.X-mx, view.Min.Y-my, view.Max.X+mx, view.Max.Y+my))
	if tileBytes(r) <= contentTileBudget {
		return r
	}
	r = within(image.Rect(view.Min.X, view.Min.Y-my, view.Max.X, view.Max.Y+my))
	for tileBytes(r) > contentTileBudget && my > 0 {
		my = my * 3 / 4
		if my < contentTileSize {
			my = 0
		}
		r = within(image.Rect(view.Min.X, view.Min.Y-my, view.Max.X, view.Max.Y+my))
	}
	return r
}

// contentTileBudget bounds the bytes of the tiles a view keeps ready
// (react.dev in a 1000×672 window at 2×: about 32 MiB).
const contentTileBudget = 64 << 20

// tileBytes is what the tiles r touches hold.
func tileBytes(r image.Rectangle) int {
	return len(tilesIn(r)) * contentTileSize * contentTileSize * 4
}

// contentExtent is the content's paint bounds in content device space.
func (s *ScrollView) contentExtent(mc Matrix, off image.Point) image.Rectangle {
	pb := PaintBoundsOf(s.Content)
	if pb.IsEmpty() {
		return image.Rectangle{}
	}
	d := mc.TransformRect(pb)
	return image.Rect(int(math.Floor(float64(d.X))), int(math.Floor(float64(d.Y))),
		int(math.Ceil(float64(d.X+d.W))), int(math.Ceil(float64(d.Y+d.H)))).Add(off)
}

// prepareTiles makes sure every tile of view has pixels (waiting for the
// rasters of those that have none) and that those of ready are on their
// way; it drops tiles far outside ready. True means the content needed
// the window's GPU canvas.
func (s *ScrollView) prepareTiles(canvas Canvas, m Matrix, off image.Point, view, ready image.Rectangle, layers []cacheLayer, seq uint64) bool {
	c := s.cache
	win := s.Window()
	need := func(k image.Point, visible bool) (*contentTile, bool) {
		t := c.tiles[k]
		if t == nil {
			t = &contentTile{}
			c.tiles[k] = t
		}
		t.used = seq
		if (t.img != nil && !t.stale) || t.pending != nil {
			return t, true
		}
		tr := tileRect(k)
		if c.pic == nil || !tr.In(c.picRect) {
			if s.recordInterest(canvas, m, off, ready.Union(tr), layers) {
				return nil, false
			}
		}
		prio := 2
		if visible {
			prio = 0
		}
		var task *RasterTask
		task = ScheduleRaster(c.pic, tileRasterRect(c.pic, tr), prio, func(*RasterTask) {
			win.PostJob(func() { s.adoptTile(c, t, task, k) })
		})
		t.pending = task
		if task.Done() {
			s.adoptTile(c, t, task, k)
		}
		return t, true
	}
	visible := tilesIn(view)
	for _, k := range visible {
		if _, ok := need(k, true); !ok {
			return true
		}
	}
	for _, k := range tilesIn(ready) {
		if _, ok := need(k, false); !ok {
			return true
		}
	}
	// Visible tiles with nothing current to show wait for their rasters,
	// which were all scheduled above and so run in parallel: a frame never
	// mixes old and new content (Chromium likewise activates a tree only
	// once its visible tiles are ready).
	for _, k := range visible {
		t := c.tiles[k]
		if (t.img == nil || t.stale) && t.pending != nil {
			task := t.pending
			t0 := time.Now()
			task.Wait()
			if debugTiles {
				tileDbg.waits++
				tileDbg.waitTime += time.Since(t0)
			}
			s.adoptTile(c, t, task, k)
		}
	}
	// Drop tiles well outside the ready area, and the stale pixels of
	// tiles out of view: they are being rasterized again, and until then
	// nothing shows them (a scroll that reaches one first waits for its
	// raster, like a tile never rasterized).
	keep := ready.Inset(-contentTileSize)
	inView := view.Inset(-contentTileSize)
	for k, t := range c.tiles {
		if t.used != seq && !tileRect(k).Overlaps(keep) {
			t.pending.Cancel()
			delete(c.tiles, k)
		} else if t.stale && !tileRect(k).Overlaps(inView) {
			t.img, t.stale, t.private = nil, false, false
		}
	}
	return false
}

// filterPad is the margin a tile is rasterized with when the content
// runs image filters (blur, drop shadow, backdrop-filter): a filter reads
// pixels around the ones it writes, and a tile cut at its own edge would
// show seams. Chromium sidesteps this with separate render surfaces.
const filterPad = 128

// tileRasterRect is what to rasterize for tile rectangle tr.
func tileRasterRect(pic *Picture, tr image.Rectangle) image.Rectangle {
	if pic.FiltersNear(tr, filterPad) {
		return tr.Inset(-filterPad)
	}
	return tr
}

// tileImage is a tile's pixels out of its raster (cropping the filter
// margin), with its rectangle at (0,0). A raster with the margin is
// copied out: a sub-image would keep the whole padded raster — four
// times the tile — alive as long as the tile.
func tileImage(img *image.RGBA) *image.RGBA {
	if img == nil || img.Rect.Dx() == contentTileSize && img.Rect.Dy() == contentTileSize {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, contentTileSize, contentTileSize))
	for y := 0; y < contentTileSize; y++ {
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride], img.Pix[img.PixOffset(filterPad, filterPad+y):])
	}
	return out
}

// adoptTile makes a finished raster tile t's pixels; a tile that was
// showing stale pixels is recomposited.
func (s *ScrollView) adoptTile(c *scrollCache, t *contentTile, task *RasterTask, k image.Point) {
	if t.pending != task || s.cache != c {
		return
	}
	t.pending = nil
	img := tileImage(task.Image())
	if img == nil {
		return
	}
	if debugTiles {
		tileDbg.rasters++
		tileDbg.rasterTime += task.Duration()
		if t.stale {
			tileDbg.staleRasters++
		}
	}
	wasStale := t.stale && t.img != nil
	t.img, t.stale = img, false
	if wasStale {
		s.invalidateTile(k)
	}
}

// invalidateTile recomposites tile k's area of the view, if it shows.
func (s *ScrollView) invalidateTile(k image.Point) {
	win := s.Window()
	c := s.cache
	if win == nil || c == nil {
		return
	}
	r := tileRect(k)
	// content device → content logical → view → window
	cr := Rect{X: float32(r.Min.X) / c.scaleX, Y: float32(r.Min.Y) / c.scaleY,
		W: float32(r.Dx()) / c.scaleX, H: float32(r.Dy()) / c.scaleY}
	m := s.ChildPaintTransform()
	win.InvalidateCompositeRect(RectInWindow(s, m.TransformRect(cr)).Intersect(RectInWindow(s, s.viewportRect())))
}

// recordInterest records the content over area (content device space),
// grown to the interest margin, as the cache's picture. True means the
// content needed the window's GPU canvas.
func (s *ScrollView) recordInterest(window Canvas, m Matrix, off image.Point, area image.Rectangle, layers []cacheLayer) bool {
	c := s.cache
	rect := area.Inset(-contentTileSize)
	cc, rec := NewPictureCanvas(rect)
	t0 := time.Now()
	if s.drawContentInto(window, cc, m, off, layers) {
		return true
	}
	c.pic, c.picRect = rec.Finish(), rect
	if debugTiles {
		tileDbg.records++
		tileDbg.recordTime += time.Since(t0)
	}
	return false
}

// drawContentInto draws the content (without the viewport boxes and
// animated layers) into cc, whose coordinates are content device space.
// True means the content asked for the window's GPU canvas, which the
// cache cannot stand in for.
func (s *ScrollView) drawContentInto(window, cc Canvas, m Matrix, off image.Point, layers []cacheLayer) bool {
	probe := &gpuProbe{}
	if _, isGPU := window.(GPUCanvas); isGPU {
		cc = gpuProbeCanvas{Canvas: cc, probe: probe}
	} else if _, isRetained := window.(RetainedCompositorCanvas); isRetained {
		cc = retainedProbeCanvas{Canvas: cc, probe: probe}
	}
	cc.Translate(float32(off.X), float32(off.Y))
	cc.Concat(m)
	s.applyContentTransform(cc)
	suppressed := s.coverBoxes()
	if len(layers) > 0 {
		suppressed = slices.Clone(suppressed)
		for _, l := range layers {
			suppressed = append(suppressed, l.box.Self())
		}
	}
	restore := s.Window().SuppressDraw(suppressed)
	if !boxChildHidden(s.Content) {
		s.Content.Draw(cc)
	}
	restore()
	return probe.used
}

// repaintDamage brings the tiles up to date with content damage dev
// (content device rectangles). Small damage is repainted into the tiles
// on this thread; tiles whose damaged share is large are re-rasterized
// instead. True means the content needed the window's GPU canvas.
func (s *ScrollView) repaintDamage(window Canvas, m Matrix, off image.Point, dev []image.Rectangle, layers []cacheLayer) bool {
	c := s.cache
	c.pic = nil // recorded before the damage
	share := map[image.Point]int{}
	for _, d := range dev {
		for _, k := range tilesIn(d) {
			if t := c.tiles[k]; t != nil {
				r := d.Intersect(tileRect(k))
				share[k] += r.Dx() * r.Dy()
			}
		}
	}
	inPlace := map[image.Point]bool{}
	for k, area := range share {
		t := c.tiles[k]
		switch {
		case t.pending != nil:
			// Its raster predates the damage: raster it again.
			t.pending.Cancel()
			t.pending = nil
			t.stale = t.img != nil
		case t.img == nil || t.stale:
		case area*2 > contentTileSize*contentTileSize:
			t.stale = true
		default:
			inPlace[k] = true
		}
	}
	if len(inPlace) == 0 {
		return false
	}
	// Each damage rectangle is drawn once, with a margin for filters
	// reading around it, and copied into the tiles it touches.
	for _, d := range dev {
		var ks []image.Point
		for _, k := range tilesIn(d) {
			if inPlace[k] {
				ks = append(ks, k)
			}
		}
		if len(ks) == 0 {
			continue
		}
		cc, rec := NewPictureCanvas(d.Inset(-filterPad))
		if s.drawContentInto(window, cc, m, off, layers) {
			return true
		}
		pic := rec.Finish()
		area := d
		if pic.FiltersNear(d, filterPad) {
			area = d.Inset(-filterPad)
		}
		scratch := image.NewRGBA(image.Rect(0, 0, area.Dx(), area.Dy()))
		pic.RasterizeInto(scratch, area.Min)
		if debugTiles {
			tileDbg.inPlace += len(ks)
		}
		for _, k := range ks {
			t := c.tiles[k]
			if !t.private {
				// The tile's pixels may be shared by a Picture a raster
				// worker is playing (the cache drawn into a recording):
				// write into a copy.
				cp := image.NewRGBA(t.img.Rect)
				for y := 0; y < cp.Rect.Dy(); y++ {
					copy(cp.Pix[y*cp.Stride:(y+1)*cp.Stride], t.img.Pix[y*t.img.Stride:])
				}
				t.img, t.private = cp, true
			}
			tr := tileRect(k)
			r := d.Intersect(tr)
			for y := r.Min.Y; y < r.Max.Y; y++ {
				src := scratch.Pix[scratch.PixOffset(r.Min.X-area.Min.X, y-area.Min.Y):]
				dst := t.img.Pix[t.img.PixOffset(r.Min.X-tr.Min.X, y-tr.Min.Y):]
				copy(dst[:r.Dx()*4], src[:r.Dx()*4])
			}
		}
	}
	return false
}

// invalidateAll marks every tile stale (kept on screen until re-rastered
// — invisible tiles are simply dropped) and the recording outdated.
func (c *scrollCache) invalidateAll() {
	c.pic = nil
	for k, t := range c.tiles {
		t.pending.Cancel()
		t.pending = nil
		if t.img == nil {
			delete(c.tiles, k)
			continue
		}
		t.stale = true
	}
}

// dropCache forgets the cache.
func (s *ScrollView) dropCache() {
	if c := s.cache; c != nil {
		for _, t := range c.tiles {
			t.pending.Cancel()
		}
	}
	s.cache = nil
	if debugMem {
		s.noteCacheMem()
	}
}

// wholeDeviceRect converts r to whole device pixels, or reports false
// when it does not sit on them.
func wholeDeviceRect(r Rect) (image.Rectangle, bool) {
	x0, y0 := math.Round(float64(r.X)), math.Round(float64(r.Y))
	x1, y1 := math.Round(float64(r.X+r.W)), math.Round(float64(r.Y+r.H))
	ok := math.Abs(float64(r.X)-x0) < 0.01 && math.Abs(float64(r.Y)-y0) < 0.01 &&
		math.Abs(float64(r.X+r.W)-x1) < 0.01 && math.Abs(float64(r.Y+r.H)-y1) < 0.01
	return image.Rect(int(x0), int(y0), int(x1), int(y1)), ok
}

// isWidgetHiddenOrCollapsed mirrors the draw guards a box's own Draw
// applies, for skipping the call entirely.
func isWidgetHiddenOrCollapsed(w Widget) bool {
	if c, ok := w.(interface{ Collapsed() bool }); ok && c.Collapsed() {
		return true
	}
	if h, ok := w.(VisibilityHider); ok && h.VisibilityHidden() {
		return true
	}
	return false
}

// gpuProbe records that content tried to queue a GPU / retained draw
// while rendering into the cache.
type gpuProbe struct{ used bool }

type gpuProbeCanvas struct {
	Canvas
	probe *gpuProbe
}

func (p gpuProbeCanvas) QueueGPUDraw(func(*GPUFrame)) { p.probe.used = true }
func (p gpuProbeCanvas) QueueRetainedGPUDraw(any, Rect, func(*GPUFrame)) {
	p.probe.used = true
}
func (p gpuProbeCanvas) QueueRetainedDraw(any, Rect, func(Compositor)) { p.probe.used = true }
func (p gpuProbeCanvas) ClipBounds() Rect                              { return p.Canvas.ClipBounds() }

type retainedProbeCanvas struct {
	Canvas
	probe *gpuProbe
}

func (p retainedProbeCanvas) QueueRetainedDraw(any, Rect, func(Compositor)) { p.probe.used = true }

// ContentCached reports whether the last paint drew the content through
// the content cache (diagnostics and tests).
func (s *ScrollView) ContentCached() bool {
	return s.cache != nil && s.cache.valid && !s.cache.disabled
}

// ContentTilesForTest reports the cache's tiles: how many, how many have
// pixels, and how many are still rasterizing.
func (s *ScrollView) ContentTilesForTest() (tiles, ready, pending int) {
	if s.cache == nil {
		return
	}
	for _, t := range s.cache.tiles {
		tiles++
		if t.img != nil && !t.stale {
			ready++
		}
		if t.pending != nil {
			pending++
		}
	}
	return
}
