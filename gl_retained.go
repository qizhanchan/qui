package qui

import (
	"image"
	"math"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// Retained GL draws.
//
// GLRenderer.End clears the whole framebuffer every frame and then
// composites the retained CPU image on top, so a raw-GL draw queued with
// QueueGLDraw is only on screen for the frame whose paint queued it. The
// CPU image does not have that problem: pixels outside a frame's dirty
// region simply survive. A widget that shows GL content (a video frame, a
// remote desktop) therefore had to repaint itself — and ask for a frame —
// every frame, merely to stay visible while a caret blinked elsewhere.
//
// A retained draw gives GL content the same lifetime rule as CPU pixels:
//
//   - it re-runs on every later frame whose damage does NOT touch its
//     bounds, without the owning widget being drawn again;
//   - the first frame that repaints any part of its bounds without the
//     owner re-queuing it drops it — exactly the frame in which an
//     unmounted, hidden or moved widget's old pixels are overwritten.
//
// On top of that, Window.RequestGLRedraw produces a frame that re-runs the
// retained draws and re-composites the existing CPU image without
// rasterizing or uploading anything: the path a widget fed by another
// goroutine (decoded video at 60 fps) uses to put a new picture on screen
// at the cost of one texture blit.

// RetainedGLCanvas is implemented by GPU canvases that support retained GL
// draws (the GLRenderer's canvas does). Type-assert the canvas passed to
// Draw, like GPUCanvas.
type RetainedGLCanvas interface {
	// QueueRetainedGLDraw queues fn like GPUCanvas.QueueGLDraw and keeps it
	// registered under key. physBounds is the area fn paints, in physical
	// framebuffer pixels with a top-left origin — typically
	// canvas.CurrentMatrix().TransformRect(localRect) intersected with
	// canvas.ClipBounds(). Re-queuing under the same key replaces the
	// previous registration.
	//
	// fn runs on the UI thread with the GL context current, between the
	// framebuffer clear and the 2D composite, on this frame and on every
	// later frame until a frame repaints part of physBounds without key
	// being re-queued. It should read the content it shows at call time
	// (not capture it), so a RequestGLRedraw frame picks up new content.
	QueueRetainedGLDraw(key any, physBounds Rect, fn func(GLState))
}

// retainedGLEntry is one registration. seen marks a re-queue during the
// frame being produced.
type retainedGLEntry struct {
	key    any
	bounds image.Rectangle
	fn     func(GLState)
	seen   bool
}

// retainedGLSet is the renderer's book of retained draws. Pure bookkeeping
// with no GL in it, so the survival rule is unit-testable.
type retainedGLSet struct {
	entries []retainedGLEntry
}

// put registers (or replaces) key's draw for the current frame.
func (s *retainedGLSet) put(key any, bounds Rect, fn func(GLState)) {
	b := rectToPixelBounds(bounds)
	for i := range s.entries {
		if s.entries[i].key == key {
			s.entries[i].bounds, s.entries[i].fn, s.entries[i].seen = b, fn, true
			return
		}
	}
	s.entries = append(s.entries, retainedGLEntry{key: key, bounds: b, fn: fn, seen: true})
}

// carryOver drops the entries a frame with the given damage invalidated
// and returns the draws that must be replayed because their owner was not
// drawn this frame. damageNone means the frame repainted nothing (a
// RequestGLRedraw frame); an empty damage otherwise means "unknown", which
// is treated as a full repaint.
//
// Entries that were re-queued this frame are kept but not returned: their
// fresh draw is already in the frame's deferred queue.
func (s *retainedGLSet) carryOver(damage image.Rectangle, damageNone bool) []func(GLState) {
	var replay []func(GLState)
	kept := s.entries[:0]
	for _, e := range s.entries {
		switch {
		case e.seen:
			kept = append(kept, e)
		case damageNone || (!damage.Empty() && !damage.Overlaps(e.bounds)):
			kept = append(kept, e)
			replay = append(replay, e.fn)
		}
	}
	for i := len(kept); i < len(s.entries); i++ {
		s.entries[i] = retainedGLEntry{}
	}
	s.entries = kept
	return replay
}

// endFrame clears the per-frame seen marks.
func (s *retainedGLSet) endFrame() {
	for i := range s.entries {
		s.entries[i].seen = false
	}
}

// forget drops key's registration, if any.
func (s *retainedGLSet) forget(key any) {
	for i := range s.entries {
		if s.entries[i].key == key {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return
		}
	}
}

func (s *retainedGLSet) len() int { return len(s.entries) }

// rectToPixelBounds grows a float physical rect outward to whole pixels.
func rectToPixelBounds(r Rect) image.Rectangle {
	if r.W <= 0 || r.H <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(
		int(math.Floor(float64(r.X))), int(math.Floor(float64(r.Y))),
		int(math.Ceil(float64(r.X+r.W))), int(math.Ceil(float64(r.Y+r.H))),
	)
}

// QueueRetainedGLDraw implements RetainedGLCanvas.
func (c gpuImageCanvas) QueueRetainedGLDraw(key any, physBounds Rect, fn func(GLState)) {
	if c.renderer == nil || fn == nil || key == nil {
		return
	}
	c.renderer.retained.put(key, physBounds, fn)
	c.renderer.deferredGL = append(c.renderer.deferredGL, fn)
}

// ForgetRetainedGLDraw drops a retained draw registered under key, for an
// owner that stops showing GL content without its area being repainted
// (it is otherwise dropped automatically). UI thread only.
func (r *GLRenderer) ForgetRetainedGLDraw(key any) {
	if r == nil {
		return
	}
	r.retained.forget(key)
}

// ForgetRetainedGLDraw drops key's retained draw from this window's
// renderer (see GLRenderer.ForgetRetainedGLDraw). UI thread.
func (w *Window) ForgetRetainedGLDraw(key any) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ForgetRetainedGLDraw")
	if glr, ok := w.renderer.(*GLRenderer); ok {
		glr.ForgetRetainedGLDraw(key)
	}
}

// redrawRetained produces a frame that replays the retained GL draws and
// re-composites the CPU image left by the last painted frame, without
// rasterizing or uploading any CPU pixels. Returns false when there is
// nothing to redraw (no frame painted yet, no retained draws).
func (r *GLRenderer) redrawRetained() bool {
	if r == nil || !r.initialized || r.img == nil || r.backend == nil || r.retained.len() == 0 {
		return false
	}
	lastActiveGLRenderer = r
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, int32(r.img.Rect.Dx()), int32(r.img.Rect.Dy()))
	r.damage = image.Rectangle{}
	r.damageNone = true
	r.composite()
	return true
}

// RequestGLRedraw asks for a frame that re-runs this window's retained GL
// draws (RetainedGLCanvas) without repainting any widget — the cheap way to
// show new content in a GL-backed widget. Safe to call from any goroutine;
// requests made before the frame runs coalesce into one.
func (w *Window) RequestGLRedraw() {
	if w == nil {
		return
	}
	if w.glRedraw.Swap(true) {
		return // already pending; the loop has been woken
	}
	w.wakeLoop()
}

// glRedrawPending reports whether RequestGLRedraw is waiting for a frame.
func (w *Window) glRedrawPending() bool { return w != nil && w.glRedraw.Load() }

// redrawGLOnly runs a RequestGLRedraw frame. Step calls it when nothing
// is dirty.
func (w *Window) redrawGLOnly() {
	glr, ok := w.renderer.(*GLRenderer)
	if !ok {
		return
	}
	glr.SetLogicalSize(w.lastSize)
	if !glr.redrawRetained() {
		return
	}
	w.present()
	w.framePainted = true
}
