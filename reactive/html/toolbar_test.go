package html_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// Five groups of three 40px buttons; every group but the first carries a
// 9px divider (1px + 4px margins), so the widths are 120, 129, 129, 129, 129
// and the More wrapper (divider + 30px button) is 39.
const toolbarTestCSS = `
body { margin: 0; padding: 0; }
.tb { display: flex; padding: 0 10px; gap: 0; height: 40px; }
.q-toolbar-group { gap: 0; }
.b { width: 40px; height: 30px; padding: 0; margin: 0; border-width: 0; }
.d { width: 1px; height: 20px; margin: 0 4px; }
.q-toolbar-more { width: 30px; min-width: 0; padding: 0; margin: 0; border-width: 0; }
`

type toolbarFixture struct {
	win    *qui.Window
	clicks map[string]int
}

func mountToolbarFixture(t *testing.T, width float32) *toolbarFixture {
	t.Helper()
	return mountToolbarFixtureCSS(t, width, "")
}

func mountToolbarFixtureCSS(t *testing.T, width float32, extraCSS string) *toolbarFixture {
	t.Helper()
	f := &toolbarFixture{
		win:    qui.NewTestWindow(qui.Size{W: width, H: 300}),
		clicks: map[string]int{},
	}
	h.Mount(f.win, toolbarTestCSS+extraCSS, func() h.Node {
		groups := make([]h.ToolbarGroup, 5)
		for i := range groups {
			items := make([]h.Node, 3)
			for j := range items {
				name := fmt.Sprintf("g%db%d", i, j)
				items[j] = h.Button(name).Class("b").OnClick(func() { f.clicks[name]++ })
			}
			groups[i] = h.ToolGroup(fmt.Sprintf("g%d", i), items...)
		}
		return h.Div(h.Toolbar(h.ToolbarOptions{Class: "tb", DividerClass: "d"}, groups...))
	})
	f.win.LayoutForTest()
	return f
}

func findOne(t *testing.T, win *qui.Window, sel string) qui.Widget {
	t.Helper()
	w, err := win.Find(sel)
	if err != nil {
		t.Fatalf("Find(%q): %v", sel, err)
	}
	return w
}

func clickWidget(win *qui.Window, w qui.Widget) {
	b := qui.InteractionBoundsOf(w)
	x, y := b.X+b.W/2, b.Y+b.H/2
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	win.LayoutForTest()
}

func overflowDiagnostics(win *qui.Window) []string {
	var out []string
	for _, d := range win.DebugLayoutDiagnostics() {
		if strings.Contains(d.Message, "overflows parent") {
			out = append(out, d.Path)
		}
	}
	return out
}

func visibleInRoot(t *testing.T, win *qui.Window, name string) bool {
	t.Helper()
	return findOne(t, win, fmt.Sprintf(`:layer(root) [name=%q]:visible`, name)) != nil
}

func TestToolbarAllFit(t *testing.T) {
	f := mountToolbarFixture(t, 656)
	for _, name := range []string{"g0b0", "g2b2", "g4b2"} {
		if !visibleInRoot(t, f.win, name) {
			t.Errorf("%s hidden at 656px; everything fits", name)
		}
	}
	if findOne(t, f.win, `[name="More"]:visible`) != nil {
		t.Errorf("More button visible though everything fits")
	}
	if d := overflowDiagnostics(f.win); len(d) != 0 {
		t.Errorf("overflow diagnostics: %v", d)
	}
}

