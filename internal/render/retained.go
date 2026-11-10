package render

import (
	"image"
	"math"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Retained draws.
//
// A GPU-presenting renderer clears the whole drawable every frame and then
// composites the retained CPU image on top, so GPU content (a video frame,
// a 3D viewport) queued for one frame would only be on screen for the
// frame whose paint queued it. The CPU image does not have that problem:
// pixels outside a frame's dirty region simply survive. A widget showing
// GPU content would therefore have to repaint itself — and ask for a frame
// — every frame, merely to stay visible while a caret blinked elsewhere.
//
// A retained draw gives GPU content the same lifetime rule as CPU pixels:
//
//   - it re-runs on every later frame whose damage does NOT touch its
//     bounds, without the owning widget being drawn again;
//   - the first frame that repaints any part of its bounds without the
//     owner re-queuing it drops it — exactly the frame in which an
//     unmounted, hidden or moved widget's old pixels are overwritten.
//
// On top of that, Window.RequestGPURedraw produces a frame that re-runs the
// retained draws and re-composites the existing CPU image without
// rasterizing or uploading anything: the path a widget fed by another
// goroutine (decoded video at 60 fps) uses to put a new picture on screen
// at the cost of one texture blit.

// retainedEntry is one registration. seen marks a re-queue during the
// frame being produced.
type retainedEntry[F any] struct {
	key    any
	bounds image.Rectangle
	fn     F
	seen   bool
}

// RetainedSet is a renderer's book of retained draws, for any draw
// signature (compositor draws, WebGPU draws). Pure bookkeeping with no GPU in it,
// so the survival rule is unit-testable.
type RetainedSet[F any] struct {
	entries []retainedEntry[F]
}

// Put registers (or replaces) key's draw for the current frame.
func (s *RetainedSet[F]) Put(key any, bounds gfx.Rect, fn F) {
	b := rectToPixelBounds(bounds)
	for i := range s.entries {
		if s.entries[i].key == key {
			s.entries[i].bounds, s.entries[i].fn, s.entries[i].seen = b, fn, true
			return
		}
	}
	s.entries = append(s.entries, retainedEntry[F]{key: key, bounds: b, fn: fn, seen: true})
}

// CarryOver drops the entries a frame with the given damage invalidated
// and returns the draws that must be replayed because their owner was not
// drawn this frame. damageNone means the frame repainted nothing (a
// RequestGLRedraw frame); an empty damage otherwise means "unknown", which
// is treated as a full repaint.
//
// Entries that were re-queued this frame are kept but not returned: their
// fresh draw is already in the frame's deferred queue.
func (s *RetainedSet[F]) CarryOver(damage image.Rectangle, damageNone bool) []F {
	var replay []F
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
		s.entries[i] = retainedEntry[F]{}
	}
	s.entries = kept
	return replay
}

// EndFrame clears the per-frame seen marks.
func (s *RetainedSet[F]) EndFrame() {
	for i := range s.entries {
		s.entries[i].seen = false
	}
}

// Forget drops key's registration, if any.
func (s *RetainedSet[F]) Forget(key any) {
	for i := range s.entries {
		if s.entries[i].key == key {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return
		}
	}
}

func (s *RetainedSet[F]) Len() int { return len(s.entries) }

// rectToPixelBounds grows a float physical rect outward to whole pixels.
func rectToPixelBounds(r gfx.Rect) image.Rectangle {
	if r.W <= 0 || r.H <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(
		int(math.Floor(float64(r.X))), int(math.Floor(float64(r.Y))),
		int(math.Ceil(float64(r.X+r.W))), int(math.Ceil(float64(r.Y+r.H))),
	)
}
