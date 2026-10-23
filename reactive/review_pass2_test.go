package reactive_test

import (
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
)

// A functional updater may call another setter: it runs without the
// runtime lock held (it used to deadlock the UI thread).
func TestFunctionalUpdaterCanCallSetter(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var update func(func(int) int)
	var setOther func(int)
	var a, b int
	rt := reactive.NewRuntime(window, func() reactive.Element {
		var x, y int
		x, _, update = reactive.UseStateFn(0)
		y, setOther = reactive.UseState(0)
		a, b = x, y
		return uiLabel("x").Build()
	})
	rt.Render()
	done := make(chan struct{})
	go func() {
		defer close(done)
		update(func(v int) int { setOther(7); return v + 1 })
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("functional updater deadlocked")
	}
	rt.Flush()
	if a != 1 || b != 7 {
		t.Fatalf("state = %d, %d; want 1, 7", a, b)
	}
}

// A setter kept past unmount (a stray ticker) neither panics nor renders.
func TestSetterAfterUnmountIsNoop(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var leaked func(int)
	renders := 0
	child := reactive.Component("C", "", struct{}{}, func(struct{}) reactive.Element {
		_, set := reactive.UseState(0)
		leaked = set
		renders++
		return uiLabel("c").Build()
	})
	var setShow func(bool)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		show, s := reactive.UseState(true)
		setShow = s
		if !show {
			return uiLabel("gone").Build()
		}
		return child
	})
	rt.Render()
	setShow(false)
	rt.Flush()
	before := renders
	leaked(5)
	rt.Flush()
	if renders != before {
		t.Fatalf("unmounted component re-rendered (%d → %d)", before, renders)
	}
}
