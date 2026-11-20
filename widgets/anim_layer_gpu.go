package widgets

import (
	"fmt"
	"image"
	"math"
	"os"
	"slices"
	"sync/atomic"
	"time"
	"unsafe"

	. "github.com/qizhanchan/qui"
)

// GPU-composited animated layers.
//
// On a renderer that can composite over the 2D layer (WebGPU:
// RetainedOverlayCanvas), an animated layer is not drawn into the frame at
// all. Its children's device bitmaps (the same per-child rasters the CPU
// path blits, anim_layer.go) become textures, and a retained overlay draw
// places them at the box's CURRENT offset each time it runs. A frame of
// the animation is then Window.RequestGPURedraw: the 2D frame — the
// content cache under the layer — is untouched, nothing is rasterized or
// uploaded, and the GPU draws a few textured quads. This is what a
// browser's compositor does with a layer whose transform animates
// (Chromium cc: the layer's tiles stay, only its transform node changes).
//
// The overlay draw covers everything the 2D frame shows in the layer's
// area, so the GPU path is taken only when nothing painted after the layer
// overlaps that area: no fixed / sticky box of the ScrollView, no later
// sibling of the ScrollView or its ancestors, no window overlay. The box
// itself must paint nothing but its children (no background, border or
// shadow of its own), its transform must stay a translation at the scale
// it was captured with, and every child must have a bitmap. Otherwise the
// layer is drawn by the CPU path as before.
//
// Children are captured over an interest area of one visible-clip size
// beyond each side, so a moving row has its next children ready; when the
// row has moved past what was captured, the draw asks for one repaint,
// which captures again.

// gpuLayersOff disables the GPU path (QUI_GPU_LAYERS=0), to bisect.
var gpuLayersOff = os.Getenv("QUI_GPU_LAYERS") == "0"

// gpuLayerStale is how long a layer may go without its overlay draw
// running before its animation falls back to repainting (the draw was
// dropped by a repaint that did not draw the ScrollView).
const gpuLayerStale = 250 * time.Millisecond

// gpuLayerSet is the GPU-composited layers of one ScrollView for the
// frame that captured them. Its draw is the retained overlay draw.
type gpuLayerSet struct {
	s      *ScrollView
	win    *Window
	seq    uint64
	layers []*gpuLayer
	bounds image.Rectangle // physical: union of the layers' clips
	// Physical → window coordinates: win = (phys - devOrigin) / k + winOrigin.
	k                    float32
	devOrigin, winOrigin Point
	ranAt                atomic.Int64 // UnixNano of the last draw (any thread)
	recapture            atomic.Bool  // a repaint was asked for (draw)
	// commit is what draw reads: an immutable snapshot of the layers,
	// republished on the UI thread whenever their tiles or curves change,
	// so the compositor thread can draw while the UI thread mutates.
	commit atomic.Pointer[layerCommit]
	// covers are what paints after the layers over their area (a sticky
	// header, the browser's status bubble, a popup): physical pixels and
	// the same rects in window coordinates. The GPU draw leaves them out;
	// the 2D frame draws the layers there itself, under what covers them.
	covers    []image.Rectangle
	coversWin []Rect
}

type gpuLayer struct {
	box      *Box
	set      *gpuLayerSet
	parent   Matrix          // device matrix the box's own transform applies under
	m0       Matrix          // parent · box transform at capture, unsnapped
	clip     image.Rectangle // physical: where the layer shows
	interest image.Rectangle // physical: what the tiles cover, at the capture offset
	tiles    []gpuTile
}

type gpuTile struct {
	cr  *childRaster
	img *image.RGBA // nil: a GPU texture (gpu) or not rasterized yet
	ver uint64
	at  image.Point // device origin at capture
	// gpu: the bitmap is the renderer's texture under cr, w×h.
	gpu  bool
	w, h int
}

// tile is cr's bitmap placed at at.
func (cr *childRaster) tile(at image.Point) gpuTile {
	t := gpuTile{cr: cr, img: cr.img, ver: cr.ver, at: at}
	if cr.img == nil && cr.gpu {
		t.gpu, t.w, t.h = true, cr.gpuW, cr.gpuH
	}
	return t
}

// gpuCapture collects a layer box's child bitmaps instead of blitting them
// (drawChildRaster) while the box is drawn into a recording canvas; real
// is the frame's canvas, whose GPU capabilities the children see.
type gpuCapture struct {
	real  Canvas
	tiles []gpuTile
}