func TestToolbarOverflowsTrailingGroups(t *testing.T) {
	f := mountToolbarFixture(t, 500)
	for _, name := range []string{"g0b0", "g2b2"} {
		if !visibleInRoot(t, f.win, name) {
			t.Errorf("%s hidden at 500px, want it in the row", name)
		}
	}
	for _, name := range []string{"g3b0", "g4b2"} {
		if visibleInRoot(t, f.win, name) {
			t.Errorf("%s visible at 500px, want it overflowed", name)
		}
	}
	more := findOne(t, f.win, `[name="More"]:visible`)
	if more == nil {
		t.Fatalf("More button not visible at 500px")
	}
	g2 := findOne(t, f.win, `:layer(root) [name="g2b2"]`)
	mb, gb := qui.InteractionBoundsOf(more), qui.InteractionBoundsOf(g2)
	if mb.X < gb.X+gb.W || mb.X > gb.X+gb.W+20 {
		t.Errorf("More at x=%v, want just after group 2 (ends %v)", mb.X, gb.X+gb.W)
	}
	if mb.X+mb.W > 490.5 {
		t.Errorf("More ends at %v, past the row's content box (490)", mb.X+mb.W)
	}
	if d := overflowDiagnostics(f.win); len(d) != 0 {
		t.Errorf("overflow diagnostics: %v", d)
	}
	if n := len(f.win.Overlays()); n != 0 {
		t.Fatalf("overlays before click = %d", n)
	}

	clickWidget(f.win, more)
	if n := len(f.win.Overlays()); n != 1 {
		t.Fatalf("overlays after More click = %d, want 1", n)
	}
	for _, name := range []string{"g3b0", "g4b2"} {
		if findOne(t, f.win, fmt.Sprintf(`:layer(topmost) [name=%q]:visible`, name)) == nil {
			t.Errorf("%s missing from the overflow panel", name)
		}
	}
	if findOne(t, f.win, `:layer(topmost) [name="g2b0"]`) != nil {
		t.Errorf("g2b0 in the overflow panel; it fits in the row")
	}
	b := findOne(t, f.win, `:layer(topmost) [name="g4b0"]:visible`)
	if b == nil {
		t.Fatalf("g4b0 not in panel")
	}
	clickWidget(f.win, b)
	if f.clicks["g4b0"] != 1 {
		t.Errorf("g4b0 clicks from panel = %d, want 1", f.clicks["g4b0"])
	}
	if n := len(f.win.Overlays()); n != 1 {
		t.Errorf("panel closed after clicking an item inside it (overlays=%d)", n)
	}

	// Growing the window brings every group back and drops the panel.
	f.win.ResizeForTest(qui.Size{W: 900, H: 300})
	f.win.LayoutForTest()
	for _, name := range []string{"g3b0", "g4b2"} {
		if !visibleInRoot(t, f.win, name) {
			t.Errorf("%s hidden after resize to 900", name)
		}
	}
	if findOne(t, f.win, `[name="More"]:visible`) != nil {
		t.Errorf("More visible after resize to 900")
	}
	if n := len(f.win.Overlays()); n != 0 {
		t.Errorf("overlays after resize to 900 = %d, want 0", n)
	}
}

func TestToolbarNarrowKeepsFirstGroup(t *testing.T) {
	f := mountToolbarFixture(t, 200)
	if !visibleInRoot(t, f.win, "g0b2") {
		t.Errorf("g0b2 hidden at 200px")
	}
	if visibleInRoot(t, f.win, "g1b0") {
		t.Errorf("g1b0 visible at 200px, want overflowed")
	}
	if findOne(t, f.win, `[name="More"]:visible`) == nil {
		t.Errorf("More not visible at 200px")
	}
}

