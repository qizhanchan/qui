package qui

import (
	"image"
	"testing"
	"time"
)

func TestRetainedGLSetSurvivesUnrelatedDamage(t *testing.T) {
	var s retainedGLSet
	ran := 0
	s.put("video", Rect{X: 100, Y: 100, W: 200, H: 100}, func(GLState) { ran++ })
	// Frame 1: the owner queued it; it is already in the deferred queue,
	// so carryOver must not replay it a second time.
	if got := s.carryOver(image.Rect(100, 100, 300, 200), false); len(got) != 0 {
		t.Fatalf("seen entry replayed: %d", len(got))
	}
	s.endFrame()

	// Frame 2: a caret blinks far away — the draw survives and replays.
	replay := s.carryOver(image.Rect(0, 0, 10, 10), false)
	if len(replay) != 1 {
		t.Fatalf("want 1 replay for unrelated damage, got %d", len(replay))
	}
	replay[0](GLState{})
	if ran != 1 {
		t.Fatalf("replayed fn not the registered one")
	}
	s.endFrame()

	// Frame 3: a GL-only frame (no CPU damage at all) replays it too.
	if got := s.carryOver(image.Rectangle{}, true); len(got) != 1 {
		t.Fatalf("GL-only frame must replay, got %d", len(got))
	}
	s.endFrame()

	// Frame 4: the area is repainted but the owner did not re-queue —
	// it was unmounted / hidden / moved. Drop it.
	if got := s.carryOver(image.Rect(150, 150, 160, 160), false); len(got) != 0 {
		t.Fatalf("overlapping damage without re-queue must drop, got %d replays", len(got))
	}
	if s.len() != 0 {
		t.Fatalf("entry not dropped: %d left", s.len())
	}
}

func TestRetainedGLSetUnknownDamageDrops(t *testing.T) {
	var s retainedGLSet
	s.put(1, Rect{X: 0, Y: 0, W: 10, H: 10}, func(GLState) {})
	s.endFrame()
	// Empty damage without damageNone means "unknown" = full repaint.
	if got := s.carryOver(image.Rectangle{}, false); len(got) != 0 || s.len() != 0 {
		t.Fatalf("unknown damage must drop unseen entries (replays=%d left=%d)", len(got), s.len())
	}
}

func TestRetainedGLSetRequeueReplaces(t *testing.T) {
	var s retainedGLSet
	first, second := 0, 0
	s.put("k", Rect{W: 10, H: 10}, func(GLState) { first++ })
	s.endFrame()
	s.put("k", Rect{X: 50, W: 10, H: 10}, func(GLState) { second++ })
	if s.len() != 1 {
		t.Fatalf("re-queue must replace, have %d entries", s.len())
	}
	s.endFrame()
	for _, fn := range s.carryOver(image.Rect(0, 0, 10, 10), false) {
		fn(GLState{})
	}
	if first != 0 || second != 1 {
		t.Fatalf("want only the replacement to run (first=%d second=%d)", first, second)
	}
	s.forget("k")
	if s.len() != 0 {
		t.Fatalf("forget left %d entries", s.len())
	}
}

func TestTextureUploadRectDamageNone(t *testing.T) {
	r := &GLRenderer{}
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	r.texSrc = img
	r.damageNone = true
	if got := r.textureUploadRect(img); !got.Empty() {
		t.Fatalf("GL-only frame must upload nothing, got %v", got)
	}
	// A different image (resize) still uploads everything.
	if got := r.textureUploadRect(image.NewRGBA(img.Rect)); got != img.Rect {
		t.Fatalf("new image must upload fully, got %v", got)
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
