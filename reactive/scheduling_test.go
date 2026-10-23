package reactive_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// schedulingApp is a root → 100 sections → 10 rows tree (1,100
// components). Each row owns a counter; setters[i] updates row i.
type schedulingApp struct {
	rt          *reactive.Runtime
	setters     []func(int)
	rootRenders int
	rowRenders  int
	secRenders  int
}

func newSchedulingApp(tb testing.TB) *schedulingApp {
	tb.Helper()
	a := &schedulingApp{setters: make([]func(int), 1000)}
	window := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	row := func(i int) reactive.Element {
		return reactive.Component("Row", fmt.Sprint(i), i, func(i int) reactive.Element {
			n, set := reactive.UseState(0)
			a.setters[i] = set
			a.rowRenders++
			return uiLabel(fmt.Sprintf("row %d: %d", i, n)).Build()
		})
	}
	section := func(s int) reactive.Element {
		return reactive.Component("Section", fmt.Sprint(s), s, func(s int) reactive.Element {
			a.secRenders++
			kids := make([]uiNode, 10)
			for j := range kids {
				kids[j] = uiElem(row(s*10 + j))
			}
			return uiVBox().Children(kids...).Build()
		})
	}
	a.rt = reactive.NewRuntime(window, func() reactive.Element {
		a.rootRenders++
		kids := make([]uiNode, 100)
		for s := range kids {
			kids[s] = uiElem(section(s))
		}
		return uiVBox().Children(kids...).Build()
	})
	a.rt.Render()
	return a
}

func (a *schedulingApp) resetCounts() { a.rootRenders, a.secRenders, a.rowRenders = 0, 0, 0 }

// BenchmarkLeafSetState measures one leaf component's setState + flush in
// a 1,100-component tree.
func BenchmarkLeafSetState(b *testing.B) {
	a := newSchedulingApp(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.setters[500](i + 1)
		a.rt.Flush()
	}
}

// allLabelTexts walks the widget tree collecting every Label's text.
func allLabelTexts(w qui.Widget, out []string) []string {
	if l, ok := w.(*qw.Label); ok {
		out = append(out, l.Text())
	}
	if p, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, c := range p.ChildList() {
			out = allLabelTexts(c, out)
		}
	}
	return out
}

func hasLabel(w qui.Widget, text string) bool {
	for _, s := range allLabelTexts(w, nil) {
		if s == text {
			return true
		}
	}
	return false
}

// A component's setState re-renders that component only: neither the root
// render function nor any other component runs.
func TestLeafSetStateSkipsRootAndSiblings(t *testing.T) {
	a := newSchedulingApp(t)
	a.resetCounts()
	a.setters[537](9)
	a.rt.Flush()
	if a.rootRenders != 0 || a.secRenders != 0 || a.rowRenders != 1 {
		t.Fatalf("renders: root %d, sections %d, rows %d; want 0, 0, 1",
			a.rootRenders, a.secRenders, a.rowRenders)
	}
	if !hasLabel(a.rt.Root(), "row 537: 9") {
		t.Fatal("the updated row did not commit")
	}
	if p := a.rt.Profile(); p.Last.ScopedRenders != 1 {
		t.Fatalf("Last.ScopedRenders = %d, want 1", p.Last.ScopedRenders)
	}

	// Two leaves in one batch: both re-render, still without the root.
	a.resetCounts()
	a.setters[1](1)
	a.setters[999](1)
	a.rt.Flush()
	if a.rootRenders != 0 || a.rowRenders != 2 {
		t.Fatalf("batched leaves: root %d, rows %d; want 0, 2", a.rootRenders, a.rowRenders)
	}
}

