package reactive_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
)

// When a boundary trips after the aborted reconcile already unmounted a
// sibling (kind change), that sibling's effect cleanup runs exactly once.
func TestErrorBoundaryDoesNotDoubleUnmount(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	cleanups := 0
	fx := reactive.Component("Fx", "", struct{}{}, func(struct{}) reactive.Element {
		reactive.UseEffectOnce(func() func() { return func() { cleanups++ } })
		return uiLabel("fx").Build()
	})
	phase := 0
	bomb := reactive.Component("Bomb", "", struct{}{}, func(struct{}) reactive.Element {
		if phase == 1 {
			panic("boom")
		}
		return uiLabel("ok").Build()
	})
	var set func(int)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		p, s := reactive.UseState(0)
		set = s
		phase = p
		first := fx
		if p == 1 {
			first = uiLabel("swapped").Build() // kind change: Fx unmounts mid-loop
		}
		return reactive.ErrorBoundary("guard",
			uiVBox().Children(uiElem(first), uiElem(bomb)).Build(),
			func(any, func()) reactive.Element { return uiLabel("fallback").Build() })
	})
	rt.Render()
	set(1)
	rt.Flush()
	if cleanups != 1 {
		t.Fatalf("Fx cleanup ran %d times, want 1", cleanups)
	}
}

// A For whose signal fires and which is then removed in the same frame
// must not run its queued scoped sync on the dead node.
func TestForScopedSyncSkipsUnmountedNode(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	mounts := 0
	items := reactive.NewSignal([]string{"a"})
	row := func(id string) reactive.Element {
		return reactive.Component("Row", id, id, func(id string) reactive.Element {
			reactive.UseEffectOnce(func() func() { mounts++; return nil })
			return uiLabel(id).Key(id).Build()
		})
	}
	var setShow func(bool)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		show, s := reactive.UseState(true)
		setShow = s
		if !show {
			return uiLabel("gone").Build()
		}
		return uiVBox().Children(
			uiForOf(items, func(_ int, s string) uiNode { return uiElem(row(s)) }),
		).Build()
	})
	rt.Render()
	items.Set([]string{"a", "b"}) // queues a scoped sync job
	setShow(false)
	rt.Flush() // removes the For before the job runs
	window.DrainJobsForTest()
	if mounts != 1 {
		t.Fatalf("row effects ran %d times, want 1 (b must not mount into the dead For)", mounts)
	}
}

// A Show whose parent swaps in a different signal follows the new one.
func TestShowFollowsSwappedSignal(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	a, b := reactive.NewSignal(false), reactive.NewSignal(false)
	var useB func(bool)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		which, set := reactive.UseState(false)
		useB = set
		cond := a
		if which {
			cond = b
		}
		return uiVBox().Children(uiShow(cond, func() uiNode { return uiLabel("on") })).Build()
	})
	rt.Render()
	useB(true)
	rt.Flush()
	host := boundContainer(t, rt, 0)

	a.Set(true) // the old signal must no longer drive it
	window.DrainJobsForTest()
	if host.ChildCount() != 0 {
		t.Fatalf("old signal still drives the Show (%d children)", host.ChildCount())
	}
	b.Set(true)
	window.DrainJobsForTest()
	if host.ChildCount() != 1 {
		t.Fatalf("new signal did not mount the subtree (%d children)", host.ChildCount())
	}
}

// Closing the window unmounts the runtime: effect cleanups run, the
// registry forgets it, and a late setState is ignored.
func TestWindowCloseUnmountsRuntime(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	rootCleanups, childCleanups, renders := 0, 0, 0
	var set func(int)
	child := reactive.Component("Child", "", struct{}{}, func(struct{}) reactive.Element {
		reactive.UseEffectOnce(func() func() { return func() { childCleanups++ } })
		return uiLabel("c").Build()
	})
	rt := reactive.NewRuntime(window, func() reactive.Element {
		renders++
		_, s := reactive.UseState(0)
		set = s
		reactive.UseEffectOnce(func() func() { return func() { rootCleanups++ } })
		return uiVBox().Children(uiElem(child)).Build()
	})
	rt.Render()
	if reactive.RuntimeForWindow(window) != rt {
		t.Fatal("runtime not registered")
	}
	window.Destroy()
	if rootCleanups != 1 || childCleanups != 1 {
		t.Fatalf("cleanups root=%d child=%d, want 1 each", rootCleanups, childCleanups)
	}
	if reactive.RuntimeForWindow(window) != nil {
		t.Error("registry still holds the closed window's runtime")
	}
	before := renders
	set(5)
	rt.Flush()
	if renders != before {
		t.Error("a setState after unmount re-rendered")
	}
	rt.Unmount() // idempotent
	if rootCleanups != 1 {
		t.Error("second Unmount re-ran cleanups")
	}
}