// GPULayerFollows reports whether b is composited on the GPU and its
// current transform can be shown there: moving it needs only
// Window.RequestGPURedraw, not a repaint.
func (b *Box) GPULayerFollows() bool {
	gl := b.gpuLayer
	if gl == nil || gl.set.s.gpuSet != gl.set || time.Since(time.Unix(0, gl.set.ranAt.Load())) > gpuLayerStale {
		return false
	}
	// The textures hold the children as captured: an effect of the box's
	// own (opacity, a filter) must be painted.
	if op := b.Style().Opacity; op > 0 && op < 1 || b.Filter != nil || b.ClipPath != nil || b.Mask != nil {
		return false
	}
	_, _, ok := gl.offset()
	return ok
}

// offset is how far, in device pixels, the box's tiles must move from
// where they were captured; false when its transform is no longer a plain
// move of it. The tiles were rasterized at the capture translation snapped
// to whole pixels; they are composited at the exact current translation,
// sub-pixel included — a compositor's layer moves smoothly, and snapping a
// slow marquee to whole pixels steps it 1, 1, 1, 2 pixels per frame, which
// shows as judder.
func (gl *gpuLayer) offset() (float32, float32, bool) {
	b := gl.box
	if b.gpuLayer != gl || b.Transform == nil && b.VisualTransform == nil {
		return 0, 0, false
	}
	m := gl.parent.Concat(b.paintTransformMatrix())
	if m.A != gl.m0.A || m.B != gl.m0.B || m.C != gl.m0.C || m.D != gl.m0.D {
		return 0, 0, false
	}
	dx := float64(m.TX) - math.Round(float64(gl.m0.TX))
	dy := float64(m.TY) - math.Round(float64(gl.m0.TY))
	return float32(dx), float32(dy), true
}

// drawGPULayers composites the frame's layers on the GPU when it can and
// reports whether it did; false leaves them to drawLayers.
func (s *ScrollView) drawGPULayers(canvas Canvas, effClip, vp Rect, layers []cacheLayer, seq uint64) bool {
	oc, ok := canvas.(RetainedOverlayCanvas)
	win := s.Window()
	if !ok || gpuLayersOff || win == nil || len(layers) == 0 {
		s.dropGPULayers()
		return false
	}
	if set := s.gpuSet; set != nil && set.seq == seq {
		// An earlier clip rectangle of this frame captured and queued it.
		s.drawCoveredLayers(canvas, effClip, vp, layers, set)
		return true
	}
	m := canvas.CurrentMatrix()
	devVP := m.TransformRect(vp)
	winVP := RectInWindow(s, vp)
	if devVP.W <= 0 || winVP.W <= 0 {
		s.dropGPULayers()
		return false
	}
	set := &gpuLayerSet{
		s: s, win: win, seq: seq, k: devVP.W / winVP.W,
		devOrigin: Point{X: devVP.X, Y: devVP.Y}, winOrigin: Point{X: winVP.X, Y: winVP.Y},
	}
	for _, l := range layers {
		gl, ok := s.captureGPULayer(canvas, vp, l, set)
		if !ok {
			gpuLayerFallback("capture")
			s.dropGPULayers()
			return false
		}
		set.layers = append(set.layers, gl)
		set.bounds = set.bounds.Union(gl.clip)
	}
	s.collectCovers(set)
	if old := s.gpuSet; old != nil {
		for _, gl := range old.layers {
			if gl.box.gpuLayer == gl {
				gl.box.gpuLayer = nil
			}
		}
	}
	s.gpuSet = set
	set.ranAt.Store(time.Now().UnixNano())
	if debugGPULayers {
		gpuDbg.captures++
	}
	for _, gl := range set.layers {
		gl.box.gpuLayer = gl
		gl.box.layerSeq = seq
	}
	b := set.bounds
	area := Rect{X: float32(b.Min.X), Y: float32(b.Min.Y), W: float32(b.Dx()), H: float32(b.Dy())}
	// With every layer's motion a curve of time, the draw reads nothing
	// but its commit: the compositor thread may run it too.
	if cc, ok := canvas.(CompositorOverlayCanvas); ok && set.publish() {
		cc.QueueCompositorOverlayDraw(s, area, set.draw)
	} else {
		oc.QueueRetainedOverlayDraw(s, area, set.draw)
	}
	s.drawCoveredLayers(canvas, effClip, vp, layers, set)
	return true
}

