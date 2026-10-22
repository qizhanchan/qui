package qui

import (
	"testing"
	"time"
)

type callbackAnimator struct{ onTick func() }

func (a *callbackAnimator) Tick(time.Time) (Rect, bool) {
	if a.onTick != nil {
		a.onTick()
	}
	return Rect{}, false
}
func (a *callbackAnimator) Stop() {}

// A Tick that unregisters a later animator (or itself) must not panic or
// leave a nil behind, and the unregistered animator must not tick.
func TestUnregisterAnimatorDuringTick(t *testing.T) {
	w := NewTestWindow(Size{W: 10, H: 10})
	cTicks := 0
	b := &callbackAnimator{}
	c := &callbackAnimator{onTick: func() { cTicks++ }}
	var a *callbackAnimator
	a = &callbackAnimator{onTick: func() {
		w.UnregisterAnimator(c)
		w.UnregisterAnimator(a)
	}}
	w.RegisterAnimator(a)
	w.RegisterAnimator(b)
	w.RegisterAnimator(c)
	w.tickAnimators(time.Now())
	if cTicks != 0 {
		t.Errorf("unregistered animator ticked %d times", cTicks)
	}
	if len(w.animators) != 1 || w.animators[0] != Animator(b) {
		t.Errorf("animators = %v, want just b", w.animators)
	}
}