// Root state, or a root request in the same batch as a leaf update, still
// runs the root render function — and the leaf's new state survives.
func TestRootUpdateStillRendersRoot(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	rootRenders := 0
	var setLeaf, setRoot func(int)
	leaf := reactive.Component("Leaf", "", struct{}{}, func(struct{}) reactive.Element {
		n, set := reactive.UseState(0)
		setLeaf = set
		return uiLabel(fmt.Sprint("leaf ", n)).Build()
	})
	rt := reactive.NewRuntime(window, func() reactive.Element {
		rootRenders++
		n, set := reactive.UseState(0)
		setRoot = set
		return uiVBox().Children(uiLabel(fmt.Sprint("root ", n)), uiElem(leaf)).Build()
	})
	rt.Render()
	rootRenders = 0
	setLeaf(1)
	setRoot(1)
	rt.Flush()
	if rootRenders != 1 {
		t.Fatalf("root rendered %d times, want 1", rootRenders)
	}
	if !hasLabel(rt.Root(), "root 1") || !hasLabel(rt.Root(), "leaf 1") {
		t.Fatalf("labels %v", allLabelTexts(rt.Root(), nil))
	}
	rootRenders = 0
	rt.RequestRender()
	rt.Flush()
	if rootRenders != 1 {
		t.Fatalf("RequestRender rendered the root %d times, want 1", rootRenders)
	}
}

// A component that panics while re-rendering on its own setState is caught
// by the enclosing ErrorBoundary, as in a full pass.
func TestScopedRenderPanicTripsBoundary(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var set func(int)
	bomb := reactive.Component("Bomb", "", struct{}{}, func(struct{}) reactive.Element {
		n, s := reactive.UseState(0)
		set = s
		if n == 1 {
			panic("boom")
		}
		return uiLabel("ok").Build()
	})
	var retry func()
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(uiElem(reactive.ErrorBoundary("guard", bomb,
			func(_ any, r func()) reactive.Element { retry = r; return uiLabel("fallback").Build() }))).Build()
	})
	rt.Render()
	set(1)
	rt.Flush()
	if !hasLabel(rt.Root(), "fallback") {
		t.Fatalf("labels %v, want the fallback", allLabelTexts(rt.Root(), nil))
	}
	set(2) // state no longer panics; retry re-renders the child
	retry()
	rt.Flush()
	if !hasLabel(rt.Root(), "ok") {
		t.Fatalf("after retry: labels %v", allLabelTexts(rt.Root(), nil))
	}
}

// A provider owned by a component: its setState (a scoped pass) still
// reaches a consumer below a memo bailout.
func TestScopedRenderPropagatesContext(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	ctx := reactive.NewContext("theme", "light")
	consumer := reactive.Component("Consumer", "", struct{}{}, func(struct{}) reactive.Element {
		return uiLabel("theme " + ctx.Use()).Build()
	})
	memo := reactive.MemoComponent("Wall", "", 0, func(a, b int) bool { return a == b },
		func(int) reactive.Element { return consumer })
	var setTheme func(string)
	owner := reactive.Component("Owner", "", struct{}{}, func(struct{}) reactive.Element {
		th, set := reactive.UseState("light")
		setTheme = set
		return ctx.Provide(th, memo)
	})
	rootRenders := 0
	rt := reactive.NewRuntime(window, func() reactive.Element {
		rootRenders++
		return uiVBox().Children(uiElem(owner)).Build()
	})
	rt.Render()
	rootRenders = 0
	setTheme("dark")
	rt.Flush()
	if rootRenders != 0 {
		t.Fatalf("root rendered %d times, want 0", rootRenders)
	}
	if !hasLabel(rt.Root(), "theme dark") {
		t.Fatalf("labels %v, want theme dark", allLabelTexts(rt.Root(), nil))
	}
}

// A root-level component that swaps its element kind on its own setState
// hands the window the new root widget.
func TestScopedRenderSwapsRootWidget(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var set func(bool)
	app := reactive.Component("App", "", struct{}{}, func(struct{}) reactive.Element {
		alt, s := reactive.UseState(false)
		set = s
		if alt {
			return uiLabel("alt").Build()
		}
		return uiVBox().Children(uiLabel("box")).Build()
	})
	rt := reactive.NewRuntime(window, func() reactive.Element { return app })
	rt.Render()
	set(true)
	rt.Flush()
	if l, ok := window.Root().(*qw.Label); !ok || l.Text() != "alt" {
		t.Fatalf("window root = %T, want the alt label", window.Root())
	}
}