// drawCoveredLayers draws the layers into the 2D frame where something
// covers them (see gpuLayerSet.covers), clipped to this paint's clip; the
// covering widgets then paint over them in their usual order.
func (s *ScrollView) drawCoveredLayers(canvas Canvas, effClip, vp Rect, layers []cacheLayer, set *gpuLayerSet) {
	if len(set.coversWin) == 0 {
		return
	}
	winVP := RectInWindow(s, vp)
	off := Point{X: winVP.X - vp.X, Y: winVP.Y - vp.Y}
	for _, c := range set.coversWin {
		local := Rect{X: c.X - off.X, Y: c.Y - off.Y, W: c.W, H: c.H}.Intersect(effClip)
		if local.IsEmpty() {
			continue
		}
		s.drawLayers(canvas, local, layers, set.seq)
	}
}

// GPULayerCovered lists, in window coordinates, the parts of b's layer
// area that something paints over: an animation frame repaints them in
// the 2D frame (a small CPU repaint) while the GPU moves the rest.
func (b *Box) GPULayerCovered() []Rect {
	if gl := b.gpuLayer; gl != nil {
		return gl.set.coversWin
	}
	return nil
}

// captureGPULayer draws l's box into a recording canvas at its place in
// the frame, collecting its children's bitmaps; false when the box paints
// anything else or a child cannot be a bitmap.
func (s *ScrollView) captureGPULayer(canvas Canvas, vp Rect, l cacheLayer, set *gpuLayerSet) (*gpuLayer, bool) {
	b := l.box
	if b.Transform == nil && b.VisualTransform == nil {
		return nil, false
	}
	// Where the layer shows: the WHOLE viewport, not this paint's clip
	// rectangle — the overlay draw stands for the layer on every later
	// frame, including those that repaint nothing (a scroll's exposed
	// strip would otherwise become the only place it shows).
	geo := &RecordingCanvas{}
	geo.Concat(canvas.CurrentMatrix())
	geo.ClipRect(vp)
	s.applyContentTransform(geo)
	if !l.clip.IsEmpty() {
		geo.ClipRect(l.clip)
	}
	geo.Concat(l.toContent)
	parent := geo.CurrentMatrix()
	vis := geo.ClipBounds()

	m0 := parent.Concat(b.paintTransformMatrix())
	if parent.B != 0 || parent.C != 0 || m0.B != 0 || m0.C != 0 || m0.A <= 0 || m0.D <= 0 {
		return nil, false
	}
	gl := &gpuLayer{box: b, set: set, parent: parent, m0: m0}
	gl.clip = image.Rect(int(math.Floor(float64(vis.X))), int(math.Floor(float64(vis.Y))),
		int(math.Ceil(float64(vis.X+vis.W))), int(math.Ceil(float64(vis.Y+vis.H))))
	if gl.clip.Empty() {
		gl.clip = image.Rectangle{}
		gl.interest = image.Rectangle{}
		return gl, true
	}
	grow := gl.clip.Dx()
	if gl.clip.Dy() > grow {
		grow = gl.clip.Dy()
	}
	gl.interest = gl.clip.Inset(-grow)

	rc := &RecordingCanvas{}
	rc.ClipRect(Rect{X: float32(gl.interest.Min.X), Y: float32(gl.interest.Min.Y),
		W: float32(gl.interest.Dx()), H: float32(gl.interest.Dy())})
	rc.Concat(parent)
	capt := &gpuCapture{real: canvas}
	b.gpuCapture = capt
	b.layerDrawing = true
	b.Self().Draw(rc)
	b.layerDrawing = false
	b.gpuCapture = nil
	if recordedAnything(rc) {
		gpuLayerFallback(fmt.Sprintf("layer box paints itself: fills=%d rounds=%d texts=%d images=%d vectors=%d shadows=%d layers=%d paths=%d",
			len(rc.Fills), len(rc.Rounds), len(rc.Texts), len(rc.Images), len(rc.Vectors), len(rc.Shadows), len(rc.Layers), len(rc.Paths)))
		return nil, false // the box paints something of its own, or a child drew directly
	}
	// Children that show now and have no bitmap yet: wait for their
	// rasters (all scheduled above, so they ran in parallel). The others
	// fill their slots when they land (adoptChildRaster).
	for i := range capt.tiles {
		t := &capt.tiles[i]
		if t.img != nil || t.cr.pending == nil {
			continue
		}
		k := t.cr.pendingKey
		if image.Rect(t.at.X, t.at.Y, t.at.X+k.w, t.at.Y+k.h).Overlaps(gl.clip) {
			b.waitChildRaster(t.cr)
			t.img, t.ver = t.cr.img, t.cr.ver
		}
	}
	gl.tiles = capt.tiles
	b.evictChildRasters()
	return gl, true
}

