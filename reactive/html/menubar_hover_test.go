package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	qw "github.com/qizhanchan/qui/widgets"
)

// A bar of h.Button triggers that open their menus with AnchorMenu slides
// between menus on hover once one is open.
func TestMenuBarTriggersSwitchOnHover(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 600, H: 300})
	opened := []string{}
	app := func() h.Node {
		var nodes []h.Node
		for _, name := range []string{"File", "Edit", "View"} {
			name := name
			ref := reactive.UseRef[*htmlcss.El](nil)
			nodes = append(nodes, h.Button(name).Class("menu-btn").
				OnClick(func() {
					opened = append(opened, name)
					h.AnchorMenu(*ref, []h.MenuItem{{Label: name + " item"}})
				}).
				Ref(func(el *htmlcss.El) { *ref = el }))
		}
		return h.Div(h.Div(nodes).Class("menubar"))
	}
	css := `.menubar { display: flex; flex-direction: row; } .menu-btn { width: 60px; height: 26px; }`
	rt := h.Mount(win, css, app)
	root := rt.Root().(*htmlcss.El)
	root.Layout(qui.Rect{W: 600, H: 300})
	bar := root.ChildList()[0].(*htmlcss.El)
	kids := bar.ChildList()
	if len(kids) != 3 {
		t.Fatalf("menubar has %d children", len(kids))
	}
	center := func(i int) qui.Point {
		b := kids[i].Bounds()
		return qui.Point{X: b.X + b.W/2, Y: b.Y + b.H/2}
	}
	move := func(p qui.Point) {
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, p.X, p.Y, qui.MouseButtonLeft, 0))
	}
	if !kids[0].(*htmlcss.El).Click() {
		t.Fatal("Click did not act")
	}
	if len(win.Overlays()) != 1 {
		t.Fatalf("File did not open a dropdown")
	}
	move(center(0))
	move(center(2))
	if len(win.Overlays()) != 1 || len(opened) != 2 || opened[1] != "View" {
		t.Fatalf("after hovering View: overlays=%d opened=%v", len(win.Overlays()), opened)
	}
	if v, _ := kids[2].(*htmlcss.El).Attr("aria-expanded"); v != "true" {
		t.Errorf("View trigger not marked expanded")
	}
	if _, ok := kids[0].(*htmlcss.El).Attr("aria-expanded"); ok {
		t.Errorf("File trigger still marked expanded")
	}
}

// MenuBarMenuStyled opens a dropdown with the caller's palette and marks
// the trigger expanded, like MenuBarMenu.
func TestMenuBarMenuStyled(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	ref := new(*htmlcss.El)
	rt := h.Mount(win, `.bar{display:flex} button{width:60px;height:24px}`, func() h.Node {
		return h.Div(h.Button("File").RefTo(ref)).Class("menubar bar")
	})
	rt.Root().(*htmlcss.El).Layout(qui.Rect{W: 400, H: 300})
	dark := qw.MenuColors{Background: qui.Color{R: 0.14, G: 0.14, B: 0.14, A: 1}, Foreground: qui.Color{R: 0.75, G: 0.75, B: 0.75, A: 1}}
	h.MenuBarMenuStyled(*ref, []h.MenuItem{{Label: "Save", ID: "m-save"}}, dark, 2)
	if len(win.Overlays()) != 1 {
		t.Fatalf("overlays = %d", len(win.Overlays()))
	}
	if v, _ := (*ref).Attr("aria-expanded"); v != "true" {
		t.Error("trigger not marked expanded")
	}
}
