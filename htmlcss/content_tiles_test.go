package htmlcss

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// The page view's content cache is tiled and rasterized on the worker
// pool (RFC 0004 R3). Whatever happened before — a jump far past the
// prepared tiles, a small change repainted into the tiles in place, a
// change large enough to re-raster tiles, blurred content cut by tile
// edges — every frame must match a full repaint of the same state.
func TestContentTilesMatchFullRepaint(t *testing.T) {
	page := func() string {
		var sb strings.Builder
		sb.WriteString(`<!doctype html><html><body style="margin:0;font:14px sans-serif">
<nav style="position:sticky;top:0;height:44px;background:rgba(255,255,255,0.6);backdrop-filter:blur(6px);z-index:5">nav</nav>`)
		for i := 0; i < 150; i++ {
			blur := ""
			if i%7 == 3 {
				blur = ";filter:blur(3px)"
			}
			fmt.Fprintf(&sb, `<div id=r%d style="margin:8px;padding:10px;border-radius:10px;background:#%02x%02x%02x%s">Row %d — tiles, pictures and worker rasters</div>`,
				i, (i*37)%256, (i*91)%256, (i*53)%256, blur, i)
		}
		sb.WriteString(`</body></html>`)
		return sb.String()
	}
	type win struct {
		w    *qui.Window
		view *widgets.ScrollView
		r    *qui.CPURenderer
		rd   RenderResult
	}
	open := func(blit bool) win {
		w, err := qui.NewHeadlessWindowForTest(520, 380)
		if err != nil {
			t.Skip(err)
		}
		r := qui.NewCPURenderer()
		w.SetRenderer(r)
		view := widgets.NewScrollView()
		view.FixedViewport = true
		view.OverlayBar = true
		view.PixelScroll = blit
		view.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}
		w.SetRoot(view)
		rd := RenderDoc(page(), "", Options{Window: w, ViewportWidth: 520, ViewportHeight: 380, DocumentRoot: true})
		view.SetContentAuto(rd.Root)
		w.Invalidate()
		w.Step()
		return win{w, view, r, rd}
	}
	got, ref := open(true), open(false)
	check := func(what string) {
		t.Helper()
		got.w.Step()
		ref.w.Invalidate()
		ref.w.Step()
		g, rf := got.r.Snapshot(), ref.r.Snapshot()
		// The overlay scrollbar fades on its own clock in each window.
		bar := g.Rect.Dx() - 24*g.Rect.Dx()/520
		for y := 0; y < g.Rect.Dy(); y++ {
			copy(g.Pix[g.PixOffset(bar, y):g.PixOffset(g.Rect.Dx(), y)], rf.Pix[rf.PixOffset(bar, y):rf.PixOffset(rf.Rect.Dx(), y)])
		}
		// A blurred row cut by the viewport's top or bottom edge: the full
		// repaint blurs it as cut (its layer is clamped to the frame), the
		// tiles blur it whole, as a browser does. Not a tile defect.
		edge := 12 * g.Rect.Dy() / 380
		for _, y0 := range []int{0, g.Rect.Dy() - edge} {
			for y := y0; y < y0+edge; y++ {
				copy(g.Pix[g.PixOffset(0, y):g.PixOffset(g.Rect.Dx(), y)], rf.Pix[rf.PixOffset(0, y):rf.PixOffset(rf.Rect.Dx(), y)])
			}
		}
		if d, at := maxPixDiff(g.Pix, rf.Pix); d > 2 {
			n := 0
			for i := 0; i < len(g.Pix); i += 4 {
				for c := 0; c < 4; c++ {
					dd := int(g.Pix[i+c]) - int(rf.Pix[i+c])
					if dd > 2 || dd < -2 {
						if n < 3 || n%10 == 0 {
							t.Logf("px (%d,%d) got %v want %v", (i/4)%g.Rect.Dx(), (i/4)/g.Rect.Dx(), g.Pix[i:i+4], rf.Pix[i:i+4])
						}
						n++
						break
					}
				}
			}
			t.Logf("%d pixels differ; frame %v", n, g.Rect)
			t.Fatalf("%s: the tiled frame differs from a full repaint (by %d at byte %d)", what, d, at)
		}
	}
	both := func(fn func(w win)) { fn(got); fn(ref) }
	for _, off := range []float32{50, 120, 2400, 2350, 0} {
		both(func(w win) { w.view.ScrollTo(off) })
		check(fmt.Sprintf("scroll to %v", off))
	}
	if !got.view.ContentCached() {
		t.Fatal("the content was not drawn through the tile cache")
	}
	tiles, _, _ := got.view.ContentTilesForTest()
	if tiles == 0 {
		t.Fatal("no content tiles")
	}
	// Prepared tiles land in the background.
	deadline := time.Now().Add(3 * time.Second)
	for {
		got.w.Step()
		if _, _, pending := got.view.ContentTilesForTest(); pending == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A change below the fold, in tiles prepared ahead: the frame paints
	// nothing, but the tiles must not keep the old row.
	both(func(w win) {
		w.rd.ByID["r13"].(*El).SetAttr("style", "margin:8px;padding:10px;background:#0a0;color:white")
	})
	check("change below the fold")
	both(func(w win) { w.view.ScrollTo(420) })
	check("scroll to the changed row")
	both(func(w win) { w.view.ScrollTo(0) })
	check("back to the top")
	// A small change: repainted into its tiles in place.
	both(func(w win) {
		w.rd.ByID["r2"].(*El).SetAttr("style", "margin:8px;padding:10px;background:#123456;color:white")
	})
	check("small change")
	// A large change across many tiles: re-rastered.
	both(func(w win) {
		for i := 0; i < 12; i++ {
			w.rd.ByID[fmt.Sprintf("r%d", i)].(*El).SetAttr("style", "margin:8px;padding:30px;background:#fec")
		}
	})
	check("large change")
	for _, off := range []float32{300, 1200, 1100} {
		both(func(w win) { w.view.ScrollTo(off) })
		check(fmt.Sprintf("after changes, scroll to %v", off))
	}
}
