package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A flex item's z-index applies even when it is static (CSS Flexbox §
// 4.3): go.dev's header (static, z-index: 10, in a flex body) must stay
// above a later position:relative box, so its open menu is what the
// pointer hits — not the page under it.
func TestFlexItemZIndexStacksAbovePositionedSibling(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><header id="h"><div id="item"><div id="menu"></div></div></header>`+
			`<main><div id="later"></div></main></body>`,
		`body { margin: 0; display: flex; flex-direction: column; }
		 header { height: 40px; z-index: 10; }
		 #item { position: relative; width: 100px; height: 40px; }
		 #menu { position: absolute; top: 40px; left: 0; width: 200px; height: 150px; background: #fff; }
		 #later { position: relative; height: 200px; }`,
		Options{ViewportWidth: 400, ViewportHeight: 300, DocumentRoot: true, Window: win},
	)
	win.SetRoot(res.Root)
	sz := res.Root.Measure(qui.Size{W: 400})
	res.Root.Layout(qui.Rect{W: 400, H: sz.H})
	hit := res.Root.HitTest(qui.Point{X: 50, Y: 100})
	el, _ := hit.(*El)
	for el != nil && el.attrs["id"] == "" {
		el = el.elParent
	}
	if el == nil || el.attrs["id"] != "menu" {
		got := "<nil>"
		if el != nil {
			got = el.attrs["id"]
		}
		t.Fatalf("hit at the menu = #%s, want #menu (the z-index: 10 flex item stacks above #later)", got)
	}
}
