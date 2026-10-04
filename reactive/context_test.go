package reactive_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// The core Context contract: a consumer deep under a memoized (bailed
// out) intermediate still re-renders when the provided value changes.
func TestContextValueReachesConsumerThroughBailout(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	labelCtx := reactive.NewContext("label", "default")

	consumerRenders := 0
	consumer := func() reactive.Element {
		return reactive.Component("Consumer", "", struct{}{}, func(struct{}) reactive.Element {
			consumerRenders++
			return uiLabel(labelCtx.Use()).Build()
		})
	}
	middleRenders := 0
	middle := func() reactive.Element {
		// Static props → bails out on every re-render after mount.
		return reactive.MemoComponent("Middle", "", "static", func(a, b string) bool { return a == b }, func(string) reactive.Element {
			middleRenders++
			return uiVBox().Children(uiElem(consumer())).Build()
		})
	}

	value := "v1"
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiElem(labelCtx.Provide(value, middle())),
		).Build()
	})
	rt.Render()

	label := func() *qw.Label {
		return rt.Root().(*qui.Container).ChildAt(0).(*qui.Container).ChildAt(0).(*qw.Label)
	}
	if got := label().Text(); got != "v1" {
		t.Fatalf("initial context value = %q, want v1", got)
	}
	if middleRenders != 1 || consumerRenders != 1 {
		t.Fatalf("mount renders: middle=%d consumer=%d", middleRenders, consumerRenders)
	}

	// Change the provided value; middle must bail out, consumer must
	// re-render with the new value.
	value = "v2"
	rt.Render()
	if got := label().Text(); got != "v2" {
		t.Fatalf("after provide change, label = %q, want v2", got)
	}
	if middleRenders != 1 {
		t.Errorf("middle re-rendered %d times; the provider change should not re-run it", middleRenders)
	}
	if consumerRenders != 2 {
		t.Errorf("consumer renders = %d, want 2", consumerRenders)
	}

	// Unchanged value → nobody re-renders.
	rt.Render()
	if consumerRenders != 2 {
		t.Errorf("consumer re-rendered on unchanged context value (renders=%d)", consumerRenders)
	}
}

// Nested providers: the closest one wins; no provider → default.
func TestContextNestingAndDefault(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	ctx := reactive.NewContext("depth", "default")

	reader := func(key string) reactive.Element {
		return reactive.Component("Reader", key, key, func(string) reactive.Element {
			return uiLabel(ctx.Use()).Key(key).Build()
		})
	}

	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiElem(reader("naked")),
			uiElem(ctx.Provide("outer", uiVBox().Children(
				uiElem(reader("outer")),
				uiElem(ctx.Provide("inner", uiVBox().Children(
					uiElem(reader("inner")),
				).Build())),
			).Build())),
		).Build()
	})
	rt.Render()

	root := rt.Root().(*qui.Container)
	if got := root.ChildAt(0).(*qw.Label).Text(); got != "default" {
		t.Errorf("naked reader = %q, want default", got)
	}
	outerBox := root.ChildAt(1).(*qui.Container)
	if got := outerBox.ChildAt(0).(*qw.Label).Text(); got != "outer" {
		t.Errorf("outer reader = %q, want outer", got)
	}
	innerBox := outerBox.ChildAt(1).(*qui.Container)
	if got := innerBox.ChildAt(0).(*qw.Label).Text(); got != "inner" {
		t.Errorf("inner reader = %q, want inner", got)
	}
}

// Unmounting a consumer drops its subscription — a later provider
// change must not touch the dead fiber (would panic/mark garbage).
func TestContextUnsubscribeOnUnmount(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	ctx := reactive.NewContext("v", 0)

	renders := 0
	consumer := func() reactive.Element {
		return reactive.Component("C", "", struct{}{}, func(struct{}) reactive.Element {
			renders++
			return uiLabel("x").Build()
		})
	}

	show := true
	value := 1
	rt := reactive.NewRuntime(window, func() reactive.Element {
		content := uiNothing()
		if show {
			content = uiElem(consumer())
		}
		return uiVBox().Children(
			uiElem(ctx.Provide(value, uiVBox().Children(content).Build())),
		).Build()
	})
	rt.Render()

	show = false
	rt.Render()
	before := renders

	value = 2
	rt.Render() // must not re-render (or crash on) the unmounted consumer
	if renders != before {
		t.Errorf("unmounted consumer re-rendered (%d → %d)", before, renders)
	}
}
