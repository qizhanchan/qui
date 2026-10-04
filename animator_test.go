package qui

import (
	"testing"
	"time"
)

// Registration + pruning of the Window.animators slice happens inside
// root package (the slice field is unexported), so this test belongs
// here even though the animator implementations live in ./anim/.
func TestWindowAnimatorRegistrationPrunes(t *testing.T) {
	w := &Window{lastSize: Size{W: 200, H: 200}}
	var ticks int
	a := fakeAnimator{
		tick: func(time.Time) (Rect, bool) {
			ticks++
			return Rect{W: 10, H: 10}, ticks >= 2
		},
	}
	w.RegisterAnimator(a)
	if len(w.animators) != 1 {
		t.Fatalf("registration didn't append, len=%d", len(w.animators))
	}
	// Simulate two Step passes of animator tick.
	now := time.Unix(0, 0)
	live := w.animators[:0]
	for _, an := range w.animators {
		_, done := an.Tick(now)
		if !done {
			live = append(live, an)
		}
	}
	w.animators = live
	live = w.animators[:0]
	for _, an := range w.animators {
		_, done := an.Tick(now)
		if !done {
			live = append(live, an)
		}
	}
	w.animators = live
	if len(w.animators) != 0 {
		t.Errorf("animator not pruned after done, len=%d", len(w.animators))
	}
}

type fakeAnimator struct {
	tick func(time.Time) (Rect, bool)
}

func (f fakeAnimator) Tick(now time.Time) (Rect, bool) { return f.tick(now) }
func (f fakeAnimator) Stop()                           {}

// TestWindowAnimatorRegisterDuringTickIsKept guards a real bug pattern:
// reactive's RequestRender path ends with RegisterAnimator, and that
// path commonly fires from inside another animator's Tick (uiPump-style
// goroutine→UI bridge writes async results back into reactive state).
// If the animator-iteration prune step silently drops the newcomer the
// scheduler is headless — its first Tick never runs and the UI freezes
// on whatever phase the async caller tried to set (reproduced as
// "stuck on Sending..." in examples/api-saw).
func TestWindowAnimatorRegisterDuringTickIsKept(t *testing.T) {
	w := NewTestWindow(Size{W: 200, H: 200})

	var addedTicks int
	added := fakeAnimator{tick: func(time.Time) (Rect, bool) {
		addedTicks++
		return Rect{}, true
	}}
	var registered bool
	host := fakeAnimator{tick: func(time.Time) (Rect, bool) {
		if !registered {
			w.RegisterAnimator(added)
			registered = true
		}
		return Rect{}, false
	}}
	w.RegisterAnimator(host)

	now := time.Unix(0, 0)
	w.tickAnimators(now)
	if len(w.animators) != 2 {
		t.Fatalf("expected host+added preserved after first tick, got %d", len(w.animators))
	}
	if addedTicks != 0 {
		t.Errorf("added animator ticked in the same pass it was registered: %d", addedTicks)
	}

	w.tickAnimators(now)
	if addedTicks != 1 {
		t.Errorf("added animator did not tick on the following pass: ticks=%d", addedTicks)
	}
	if len(w.animators) != 1 {
		t.Errorf("expected added animator pruned after one-shot done, got len=%d", len(w.animators))
	}
}
