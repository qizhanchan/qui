package reactive_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// boundContainer digs the Show/For host container out of the root VBox.
func boundContainer(t *testing.T, rt *reactive.Runtime, idx int) *qui.Container {
	t.Helper()
	c, ok := rt.Root().(*qui.Container).ChildAt(idx).(*qui.Container)
	if !ok {
		t.Fatalf("child %d is %T, want bound *qui.Container", idx, rt.Root().(*qui.Container).ChildAt(idx))
	}
	return c
}

// Show mounts/unmounts from the signal with ZERO render passes.
func TestShowTogglesWithoutRender(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	visible := reactive.NewSignal(false)
	rootRenders := 0
	rt := reactive.NewRuntime(window, func() reactive.Element {
		rootRenders++
		return uiVBox().Children(
			uiShow(visible, func() uiNode { return uiLabel("secret") }),
		).Build()
	})
	rt.Render()

	host := boundContainer(t, rt, 0)
	if host.ChildCount() != 0 {
		t.Fatalf("hidden: host has %d children", host.ChildCount())
	}
	rendersBefore := rt.Profile().Total.Renders

	visible.Set(true)
	window.DrainJobsForTest()
	if host.ChildCount() != 1 {
		t.Fatalf("shown: host has %d children, want 1", host.ChildCount())
	}
	if got := host.ChildAt(0).(*qw.Label).Text(); got != "secret" {
		t.Fatalf("mounted text %q", got)
	}

	visible.Set(false)
	window.DrainJobsForTest()
	if host.ChildCount() != 0 {
		t.Fatalf("re-hidden: host has %d children", host.ChildCount())
	}

	if rootRenders != 1 {
		t.Errorf("root render ran %d times; Show must not re-render", rootRenders)
	}
	if got := rt.Profile().Total.Renders; got != rendersBefore {
		t.Errorf("full reconcile passes went %d → %d; Show must sync scoped", rendersBefore, got)
	}
}

// For tracks the slice signal with keyed reuse and zero render passes.
func TestForSyncsKeyedRowsWithoutRender(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	items := reactive.NewSignal([]string{"a", "b", "c"})
	rootRenders := 0
	rt := reactive.NewRuntime(window, func() reactive.Element {
		rootRenders++
		return uiVBox().Children(
			uiForOf(items, func(_ int, s string) uiNode {
				return uiLabel(s).Key(s)
			}),
		).Build()
	})
	rt.Render()

	host := boundContainer(t, rt, 0)
	texts := func() []string {
		out := make([]string, 0, host.ChildCount())
		for _, c := range host.Children() {
			out = append(out, c.(*qw.Label).Text())
		}
		return out
	}
	if fmt.Sprint(texts()) != "[a b c]" {
		t.Fatalf("initial rows %v", texts())
	}
	widgetB := host.ChildAt(1)
	rendersBefore := rt.Profile().Total.Renders

	// Append + remove + reorder in one update.
	items.Set([]string{"c", "b", "d"})
	window.DrainJobsForTest()

	if fmt.Sprint(texts()) != "[c b d]" {
		t.Fatalf("after update rows %v", texts())
	}
	if host.ChildAt(1) != widgetB {
		t.Error("keyed row 'b' was remounted instead of reused")
	}
	if rootRenders != 1 || rt.Profile().Total.Renders != rendersBefore {
		t.Errorf("For triggered a render pass (renders=%d)", rootRenders)
	}
}

// A stateful component INSIDE a For row keeps working: its setState
// re-renders it (through the scoped-dirty path), and its state survives
// signal-driven list updates that keep the row.
func TestForRowComponentsStayStateful(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	items := reactive.NewSignal([]string{"x", "y"})
	counter := func(id string) reactive.Element {
		return reactive.Component("RowCounter", id, id, func(id string) reactive.Element {
			n, set := reactive.UseState(0)
			return uiButton(fmt.Sprintf("%s:%d", id, n)).
				OnClick(func() { set(n + 1) }).Build()
		})
	}
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiForOf(items, func(_ int, s string) uiNode {
				return uiElem(counter(s))
			}),
		).Build()
	})
	rt.Render()

	host := boundContainer(t, rt, 0)
	btnX := host.ChildAt(0).(*qw.Button)
	btnX.OnClick()
	rt.Flush()
	window.DrainJobsForTest()
	if btnX.Text != "x:1" {
		t.Fatalf("row setState didn't re-render: %q", btnX.Text)
	}

	// Prepend a row — 'x' keeps its widget AND its count.
	items.Set([]string{"w", "x", "y"})
	window.DrainJobsForTest()
	if host.ChildAt(1) != btnX {
		t.Fatal("keyed stateful row remounted on list update")
	}
	if btnX.Text != "x:1" {
		t.Fatalf("row state lost on list update: %q", btnX.Text)
	}

	// And its setState still works after the scoped remount pass.
	btnX.OnClick()
	rt.Flush()
	window.DrainJobsForTest()
	if btnX.Text != "x:2" {
		t.Fatalf("row setState broken after list update: %q", btnX.Text)
	}
}

