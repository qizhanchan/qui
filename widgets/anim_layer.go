package widgets

import (
	"image"
	"math"
	"slices"
	"time"

	. "github.com/qizhanchan/qui"
)

// Animated layers: a box whose transform animates (a CSS marquee sliding
// a row of photos) is drawn over a ScrollView's content cache instead of
// into it, the way a browser composites it as its own layer:
//
//   - the content cache holds what is under the box, so a frame of the
//     animation copies that back instead of re-rendering it (the box's
//     damage is composite-only, see CompositeLayerActive);
//   - the box's in-flow children are kept as device-pixel bitmaps and the
//     box's translation is snapped to whole device pixels, so moving the
//     row is a copy of a few bitmaps, not a re-raster of every photo.
//
// The box must paint as if alone: nothing between it and the content may
// transform it or apply an effect, rectangular overflow clips are applied
// over it, and no later sibling (of it or an ancestor) may overlap the
// area it can paint in. Otherwise it stays an ordinary box.

// animLayers are the boxes marked with SetAnimatedLayer.
var animLayers = map[*Box]struct{}{}

// SetAnimatedLayer marks (or unmarks) b as an animated layer candidate:
// its transform is changing every frame.
func (b *Box) SetAnimatedLayer(on bool) {
	if on {
		animLayers[b] = struct{}{}
		return
	}
	delete(animLayers, b)
	b.layerSeq = 0
	b.releaseLayerRasters(b.Window())
}

// releaseLayerRasters drops b's child bitmaps, their GPU textures in win's
// renderer and their pending rasters.
func (b *Box) releaseLayerRasters(win *Window) {
	for _, cr := range b.childRasters {
		cr.pending.Cancel()
		if win != nil {
			win.ForgetRetainedDraw(cr) // its texture
		}
	}
	b.childRasters = nil
	b.gpuLayer = nil
}

// SetWindow releases what b kept as an animated layer when it leaves its
// window (an element removed while it animates), before the window is
// forgotten: its textures live in that window's renderer.
func (b *Box) SetWindow(w *Window) {
	if old := b.Window(); old != nil && old != w {
		if b.childRasters != nil || b.gpuLayer != nil {
			b.releaseLayerRasters(old)
		}
	}
	b.Container.SetWindow(w)
}

// CompositeLayerActive reports whether b was drawn as a layer over a
// content cache in the last painted frame: its movement then only needs
// recomposition (Window.InvalidateCompositeRect semantics).
func (b *Box) CompositeLayerActive() bool {
	if b.layerSeq == 0 {
		return false
	}
	win := b.Window()
	return win != nil && win.PaintSeq()-b.layerSeq <= 1
}

// cacheLayer is one animated layer chosen for a frame.
type cacheLayer struct {
	box       *Box
	clip      Rect   // content coordinates; empty = none
	toContent Matrix // the box's parent space → content space (a translation)
	path      []int
	levels    []layerLevel
}

// layerLevel is one step of a layer's path: child's later siblings under
// parent must not overlap area (child's coordinate space).
type layerLevel struct {
	parent, child Widget
	area          Rect
}

// cacheLayers lists the animated layers that can be drawn over the
// content cache, in tree order.
func (s *ScrollView) cacheLayers() []cacheLayer {
	if len(animLayers) == 0 || s.Content == nil {
		return nil
	}
	win := s.Window()
	var out []cacheLayer
	for b := range animLayers {
		if b.Window() != win || isWidgetHiddenOrCollapsed(b.Self()) {
			continue
		}
		if l, ok := s.layerFor(b); ok {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b cacheLayer) int { return slices.Compare(a.path, b.path) })
	// Later siblings may overlap a layer only when they are layers too
	// (drawn on top, in tree order).
	isLayer := map[Widget]bool{}
	for _, l := range out {
		isLayer[l.box.Self()] = true
	}
	kept := out[:0]
	for _, l := range out {
		if layerUncovered(l, isLayer) {
			kept = append(kept, l)
		}
	}
	return kept
}

