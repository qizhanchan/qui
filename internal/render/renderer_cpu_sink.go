package render

import (
	"image"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Retained draws on the CPU renderer, for a page window (RFC 0005 P3).
//
// A page renders on its own thread with a CPURenderer, and its frames are
// shown by a host window whose renderer may composite. With a sink set, the
// renderer's canvas accepts compositor-safe overlay draws (an animated
// layer whose motion is a curve of time) and hands them to the sink, which
// commits them with the frame for the host to re-queue on its own canvas:
// a CSS marquee then keeps moving on the host's compositor thread while the
// page thread runs a long script. Draws that are not compositor-safe are
// refused (CompositorOnly), so their owners draw on the CPU.

// RetainedSink receives the retained draws of a CPURenderer's frames.
type RetainedSink interface {
	// RetainedBegin starts a frame repainting damage (empty: all of it).
	RetainedBegin(damage image.Rectangle)
	// QueueOverlay takes a compositor-safe overlay draw for this frame.
	QueueOverlay(key any, physBounds gfx.Rect, fn func(Compositor))
	// QueueUnder takes a thread-safe retained draw composited under the
	// 2D layer (CrossThreadRetainedCanvas).
	QueueUnder(key any, physBounds gfx.Rect, fn func(Compositor))
	// RetainedEnd ends the frame.
	RetainedEnd()
	// RetainedRedraw is a frame that only replays the retained draws.
	RetainedRedraw()
	// Forget releases what was cached under key (a draw, a texture).
	Forget(key any)
}

// sinkImageCanvas is the CPU canvas plus the overlay queues of a sink.
type sinkImageCanvas struct {
	imageCanvas
	sink RetainedSink
}

func (c sinkImageCanvas) QueueCompositorOverlayDraw(key any, physBounds gfx.Rect, fn func(Compositor)) {
	c.sink.QueueOverlay(key, physBounds, fn)
}

func (c sinkImageCanvas) QueueCrossThreadRetainedDraw(key any, physBounds gfx.Rect, fn func(Compositor)) {
	c.sink.QueueUnder(key, physBounds, fn)
}

// QueueRetainedOverlayDraw exists so overlay owners find the canvas; only
// compositor-safe draws may cross to the host, and CompositorOnly tells
// owners to queue those through QueueCompositorOverlayDraw.
func (c sinkImageCanvas) QueueRetainedOverlayDraw(key any, physBounds gfx.Rect, fn func(Compositor)) {
}

// CompositorOnly reports that only compositor-safe overlay draws are
// accepted: an owner that cannot make one draws on the CPU instead.
func (c sinkImageCanvas) CompositorOnly() bool { return true }

// SetRetainedSink routes this renderer's retained overlay draws to s (nil:
// none, the plain CPU canvas).
func (r *CPURenderer) SetRetainedSink(s RetainedSink) { r.sink = s }

// RedrawRetained implements RetainedRenderer: with a sink, a redraw-only
// frame is the sink's business; without one there is nothing to replay.
func (r *CPURenderer) RedrawRetained() bool {
	if r == nil || r.sink == nil {
		return false
	}
	r.sink.RetainedRedraw()
	return true
}

// ForgetRetainedDraw implements RetainedRenderer.
func (r *CPURenderer) ForgetRetainedDraw(key any) {
	if r != nil && r.sink != nil {
		r.sink.Forget(key)
	}
}
