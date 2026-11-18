package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A hover menu: `li:hover > a:not(.closed) ~ .submenu { display: flex }`
// gives a display:none element a box while an ancestor (reached through a
// sibling combinator) is hovered, and takes it away on leave. No paint swap
// can show that — the hover boundary must restyle and re-lay out the
// submenu (go.dev's header menus).
func TestAncestorHoverChangesLayout(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><ul class="menu"><li class="item"><a class="hov" href="#">Why</a><div hidden>x</div>`+
			`<ul class="sub"><li>Case studies</li><li>Use cases</li></ul></li></ul></body>`,
		`body { margin: 0; }
		 ul { margin: 0; padding: 0; list-style: none; }
		 .item { position: relative; display: inline-flex; }
		 .sub { display: none; position: absolute; top: 20px; left: 0; width: 120px; flex-direction: column; }
		 .item:hover > .hov:not(.closed) ~ .sub { display: flex; }`,
		Options{ViewportWidth: 400, DocumentRoot: true, Window: win},
	)
	win.SetRoot(res.Root)
	var item, sub *El
	var walk func(e *El)
	walk = func(e *El) {
		switch {
		case e.node.hasClass("item"):
			item = e
		case e.node.hasClass("sub"):
			sub = e
		}
		for _, k := range e.elementKids {
			if ke, ok := k.(*El); ok {
				walk(ke)
			}
		}
	}
	walk(res.Root.(*El))
	if item == nil || sub == nil {
		t.Fatal("missing .item / .sub")
	}
	layout := func() {
		sz := res.Root.Measure(qui.Size{W: 400})
		res.Root.Layout(qui.Rect{W: 400, H: sz.H})
	}
	layout()
	if !sub.displayNone {
		t.Fatal("resting .sub has a box")
	}
	item.Handle(qui.NewMouseEvent(qui.EventMouseEnter, 2, 2, 0, 0))
	win.DrainJobsForTest()
	layout()
	if sub.displayNone || sub.lastCS.Display != "flex" {
		t.Fatalf("hovered .sub display = %q, want flex", sub.lastCS.Display)
	}
	if b := sub.Bounds(); b.W != 120 || b.H <= 0 {
		t.Errorf("hovered .sub bounds = %+v, want 120 wide with its items", b)
	}
	item.Handle(qui.NewMouseEvent(qui.EventMouseLeave, 2, 2, 0, 0))
	win.DrainJobsForTest()
	layout()
	if !sub.displayNone {
		t.Error(".sub kept its box after the hover left")
	}
}
