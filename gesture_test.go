package qui

import "testing"

// gestureSpy records every gesture it receives and can be told to
// consume them (the way a document view that implements zoom would).
type gestureSpy struct {
	BaseWidget
	name     string
	got      []GestureEvent
	scrolls  []MouseEvent
	consume  bool
	consumeK EventType
}

func newGestureSpy(name string, r Rect) *gestureSpy {
	s := &gestureSpy{BaseWidget: NewBaseWidget(), name: name}
	s.rect = r
	s.SetSelf(s)
	return s
}

func (s *gestureSpy) Handle(e Event) bool {
	switch ev := e.(type) {
	case GestureEvent:
		if ev.Phase() != PhaseTarget {
			return false // only count the target phase, not capture/bubble
		}
		s.got = append(s.got, ev)
		return s.consume && (s.consumeK == 0 || s.consumeK == ev.Type())
	case MouseEvent:
		if ev.Type() == EventScroll && ev.Phase() == PhaseTarget {
			s.scrolls = append(s.scrolls, ev)
		}
	}
	return false
}

func (s *gestureSpy) HitTest(p Point) Widget {
	if s.rect.Contains(p) {
		return s
	}
	return nil
}

func (s *gestureSpy) Measure(Size) Size { return Size{W: s.rect.W, H: s.rect.H} }

func TestIngestGestureAccumulatesCumulativeScale(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	// A real trackpad pinch: Began then a run of increments.
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseBegan, 0, 0, 0)
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseChanged, 0.5, 0, 0)
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseChanged, 0.5, 0, 0)
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseEnded, 0, 0, 0)

	if len(spy.got) != 4 {
		t.Fatalf("expected 4 gesture events, got %d", len(spy.got))
	}
	if spy.got[0].Scale != 1 {
		t.Errorf("Began should reset cumulative scale to 1, got %v", spy.got[0].Scale)
	}
	// 1 * 1.5 * 1.5 = 2.25 — increments compound, they don't add.
	if got := spy.got[2].Scale; got < 2.24 || got > 2.26 {
		t.Errorf("cumulative scale after two 0.5 increments = %v, want ~2.25", got)
	}
	if spy.got[2].DScale != 0.5 {
		t.Errorf("DScale should stay the per-event increment, got %v", spy.got[2].DScale)
	}
}

func TestIngestGestureBeganResetsPreviousGesture(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	w.IngestGestureForTest(EventGesturePinch, GesturePhaseBegan, 0, 0, 0)
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseChanged, 1, 0, 0) // ×2
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseEnded, 0, 0, 0)
	// Second, independent gesture must start from 1 again, not from 2.
	w.IngestGestureForTest(EventGesturePinch, GesturePhaseBegan, 0, 0, 0)

	last := spy.got[len(spy.got)-1]
	if last.Scale != 1 {
		t.Errorf("a fresh Began must reset cumulative scale to 1, got %v", last.Scale)
	}
}

func TestGestureCaptureKeepsTargetAfterBegan(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	doc := newGestureSpy("doc", Rect{X: 0, Y: 0, W: 400, H: 100})
	bar := newGestureSpy("bar", Rect{X: 0, Y: 100, W: 400, H: 200})
	root := NewContainer(&AbsoluteLayout{}, doc, bar)
	root.SetSelf(root)
	// Set bounds directly rather than via Layout: Container.HitTest gates
	// on its own bounds, but a layout pass would overwrite the spies'
	// pre-set rects that their HitTest depends on.
	root.rect = Rect{W: 400, H: 300}
	w.SetRoot(root)

	// Began inside doc.
	w.DispatchTestEvent(NewGestureEvent(EventGesturePinch, 10, 10, GesturePhaseBegan, 0, 0, 0))
	// Fingers drift over bar — capture must keep routing to doc, exactly
	// like mouse capture does between MouseDown and MouseUp.
	w.DispatchTestEvent(NewGestureEvent(EventGesturePinch, 10, 200, GesturePhaseChanged, 0.2, 0, 0))
	w.DispatchTestEvent(NewGestureEvent(EventGesturePinch, 10, 200, GesturePhaseEnded, 0, 0, 0))

	if len(doc.got) != 3 {
		t.Errorf("captured target should get all 3 events, got %d", len(doc.got))
	}
	if len(bar.got) != 0 {
		t.Errorf("non-capturing widget should get none, got %d", len(bar.got))
	}

	// Capture released: a new Began over bar targets bar.
	w.DispatchTestEvent(NewGestureEvent(EventGesturePinch, 10, 200, GesturePhaseBegan, 0, 0, 0))
	if len(bar.got) != 1 {
		t.Errorf("after release, a fresh Began should target bar; got %d", len(bar.got))
	}
}

func TestCtrlWheelSynthesizesPinch(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	spy.consume = true
	w.SetRoot(spy)

	w.IngestScrollForTest(10, 10, 0, 3, ModControl, GesturePhaseChanged)

	if len(spy.got) != 1 {
		t.Fatalf("Ctrl+wheel should synthesize one pinch, got %d", len(spy.got))
	}
	ev := spy.got[0]
	if ev.Type() != EventGesturePinch {
		t.Errorf("synthesized event kind = %v, want EventGesturePinch", ev.Type())
	}
	if !ev.Synthetic {
		t.Error("wheel-derived pinch must be marked Synthetic")
	}
	if ev.DScale <= 0 {
		t.Errorf("scrolling up should zoom in (DScale > 0), got %v", ev.DScale)
	}
	if len(spy.scrolls) != 0 {
		t.Errorf("a consumed pinch must not also deliver a scroll; got %d", len(spy.scrolls))
	}
}

