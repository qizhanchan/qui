package reactive_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

func childTexts(t *testing.T, root qui.Widget) []string {
	t.Helper()
	box, ok := root.(*qui.Container)
	if !ok {
		t.Fatalf("root is %T, want *qui.Container", root)
	}
	out := make([]string, 0, box.ChildCount())
	for _, c := range box.Children() {
		switch w := c.(type) {
		case *qw.Label:
			out = append(out, w.Text())
		case *qw.Button:
			out = append(out, w.Text)
		default:
			out = append(out, fmt.Sprintf("%T", c))
		}
	}
	return out
}

// A fragment's children splice into the parent as direct siblings — no
// wrapper container appears in the widget tree.
func TestFragmentSplicesIntoParent(t *testing.T) {
	var r reactive.Reconciler
	root, _ := r.Render(uiVBox().Children(
		uiLabel("before"),
		uiFrag(uiLabel("f1"), uiLabel("f2")),
		uiLabel("after"),
	).Build())

	got := childTexts(t, root)
	want := []string{"before", "f1", "f2", "after"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
}

// A component whose render returns a Fragment contributes multiple
// sibling widgets to its parent, and re-rendering it (fragment shrinks
// or grows) re-syncs the parent's child list.
func TestComponentReturningFragment(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	expandable := func() reactive.Element {
		return reactive.Component("Expandable", "", struct{}{}, func(struct{}) reactive.Element {
			open, set := reactive.UseState(false)
			return reactive.Fragment(
				uiButton("row").OnClick(func() { set(!open) }).Build(),
				uiWhen(open, func() uiNode { return uiLabel("detail") }).Build(),
			)
		})
	}

	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiLabel("top"),
			uiElem(expandable()),
			uiLabel("bottom"),
		).Build()
	})
	rt.Render()

	got := childTexts(t, rt.Root())
	if fmt.Sprint(got) != fmt.Sprint([]string{"top", "row", "bottom"}) {
		t.Fatalf("collapsed children = %v", got)
	}

	// Click the row → fragment grows to two widgets, spliced in place.
	rt.Root().(*qui.Container).ChildAt(1).(*qw.Button).OnClick()
	rt.Flush()
	got = childTexts(t, rt.Root())
	if fmt.Sprint(got) != fmt.Sprint([]string{"top", "row", "detail", "bottom"}) {
		t.Fatalf("expanded children = %v", got)
	}

	// Collapse again — and the stable siblings keep their instances.
	top1 := rt.Root().(*qui.Container).ChildAt(0)
	rt.Root().(*qui.Container).ChildAt(1).(*qw.Button).OnClick()
	rt.Flush()
	got = childTexts(t, rt.Root())
	if fmt.Sprint(got) != fmt.Sprint([]string{"top", "row", "bottom"}) {
		t.Fatalf("re-collapsed children = %v", got)
	}
	if rt.Root().(*qui.Container).ChildAt(0) != top1 {
		t.Fatal("fragment toggle remounted an unrelated sibling")
	}
}

// Fragment as runtime root is rejected loudly.
func TestFragmentRootPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for fragment root")
		}
	}()
	var r reactive.Reconciler
	r.Render(reactive.Fragment(uiLabel("a").Build()))
}
