package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// ::before / ::after on an element with children are boxes beside the
// children — in a block flow and as flex items — never a replacement.
func TestPseudoElementBoxesKeepChildren(t *testing.T) {
	for _, mode := range []bool{false, true} {
		SetWebCompat(mode)
		src := `<html><head><style>
#c::before{display:block;height:20px;content:"banner"}
#f{display:flex}
#f::before{content:"*";width:10px}
#f::after{content:"!";width:10px}
.k{width:50px;height:10px}
</style></head><body style="margin:0"><div id="c"><div id="a" class="k"></div><p id="p" style="margin:0" class="k"></p></div><div id="f"><div id="x" class="k"></div><div id="y" class="k"></div></div></body></html>`
		win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
		res := RenderDoc(src, "", Options{Window: win, ViewportWidth: 400, DocumentRoot: true})
		win.SetRoot(res.Root)
		sz := res.Root.Measure(qui.Size{W: 400})
		res.Root.Layout(qui.Rect{W: 400, H: sz.H})
		pos := func(id string) qui.Point {
			w := res.ByID[id]
			if w == nil {
				t.Fatalf("webCompat=%v: no #%s", mode, id)
			}
			b := w.Bounds()
			return qui.LocalPointToWindow(w, qui.Point{X: b.X, Y: b.Y})
		}
		if a := pos("a"); a.Y != 20 {
			t.Errorf("webCompat=%v: #a should sit below the 20px ::before, y=%v", mode, a.Y)
		}
		if p := pos("p"); p.Y != 30 {
			t.Errorf("webCompat=%v: #p y=%v, want 30", mode, p.Y)
		}
		if x, y := pos("x"), pos("y"); x.X != 10 || y.X != 60 {
			t.Errorf("webCompat=%v: flex items should follow the 10px ::before item: x=%v y=%v", mode, x.X, y.X)
		}
	}
	SetWebCompat(false)
}
