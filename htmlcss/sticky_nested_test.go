package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Sticky headers inside a nested overflow:auto list (DocSearch's result
// dropdown in a fixed modal) stick to that list, not to the page. When the
// list shrinks and stops overflowing, they must sit in flow — not be
// pinned against the page viewport's top, above the search field.
func TestStickyInNestedScrollerAfterShrink(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(`<!doctype html><html><head><style>
body { margin: 0 }
#modal { position: fixed; top: 20px; left: 0; width: 300px }
#bar { height: 60px }
#dd { overflow-y: auto; max-height: 200px }
.src { position: sticky; top: 0; height: 30px; background: #fff }
.hit { height: 50px }
#dd.short .extra { display: none }
</style></head><body>
<div id=modal><div id=bar></div><div id=dd>
 <section><div id=h1 class=src>A</div><div class=hit></div><div class="hit extra"></div><div class="hit extra"></div></section>
 <section><div id=h2 class=src>B</div><div class=hit></div><div class="hit extra"></div></section>
</div></div>
<div style="height:2000px"></div>
</body></html>`, "", Options{Window: win, DocumentRoot: true, ViewportWidth: 400})
	sv := widgets.NewScrollView()
	sv.FixedViewport = true
	sv.OverlayBar = true
	sv.SetContent(res.Root, qui.Size{W: 400})
	win.SetRoot(sv)
	relayout := func() {
		res.Engine.FlushPendingStyles()
		sv.Measure(qui.Size{W: 400, H: 300})
		sv.Layout(qui.Rect{W: 400, H: 300})
	}
	pos := func(phase string) (float32, float32) {
		h1, h2 := qui.PaintBoundsInWindow(res.ByID["h1"]), qui.PaintBoundsInWindow(res.ByID["h2"])
		t.Logf("%s: h1=%v h2=%v", phase, h1, h2)
		return h1.Y, h2.Y
	}
	dd := res.ByID["dd"].(*El)
	dd.SetClass("short")
	relayout()
	pos("short")
	dd.SetClass("")
	relayout()
	pos("long")
	dd.SetClass("short")
	relayout()
	sv.ScrollTo(400) // the page under the modal scrolls
	y1, y2 := pos("short, page scrolled")
	h1, h2 := qui.Rect{Y: y1}, qui.Rect{Y: y2}
	if h1.Y != 80 || h2.Y != 160 {
		// A broken pass pins h1 at the page viewport's top (y=0) and moves
		// h2 up by the page scroll.
		t.Fatalf("headers at y=%v and y=%v, want 80 and 160 (in flow below the bar)", h1.Y, h2.Y)
	}
}

// A sticky box inside a fixed box never sticks to the document scroll: the
// fixed box does not move with it. Scrolling the page under an open modal
// left its sticky section headers pinned at the viewport top.
func TestStickyInsideFixedIgnoresPageScroll(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(`<!doctype html><html><body style="margin:0">
<div style="position:fixed;top:20px;left:0;width:300px"><div style="height:60px"></div>
 <section><div id=h class="src" style="position:sticky;top:0;height:30px">A</div><div style="height:100px"></div></section>
</div>
<div style="height:2000px"></div>
</body></html>`, "", Options{Window: win, DocumentRoot: true, ViewportWidth: 400})
	sv := widgets.NewScrollView()
	sv.FixedViewport = true
	sv.OverlayBar = true
	sv.SetContent(res.Root, qui.Size{W: 400})
	win.SetRoot(sv)
	sv.Measure(qui.Size{W: 400, H: 300})
	sv.Layout(qui.Rect{W: 400, H: 300})
	for _, scroll := range []float32{0, 120, 500} {
		sv.ScrollTo(scroll)
		if got := qui.PaintBoundsInWindow(res.ByID["h"]); got.Y != 80 {
			t.Errorf("page scroll %v: sticky header in the fixed box at y=%v, want 80", scroll, got.Y)
		}
	}
}

// overflow on <body> (or <html>) propagates to the viewport: hidden locks
// the user's page scrolling (DocSearch--active), overflow-x alone does not.
func TestViewportScrollLocked(t *testing.T) {
	for _, c := range []struct {
		css  string
		want bool
	}{
		{"", false},
		{"body{overflow:hidden}", true},
		{"body{overflow-x:hidden}", false},
		{"html{overflow:hidden}", true},
		{"html{overflow:auto} body{overflow:hidden}", false}, // body's no longer propagates
	} {
		res := RenderDoc(`<!doctype html><html><head><style>`+c.css+`</style></head><body><p>x</p></body></html>`,
			"", Options{DocumentRoot: true, ViewportWidth: 400})
		root, ok := res.Root.(*El)
		if !ok || root.Tag() != "html" {
			t.Fatalf("root is %T", res.Root)
		}
		if got := root.ViewportScrollLocked(); got != c.want {
			t.Errorf("%q: locked=%v, want %v", c.css, got, c.want)
		}
	}
}

// overflow: overlay is Chrome's legacy alias of auto (DocSearch's result
// list uses it): the box scrolls.
func TestOverflowOverlayScrolls(t *testing.T) {
	cs := &ComputedStyle{}
	parseOverflow(map[string]string{"overflow-y": "overlay"}, cs)
	if !cs.OverflowScroll || cs.OverflowScrollX {
		t.Fatalf("overflow-y: overlay → scroll=%v scrollX=%v, want a vertical scroller", cs.OverflowScroll, cs.OverflowScrollX)
	}
}

// Sticky section headers inside an overflow:auto list stick to the list's
// top as it scrolls (DocSearch's "LEARN" / "REACT APIS" headings).
func TestStickyHeaderSticksInNestedScroller(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(`<!doctype html><html><head><style>
body { margin: 0 }
#dd { overflow-y: auto; max-height: 200px; margin-top: 50px }
.src { position: sticky; top: 0; height: 30px }
.hit { height: 50px }
</style></head><body><div id=dd>
 <section><div id=h1 class=src>A</div><div class=hit></div><div class=hit></div><div class=hit></div></section>
 <section><div id=h2 class=src>B</div><div class=hit></div><div class=hit></div><div class=hit></div><div class=hit></div></section>
</div><div style="height:2000px"></div></body></html>`, "", Options{Window: win, DocumentRoot: true, ViewportWidth: 400})
	sv := widgets.NewScrollView()
	sv.FixedViewport = true
	sv.OverlayBar = true
	sv.SetContent(res.Root, qui.Size{W: 400})
	win.SetRoot(sv)
	sv.Measure(qui.Size{W: 400, H: 300})
	sv.Layout(qui.Rect{W: 400, H: 300})
	inner := res.ByID["dd"].(*El).scrollView
	if inner == nil {
		t.Fatal("#dd is not a scroller")
	}
	inner.ScrollTo(60)
	h1, h2 := qui.PaintBoundsInWindow(res.ByID["h1"]), qui.PaintBoundsInWindow(res.ByID["h2"])
	if h1.Y != 50 {
		t.Errorf("h1 at y=%v after scrolling the list 60px, want stuck at the list top 50", h1.Y)
	}
	if h2.Y != 50+180-60 {
		t.Errorf("h2 at y=%v, want in flow at %v", h2.Y, 50+180-60)
	}
	inner.ScrollTo(210) // past section A: B's header is the stuck one
	if h2 := qui.PaintBoundsInWindow(res.ByID["h2"]); h2.Y != 50 {
		t.Errorf("h2 at y=%v at scroll 210, want stuck at the list top 50", h2.Y)
	}
}