func TestUnconsumedCtrlWheelFallsThroughToScroll(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	spy.consume = false // nobody implements zoom here
	w.SetRoot(spy)

	w.IngestScrollForTest(10, 10, 0, 3, ModControl, GesturePhaseChanged)

	if len(spy.got) != 1 {
		t.Fatalf("pinch should still be offered, got %d", len(spy.got))
	}
	// The point of the fall-through: apps with existing Ctrl+scroll
	// behavior don't silently lose it to gesture synthesis.
	if len(spy.scrolls) != 1 {
		t.Fatalf("unconsumed pinch must fall through to a scroll, got %d", len(spy.scrolls))
	}
	if spy.scrolls[0].Mods&ModControl == 0 {
		t.Error("fall-through scroll should keep its modifiers")
	}
}

func TestPlainWheelIsNotAPinch(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	spy.consume = true
	w.SetRoot(spy)

	w.IngestScrollForTest(10, 10, 0, 3, 0, GesturePhaseChanged)

	if len(spy.got) != 0 {
		t.Errorf("unmodified wheel must not synthesize a pinch, got %d", len(spy.got))
	}
	if len(spy.scrolls) != 1 {
		t.Fatalf("unmodified wheel should scroll, got %d", len(spy.scrolls))
	}
}

func TestScrollCarriesModsAndPhase(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	// Shift+scroll is what graphs uses to zoom the Y axis; before the
	// platform bridge snapshotted them, scroll modifiers were always 0
	// in a real app even though tests could fake them.
	w.IngestScrollForTest(10, 10, 0, -2, ModShift, GesturePhaseMomentum)

	if len(spy.scrolls) != 1 {
		t.Fatalf("expected 1 scroll, got %d", len(spy.scrolls))
	}
	if spy.scrolls[0].Mods&ModShift == 0 {
		t.Error("scroll should carry ModShift")
	}
	if spy.scrolls[0].ScrollPhase != GesturePhaseMomentum {
		t.Errorf("ScrollPhase = %v, want GesturePhaseMomentum", spy.scrolls[0].ScrollPhase)
	}
}

func TestScrollModifiersDoNotLeakBetweenEvents(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	// Modifiers belong to the event that carried them: a later wheel from
	// a plain mouse must not inherit Shift, and (worse, with Ctrl)
	// silently become a zoom.
	w.IngestScrollForTest(10, 10, 0, -2, ModShift, GesturePhaseChanged)
	w.IngestScrollForTest(10, 10, 0, -2, 0, GesturePhaseNone)

	if len(spy.scrolls) != 2 {
		t.Fatalf("expected 2 scrolls, got %d", len(spy.scrolls))
	}
	if spy.scrolls[1].Mods != 0 {
		t.Errorf("second scroll should have no modifiers, got %v", spy.scrolls[1].Mods)
	}
}

func TestSmartMagnifyIsOneShot(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	w.IngestGestureForTest(EventGestureSmartMagnify, GesturePhaseNone, 0, 0, 0)

	if len(spy.got) != 1 {
		t.Fatalf("expected 1 smart-magnify event, got %d", len(spy.got))
	}
	if spy.got[0].Type() != EventGestureSmartMagnify {
		t.Errorf("kind = %v, want EventGestureSmartMagnify", spy.got[0].Type())
	}
	if spy.got[0].GesturePhase != GesturePhaseNone {
		t.Errorf("smart magnify has no lifecycle; phase = %v", spy.got[0].GesturePhase)
	}
}

func TestRotateGestureAccumulatesDegrees(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	w.IngestGestureForTest(EventGestureRotate, GesturePhaseBegan, 0, 0, 0)
	w.IngestGestureForTest(EventGestureRotate, GesturePhaseChanged, 0, 10, 0)
	w.IngestGestureForTest(EventGestureRotate, GesturePhaseChanged, 0, 15, 0)

	last := spy.got[len(spy.got)-1]
	if last.Rotation != 25 {
		t.Errorf("cumulative rotation = %v, want 25", last.Rotation)
	}
	if last.DRotation != 15 {
		t.Errorf("DRotation = %v, want 15", last.DRotation)
	}
	if last.Scale != 1 {
		t.Errorf("a rotation must not change scale; got %v", last.Scale)
	}
}

func TestDispatchPinchAtEmitsFullGesture(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 300})
	spy := newGestureSpy("doc", Rect{W: 400, H: 300})
	w.SetRoot(spy)

	// This is what the agent's Pinch action drives.
	w.dispatchPinchAt(Point{X: 20, Y: 20}, 1.5)

	if len(spy.got) != 3 {
		t.Fatalf("expected Began/Changed/Ended, got %d events", len(spy.got))
	}
	phases := []GesturePhase{
		spy.got[0].GesturePhase, spy.got[1].GesturePhase, spy.got[2].GesturePhase,
	}
	want := []GesturePhase{GesturePhaseBegan, GesturePhaseChanged, GesturePhaseEnded}
	for i := range want {
		if phases[i] != want[i] {
			t.Errorf("phase[%d] = %v, want %v", i, phases[i], want[i])
		}
	}
	if spy.got[1].Scale != 1.5 {
		t.Errorf("cumulative scale should reach the requested 1.5, got %v", spy.got[1].Scale)
	}
	if spy.got[0].X != 20 || spy.got[0].Y != 20 {
		t.Errorf("anchor = (%v,%v), want (20,20)", spy.got[0].X, spy.got[0].Y)
	}
}
