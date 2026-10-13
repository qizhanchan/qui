package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func tabDoc(t *testing.T, html string) (*qui.Window, RenderResult) {
	t.Helper()
	res := RenderDoc(html, `div { height: 24px }`, Options{})
	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 400, H: 400})
	return win, res
}

// tabCycle presses Tab n times from no focus and records the ids reached.
func tabCycle(t *testing.T, win *qui.Window, res RenderResult, n int) []string {
	t.Helper()
	ids := map[qui.Widget]string{}
	for id, w := range res.ByID {
		ids[w] = id
		if el, ok := w.(*El); ok && el.backing != nil {
			ids[el.backing] = id // a control's focus lands on its backing
		}
	}
	win.ClearFocus()
	var got []string
	for i := 0; i < n; i++ {
		win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyTab, 0))
		got = append(got, ids[win.Focused()])
	}
	return got
}

func TestTabIndexMakesElementFocusable(t *testing.T) {
	win, res := tabDoc(t, `<body>
		<div id="plain">plain</div>
		<div id="zero" tabindex="0">zero</div>
		<div id="neg" tabindex="-1">neg</div>
		<div id="bad" tabindex="x">bad</div>
	</body>`)
	focusable := func(id string) bool { return res.ByID[id].(*El).Focusable() }
	if focusable("plain") || focusable("bad") {
		t.Error("an element without a valid tabindex must not be focusable")
	}
	if !focusable("zero") || !focusable("neg") {
		t.Error("tabindex=0 and tabindex=-1 must both be focusable")
	}
	if got := tabCycle(t, win, res, 2); got[0] != "zero" || got[1] != "zero" {
		t.Errorf("Tab cycle = %v, want only #zero (-1 is not a Tab stop)", got)
	}
	// -1 still takes focus programmatically.
	if !res.ByID["neg"].(*El).RequestFocus() {
		t.Error("tabindex=-1 should accept RequestFocus")
	}
}

// HTML order: positive indices ascending (ties in document order), then
// tabindex=0 and natively focusable elements in document order.
func TestTabIndexOrder(t *testing.T) {
	win, res := tabDoc(t, `<body>
		<button id="b">b</button>
		<div id="d3" tabindex="3">d3</div>
		<input id="i1" tabindex="1">
		<div id="z" tabindex="0">z</div>
		<div id="d1" tabindex="1">d1</div>
		<input id="skip" tabindex="-1">
		<input id="last">
	</body>`)
	want := []string{"i1", "d1", "d3", "b", "z", "last", "i1"}
	got := tabCycle(t, win, res, len(want))
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Tab order = %v, want %v", got, want)
		}
	}
}

// A click on text inside a tabindex container focuses the container, not
// the text label — the container is the control.
func TestTabIndexClickFocusesContainer(t *testing.T) {
	win, res := tabDoc(t, `<body><div id="tree" tabindex="0"><div id="row">row text</div></div></body>`)
	if err := win.Click("#row", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if win.Focused() != res.ByID["tree"] {
		t.Errorf("focused = %T %v, want #tree", win.Focused(), win.Focused())
	}
}

// Setting / removing the attribute at runtime toggles focusability.
func TestTabIndexDynamic(t *testing.T) {
	_, res := tabDoc(t, `<body><div id="d">d</div></body>`)
	d := res.ByID["d"].(*El)
	d.SetAttr("tabindex", "0")
	if !d.Focusable() {
		t.Fatal("SetAttr(tabindex) should make the element focusable")
	}
	d.RemoveAttr("tabindex")
	if d.Focusable() {
		t.Error("removing tabindex should make it unfocusable again")
	}
}

// A tabindex scroll container is ONE focus target: Tab stops on the
// element (not also on its internal ScrollView), a click on blank space
// inside it focuses the element, and the arrow keys scroll it.
func TestTabIndexScrollContainer(t *testing.T) {
	eng := NewStyleEngine(`
		.list { width:200px; height:100px; overflow-y:auto }
		.row  { height:20px }`)
	list := eng.NewEl("div")
	list.SetClass("list")
	list.SetAttr("tabindex", "0")
	rows := make([]qui.Widget, 20)
	for i := range rows {
		r := eng.NewEl("div")
		r.SetClass("row")
		rows[i] = r
	}
	list.SetElementChildren(rows)
	eng.SetRoot(list)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(list)
	eng.Restyle()
	list.Layout(qui.Rect{W: 200, H: 100})

	if got := win.CollectFocusables(); len(got) != 1 || got[0] != qui.Widget(list) {
		t.Fatalf("focusables = %v, want just the list element", got)
	}
	// A press on a blank row lands on the row El (not focusable) → walks up
	// past the delegated ScrollView to the list.
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 10, 30, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, 10, 30, qui.MouseButtonLeft, 0))
	if win.Focused() != qui.Widget(list) {
		t.Fatalf("click focused %T, want the list element", win.Focused())
	}
	sv := list.scrollView
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyDown, 0))
	if sv.ScrollOffset() <= 0 {
		t.Errorf("Down on the focused list did not scroll (offset %v)", sv.ScrollOffset())
	}
	// Without tabindex the ScrollView stays its own focus target.
	list.RemoveAttr("tabindex")
	eng.Restyle()
	if !sv.Focusable() {
		t.Error("removing tabindex should hand focus back to the ScrollView")
	}
}