// Row effects fire on scoped mounts and clean up on scoped removals.
func TestForRowEffectsAcrossScopedSyncs(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	mounts := map[string]int{}
	cleanups := map[string]int{}
	items := reactive.NewSignal([]string{"a"})
	row := func(id string) reactive.Element {
		return reactive.Component("FxRow", id, id, func(id string) reactive.Element {
			reactive.UseEffectOnce(func() func() {
				mounts[id]++
				return func() { cleanups[id]++ }
			})
			return uiLabel(id).Key(id).Build()
		})
	}
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiForOf(items, func(_ int, s string) uiNode { return uiElem(row(s)) }),
		).Build()
	})
	rt.Render()
	if mounts["a"] != 1 {
		t.Fatalf("mount effect a = %d", mounts["a"])
	}

	items.Set([]string{"a", "b"}) // scoped mount of b
	window.DrainJobsForTest()
	if mounts["b"] != 1 {
		t.Fatalf("scoped mount effect b = %d", mounts["b"])
	}

	items.Set([]string{"b"}) // scoped removal of a
	window.DrainJobsForTest()
	if cleanups["a"] != 1 {
		t.Fatalf("scoped cleanup a = %d", cleanups["a"])
	}

	// Unmounting the whole For (via root) cleans the rest.
	rt.SetRender(func() reactive.Element { return uiLabel("gone").Build() })
	rt.Render()
	if cleanups["b"] != 1 {
		t.Fatalf("cleanup b on For unmount = %d", cleanups["b"])
	}
}

// Context read inside a For row resolves through scoped syncs (provider
// stack snapshot).
func TestForRowsReadContextInScopedSync(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	ctx := reactive.NewContext("prefix", "?")
	items := reactive.NewSignal([]string{})
	row := func(id string) reactive.Element {
		return reactive.Component("CtxRow", id, id, func(id string) reactive.Element {
			return uiLabel(ctx.Use() + id).Key(id).Build()
		})
	}
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiElem(ctx.Provide("p-", uiForOf(items, func(_ int, s string) uiNode {
				return uiElem(row(s))
			}).Build())),
		).Build()
	})
	rt.Render()

	// Mount a row PURELY via the signal — its Use() must see "p-".
	items.Set([]string{strconv.Itoa(7)})
	window.DrainJobsForTest()

	host := boundContainer(t, rt, 0)
	if got := host.ChildAt(0).(*qw.Label).Text(); got != "p-7" {
		t.Fatalf("scoped-mounted row read context %q, want p-7", got)
	}
}

// ForOf's builder layout options land on the hosting container, and a
// layout change across renders is re-applied.
func TestForLayoutOptions(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	items := reactive.NewSignal([]string{"a", "b"})
	gap := float32(6)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiForOf(items, func(_ int, s string) uiNode {
				return uiLabel(s).Key(s)
			}).Horizontal().Gap(gap).Grow(1),
		).Build()
	})
	rt.Render()

	host := boundContainer(t, rt, 0)
	fl, ok := host.LayoutEngine.(qui.FlexLayout)
	if !ok {
		t.Fatalf("engine is %T", host.LayoutEngine)
	}
	if fl.Direction != qui.Horizontal || fl.Gap != 6 {
		t.Fatalf("layout = %+v, want horizontal gap 6", fl)
	}
	if host.FlexItemValue().Grow != 1 {
		t.Fatalf("grow = %v, want 1", host.FlexItemValue().Grow)
	}

	// Change the gap on a later render → re-applied to the live container.
	gap = 12
	rt.Render()
	fl = host.LayoutEngine.(qui.FlexLayout)
	if fl.Gap != 12 {
		t.Fatalf("layout gap after render = %v, want 12", fl.Gap)
	}
}

// styleFlushRecorder is a fake host backend (SetHostData) that records
// FlushPendingStyles calls — the optional hook the runtime invokes so a host
// with DEFERRED styling (htmlcss's StyleEngine) can apply pending styles in
// the same pass, before the window is invalidated. Without this, a pass that
// mounts fresh host elements paints one unstyled frame (the "filtered list
// grows back" flash).
type styleFlushRecorder struct{ flushes int }

func (s *styleFlushRecorder) FlushPendingStyles() { s.flushes++ }

// A reconcile render and a For-growth bound sync must each flush the host's
// pending styles, so newly-mounted host elements are styled before they paint.
func TestRuntimeFlushesHostStylesBeforePaint(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	items := reactive.NewSignal([]string{"a"})
	rec := &styleFlushRecorder{}
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiForOf(items, func(_ int, s string) uiNode {
				return uiLabel(s).Key(s)
			}),
		).Build()
	})
	rt.SetHostData(rec)

	// A reconcile pass (initial render) flushes host styles.
	rt.Render()
	if rec.flushes == 0 {
		t.Fatalf("render did not flush host styles")
	}

	// A For-growth bound sync (no render pass) must flush host styles too —
	// the new row's element would otherwise style a frame late.
	before := rec.flushes
	items.Set([]string{"a", "b"})
	window.DrainJobsForTest()
	if rec.flushes <= before {
		t.Fatalf("bound sync (list grow) did not flush host styles (flushes stayed %d)", before)
	}
}
