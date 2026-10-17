package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// A checkbox inserted by a re-render (not at mount) is a checkbox: its id
// restyles it before its type attribute is set.
func TestCheckboxInsertedLater(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	var show func(bool)
	toggled := 0
	rt := h.Mount(win, ``, func() h.Node {
		on, set := reactive.UseState(false)
		show = set
		if !on {
			return h.Div(h.Span("none"))
		}
		return h.Div(h.Label(h.Checkbox().ID("cb").OnToggle(func(bool) { toggled++ }), h.Span("x")))
	})
	win.SetRoot(rt.Root())
	win.DrainJobsForTest()
	rt.Flush()
	show(true)
	win.DrainJobsForTest()
	rt.Flush()
	win.LayoutForTest()
	var b qui.Rect
	var walk func(n *htmlcss.DOMNode)
	walk = func(n *htmlcss.DOMNode) {
		if n.ID == "cb" {
			b = n.Bounds
		}
		for _, k := range n.Children {
			walk(k)
		}
	}
	walk(htmlcss.InspectWindow(win).Root)
	t.Logf("%T", win.HitTestForTest(qui.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}))
	win.ClickAtWith(b.X+b.W/2, b.Y+b.H/2, qui.ClickOptions{})
	win.DrainJobsForTest()
	if toggled != 1 {
		t.Fatalf("toggled %d", toggled)
	}
}
