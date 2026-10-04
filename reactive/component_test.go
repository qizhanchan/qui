package reactive_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// counterProps is the input to the Counter component. Only the id varies
// between instances; the renders/cleanups maps are shared bookkeeping so
// the test can observe how often each instance actually re-rendered.
type counterProps struct {
	id string
}

// makeCounter returns a reusable, independently-stateful Counter component.
// This is the thing the pre-component design could NOT express: each
// instance owns its own UseState, so dropping several in a list gives each
// its own count.
func makeCounter(renders map[string]int, cleanups map[string]int) func(id string) reactive.Element {
	return func(id string) reactive.Element {
		return reactive.MemoComponent("Counter", id, counterProps{id: id}, func(a, b counterProps) bool {
			return a.id == b.id
		},
			func(p counterProps) reactive.Element {
				renders[p.id]++
				count, set := reactive.UseState(0)

				reactive.UseEffectOnce(func() func() {
					return func() { cleanups[p.id]++ } // runs on unmount
				})

				return uiButton(fmt.Sprintf("%s:%d", p.id, count)).
					OnClick(func() { set(count + 1) }).
					Build()
			})
	}
}

// buttonFor digs the concrete *qw.Button out of the i-th child of the root
// VBox. Component instances resolve transparently to their subtree widget,
// so the container's child IS the button.
func buttonFor(t *testing.T, root qui.Widget, i int) *qw.Button {
	t.Helper()
	box, ok := root.(*qui.Container)
	if !ok {
		t.Fatalf("root is %T, want *qui.Container", root)
	}
	if i >= box.ChildCount() {
		t.Fatalf("child %d out of range (have %d)", i, box.ChildCount())
	}
	btn, ok := box.ChildAt(i).(*qw.Button)
	if !ok {
		t.Fatalf("child %d is %T, want *qw.Button", i, box.ChildAt(i))
	}
	return btn
}

func TestComponentsHaveIsolatedStateAndScopedRerender(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	renders := map[string]int{}
	cleanups := map[string]int{}
	Counter := makeCounter(renders, cleanups)

	showSecond := true
	var rt *reactive.Runtime
	rt = reactive.NewRuntime(window, func() reactive.Element {
		kids := []uiNode{uiElem(Counter("A"))}
		if showSecond {
			kids = append(kids, uiElem(Counter("B")))
		}
		return uiVBox().Gap(4).Children(kids...).Build()
	})
	rt.Render()

	// Both counters mounted, each rendered once, each shows its own 0.
	if renders["A"] != 1 || renders["B"] != 1 {
		t.Fatalf("initial renders: A=%d B=%d, want 1/1", renders["A"], renders["B"])
	}
	if got := buttonFor(t, rt.Root(), 0).Text; got != "A:0" {
		t.Fatalf("A button text = %q, want A:0", got)
	}
	if got := buttonFor(t, rt.Root(), 1).Text; got != "B:0" {
		t.Fatalf("B button text = %q, want B:0", got)
	}

	// Click A twice. A's own state advances; B must NOT re-render (scoped
	// re-render / props-unchanged bailout) and keeps its own count.
	buttonFor(t, rt.Root(), 0).OnClick()
	rt.Flush()
	buttonFor(t, rt.Root(), 0).OnClick()
	rt.Flush()

	if got := buttonFor(t, rt.Root(), 0).Text; got != "A:2" {
		t.Fatalf("after 2 clicks A = %q, want A:2", got)
	}
	if got := buttonFor(t, rt.Root(), 1).Text; got != "B:0" {
		t.Fatalf("B leaked state from A: %q, want B:0", got)
	}
	if renders["A"] != 3 {
		t.Errorf("A render count = %d, want 3 (mount + 2 clicks)", renders["A"])
	}
	if renders["B"] != 1 {
		t.Errorf("B re-rendered %d times; expected 1 — A's update should not re-run B", renders["B"])
	}

	// Independence the other way: click B once.
	buttonFor(t, rt.Root(), 1).OnClick()
	rt.Flush()
	if got := buttonFor(t, rt.Root(), 1).Text; got != "B:1" {
		t.Fatalf("after click B = %q, want B:1", got)
	}
	if got := buttonFor(t, rt.Root(), 0).Text; got != "A:2" {
		t.Fatalf("A disturbed by B click: %q, want A:2", got)
	}

	// Unmount B: its UseEffectOnce cleanup must run exactly once.
	showSecond = false
	rt.Render()
	if cleanups["B"] != 1 {
		t.Errorf("B unmount cleanup ran %d times, want 1", cleanups["B"])
	}
	if box := rt.Root().(*qui.Container); box.ChildCount() != 1 {
		t.Fatalf("after unmount, root has %d children, want 1", box.ChildCount())
	}
}