// recordedAnything reports whether anything was drawn on rc.
func recordedAnything(rc *RecordingCanvas) bool {
	return len(rc.Clears)+len(rc.ClearRects)+len(rc.Fills)+len(rc.Rounds)+len(rc.Strokes)+
		len(rc.Texts)+len(rc.Images)+len(rc.Vectors)+len(rc.Lines)+len(rc.Polylines)+
		len(rc.Shadows)+len(rc.Layers)+len(rc.Paths) > 0
}

// collectCovers records what paints after the layers over their area:
// the ScrollView's fixed / sticky boxes, later siblings of the ScrollView
// or of any ancestor, window overlays.
func (s *ScrollView) collectCovers(set *gpuLayerSet) {
	if set.bounds.Empty() {
		return
	}
	area := set.toWindow(set.bounds)
	add := func(r Rect, what string) {
		r = r.Intersect(area)
		if r.IsEmpty() {
			return
		}
		p := set.toPhys(r)
		set.covers = append(set.covers, p)
		set.coversWin = append(set.coversWin, set.toWindow(p))
		gpuLayerFallback("covered in part by " + what)
	}
	win := s.Window()
	for _, r := range win.OverlaysIntersecting(area) {
		add(r, "a window overlay")
	}
	for _, w := range s.vpBoxes {
		if !isWidgetHiddenOrCollapsed(w) {
			add(RectInWindow(w, PaintBoundsOf(w)), fmt.Sprintf("fixed/sticky %T", w))
		}
	}
	child := Widget(s.Self())
	for cur := child.Parent(); cur != nil; child, cur = cur, cur.Parent() {
		cl, ok := cur.(ChildLister)
		if !ok {
			continue
		}
		kids := ChildrenInPaintOrder(cl.ChildList())
		i := -1
		for j, k := range kids {
			if k == child {
				i = j
				break
			}
		}
		for _, sib := range kids[i+1:] {
			if i < 0 || boxChildHidden(sib) {
				continue
			}
			add(RectInWindow(sib, PaintBoundsOf(sib)), fmt.Sprintf("later sibling %T", sib))
		}
	}
}

// toPhys maps a window rect to physical pixels, grown to whole pixels.
func (set *gpuLayerSet) toPhys(r Rect) image.Rectangle {
	x0 := (r.X-set.winOrigin.X)*set.k + set.devOrigin.X
	y0 := (r.Y-set.winOrigin.Y)*set.k + set.devOrigin.Y
	return image.Rect(int(math.Floor(float64(x0))), int(math.Floor(float64(y0))),
		int(math.Ceil(float64(x0+r.W*set.k))), int(math.Ceil(float64(y0+r.H*set.k))))
}

// toWindow maps a physical rect to window coordinates.
func (set *gpuLayerSet) toWindow(r image.Rectangle) Rect {
	return Rect{
		X: (float32(r.Min.X)-set.devOrigin.X)/set.k + set.winOrigin.X,
		Y: (float32(r.Min.Y)-set.devOrigin.Y)/set.k + set.winOrigin.Y,
		W: float32(r.Dx()) / set.k, H: float32(r.Dy()) / set.k,
	}
}

