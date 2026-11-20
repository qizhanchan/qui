package htmlcss

import (
	"fmt"
	"image/color"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// RFC 0005 §8.2 / P3: a nested scroll container (a code block, a sidebar)
// in a page on a page thread scrolls on the host's UI thread while the
// page thread is stuck.
func TestNestedScrollerScrollsWhilePageBlocked(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	band := func(i int) string { return fmt.Sprintf("#%02x%02x%02x", i*37%256, i*91%256, i*53%256) }
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><body style="margin:0;background:#fff">
<div style="height:40px"></div>
<div id=box style="margin-left:20px;width:200px;height:120px;overflow:auto;background:#eeeeee">`)
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&sb, `<div style="height:40px;background:%s"></div>`, band(i))
	}
	sb.WriteString(`</div><div style="height:2000px"></div></body></html>`)
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })

	host, err := qui.NewHeadlessWindowForTest(400, 300)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	th := qui.NewPageThread("nested")
	defer th.Close()
	h := th.NewPageHost(qui.PageHostConfig{Width: 400, Height: 300, Setup: func(w *qui.Window) {
		view := widgets.NewScrollView()
		view.FixedViewport, view.PixelScroll = true, true
		w.SetRoot(view)
		rd := RenderDoc(sb.String(), "", Options{Window: w, ViewportWidth: 400, ViewportHeight: 300, DocumentRoot: true})
		view.SetContentAuto(rd.Root)
	}})
	host.SetRoot(qui.NewPageView(h))
	step := func(cond func() bool, what string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			host.Step()
			time.Sleep(5 * time.Millisecond)
		}
		host.Step()
	}
	nested := func() bool {
		ls, _ := h.TakeFrameLayersForTest()
		for _, l := range ls {
			if l.Depth > 0 && l.Composited && l.Max.Y > 0 && l.Max.Y < 2000 {
				return true
			}
		}
		return false
	}
	step(nested, "a composited nested scroller")
	px := func(x, y int) color.RGBA {
		host.Invalidate()
		host.Step()
		img := host.Snapshot()
		s := img.Bounds().Dx() / 400
		return img.RGBAAt(x*s, y*s+s/2)
	}
	want := func(i int) color.RGBA {
		var r, g, b uint8
		fmt.Sscanf(band(i), "#%02x%02x%02x", &r, &g, &b)
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	near := func(a, b color.RGBA) bool {
		d := func(x, y uint8) bool { return x-y <= 3 || y-x <= 3 }
		return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
	}
	if got := px(100, 45); !near(got, want(0)) {
		t.Fatalf("before: %v, want band 0 %v", got, want(0))
	}
	release := make(chan struct{})
	blocked := make(chan struct{})
	h.Do(func(*qui.Window) { close(blocked); <-release })
	<-blocked
	defer close(release)
	// Two notches over the box: 80px, the top of band 2.
	host.DispatchTestEvent(qui.NewScrollEvent(100, 80, 0, -2, 0))
	if got := px(100, 45); !near(got, want(2)) {
		t.Fatalf("box while the page is blocked: %v, want band 2 %v", got, want(2))
	}
	if got := px(300, 45); !near(got, color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("outside the box the document must not move: %v", got)
	}
}
