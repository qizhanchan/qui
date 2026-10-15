package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A flex-grow column inside a scroll container (overflow-y:auto) grows to
// the scroller's height, not to the unbounded height the scroller measures
// its content against — and a flex-grow list inside that column takes
// what is left. The go-code Keymap page (scroll page > column > list)
// laid the list out ~16000px tall before the fix.
func TestFlexGrowInsideScrollContainer(t *testing.T) {
	eng := NewStyleEngine(`
.page { display:flex; flex-direction:column; width:300px; height:200px; overflow-y:auto }
.col  { display:flex; flex-direction:column; flex-grow:1; min-height:0 }
.head { height:20px }
.list { display:flex; flex-direction:column; flex-grow:1; min-height:0; overflow-y:auto }
.row  { height:30px }`)
	page := eng.NewEl("div")
	page.SetClass("page")
	col := eng.NewEl("div")
	col.SetClass("col")
	head := eng.NewEl("div")
	head.SetClass("head")
	list := eng.NewEl("div")
	list.SetClass("list")
	row := eng.NewEl("div")
	row.SetClass("row")
	list.SetElementChildren([]qui.Widget{row})
	col.SetElementChildren([]qui.Widget{head, list})
	page.SetElementChildren([]qui.Widget{col})
	eng.SetRoot(page)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(page)
	eng.Restyle()
	page.Layout(qui.Rect{W: 300, H: 200})

	if got := col.Bounds().H; got != 200 {
		t.Errorf("column height = %v, want 200 (the scroller's height)", got)
	}
	if got := list.Bounds().H; got != 180 {
		t.Errorf("list height = %v, want 180 (column minus the 20px head)", got)
	}
}

func scrollFixture(t *testing.T, css string, build func(eng *StyleEngine) []qui.Widget) *El {
	t.Helper()
	eng := NewStyleEngine(css)
	page := eng.NewEl("div")
	page.SetClass("page")
	page.SetElementChildren(build(eng))
	eng.SetRoot(page)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(page)
	eng.Restyle()
	page.Layout(qui.Rect{W: 300, H: 200})
	return page
}

func rows(eng *StyleEngine, n int) []qui.Widget {
	out := make([]qui.Widget, n)
	for i := range out {
		r := eng.NewEl("div")
		r.SetClass("row")
		out[i] = r
	}
	return out
}

// Content with nothing growing keeps its natural height: the scroll range
// is the rows, not the viewport.
func TestScrollContainerNaturalOverflowUnchanged(t *testing.T) {
	page := scrollFixture(t, `
.page { display:flex; flex-direction:column; width:300px; height:200px; overflow-y:auto }
.row  { height:30px; flex-shrink:0 }`, func(eng *StyleEngine) []qui.Widget { return rows(eng, 10) })
	if got := page.scrollView.ContentSize.H; got != 300 {
		t.Fatalf("scroll content height = %v, want 300", got)
	}
}

// A growing column whose rows outgrow the viewport still scrolls to all of
// them, and the scroll range stops there.
func TestScrollContainerGrowingColumnOverflows(t *testing.T) {
	page := scrollFixture(t, `
.page { display:flex; flex-direction:column; width:300px; height:200px; overflow-y:auto }
.col  { display:flex; flex-direction:column; flex-grow:1 }
.row  { height:30px; flex-shrink:0 }`, func(eng *StyleEngine) []qui.Widget {
		col := eng.NewEl("div")
		col.SetClass("col")
		col.SetElementChildren(rows(eng, 10))
		return []qui.Widget{col}
	})
	if got := page.scrollView.ContentSize.H; got != 300 {
		t.Fatalf("scroll content height = %v, want the rows' 300 (not the viewport, not the sentinel)", got)
	}
}

// Content taller than the old 8192 px probe height (a file tree with a big
// folder open) keeps its real scroll range, and changing its children
// keeps the scroll position. The probe used to cap the measure, so the
// tree read as "grows with the offer", collapsed to the viewport, and the
// scroll offset was clamped to 0 — the tree jumped back to the top.
func TestScrollContainerTallContentKeepsOffset(t *testing.T) {
	var eng *StyleEngine
	page := scrollFixture(t, `
.page { display:flex; flex-direction:column; width:300px; height:200px; overflow-y:auto }
.row  { height:30px }`, func(e *StyleEngine) []qui.Widget { eng = e; return rows(e, 300) })
	if got := page.scrollView.ContentSize.H; got != 9000 {
		t.Fatalf("scroll content height = %v, want 9000", got)
	}
	page.scrollView.ScrollTo(4000)
	page.SetElementChildren(rows(eng, 360))
	page.Layout(qui.Rect{W: 300, H: 200})
	if got := page.scrollView.ScrollY(); got != 4000 {
		t.Errorf("scroll offset after the children changed = %v, want 4000", got)
	}
	if got := page.scrollView.ContentSize.H; got != 10800 {
		t.Errorf("scroll content height = %v, want 10800", got)
	}
}