// draw is the retained overlay draw: every tile at the box's current
// offset, cut to the layer's clip.
func (set *gpuLayerSet) draw(c Compositor) {
	cm := set.commit.Load()
	if cm == nil {
		return
	}
	set.ranAt.Store(time.Now().UnixNano())
	var t time.Time
	if ft, ok := c.(FrameTimer); ok {
		t = ft.FrameTime()
	} else {
		t = time.Now()
	}
	stale := false
	onUI := set.win.OnUIThread()
	for i := range cm.layers {
		cl := &cm.layers[i]
		gl := cl.gl
		if gl.clip.Empty() {
			continue
		}
		dx, dy, ok := cl.offset(t, onUI)
		if !ok {
			stale = true
			continue
		}
		if debugGPULayers && i == 0 {
			noteGPULayerDraw(float32(math.Round(float64(gl.m0.TX))) + dx)
		}
		moved := gl.interest.Add(image.Pt(int(math.Round(float64(dx))), int(math.Round(float64(dy)))))
		if !gl.clip.In(moved.Inset(1)) {
			stale = true // moved past the captured children
		}
		clip := Rect{X: float32(gl.clip.Min.X), Y: float32(gl.clip.Min.Y), W: float32(gl.clip.Dx()), H: float32(gl.clip.Dy())}
		tc, _ := c.(TextureCompositor)
		for _, tl := range cl.tiles {
			w, h := tl.w, tl.h
			switch {
			case tl.img != nil:
				w, h = tl.img.Rect.Dx(), tl.img.Rect.Dy()
			case !tl.gpu || tc == nil:
				continue // its raster has not landed yet
			}
			r := Rect{X: float32(tl.at.X) + dx, Y: float32(tl.at.Y) + dy, W: float32(w), H: float32(h)}
			vis := r.Intersect(clip)
			if vis.W <= 0 || vis.H <= 0 || tl.img != nil && len(tl.img.Pix) == 0 {
				continue
			}
			for _, part := range subtractRects(vis, set.covers) {
				src := Rect{X: part.X - r.X, Y: part.Y - r.Y, W: part.W, H: part.H}
				if tl.img == nil {
					tc.DrawTexture(tl.cr, tl.ver, src, part)
					continue
				}
				c.DrawPixels(tl.cr, tl.ver, unsafe.Pointer(&tl.img.Pix[0]), tl.img.Stride, w, h, false, src, part)
			}
		}
	}
	if stale && !set.recapture.Load() {
		set.requestRepaint()
	}
}

// layerCommit is a published snapshot of a gpuLayerSet's layers.
type layerCommit struct {
	layers []committedLayer
}

type committedLayer struct {
	gl    *gpuLayer // its capture-time fields (clip, interest, parent, m0) never change
	tiles []gpuTile
	// curve samples the box's transform at a time (Box.SetCompositorCurve);
	// bounds and visual are what it is resolved against.
	curve  func(time.Time) (*BoxTransform, bool)
	bounds Rect
	visual Matrix
	// frozen is the box's offset at publish, for a layer without a curve
	// drawn off the UI thread (between its curve being cleared and the
	// repaint that hands it back to the UI thread): never live state.
	fdx, fdy float32
	fok      bool
}

// publish snapshots the layers for draw; true when every layer moves by a
// curve of time, so the draw is compositor-safe. UI thread.
func (set *gpuLayerSet) publish() bool {
	cm := &layerCommit{layers: make([]committedLayer, len(set.layers))}
	safe := len(set.layers) > 0
	for i, gl := range set.layers {
		b := gl.box
		cl := committedLayer{gl: gl, tiles: slices.Clone(gl.tiles), curve: b.compCurve, bounds: b.Bounds(), visual: IdentityMatrix()}
		if b.VisualTransform != nil {
			cl.visual = boxTransformMatrix(cl.bounds, b.VisualTransform)
		}
		if cl.curve == nil {
			safe = false
			cl.fdx, cl.fdy, cl.fok = gl.offset()
		}
		cm.layers[i] = cl
	}
	set.commit.Store(cm)
	// A covered layer needs the 2D frame repainted under its covers every
	// frame: the UI thread drives it.
	return safe && len(set.covers) == 0
}

// offset is how far the layer's tiles move at time t: from the curve when
// it has one, else from the box as it is now (UI thread only).
func (cl *committedLayer) offset(t time.Time, onUI bool) (float32, float32, bool) {
	gl := cl.gl
	if cl.curve == nil {
		if !onUI {
			return cl.fdx, cl.fdy, cl.fok
		}
		return gl.offset()
	}
	bt, ok := cl.curve(t)
	if !ok {
		return 0, 0, false
	}
	m := gl.parent.Concat(cl.visual)
	if bt != nil {
		m = m.Concat(boxTransformMatrix(cl.bounds, bt))
	}
	if m.A != gl.m0.A || m.B != gl.m0.B || m.C != gl.m0.C || m.D != gl.m0.D {
		return 0, 0, false
	}
	dx := float64(m.TX) - math.Round(float64(gl.m0.TX))
	dy := float64(m.TY) - math.Round(float64(gl.m0.TY))
	return float32(dx), float32(dy), true
}

