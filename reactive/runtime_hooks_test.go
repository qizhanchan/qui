package reactive

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

func TestRuntimeRequestRenderBatchesUntilSchedulerTick(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 640, H: 480})
	renderCount := 0

	runtime := NewRuntime(window, func() Element {
		renderCount++
		return containerElem(leafElem("only", "item"))
	})
	runtime.Render()
	if renderCount != 1 {
		t.Fatalf("expected initial render count 1, got %d", renderCount)
	}

	var queued []qui.Animator
	runtime.mu.Lock()
	runtime.scheduleFn = func(animator qui.Animator) bool {
		queued = append(queued, animator)
		return true
	}
	runtime.mu.Unlock()

	runtime.RequestRender()
	runtime.RequestRender()

	if renderCount != 1 {
		t.Fatalf("batched RequestRender should not render immediately, got %d renders", renderCount)
	}
	if len(queued) != 1 {
		t.Fatalf("expected one scheduled animator, got %d", len(queued))
	}

	queued[0].Tick(time.Now())
	if renderCount != 2 {
		t.Fatalf("scheduler tick should flush one render, got %d", renderCount)
	}
}

func TestHooksUseStateAndUseMemo(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	memoComputes := 0
	renders := 0
	var setCount func(int)

	runtime := NewRuntime(window, func() Element {
		renders++
		count, set := UseState(1)
		setCount = set
		double := UseMemo(func() int {
			memoComputes++
			return count * 2
		}, count)
		return containerElem(leafElem("counter", fmt.Sprintf("%d/%d", count, double)))
	})

	runtime.Render()
	if renders != 1 {
		t.Fatalf("expected one render after initial Render, got %d", renders)
	}
	if memoComputes != 1 {
		t.Fatalf("expected memo to compute once, got %d", memoComputes)
	}
	if setCount == nil {
		t.Fatal("expected UseState setter to be captured")
	}

	setCount(2)
	if renders != 2 {
		t.Fatalf("state change should trigger rerender, got %d", renders)
	}
	if memoComputes != 2 {
		t.Fatalf("memo should recompute on dependency change, got %d", memoComputes)
	}

	setCount(2)
	if renders != 2 {
		t.Fatalf("setting same state value should skip rerender, got %d", renders)
	}
	if memoComputes != 2 {
		t.Fatalf("memo compute count should remain stable, got %d", memoComputes)
	}

	leaf := window.Root().(*qui.Container).ChildAt(0).(*testLeaf)
	if leaf.text != "2/4" {
		t.Fatalf("unexpected rendered text %q", leaf.text)
	}
}

func TestHooksUseStateFnBatchedFunctionalUpdates(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	var inc func()

	renders := 0
	runtime := NewRuntime(window, func() Element {
		renders++
		value, _, update := UseStateFn(0)
		inc = func() {
			update(func(prev int) int { return prev + 1 })
		}
		return containerElem(leafElem("counter", fmt.Sprintf("%d", value)))
	})
	runtime.Render()
	if inc == nil {
		t.Fatal("expected increment closure")
	}

	var queued []qui.Animator
	runtime.mu.Lock()
	runtime.scheduleFn = func(animator qui.Animator) bool {
		queued = append(queued, animator)
		return true
	}
	runtime.mu.Unlock()

	inc()
	inc()
	if renders != 1 {
		t.Fatalf("functional updates should be batched before flush, renders=%d", renders)
	}
	if len(queued) != 1 {
		t.Fatalf("expected one scheduled flush, got %d", len(queued))
	}

	queued[0].Tick(time.Now())
	if renders != 2 {
		t.Fatalf("expected one batched render after tick, renders=%d", renders)
	}
	leaf := window.Root().(*qui.Container).ChildAt(0).(*testLeaf)
	if leaf.text != "2" {
		t.Fatalf("expected accumulated value 2, got %q", leaf.text)
	}
}

func TestHooksUseEffectLifecycle(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	events := make([]string, 0, 8)
	var setValue func(int)

	runtime := NewRuntime(window, func() Element {
		value, set := UseState(0)
		setValue = set
		UseEffect(func() func() {
			events = append(events, fmt.Sprintf("run:%d", value))
			captured := value
			return func() {
				events = append(events, fmt.Sprintf("cleanup:%d", captured))
			}
		}, value)
		return containerElem(leafElem("effect", fmt.Sprintf("value:%d", value)))
	})

	runtime.Render()
	if !reflect.DeepEqual(events, []string{"run:0"}) {
		t.Fatalf("unexpected events after first render: %v", events)
	}
	if setValue == nil {
		t.Fatal("expected UseState setter to be captured")
	}

	setValue(1)
	if !reflect.DeepEqual(events, []string{"run:0", "cleanup:0", "run:1"}) {
		t.Fatalf("unexpected events after dependency update: %v", events)
	}

	runtime.SetRender(func() Element { return Empty() })
	runtime.Render()
	if !reflect.DeepEqual(events, []string{"run:0", "cleanup:0", "run:1", "cleanup:1"}) {
		t.Fatalf("unexpected events after unmount: %v", events)
	}
}

func TestReconcilerProfilerAndDebugTree(t *testing.T) {
	var r Reconciler

	r.Render(containerElem(
		leafElem("a", "a"),
		leafElem("b", "b"),
		leafElem("c", "c"),
	))
	first := r.Profile().Last
	if first.Mounts == 0 || first.Reuses != 0 {
		t.Fatalf("unexpected initial profile: %+v", first)
	}

	r.Render(containerElem(
		leafElem("c", "c"),
		leafElem("a", "a"),
		leafElem("b", "b"),
	))
	second := r.Profile().Last
	if second.Reuses == 0 {
		t.Fatalf("expected reuse hits on reorder, got %+v", second)
	}
	if second.Reorders == 0 {
		t.Fatalf("expected reorder hit on keyed reorder, got %+v", second)
	}
	if second.ReuseHitRate() <= 0 {
		t.Fatalf("expected positive reuse hit rate, got %+v", second)
	}
	if second.ReorderHitRate() <= 0 {
		t.Fatalf("expected positive reorder hit rate, got %+v", second)
	}

	tree := r.DebugTree()
	if tree == nil {
		t.Fatal("expected debug tree snapshot")
	}
	if tree.Kind != "container" {
		t.Fatalf("unexpected debug root kind %q", tree.Kind)
	}
	if len(tree.Children) != 3 {
		t.Fatalf("expected 3 debug children, got %d", len(tree.Children))
	}
	text := tree.String()
	if !strings.Contains(text, "leaf[a]") || !strings.Contains(text, "leaf[b]") || !strings.Contains(text, "leaf[c]") {
		t.Fatalf("debug tree string missing children:\n%s", text)
	}
}
