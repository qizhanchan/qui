package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// overflowXY lays out a 160×80 scroll box holding a nowrap row wider than
// it, and returns the box's horizontal scroll range.
func overflowXY(t *testing.T, overflow string) float32 {
	t.Helper()
	html := `<style>
.box { width: 160px; height: 80px; ` + overflow + ` }
.rows { display: flex; flex-direction: column; align-self: flex-start; }
.row { display: flex; white-space: nowrap; height: 20px; }
.name { flex-shrink: 0; }
</style><div class="box" id="box"><div class="row"><span class="name">a_very_long_name_that_does_not_fit_in_the_box.go</span></div>` +
		`<div style="height: 200px"></div></div>`
	win := qui.NewTestWindow(qui.Size{W: 640, H: 480})
	res := RenderDoc(html, "", Options{ViewportWidth: 640, Window: win})
	win.SetRoot(res.Root)
	sz := res.Root.Measure(qui.Size{W: 640})
	res.Root.Layout(qui.Rect{W: 640, H: max(sz.H, 480)})
	box, ok := res.ByID["box"].(*El)
	if !ok || box.scrollView == nil {
		t.Fatalf("%s: #box is not a scroll container", overflow)
	}
	return box.scrollView.MaxScrollX()
}

// `overflow: auto` scrolls both axes; `overflow-y: auto` alone does too
// (visible beside a scrolling axis computes to auto, as in Chrome); an
// explicit `overflow-x: hidden` keeps X from scrolling.
func TestOverflowScrollsBothAxes(t *testing.T) {
	for _, c := range []struct {
		css  string
		want bool
	}{
		{"overflow: auto;", true},
		{"overflow-y: auto;", true},
		{"overflow-y: auto; overflow-x: hidden;", false},
		{"overflow: hidden auto;", false},
	} {
		if got := overflowXY(t, c.css) > 0; got != c.want {
			t.Errorf("%s scrolls sideways = %v, want %v", c.css, got, c.want)
		}
	}
}