// SetCompositorCurve gives the box's transform as a pure function of time
// (a running CSS animation's keyframes and timing, captured), or nil. With
// one, a GPU-composited layer of the box can be moved by the compositor
// thread while the UI thread is busy (RFC 0004 R4). fn must not read
// mutable state; it may be called on any thread.
func (b *Box) SetCompositorCurve(fn func(time.Time) (*BoxTransform, bool)) {
	had := b.compCurve != nil
	b.compCurve = fn
	if gl := b.gpuLayer; gl != nil && gl.set.s.gpuSet == gl.set {
		gl.set.publish()
		if had && fn == nil {
			// Its draw is queued as compositor-safe: capture it again so
			// the UI thread takes the layer back.
			gl.set.requestRepaint()
		}
	}
}

// requestRepaint asks for a frame that draws the ScrollView over the
// layers' area, which captures them again (or draws them on the CPU).
func (set *gpuLayerSet) requestRepaint() {
	win := set.win // not s.Window(): this may run on the compositor thread
	if win == nil {
		return
	}
	set.recapture.Store(true)
	area := set.toWindow(set.bounds)
	win.PostJob(func() { win.InvalidateCompositeRect(area) })
}

// dropGPULayers stops compositing the layers on the GPU. The overlay draw
// is withdrawn at once, and the layers' area repainted on the next frame:
// the 2D frame under it holds only what is behind the layers.
func (s *ScrollView) dropGPULayers() {
	set := s.gpuSet
	if set == nil {
		return
	}
	s.gpuSet = nil
	for _, gl := range set.layers {
		if gl.box.gpuLayer == gl {
			gl.box.gpuLayer = nil
		}
	}
	if win := s.Window(); win != nil {
		win.ForgetRetainedDraw(s)
		if !set.bounds.Empty() {
			set.requestRepaint()
		}
	}
}

// subtractRects is r minus every hole, as disjoint rectangles.
func subtractRects(r Rect, holes []image.Rectangle) []Rect {
	parts := []Rect{r}
	for _, h := range holes {
		hr := Rect{X: float32(h.Min.X), Y: float32(h.Min.Y), W: float32(h.Dx()), H: float32(h.Dy())}
		var next []Rect
		for _, p := range parts {
			in := p.Intersect(hr)
			if in.IsEmpty() {
				next = append(next, p)
				continue
			}
			// Above, below, then left and right of the hole within p.
			if in.Y > p.Y {
				next = append(next, Rect{X: p.X, Y: p.Y, W: p.W, H: in.Y - p.Y})
			}
			if b := in.Y + in.H; b < p.Y+p.H {
				next = append(next, Rect{X: p.X, Y: b, W: p.W, H: p.Y + p.H - b})
			}
			if in.X > p.X {
				next = append(next, Rect{X: p.X, Y: in.Y, W: in.X - p.X, H: in.H})
			}
			if rgt := in.X + in.W; rgt < p.X+p.W {
				next = append(next, Rect{X: rgt, Y: in.Y, W: p.X + p.W - rgt, H: in.H})
			}
		}
		parts = next
	}
	return parts
}

// GPULayerPendingForTest counts b's GPU layer tiles still waiting for
// their raster.
func (b *Box) GPULayerPendingForTest() int {
	gl := b.gpuLayer
	if gl == nil {
		return -1
	}
	n := 0
	for _, t := range gl.tiles {
		if (t.img == nil && !t.gpu) || t.cr.pending != nil {
			n++
		}
	}
	return n
}

// GPULayerOffsetsForTest reports b's GPU layer offset as the UI thread
// sees it (the box's current transform) and as its committed curve gives
// it at t.
func (b *Box) GPULayerOffsetsForTest(t time.Time) (live, curve [2]float32, ok bool) {
	gl := b.gpuLayer
	if gl == nil {
		return
	}
	cm := gl.set.commit.Load()
	if cm == nil {
		return
	}
	for i := range cm.layers {
		if cm.layers[i].gl == gl && cm.layers[i].curve != nil {
			lx, ly, ok1 := gl.offset()
			cx, cy, ok2 := cm.layers[i].offset(t, true)
			return [2]float32{lx, ly}, [2]float32{cx, cy}, ok1 && ok2
		}
	}
	return
}
