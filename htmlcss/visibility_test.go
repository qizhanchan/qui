package htmlcss

import (
	"errors"
	"testing"

	"github.com/qizhanchan/qui"
)

// visibility:hidden keeps the element's layout box (unlike display:none) but
// paints nothing.
func TestVisibilityHiddenKeepsLayout(t *testing.T) {
	res := RenderDoc(`<body>
		<div id="a" style="height:30px">a</div>
		<div id="b" style="height:30px;visibility:hidden">b</div>
		<div id="c" style="height:30px">c</div>
	</body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 200})

	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	c := res.ByID["c"].(*El).Bounds()
	// The hidden box still occupies its row: c sits below b, which sits below a.
	if !(a.Y < b.Y && b.Y < c.Y) {
		t.Errorf("hidden element should keep layout space: a.Y=%v b.Y=%v c.Y=%v", a.Y, b.Y, c.Y)
	}
	if el := res.ByID["b"].(*El); !el.hidden {
		t.Error("visibility:hidden element should have hidden=true")
	}
	if el := res.ByID["a"].(*El); el.hidden {
		t.Error("visible element should not be hidden")
	}
}

func TestVisibilityHiddenRemovesInteractionAndAXVisibility(t *testing.T) {
	res := RenderDoc(
		`<body><button id="hidden">Hidden action</button><button id="shown">Shown</button></body>`,
		`#hidden { visibility: hidden; width: 120px; height: 30px; }
		 #shown { width: 120px; height: 30px; }`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 300, H: 120})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 120})
	hidden := res.ByID["hidden"].(*El)

	point := qui.Point{X: hidden.Bounds().X + hidden.Bounds().W/2, Y: hidden.Bounds().Y + hidden.Bounds().H/2}
	if hit := res.Root.HitTest(point); hit == hidden {
		t.Fatal("visibility:hidden element remained hit-testable")
	}
	nodes, err := win.FindNodes("#hidden")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("FindNodes(#hidden) = %v, %v", nodes, err)
	}
	if nodes[0].Visible {
		t.Fatal("visibility:hidden element reported AX-visible")
	}
	if err := win.Click("#hidden", qui.ClickOptions{}); !errors.Is(err, qui.ErrNotVisible) {
		t.Fatalf("Click(#hidden) error = %v, want ErrNotVisible", err)
	}
}
