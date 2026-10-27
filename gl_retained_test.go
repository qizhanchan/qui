package qui

import (
	"image"
	"testing"
	"time"

	"github.com/qizhanchan/qui/internal/render"
)

func TestRetainedGLSetSurvivesUnrelatedDamage(t *testing.T) {
	var s render.RetainedGLSet
	ran := 0
	s.Put("video", Rect{X: 100, Y: 100, W: 200, H: 100}, func(GLState) { ran++ })
	// Frame 1: the owner queued it; it is already in the deferred queue,
	// so CarryOver must not replay it a second time.
	if got := s.CarryOver(image.Rect(100, 100, 300, 200), false); len(got) != 0 {
		t.Fatalf("seen entry replayed: %d", len(got))
	}
	s.EndFrame()

	// Frame 2: a caret blinks far away — the draw survives and replays.
	replay := s.CarryOver(image.Rect(0, 0, 10, 10), false)
	if len(replay) != 1 {
		t.Fatalf("want 1 replay for unrelated damage, got %d", len(replay))
	}
	replay[0](GLState{})
	if ran != 1 {
		t.Fatalf("replayed fn not the registered one")
	}
	s.EndFrame()

	// Frame 3: a GL-only frame (no CPU damage at all) replays it too.
	if got := s.CarryOver(image.Rectangle{}, true); len(got) != 1 {
		t.Fatalf("GL-only frame must replay, got %d", len(got))
	}
	s.EndFrame()

	// Frame 4: the area is repainted but the owner did not re-queue —
	// it was unmounted / hidden / moved. Drop it.
	if got := s.CarryOver(image.Rect(150, 150, 160, 160), false); len(got) != 0 {
		t.Fatalf("overlapping damage without re-queue must drop, got %d replays", len(got))
	}
	if s.Len() != 0 {
		t.Fatalf("entry not dropped: %d left", s.Len())
	}
}

func TestRetainedGLSetUnknownDamageDrops(t *testing.T) {
	var s render.RetainedGLSet
	s.Put(1, Rect{X: 0, Y: 0, W: 10, H: 10}, func(GLState) {})
	s.EndFrame()
	// Empty damage without damageNone means "unknown" = full repaint.
	if got := s.CarryOver(image.Rectangle{}, false); len(got) != 0 || s.Len() != 0 {
		t.Fatalf("unknown damage must drop unseen entries (replays=%d left=%d)", len(got), s.Len())
	}
}

func TestRetainedGLSetRequeueReplaces(t *testing.T) {
	var s render.RetainedGLSet
	first, second := 0, 0
	s.Put("k", Rect{W: 10, H: 10}, func(GLState) { first++ })
	s.EndFrame()
	s.Put("k", Rect{X: 50, W: 10, H: 10}, func(GLState) { second++ })
	if s.Len() != 1 {
		t.Fatalf("re-queue must replace, have %d entries", s.Len())
	}
	s.EndFrame()
	for _, fn := range s.CarryOver(image.Rect(0, 0, 10, 10), false) {
		fn(GLState{})
	}
	if first != 0 || second != 1 {
		t.Fatalf("want only the replacement to run (first=%d second=%d)", first, second)
	}
	s.Forget("k")
	if s.Len() != 0 {
		t.Fatalf("forget left %d entries", s.Len())
	}
}

func TestRequestGLRedrawKeepsLoopAwake(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	w.framePainted = false
	w.dirtyRegion = Rect{}
	w.RequestGLRedraw()
	if !w.glRedrawPending() {
		t.Fatal("request not recorded")
	}
	if d := w.nextFrameWait(time.Now()); d != frameInterval {
		t.Fatalf("pending GL redraw must schedule a frame, wait=%v", d)
	}
	// A second request before the frame coalesces.
	w.RequestGLRedraw()
	if !w.glRedraw.Swap(false) || w.glRedraw.Load() {
		t.Fatal("requests must coalesce into one pending flag")
	}
}
