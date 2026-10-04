package reactive_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// A stateful Counter nested under a memoized wrapper whose props never
// change. Clicking the counter must still re-render it, even though the
// wrapper bails out.
func TestDirtyDescendantSurvivesParentBailout(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	counter := func() reactive.Element {
		return reactive.Component("Counter", "", struct{}{}, func(struct{}) reactive.Element {
			n, set := reactive.UseState(0)
			return uiButton(fmt.Sprintf("n:%d", n)).OnClick(func() { set(n + 1) }).Build()
		})
	}
	wrapper := func() reactive.Element {
		// MemoComponent opts into the bailout required for this test.
		return reactive.MemoComponent("Wrapper", "", "static", func(a, b string) bool { return a == b }, func(string) reactive.Element {
			return uiVBox().Children(uiElem(counter())).Build()
		})
	}

	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(uiElem(wrapper())).Build()
	})
	rt.Render()
	// Force one extra root render so the wrapper has bailed out at least once.
	rt.Render()

	btn := rt.Root().(*qui.Container).ChildAt(0).(*qui.Container).ChildAt(0).(*qw.Button)
	if btn.Text != "n:0" {
		t.Fatalf("initial text %q", btn.Text)
	}
	btn.OnClick()
	rt.Flush()
	btn2 := rt.Root().(*qui.Container).ChildAt(0).(*qui.Container).ChildAt(0).(*qw.Button)
	if btn2.Text != "n:1" {
		t.Fatalf("dirty counter under bailed-out wrapper did not re-render: text %q", btn2.Text)
	}
}

// Memoization is explicitly opt-in. The comparator owns the callback
// contract, rather than Component guessing that two closures have the same
// captured state from a shared code address.
func TestMemoComponentBailsOutWithExplicitComparator(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	type rowProps struct {
		Label    string
		OnSelect func()
	}
	renders := 0
	row := func(p rowProps) reactive.Element {
		return reactive.MemoComponent("Row", "", p, func(a, b rowProps) bool {
			return a.Label == b.Label
		}, func(p rowProps) reactive.Element {
			renders++
			return uiButton(p.Label).OnClick(p.OnSelect).Build()
		})
	}

	tick := 0
	rt := reactive.NewRuntime(window, func() reactive.Element {
		_ = tick
		return uiVBox().Children(
			uiElem(row(rowProps{Label: "stable", OnSelect: func() {}})),
		).Build()
	})
	rt.Render()
	if renders != 1 {
		t.Fatalf("mount renders = %d, want 1", renders)
	}

	// Re-render the root twice; the explicit comparator authorizes the
	// bailout even though the closure literal is recreated each time.
	tick++
	rt.Render()
	tick++
	rt.Render()
	if renders != 1 {
		t.Errorf("row re-rendered %d times despite unchanged props with callback; bailout broken", renders)
	}

	profile := rt.Profile()
	if profile.Total.Bailouts < 2 {
		t.Errorf("bailouts = %d, want >= 2", profile.Total.Bailouts)
	}
}

// Ordinary Component always refreshes callback props. Two closures created
// at the same source location can capture different render-local values, so
// treating them as equal would leave the old event handler installed.
func TestComponentRefreshesCallbackClosuresByDefault(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	tick := 0
	fired := -1
	renders := 0
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(uiElem(reactive.Component("Row", "", "stable", func(string) reactive.Element {
			renders++
			return uiButton("run").OnClick(func() { fired = tick }).Build()
		}))).Build()
	})
	rt.Render()
	tick = 1
	rt.Render()

	buttonFor(t, rt.Root(), 0).OnClick()
	if fired != 1 {
		t.Fatalf("callback kept stale closure: got %d, want 1", fired)
	}
	if renders != 2 {
		t.Errorf("ordinary Component rendered %d times, want 2", renders)
	}
}
