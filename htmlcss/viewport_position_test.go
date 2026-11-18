package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A document in a FixedViewport scroll view: fixed boxes stay on the
// visible area, sticky boxes stick within their parent, as in a browser.
func TestFixedAndStickyFollowScroll(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(`<!doctype html><html><body style="margin:0">
<div id=bar style="position:fixed;top:0;left:0;right:0;height:50px;background:#333"></div>
<div id=corner style="position:fixed;bottom:10px;right:10px;width:30px;height:20px"></div>
<div style="height:300px"></div>
<section id=sec style="height:800px">
  <h2 id=st style="position:sticky;top:50px;height:40px;margin:0">Sticky</h2>
</section>
<div style="height:2000px"></div>
</body></html>`, "", Options{Window: win, DocumentRoot: true, ViewportWidth: 400})
	sv := widgets.NewScrollView()
	sv.FixedViewport = true
	sv.OverlayBar = true
	sv.SetContent(res.Root, qui.Size{W: 400})
	win.SetRoot(sv)
	sv.Measure(qui.Size{W: 400, H: 300})
	sv.Layout(qui.Rect{W: 400, H: 300})

	bar, corner, st := res.ByID["bar"], res.ByID["corner"], res.ByID["st"]
	check := func(scroll float32, barY, cornerY, stY float32) {
		t.Helper()
		sv.ScrollTo(scroll)
		if got := bar.Bounds(); got.Y != barY || got.W != 400 || got.H != 50 {
			t.Errorf("scroll %v: bar %v, want y=%v 400×50", scroll, got, barY)
		}
		if got := corner.Bounds(); got.Y != cornerY || got.X != 360 {
			t.Errorf("scroll %v: corner %v, want (360, %v)", scroll, got, cornerY)
		}
		if got := st.Bounds(); got.Y != stY {
			t.Errorf("scroll %v: sticky y=%v, want %v", scroll, got.Y, stY)
		}
	}
	check(0, 0, 270, 300)         // in flow
	check(500, 500, 770, 550)     // stuck 50px under the top
	check(1200, 1200, 1470, 1060) // pushed out by its section's end
	check(100, 100, 370, 300)     // back in flow
	// Clicks at the bar's screen spot reach it wherever the page is.
	sv.ScrollTo(700)
	if hit := win.HitTestForTest(qui.Point{X: 200, Y: 20}); hit != bar {
		t.Errorf("hit at the fixed bar = %v", hit)
	}
	// A fixed box does not make the document scroll further.
	if max := sv.MaxScroll(); max != 300+800+2000-300 {
		t.Errorf("max scroll %v", max)
	}
}
