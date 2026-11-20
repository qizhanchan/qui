package qui

import (
	"image"
	"math"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/render"
)

// Retained overlay draws across the page boundary (RFC 0005 P3).
//
// The page window's CPURenderer has the PageHost as its retained sink
// (renderer_cpu_sink.go) while the host can composite. Compositor-safe
// overlay draws — animated layers whose motion is a curve of time — are
// kept with the RetainedSet lifetime rules and committed with each frame;
// PageView re-queues them on the host window's canvas, offset to where it
// shows the page. The host's compositor thread then moves a CSS marquee
// while the page thread is busy, and while the host's UI thread is.

// PageRetainedDraw is one committed overlay draw. Bounds are page device
// pixels; Draw must only be run with the frame's Compositor.
type PageRetainedDraw struct {
	Key    any
	Bounds gfx.Rect
	Draw   func(Compositor)
}

// retainedState is the page thread's side of the sink.
type retainedState struct {
	live    map[any]PageRetainedDraw
	queued  map[any]PageRetainedDraw
	damage  image.Rectangle
	changed bool
	redraw  bool
	forget  []any
	// hostOK: the host canvas composites overlays (set by PageView).
	hostOK atomic.Bool
	// sinkOn: the renderer currently has the sink.
	sinkOn bool
}

// pageSink adapts PageHost to render.RetainedSink (page thread).
type pageSink struct{ h *PageHost }

func (s pageSink) RetainedBegin(damage image.Rectangle) {
	st := &s.h.retained
	st.queued = map[any]PageRetainedDraw{}
	st.damage = damage
}

func (s pageSink) QueueOverlay(key any, b gfx.Rect, fn func(render.Compositor)) {
	s.h.retained.queued[key] = PageRetainedDraw{Key: key, Bounds: b, Draw: fn}
}

func (s pageSink) RetainedEnd() {
	st := &s.h.retained
	for k, d := range st.live {
		if _, again := st.queued[k]; again {
			continue
		}
		pb := image.Rect(int(d.Bounds.X), int(d.Bounds.Y), int(d.Bounds.X+d.Bounds.W+1), int(d.Bounds.Y+d.Bounds.H+1))
		if st.damage.Empty() || st.damage.Overlaps(pb) {
			delete(st.live, k)
			st.changed = true
		}
	}
	if st.live == nil {
		st.live = map[any]PageRetainedDraw{}
	}
	for k, d := range st.queued {
		st.live[k] = d
		st.changed = true
	}
	st.queued = nil
}

func (s pageSink) RetainedRedraw() { s.h.retained.redraw = true }
func (s pageSink) Forget(key any) {
	st := &s.h.retained
	st.forget = append(st.forget, key)
	if _, ok := st.live[key]; ok {
		delete(st.live, key)
		st.changed = true
	}
}

// syncRetainedSink gives the page renderer the sink exactly while the host
// can composite overlays. Page thread, before a Step.
func (h *PageHost) syncRetainedSink() {
	w := h.win
	if w == nil {
		return
	}
	want := h.retained.hostOK.Load()
	if want == h.retained.sinkOn {
		return
	}
	cr, ok := w.renderer.(*CPURenderer)
	if !ok {
		return
	}
	h.retained.sinkOn = want
	if want {
		cr.SetRetainedSink(pageSink{h})
	} else {
		cr.SetRetainedSink(nil)
		h.retained.live = nil
		h.retained.changed = true
	}
	// Owners decide GPU or CPU at paint: repaint everything once.
	w.Invalidate()
}

// takeRetainedLocked moves the retained state into the commit. Page
// thread, frameMu held.
func (h *PageHost) takeRetainedLocked() {
	st := &h.retained
	if st.changed {
		draws := make([]PageRetainedDraw, 0, len(st.live))
		for _, d := range st.live {
			draws = append(draws, d)
		}
		h.draws = draws
		h.drawsSeq++
		st.changed = false
	}
	if st.redraw {
		h.redrawPending = true
		st.redraw = false
	}
	h.forgetPending = append(h.forgetPending, st.forget...)
	st.forget = nil
}