// A narrow toolbar in a wide window: the four overflowed groups (507px)
// exceed the panel's 480px max-width though the window could fit them on
// one line. The panel wraps onto a second line and must grow to hold it,
// or that line would sit outside the popover and a press on it would
// dismiss the panel instead of clicking the control.
func TestToolbarPanelWrapsInsideItself(t *testing.T) {
	f := mountToolbarFixtureCSS(t, 900, `.tb { width: 200px; }`)
	if visibleInRoot(t, f.win, "g1b0") || !visibleInRoot(t, f.win, "g0b2") {
		t.Fatalf("want exactly group 0 in a 200px toolbar")
	}
	more := findOne(t, f.win, `[name="More"]:visible`)
	if more == nil {
		t.Fatalf("More not visible")
	}
	clickWidget(f.win, more)
	ovs := f.win.Overlays()
	if len(ovs) != 1 {
		t.Fatalf("overlays after More = %d", len(ovs))
	}
	panel := findClass(ovs[0], "q-toolbar-overflow")
	if panel == nil {
		t.Fatalf("no .q-toolbar-overflow in the overlay")
	}
	pb := panel.Bounds()
	first := qui.InteractionBoundsOf(findOne(t, f.win, `:layer(topmost) [name="g1b0"]`))
	last := findOne(t, f.win, `:layer(topmost) [name="g4b2"]:visible`)
	if last == nil {
		t.Fatalf("g4b2 not in the panel")
	}
	lb := qui.InteractionBoundsOf(last)
	if lb.Y <= first.Y {
		t.Errorf("panel did not wrap: g4b2 at y=%v, g1b0 at y=%v", lb.Y, first.Y)
	}
	if lb.Y+lb.H > pb.Y+pb.H+0.5 || lb.X+lb.W > pb.X+pb.W+0.5 {
		t.Errorf("wrapped item %v sticks out of the panel %v", lb, pb)
	}
	clickWidget(f.win, last)
	if f.clicks["g4b2"] != 1 || len(f.win.Overlays()) != 1 {
		t.Errorf("click on the wrapped line: clicks=%d overlays=%d, want 1 / 1",
			f.clicks["g4b2"], len(f.win.Overlays()))
	}
}

// A parent re-render (new group builders, same keys) keeps the fit.
func TestToolbarParentRerenderKeepsFit(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 500, H: 300})
	var setTick func(int)
	rt := h.Mount(win, toolbarTestCSS, func() h.Node {
		tick, set := reactive.UseState(0)
		setTick = set
		groups := make([]h.ToolbarGroup, 5)
		for i := range groups {
			items := make([]h.Node, 3)
			for j := range items {
				items[j] = h.Button(fmt.Sprintf("g%db%d", i, j)).Class("b")
			}
			groups[i] = h.ToolGroup(fmt.Sprintf("g%d", i), items...)
		}
		return h.Div(h.Span(fmt.Sprint(tick)), h.Toolbar(h.ToolbarOptions{Class: "tb", DividerClass: "d"}, groups...))
	})
	win.LayoutForTest()
	if visibleInRoot(t, win, "g3b0") || !visibleInRoot(t, win, "g2b2") {
		t.Fatalf("initial fit wrong")
	}
	setTick(1)
	rt.Flush()
	win.LayoutForTest()
	if visibleInRoot(t, win, "g3b0") || !visibleInRoot(t, win, "g2b2") {
		t.Errorf("fit changed after parent re-render")
	}

	// …and so does an open panel.
	more := findOne(t, win, `[name="More"]:visible`)
	clickWidget(win, more)
	if n := len(win.Overlays()); n != 1 {
		t.Fatalf("overlays after More = %d", n)
	}
	setTick(2)
	rt.Flush()
	win.LayoutForTest()
	if n := len(win.Overlays()); n != 1 {
		t.Errorf("parent re-render closed the panel (overlays=%d)", n)
	}
	if findOne(t, win, `:layer(topmost) [name="g4b0"]:visible`) == nil {
		t.Errorf("panel lost its items after parent re-render")
	}
}

// OnClickEl hands each mounted copy of a builder its own element.
func TestOnClickElMountedTwice(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var got []*htmlcss.El
	h.Mount(win, `.x { width: 50px; height: 20px; }`, func() h.Node {
		b := h.Button("dup").Class("x").OnClickEl(func(el *htmlcss.El) { got = append(got, el) })
		return h.Div(h.Div(b).Class("one"), h.Div(b).Class("two"))
	})
	win.LayoutForTest()
	all, err := win.FindAll(`[role=button][name="dup"]`)
	if err != nil || len(all) != 2 {
		t.Fatalf("FindAll dup = %d, %v", len(all), err)
	}
	clickWidget(win, all[0])
	clickWidget(win, all[1])
	if len(got) != 2 || got[0] == nil || got[1] == nil || got[0] == got[1] {
		t.Fatalf("OnClickEl elements = %v, want two distinct", got)
	}
	if qui.Widget(got[0]) != all[0] || qui.Widget(got[1]) != all[1] {
		t.Errorf("OnClickEl handed the wrong element")
	}
}