// layerFor checks b's path to the content: nothing on it may transform
// (beyond a nested scroller's translation), apply an effect or clip other
// than to a rectangle. It collects the clip and the translation.
func (s *ScrollView) layerFor(b *Box) (cacheLayer, bool) {
	self := b.Self()
	if slices.Contains(s.vpBoxes, self) {
		return cacheLayer{}, false
	}
	l := cacheLayer{box: b, toContent: IdentityMatrix()}
	var clip Rect           // in the current level's child space
	area := b.PaintBounds() // what the box covers, same space
	child := self
	for cur := self.Parent(); cur != nil; child, cur = cur, cur.Parent() {
		if slices.Contains(s.vpBoxes, cur) {
			return cacheLayer{}, false // drawn with its fixed / sticky ancestor
		}
		idx := -1
		if cl, ok := cur.(ChildLister); ok {
			idx = slices.Index(cl.ChildList(), child)
		}
		l.path = append(l.path, idx)
		l.levels = append(l.levels, layerLevel{parent: cur, child: child, area: area})
		if cur != s.Content {
			if t, ok := cur.(ChildPaintTransformer); ok {
				m := t.ChildPaintTransform()
				if m.A != 1 || m.B != 0 || m.C != 0 || m.D != 1 {
					return cacheLayer{}, false
				}
				if !clip.IsEmpty() {
					clip = m.TransformRect(clip)
				}
				area = m.TransformRect(area)
				l.toContent = m.Concat(l.toContent)
			}
		}
		if t, ok := cur.(PaintTransformer); ok && t.PaintTransform() != IdentityMatrix() {
			return cacheLayer{}, false
		}
		if c, ok := cur.(PaintClipper); ok && !c.PaintClip().IsEmpty() {
			clip = intersectClip(clip, c.PaintClip())
		}
		if pb := asStackBox(cur); pb != nil {
			op := pb.Style().Opacity
			if (op > 0 && op < 1) || pb.Filter != nil || pb.ClipPath != nil || pb.Mask != nil || pb.BackdropFilter != nil {
				return cacheLayer{}, false
			}
			if pb.ClipChildren {
				st := pb.Style()
				if pb.BorderPath != nil || st.Radius > 0 || !st.Corners.IsZero() {
					return cacheLayer{}, false
				}
				clip = intersectClip(clip, pb.Bounds())
			}
		}
		if !clip.IsEmpty() {
			area = clip // the box moves; it can paint anywhere in its clip
		}
		if cur == s.Content {
			slices.Reverse(l.path)
			l.clip = clip
			return l, true
		}
	}
	return cacheLayer{}, false
}

func intersectClip(clip, r Rect) Rect {
	if clip.IsEmpty() {
		return r
	}
	return clip.Intersect(r)
}

// layerUncovered reports that nothing painted after l (other layers
// aside) overlaps the area l can paint in.
func layerUncovered(l cacheLayer, isLayer map[Widget]bool) bool {
	for _, lv := range l.levels {
		cl, ok := lv.parent.(ChildLister)
		if !ok {
			continue
		}
		kids := cl.ChildList()
		i := slices.Index(kids, lv.child)
		if i < 0 {
			return false
		}
		for _, sib := range kids[i+1:] {
			if isLayer[sib] || boxChildHidden(sib) {
				continue
			}
			if PaintBoundsOf(sib).Intersects(lv.area) {
				return false
			}
		}
	}
	return true
}

// drawLayers draws the frame's layers over the composited cache.
func (s *ScrollView) drawLayers(canvas Canvas, effClip Rect, layers []cacheLayer, seq uint64) {
	for _, l := range layers {
		depth := canvas.Save()
		canvas.ClipRect(effClip)
		s.applyContentTransform(canvas)
		if !l.clip.IsEmpty() {
			canvas.ClipRect(l.clip)
		}
		canvas.Concat(l.toContent)
		l.box.layerSeq = seq
		l.box.layerDrawing = true
		l.box.Self().Draw(canvas)
		l.box.layerDrawing = false
		canvas.RestoreTo(depth)
		l.box.evictChildRasters()
	}
}

// layerSignature identifies a frame's layer set.
func layerSignature(layers []cacheLayer) []*Box {
	out := make([]*Box, len(layers))
	for i, l := range layers {
		out[i] = l.box
	}
	return out
}

// --- per-child bitmaps -------------------------------------------------

type childRaster struct {
	img      *image.RGBA
	key      childRasterKey
	lastUsed time.Time
	noCache  bool   // the child draws through the GPU
	ver      uint64 // bumped on every new bitmap (its texture's version)
	// pending is the raster of the child's latest recording, on the
	// worker pool (RFC 0004 R2); img stays what was shown until it lands.
	pending    *RasterTask
	pendingKey childRasterKey
	// gpu: the bitmap is a renderer texture rasterized on the GPU (RFC 0004
	// R5) under this childRaster as key, gpuW×gpuH; img is nil then.
	gpu        bool
	gpuW, gpuH int
}

// bytes is what cr's bitmap holds, on the CPU or the GPU.
func (cr *childRaster) bytes() int {
	if cr.img != nil {
		return len(cr.img.Pix)
	}
	if cr.gpu {
		return cr.gpuW * cr.gpuH * 4
	}
	return 0
}

type childRasterKey struct {
	a, d   float32
	fx, fy float32 // sub-pixel phase of the child's device origin
	w, h   int
	pb     Rect
}