// SetHostCompositing tells the page whether the host canvas composites
// overlay draws (PageView sets it from what its canvas offers).
func (h *PageHost) SetHostCompositing(on bool) {
	if h.retained.hostOK.Swap(on) != on {
		h.thread.wake()
	}
}

// pageDrawKey keys a page's draw on the host renderer.
type pageDrawKey struct {
	view *PageView
	key  any
}

// offsetCompositor runs a page draw on the host's Compositor, moved to
// where the PageView shows the page. It deliberately hides the host's
// TextureCompositor: page texture keys mean nothing to the host renderer.
type offsetCompositor struct {
	c      Compositor
	dx, dy float32
}

func (o offsetCompositor) mv(r gfx.Rect) gfx.Rect { r.X += o.dx; r.Y += o.dy; return r }

func (o offsetCompositor) DrawPixelBuffer(pb unsafe.Pointer, src, dst gfx.Rect) bool {
	return o.c.DrawPixelBuffer(pb, src, o.mv(dst))
}

func (o offsetCompositor) DrawPixels(key any, version uint64, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst gfx.Rect) bool {
	return o.c.DrawPixels(key, version, pix, stride, w, h, bgra, src, o.mv(dst))
}

func (o offsetCompositor) DrawYUV420(key any, version uint64, planes [3]render.YUVPlane, m [9]float32, off, scale [3]float32, src, dst gfx.Rect) bool {
	return o.c.DrawYUV420(key, version, planes, m, off, scale, src, o.mv(dst))
}

func (o offsetCompositor) FramebufferSize() gfx.Size { return o.c.FramebufferSize() }

func (o offsetCompositor) FrameTime() time.Time {
	if ft, ok := o.c.(render.FrameTimer); ok {
		return ft.FrameTime()
	}
	return time.Now()
}

// adoptDraws takes a commit's overlay draws. UI thread.
func (v *PageView) adoptDraws(u PageFrameUpdate) {
	w := v.Window()
	if u.DrawsSeq != v.drawsSeq {
		for _, d := range v.draws {
			v.invalidateDevice(drawRect(d.Bounds))
		}
		v.draws, v.drawsSeq = u.Draws, u.DrawsSeq
		for _, d := range v.draws {
			v.invalidateDevice(drawRect(d.Bounds))
		}
	}
	if w == nil {
		return
	}
	for _, k := range u.Forget {
		w.ForgetRetainedDraw(k)
		w.ForgetRetainedDraw(pageDrawKey{view: v, key: k})
	}
	if u.Redraw && len(v.draws) > 0 {
		w.RequestGPURedraw()
	}
}

func drawRect(b gfx.Rect) image.Rectangle {
	return image.Rect(int(b.X), int(b.Y), int(b.X+b.W+1), int(b.Y+b.H+1))
}

// queueDraws re-queues the page's overlay draws on the host canvas, moved
// to where this view shows the page. A host canvas without compositor
// overlays tells the page to draw everything itself.
func (v *PageView) queueDraws(c Canvas) {
	cc, ok := c.(CompositorOverlayCanvas)
	v.host.SetHostCompositing(ok)
	if !ok || len(v.draws) == 0 {
		return
	}
	b := v.Bounds()
	m := c.CurrentMatrix()
	dx := float32(math.Round(float64(m.A*b.X + m.TX)))
	dy := float32(math.Round(float64(m.D*b.Y + m.TY)))
	for _, d := range v.draws {
		fn := d.Draw
		bounds := d.Bounds
		bounds.X += dx
		bounds.Y += dy
		cc.QueueCompositorOverlayDraw(pageDrawKey{view: v, key: d.Key}, bounds, func(c Compositor) {
			fn(offsetCompositor{c: c, dx: dx, dy: dy})
		})
	}
}
