package htmlcss

import (
	"bytes"
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
		if !bytes.Equal(got.Pix, want.Pix) {
			t.Fatalf("scroll to %v: the blitted frame differs from a full repaint", off)
		}
	}
	if !moved {
		t.Fatal("no frame moved pixels: PixelScroll never blitted")
	}
}
