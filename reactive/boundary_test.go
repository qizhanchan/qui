package reactive_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// A panic during a descendant component render swaps in the fallback;
// retry restores the child once the cause is fixed. Siblings outside
// the boundary are untouched.
func TestErrorBoundaryCatchesAndRetries(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	shouldPanic := true
	bomb := func() reactive.Element {
		return reactive.Component("Bomb", "", struct{}{}, func(struct{}) reactive.Element {
			if shouldPanic {
				panic("boom")
			}
			return uiLabel("recovered").Build()
		})
	}

	var retryFn func()
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiLabel("sibling"),
			uiElem(reactive.ErrorBoundary("guard",
				uiVBox().Children(uiElem(bomb())).Build(),
				func(err any, retry func()) reactive.Element {
					retryFn = retry
					return uiLabel(fmt.Sprintf("fallback: %v", err)).Build()
				})),
		).Build()
	})
	rt.Render()

	root := rt.Root().(*qui.Container)
	if got := root.ChildAt(0).(*qw.Label).Text(); got != "sibling" {
		t.Fatalf("sibling disturbed: %q", got)
	}
	fb, ok := root.ChildAt(1).(*qw.Label)
	if !ok || !strings.Contains(fb.Text(), "boom") {
		t.Fatalf("fallback not shown, child[1] = %#v", root.ChildAt(1))
	}

	// Fix the cause, retry — the original child mounts.
	shouldPanic = false
	retryFn()
	rt.Flush()

	inner, ok := rt.Root().(*qui.Container).ChildAt(1).(*qui.Container)
	if !ok {
		t.Fatalf("child not restored after retry: %T", rt.Root().(*qui.Container).ChildAt(1))
	}
	if got := inner.ChildAt(0).(*qw.Label).Text(); got != "recovered" {
		t.Fatalf("restored child text = %q", got)
	}
}

// A panic on a LATER render (after a healthy mount) is also caught.
func TestErrorBoundaryCatchesLateRenderPanic(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	var explode func()
	bomb := func() reactive.Element {
		return reactive.Component("Bomb", "", struct{}{}, func(struct{}) reactive.Element {
			armed, setArmed := reactive.UseState(false)
			explode = func() { setArmed(true) }
			if armed {
				panic("late boom")
			}
			return uiLabel("healthy").Build()
		})
	}

	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiElem(reactive.ErrorBoundary("guard",
				uiVBox().Children(uiElem(bomb())).Build(),
				func(err any, retry func()) reactive.Element {
					return uiLabel(fmt.Sprintf("fallback: %v", err)).Build()
				})),
		).Build()
	})
	rt.Render()

	inner := rt.Root().(*qui.Container).ChildAt(0).(*qui.Container)
	if got := inner.ChildAt(0).(*qw.Label).Text(); got != "healthy" {
		t.Fatalf("initial text %q", got)
	}

	explode()
	rt.Flush()

	fb, ok := rt.Root().(*qui.Container).ChildAt(0).(*qw.Label)
	if !ok || !strings.Contains(fb.Text(), "late boom") {
		t.Fatalf("late panic not caught: %#v", rt.Root().(*qui.Container).ChildAt(0))
	}
}

// A panicking FALLBACK propagates (no infinite loop, no swallow).
func TestErrorBoundaryFallbackPanicPropagates(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	defer func() {
		if recover() == nil {
			t.Fatal("fallback panic was swallowed")
		}
	}()

	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiElem(reactive.ErrorBoundary("guard",
				reactive.Component("Bomb", "", struct{}{}, func(struct{}) reactive.Element {
					panic("boom")
				}),
				func(err any, retry func()) reactive.Element {
					panic("fallback is broken too")
				})),
		).Build()
	})
	rt.Render()
}