const childRasterIdle = 2 * time.Second

// snapLayerTranslation moves the canvas by less than a device pixel so
// the box's content lands on whole device pixels: its children's bitmaps
// then composite 1:1 wherever the animation has moved them.
func snapLayerTranslation(canvas Canvas) {
	m := canvas.CurrentMatrix()
	if m.B != 0 || m.C != 0 || m.A <= 0 || m.D <= 0 {
		return
	}
	dx := float32(math.Round(float64(m.TX))) - m.TX
	dy := float32(math.Round(float64(m.TY))) - m.TY
	if dx != 0 || dy != 0 {
		canvas.Translate(dx/m.A, dy/m.D)
	}
}

// drawChildRaster draws child from its bitmap. A missing, stale or
// changed bitmap is re-recorded as a Picture on this thread — cheap — and
// rasterized on the worker pool; until the raster lands the old bitmap
// keeps showing. Only a child with no bitmap at all that must show now
// waits for its raster (and its siblings', which run in parallel).
func (b *Box) drawChildRaster(canvas Canvas, child Widget) {
	m := canvas.CurrentMatrix()
	win := b.Window()
	if win == nil || m.B != 0 || m.C != 0 || m.A <= 0 || m.D <= 0 {
		child.Draw(canvas)
		return
	}
	pb := PaintBoundsOf(child)
	dr := m.TransformRect(pb)
	x0, y0 := math.Floor(float64(dr.X)), math.Floor(float64(dr.Y))
	x1, y1 := math.Ceil(float64(dr.X+dr.W)), math.Ceil(float64(dr.Y+dr.H))
	w, h := int(x1-x0), int(y1-y0)
	if w <= 0 || h <= 0 || w*h > 4<<20 {
		child.Draw(canvas)
		return
	}
	key := childRasterKey{a: m.A, d: m.D, fx: dr.X - float32(x0), fy: dr.Y - float32(y0), w: w, h: h, pb: pb}
	// While capturing for the GPU (anim_layer_gpu.go) canvas records;
	// children must see the frame's real canvas capabilities.
	probeBase := canvas
	if b.gpuCapture != nil {
		probeBase = b.gpuCapture.real
	}
	if b.childRasters == nil {
		b.childRasters = map[Widget]*childRaster{}
	}
	cr := b.childRasters[child]
	if cr != nil && cr.noCache {
		child.Draw(canvas)
		return
	}
	at := image.Pt(int(x0), int(y0))
	if cr == nil {
		cr = &childRaster{}
		b.childRasters[child] = cr
	}
	current := cr.key
	if cr.pending != nil {
		current = cr.pendingKey
	}
	// Drawn into the 2D frame, a GPU texture is no use: raster on the CPU.
	cpuNeeded := b.gpuCapture == nil && cr.gpu && cr.img == nil && cr.pending == nil
	if (cr.img == nil && !cr.gpu && cr.pending == nil) || cpuNeeded || current != key || childContentDamaged(win, child, pb) {
		var cc Canvas
		cc, rec := NewPictureCanvas(image.Rect(0, 0, w, h))
		probe := &gpuProbe{}
		if _, isGPU := probeBase.(GPUCanvas); isGPU {
			cc = gpuProbeCanvas{Canvas: cc, probe: probe}
		} else if _, isRetained := probeBase.(RetainedCompositorCanvas); isRetained {
			cc = retainedProbeCanvas{Canvas: cc, probe: probe}
		}
		cc.Translate(-float32(x0), -float32(y0))
		cc.Concat(m)
		tRec := time.Now()
		child.Draw(cc)
		pic := rec.Finish()
		if debugGPULayers {
			gpuDbg.records++
			gpuDbg.recordTime += time.Since(tRec)
		}
		if probe.used {
			cr.pending.Cancel()
			cr.noCache, cr.img, cr.pending = true, nil, nil
			child.Draw(canvas)
			return
		}
		cr.pending.Cancel()
		cr.pending = nil
		// Composited on the GPU, the child is rasterized there, now: a few
		// GPU commands instead of a CPU raster and an upload.
		if b.gpuCapture != nil {
			if gr := win.GPURasterizer(); gr != nil {
				t0 := time.Now()
				if gr.RasterizePicture(cr, cr.ver+1, pic, image.Rect(0, 0, w, h)) {
					cr.ver++
					cr.key, cr.img, cr.gpu, cr.gpuW, cr.gpuH = key, nil, true, w, h
					if debugGPULayers {
						gpuDbg.gpuRasters++
						gpuDbg.gpuRasterTime += time.Since(t0)
					}
					goto placed
				}
			}
		}
		cr.pendingKey = key
		var task *RasterTask
		task = ScheduleRaster(pic, image.Rect(0, 0, w, h), 1, func(*RasterTask) {
			win.PostJob(func() { b.adoptChildRaster(cr, task) })
		})
		cr.pending = task
		if task.Done() { // ran inline (not worker-safe, or raster threads off)
			b.adoptChildRaster(cr, task)
		}
	}
placed:
	cr.lastUsed = time.Now()
	if b.gpuCapture != nil {
		// A slot in paint order; an empty one is filled when its raster
		// lands (captureGPULayer waits for those that show now).
		b.gpuCapture.tiles = append(b.gpuCapture.tiles, cr.tile(at))
		return
	}
	if cr.img == nil {
		b.waitChildRaster(cr)
	}
	if cr.img == nil {
		return
	}
	iw, ih := cr.img.Rect.Dx(), cr.img.Rect.Dy()
	canvas.DrawImage(cr.img, Rect{
		X: (float32(x0) - m.TX) / m.A, Y: (float32(y0) - m.TY) / m.D,
		W: float32(iw) / m.A, H: float32(ih) / m.D,
	})
}

