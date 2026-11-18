package htmlcss

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A page view with PixelScroll moves the painted pixels on scroll and
// repaints only what the move exposed plus the boxes that do not move with
// the page (a fixed header, a sticky heading, the scrollbar). Every frame
// must match a full repaint of the same scroll position.

func pixelScrollPage() string {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><body style="margin:0">
<div style="position:fixed;top:0;left:0;right:0;height:40px;background:#c33">fixed</div>
<h2 style="position:sticky;top:40px;background:#3c3;margin:0">sticky</h2>`)
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&sb, `<p style="margin:0;padding:6px;background:#%02x%02x%02x">Row %d of a long page, with some text to rasterize.</p>`,
			(i*37)%256, (i*91)%256, (i*53)%256, i)
	}
	sb.WriteString(`</body></html>`)
	return sb.String()
}

func pixelScrollWindow(t *testing.T, blit bool) (*qui.Window, *widgets.ScrollView, *qui.CPURenderer) {
	t.Helper()
	win, err := qui.NewHeadlessWindowForTest(600, 400)
	if err != nil {
		t.Skip(err)
	}
	r := qui.NewCPURenderer()
	win.SetRenderer(r)
	view := widgets.NewScrollView()
	view.FixedViewport = true
	view.OverlayBar = true
	view.PixelScroll = blit
	view.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}
	win.SetRoot(view)
	rd := RenderDoc(pixelScrollPage(), "", Options{Window: win, ViewportWidth: 600, ViewportHeight: 400, DocumentRoot: true})
	view.SetContentAuto(rd.Root)
	win.Invalidate()
	win.Step()
	return win, view, r
}

// maxPixDiff is the largest per-channel difference between two frames.
// The content cache composites antialiased edges in a different blend
// order than a direct paint (content over transparent, then over the
// background), which rounds 1/255 differently; stale or missing pixels
// differ by far more.
func maxPixDiff(a, b []byte) (int, int) {
	if len(a) != len(b) {
		return 255, -1
	}
	worst, at := 0, -1
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst, at = d, i
		}
	}
	return worst, at
}

func TestPixelScrollMatchesFullRepaint(t *testing.T) {
	win, view, r := pixelScrollWindow(t, true)
	moved := false
	for _, off := range []float32{30, 75, 420, 380, 381, 0, 900} {
		view.ScrollTo(off)
		win.Step()
		got := r.Snapshot()
		if fr := win.RecentFrames(1); len(fr) == 1 && fr[0].Scrolled > 0 {
			moved = true
		}
		ref, refView, refR := pixelScrollWindow(t, false)
		refView.ScrollTo(off)
		ref.Invalidate()
		ref.Step()
		want := refR.Snapshot()
		if d, at := maxPixDiff(got.Pix, want.Pix); d > 2 {
			t.Fatalf("scroll to %v: the blitted frame differs from a full repaint (by %d at byte %d)", off, d, at)
		}
	}
	if !moved {
		t.Fatal("no frame moved pixels: PixelScroll never blitted")
	}
}

// A translucent sticky header with a backdrop blur and a shadow over
// gradient content (react.dev's nav) is composited from the content cache
// on every scroll step; each frame must still match a full repaint.
func TestPixelScrollCacheTranslucentSticky(t *testing.T) {
	page := func() string {
		var sb strings.Builder
		sb.WriteString(`<!doctype html><html><body style="margin:0;background:conic-gradient(from 30deg at 40% 30%, #fde, #def, #efd, #fde)">
<nav style="position:sticky;top:0;height:48px;background:rgba(255,255,255,0.6);backdrop-filter:blur(8px);box-shadow:0 8px 24px rgba(0,0,0,0.3);z-index:10">nav</nav>`)
		for i := 0; i < 60; i++ {
			fmt.Fprintf(&sb, `<div style="margin:12px;padding:10px;border-radius:12px;box-shadow:0 2px 8px rgba(0,0,0,0.2);background:linear-gradient(90deg,#%02x%02x%02x,#fff)">Card %d with text</div>`,
				(i*37)%256, (i*91)%256, (i*53)%256, i)
		}
		sb.WriteString(`</body></html>`)
		return sb.String()
	}
	open := func(blit bool) (*qui.Window, *widgets.ScrollView, *qui.CPURenderer) {
		win, err := qui.NewHeadlessWindowForTest(500, 360)
		if err != nil {
			t.Skip(err)
		}
		r := qui.NewCPURenderer()
		win.SetRenderer(r)
		view := widgets.NewScrollView()
		view.FixedViewport = true
		view.OverlayBar = true
		view.PixelScroll = blit
		view.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}
		win.SetRoot(view)
		rd := RenderDoc(page(), "", Options{Window: win, ViewportWidth: 500, ViewportHeight: 360, DocumentRoot: true})
		view.SetContentAuto(rd.Root)
		win.Invalidate()
		win.Step()
		return win, view, r
	}
	win, view, r := open(true)
	cached := false
	for _, off := range []float32{40, 80, 120, 300, 260, 261, 700, 0} {
		view.ScrollTo(off)
		win.Step()
		cached = cached || view.ContentCached()
		got := r.Snapshot()
		ref, refView, refR := open(false)
		refView.ScrollTo(off)
		ref.Invalidate()
		ref.Step()
		if d, at := maxPixDiff(got.Pix, refR.Snapshot().Pix); d > 3 {
			t.Fatalf("scroll to %v: cached frame differs from a full repaint by %d at byte %d", off, d, at)
		}
	}
	if !cached {
		t.Fatal("the content cache was never used")
	}
}
