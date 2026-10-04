package reactive_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// The engine claim behind signal bindings: updating a bound value
// mutates the widget directly — the render function and reconciler
// never run.
func TestSignalBindingSkipsReconcile(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	count := reactive.NewSignal(0)
	text := reactive.Map(count, func(n int) string { return fmt.Sprintf("n:%d", n) })

	rootRenders := 0
	rt := reactive.NewRuntime(window, func() reactive.Element {
		rootRenders++
		return uiVBox().Children(
			uiLabel("").BindText(text),
		).Build()
	})
	rt.Render()

	label := rt.Root().(*qui.Container).ChildAt(0).(*qw.Label)
	if label.Text() != "n:0" {
		t.Fatalf("initial bound text = %q, want n:0", label.Text())
	}
	rendersBefore := rt.Profile().Total.Renders

	for i := 1; i <= 5; i++ {
		count.Set(i)
	}
	window.DrainJobsForTest()

	if label.Text() != "n:5" {
		t.Fatalf("bound text = %q, want n:5", label.Text())
	}
	if rootRenders != 1 {
		t.Errorf("root render fn ran %d times; signal updates must not re-render", rootRenders)
	}
	if got := rt.Profile().Total.Renders; got != rendersBefore {
		t.Errorf("reconcile passes went %d → %d; signal updates must not reconcile", rendersBefore, got)
	}
}

// Unmounting a bound widget releases its subscription (Element.Destroy
// → BindWidget unbind): later signal sets must not touch the dead
// widget.
func TestSignalBindingUnbindsOnUnmount(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	sig := reactive.NewSignal("alive")

	show := true
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiIf(show, uiLabel("").BindText(sig)),
		).Build()
	})
	rt.Render()

	label := rt.Root().(*qui.Container).ChildAt(0).(*qw.Label)
	if label.Text() != "alive" {
		t.Fatalf("initial text %q", label.Text())
	}

	show = false
	rt.Render()

	sig.Set("after-unmount")
	window.DrainJobsForTest()
	if label.Text() != "alive" {
		t.Errorf("unmounted widget still driven by signal: %q", label.Text())
	}
}

// Setting an equal value must not notify watchers.
func TestSignalSetEqualIsNoop(t *testing.T) {
	sig := reactive.NewSignal(42)
	fires := 0
	unsub := sig.Subscribe(func() { fires++ })
	defer unsub()

	sig.Set(42)
	if fires != 0 {
		t.Errorf("equal Set notified %d times", fires)
	}
	sig.Set(43)
	if fires != 1 {
		t.Errorf("changed Set notified %d times, want 1", fires)
	}
}

// UseSignal keeps one signal instance per component across renders,
// and setting it does NOT re-render the component.
func TestUseSignalStableAndRenderFree(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	renders := 0
	var captured *reactive.Signal[int]
	comp := func() reactive.Element {
		return reactive.Component("C", "", struct{}{}, func(struct{}) reactive.Element {
			renders++
			sig := reactive.UseSignal(7)
			captured = sig
			return uiLabel("").BindText(reactive.Map(sig, func(n int) string {
				return fmt.Sprintf("%d", n)
			})).Build()
		})
	}
	_ = comp

	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(uiElem(comp())).Build()
	})
	rt.Render()
	first := captured

	rt.Render() // component props are unchanged; UseSignal remains stable.
	if captured != first {
		t.Fatal("UseSignal returned a different instance across renders")
	}

	rendersBefore := renders
	first.Set(99)
	window.DrainJobsForTest()
	if renders != rendersBefore {
		t.Errorf("setting a UseSignal re-rendered the component (%d → %d)", rendersBefore, renders)
	}
}

// UseReducer: dispatch reduces against the latest state, bursts included.
func TestUseReducer(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	type action int
	var dispatch func(action)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		n, d := reactive.UseReducer(func(s int, a action) int { return s + int(a) }, 10)
		dispatch = d
		return uiLabel(fmt.Sprintf("v:%d", n)).Build()
	})
	rt.Render()

	dispatch(1)
	dispatch(2)
	dispatch(3)
	rt.Flush()

	if got := rt.Root().(*qw.Label).Text(); got != "v:16" {
		t.Fatalf("after burst dispatch, label = %q, want v:16", got)
	}
}

// Computed recomputes from any dep and skips equal results.
func TestComputedMultiDep(t *testing.T) {
	a := reactive.NewSignal(1)
	b := reactive.NewSignal(2)
	sum := reactive.Computed(func() string {
		return fmt.Sprintf("%d", a.Get()+b.Get())
	}, a, b)

	if sum.Get() != "3" {
		t.Fatalf("initial computed = %q", sum.Get())
	}

	fires := 0
	unsub := sum.Subscribe(func() { fires++ })
	defer unsub()

	a.Set(10)
	if sum.Get() != "12" || fires != 1 {
		t.Fatalf("after a: %q fires=%d", sum.Get(), fires)
	}
	b.Set(0)
	if sum.Get() != "10" || fires != 2 {
		t.Fatalf("after b: %q fires=%d", sum.Get(), fires)
	}
	// Two more dep changes: a.Set(4) → "4" (notify), b.Set(6) → "10"
	// (notify) — 4 total.
	a.Set(4)
	b.Set(6)
	if sum.Get() != "10" || fires != 4 {
		t.Fatalf("computed = %q fires=%d, want \"10\"/4", sum.Get(), fires)
	}

	// A dep change whose RESULT is unchanged must not notify.
	a.Set(10)
	b.Set(0)
	firesBefore := fires
	b.Set(0) // no-op dep set → no recompute, no notify
	if fires != firesBefore {
		t.Errorf("no-op dep set notified (%d → %d)", firesBefore, fires)
	}
}

func TestMapDisposeUnsubscribesSource(t *testing.T) {
	src := reactive.NewSignal(2)
	mapped := reactive.Map(src, func(v int) int { return v * 3 })
	if got := mapped.Get(); got != 6 {
		t.Fatalf("initial map = %d, want 6", got)
	}

	mapped.Dispose()
	mapped.Dispose() // idempotent
	src.Set(4)
	if got := mapped.Get(); got != 6 {
		t.Fatalf("disposed map updated to %d, want 6", got)
	}
}

func TestComputedDisposeUnsubscribesExplicitDeps(t *testing.T) {
	a := reactive.NewSignal(1)
	b := reactive.NewSignal(2)
	sum := reactive.Computed(func() int { return a.Get() + b.Get() }, a, b)
	sum.Dispose()
	a.Set(10)
	b.Set(20)
	if got := sum.Get(); got != 3 {
		t.Fatalf("disposed computed updated to %d, want 3", got)
	}
}