// waitChildRaster blocks until cr's pending raster is in (running it on
// this thread if no worker has started it).
func (b *Box) waitChildRaster(cr *childRaster) {
	t := cr.pending
	if t == nil {
		return
	}
	start := time.Now()
	t.Wait()
	if debugGPULayers {
		gpuDbg.waits++
		gpuDbg.waitTime += time.Since(start)
	}
	b.adoptChildRaster(cr, t)
}

// adoptChildRaster makes a finished raster cr's bitmap, on the UI thread.
// A GPU-composited layer shows it on the next GPU redraw — no repaint;
// a layer drawn in the 2D frame repaints the child's area.
func (b *Box) adoptChildRaster(cr *childRaster, t *RasterTask) {
	if cr.pending != t {
		return // superseded or dropped
	}
	cr.pending = nil
	img := t.Image()
	if img == nil {
		return
	}
	cr.img, cr.key, cr.gpu = img, cr.pendingKey, false
	cr.ver++
	if debugGPULayers {
		gpuDbg.rasters++
		gpuDbg.rasterTime += t.Duration()
	}
	win := b.Window()
	if win == nil {
		return
	}
	if gl := b.gpuLayer; gl != nil {
		for i := range gl.tiles {
			if gl.tiles[i].cr == cr {
				gl.tiles[i] = cr.tile(gl.tiles[i].at)
			}
		}
		gl.set.publish()
		win.RequestGPURedraw()
		return
	}
	if b.layerSeq != 0 {
		win.InvalidateCompositeRect(RectInWindow(b, PaintBoundsOf(b)))
	}
}

// childContentDamaged reports whether this frame's content damage touches
// child (a hover restyle, an image that finished loading).
func childContentDamaged(win *Window, child Widget, pb Rect) bool {
	rects, all := win.ContentDirty()
	if all {
		return true
	}
	if len(rects) == 0 {
		return false
	}
	r := RectInWindow(child, pb)
	for _, d := range rects {
		if d.Intersects(r) {
			return true
		}
	}
	return false
}

// childRasterBudget bounds the bitmaps one layer keeps. A layer cycles
// through its children (a marquee shows the same photos over and over),
// so keeping them all — like a compositor keeping a layer's tiles within
// its memory budget — makes every later pass free; only beyond the budget
// are the least recently drawn ones (idle for childRasterIdle) dropped.
const childRasterBudget = 64 << 20

// evictChildRasters drops the least recently used bitmaps while the layer
// is over its budget.
func (b *Box) evictChildRasters() {
	total := 0
	for _, cr := range b.childRasters {
		total += cr.bytes()
	}
	if total <= childRasterBudget {
		return
	}
	type aged struct {
		w  Widget
		cr *childRaster
	}
	list := make([]aged, 0, len(b.childRasters))
	for w, cr := range b.childRasters {
		list = append(list, aged{w, cr})
	}
	slices.SortFunc(list, func(x, y aged) int { return x.cr.lastUsed.Compare(y.cr.lastUsed) })
	now := time.Now()
	win := b.Window()
	for _, a := range list {
		if total <= childRasterBudget || now.Sub(a.cr.lastUsed) <= childRasterIdle {
			break
		}
		delete(b.childRasters, a.w)
		a.cr.pending.Cancel()
		total -= a.cr.bytes()
		if win != nil {
			win.ForgetRetainedDraw(a.cr) // its texture, if composited on the GPU
		}
	}
}

// ChildRasterCountForTest lists the children whose bitmaps b keeps.
func (b *Box) ChildRasterCountForTest() []Widget {
	out := make([]Widget, 0, len(b.childRasters))
	for w := range b.childRasters {
		out = append(out, w)
	}
	return out
}
