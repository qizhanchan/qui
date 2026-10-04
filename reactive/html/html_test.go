package html_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// Clicking a button re-renders the component and updates the element's
// text in place (reused instance), proving hooks + events over htmlcss.
func TestClickUpdatesText(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	app := func() h.Node {
		n, set := reactive.UseState(0)
		return h.Div(
			h.Button(fmt.Sprintf("count %d", n)).ID("btn").
				OnClick(func() { set(n + 1) }),
		)
	}
	rt := h.Mount(win, ``, app)

	root := rt.Root().(*htmlcss.El)
	btn := root.ChildList()[0].(*htmlcss.El)
	if got := btn.AccessibleName(); got != "count 0" {
		t.Fatalf("initial button text = %q", got)
	}

	// Synthetic left mouse-up fires the El's click handler → setState.
	btn.Handle(qui.NewMouseEvent(qui.EventMouseUp, 1, 1, qui.MouseButtonLeft, 0))
	rt.Flush()

	btn2 := rt.Root().(*htmlcss.El).ChildList()[0].(*htmlcss.El)
	if btn2 != btn {
		t.Fatalf("button instance was not reused across re-render")
	}
	if got := btn2.AccessibleName(); got != "count 1" {
		t.Fatalf("after click, button text = %q, want count 1", got)
	}
}

// Two windows mounted with DIFFERENT stylesheets must each style against
// their own engine — including elements created by LATER re-renders of the
// first window (the historical package-global engine broke exactly this).
func TestTwoWindowsResolveOwnEngines(t *testing.T) {
	winA := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	winB := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	// Block-level rows (P) so each stays its own widget with an
	// inspectable Label — inline spans would fold into one InlineBox.
	showExtra := false
	appA := func() h.Node {
		kids := []h.Node{h.P("a").Class("probe")}
		if showExtra {
			kids = append(kids, h.P("extra").Class("probe").Key("extra"))
		}
		return h.Div(kids)
	}
	rtA := h.Mount(winA, `.probe { color: #ff0000; }`, appA)
	h.Mount(winB, `.probe { color: #0000ff; }`, func() h.Node {
		return h.Div(h.P("b").Class("probe"))
	})

	// Re-render window A AFTER window B mounted; the new element must pick
	// up A's red stylesheet, not B's blue one.
	showExtra = true
	rtA.Render()

	rows := rtA.Root().(*htmlcss.El).ChildList()
	if len(rows) != 2 {
		t.Fatalf("want 2 rows in window A, got %d", len(rows))
	}
	red := qui.Color{R: 1, A: 1}
	for i, w := range rows {
		lbl := w.(*htmlcss.El).ChildList()[0]
		if got := lbl.Style().Foreground; got != red {
			t.Fatalf("window A row %d color = %+v, want red (window B's engine leaked in)", i, got)
		}
	}
}

// A keyed list reuses element instances for surviving keys when an item is
// removed, and drops the removed one.
func TestKeyedListReuse(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	items := []string{"a", "b", "c"}
	// Block-level rows (Li) so they stay separate widgets — plain inline
	// spans would fold into the parent's inline formatting context.
	// Value-based keys so a surviving item keeps its key across renders.
	render := func() h.Node {
		rows := make([]h.Node, 0, len(items))
		for _, it := range items {
			rows = append(rows, h.Li(it).Key("k:"+it))
		}
		return h.Ul(rows)
	}
	rt := h.Mount(win, ``, render)

	before := rt.Root().(*htmlcss.El).ChildList()
	if len(before) != 3 {
		t.Fatalf("want 3 rows, got %d", len(before))
	}
	spanA, spanC := before[0], before[2]

	// Remove the middle item and re-render.
	items = []string{"a", "c"}
	rt.Render()

	after := rt.Root().(*htmlcss.El).ChildList()
	if len(after) != 2 {
		t.Fatalf("want 2 rows after removal, got %d", len(after))
	}
	// Surviving keyed elements must be the SAME instances (reused, not rebuilt).
	if after[0] != spanA || after[1] != spanC {
		t.Fatalf("keyed elements were not reused across removal")
	}
	for _, w := range after {
		if w.(*htmlcss.El).AccessibleName() == "b" {
			t.Fatalf("removed item 'b' still present")
		}
	}
}
