package widgets

import (
	"image"
	"math"

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

type scrollCache struct {
	img      *image.RGBA     // origin (0,0); pixel (x,y) is device (phys.Min + (x,y))
	phys     image.Rectangle // device rect of the viewport it covers
	scaleX   float32         // canvas scale it was rendered at
	scaleY   float32
	content  Widget
	valid    bool
	disabled bool   // content needed the window canvas (GPU draws)
	seq      uint64 // window paint the damage was last applied for
	seqSet   bool
}

// cacheWanted reports whether the content cache applies to this view now.
func (s *ScrollView) cacheWanted() bool {
	if !s.PixelScroll || !s.FixedViewport || len(s.vpBoxes) == 0 || s.Content == nil {
		return false
	}
	if s.cache != nil && s.cache.disabled && s.cache.content == s.Content {
		return false
	}
	for _, w := range s.vpBoxes {
		if !plainPathToContent(w, s.Content) {
			return false
		}
	}
	return true
}

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

// drawCached draws the content through the cache. effClip is the logical
// part of the viewport this frame paints (already clipped on canvas), vp
// the viewport. False means the caller draws the content directly.
func (s *ScrollView) drawCached(canvas Canvas, effClip, vp Rect) bool {
	if !s.cacheWanted() {
		s.cache = nil
		return false
	}
	win := s.Window()
	ca, ok := canvas.(ClipAware)
	if win == nil || !ok {
		return false
	}
	m := canvas.CurrentMatrix()
	if m.B != 0 || m.C != 0 || m.A <= 0 || m.D <= 0 {
		return false
	}
	pr := m.TransformRect(vp)
	phys, ok := wholeDeviceRect(pr)
	if !ok || phys.Empty() {
		return false
	}
	c := s.cache
	if c == nil || c.content != s.Content {
		c = &scrollCache{content: s.Content}
		s.cache = c
	}
	if c.img == nil || c.phys != phys || c.scaleX != m.A || c.scaleY != m.D {
		if c.img == nil || c.img.Rect.Dx() != phys.Dx() || c.img.Rect.Dy() != phys.Dy() {
			c.img = image.NewRGBA(image.Rect(0, 0, phys.Dx(), phys.Dy()))
		}
		c.phys, c.scaleX, c.scaleY, c.valid = phys, m.A, m.D, false
	}

	// Re-render what changed: everything when the cache is new or the
	// frame is a full repaint, else the content damage in the viewport.
	vpWin := RectInWindow(s, vp)
	off := Point{X: vpWin.X - vp.X, Y: vpWin.Y - vp.Y}
	rects, all := win.ContentDirty()
	var gpu bool
	// A frame paints its clip rectangles one by one; the damage is the
	// frame's, so render it once and only composite on the later calls.
	if seq := win.PaintSeq(); c.valid && c.seqSet && c.seq == seq {
		rects, all = nil, false
	} else {
		c.seq, c.seqSet = seq, true
	}
	if !c.valid || all {
		gpu = s.renderCache(canvas, m, vp)
		c.valid = !gpu
	} else {
		for _, r := range rects {
			lr := Rect{X: r.X - off.X, Y: r.Y - off.Y, W: r.W, H: r.H}.Intersect(vp)
			if lr.IsEmpty() {
				continue
			}
			if gpu = s.renderCache(canvas, m, lr); gpu {
				break
			}
		}
	}
	if gpu {
		c.disabled, c.valid, c.img = true, false, nil
		return false
	}

	// Composite the cache for this frame's clip, 1:1 in device pixels.
	cb := ca.ClipBounds()
	clip := image.Rect(int(math.Floor(float64(cb.X))), int(math.Floor(float64(cb.Y))),
		int(math.Ceil(float64(cb.X+cb.W))), int(math.Ceil(float64(cb.Y+cb.H)))).Intersect(phys)
	if !clip.Empty() {
		sub := c.img.SubImage(clip.Sub(phys.Min)).(*image.RGBA)
		logical := Rect{
			X: (float32(clip.Min.X) - m.TX) / m.A, Y: (float32(clip.Min.Y) - m.TY) / m.D,
			W: float32(clip.Dx()) / m.A, H: float32(clip.Dy()) / m.D,
		}
		canvas.DrawImage(sub, logical)
	}

	// The fixed / sticky boxes on top, as the content would paint them.
	depth := canvas.Save()
	canvas.ClipRect(effClip)
	s.applyContentTransform(canvas)
	for _, w := range s.vpBoxes {
		if !isWidgetHiddenOrCollapsed(w) {
			w.Draw(canvas)
		}
	}
	canvas.RestoreTo(depth)
	return true
}

// renderCache renders the content (without the viewport boxes) into the
// cache over the logical rect r. It reports true when the content asked
// for the window's GPU canvas, which the cache cannot stand in for.
func (s *ScrollView) renderCache(window Canvas, m Matrix, r Rect) (usedGPU bool) {
	c := s.cache
	var cc Canvas = NewImageCanvas(c.img)
	probe := &gpuProbe{}
	if _, isGPU := window.(GPUCanvas); isGPU {
		cc = gpuProbeCanvas{Canvas: cc, probe: probe}
	} else if _, isRetained := window.(RetainedCompositorCanvas); isRetained {
		cc = retainedProbeCanvas{Canvas: cc, probe: probe}
	}
	cc.Translate(-float32(c.phys.Min.X), -float32(c.phys.Min.Y))
	cc.Concat(m)
	cc.ClipRect(r)
	cc.ClearRect(r)
	s.applyContentTransform(cc)
	SuppressDraw(s.vpBoxes)
	if !boxChildHidden(s.Content) {
		s.Content.Draw(cc)
	}
	ResumeDraw()
	return probe.used
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
