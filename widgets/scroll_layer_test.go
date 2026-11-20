package widgets

import (
	"image/color"
	"runtime"
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// stripes is a tall document of 50pt bands, each its own colour, so a
// pixel tells which content row is on screen.
type stripes struct{ BaseWidget }

func newStripes() *stripes {
	s := &stripes{BaseWidget: NewBaseWidget()}
	s.SetSelf(s)
	return s
}

func stripeColor(band int) Color {
	return Color{R: float32(band*37%256) / 255, G: float32(band*91%256) / 255, B: float32(band*53%256) / 255, A: 1}
}

func (s *stripes) Measure(Size) Size { return Size{W: 300, H: 4000} }
func (s *stripes) Draw(c Canvas) {
	b := s.Bounds()
	clip := b
	if ca, ok := c.(ClipAware); ok {
		clip = ca.ClipBoundsLogical().Intersect(b)
	}
	for band := int((clip.Y - b.Y) / 50); float32(band*50) < clip.Y+clip.H-b.Y; band++ {
		c.FillRect(Rect{X: b.X, Y: b.Y + float32(band*50), W: b.W, H: 50}, stripeColor(band))
	}
}

func rgbaOf(c Color) color.RGBA {
	return color.RGBA{R: uint8(c.R*255 + 0.5), G: uint8(c.G*255 + 0.5), B: uint8(c.B*255 + 0.5), A: 255}
}

func nearRGBA(a, b color.RGBA) bool {
	d := func(x, y uint8) bool { return x-y <= 2 || y-x <= 2 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

// TestPageScrollLayerScrollsWhilePageBlocked is the P3 acceptance in
// miniature: with the page thread stuck, a wheel on the host window still
// scrolls the page, from the tiles the page committed.
func TestPageScrollLayerScrollsWhilePageBlocked(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, err := NewHeadlessWindowForTest(300, 200)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	th := NewPageThread("test")
	defer th.Close()
	var sv *ScrollView
	h := th.NewPageHost(PageHostConfig{Width: 300, Height: 200, Setup: func(w *Window) {
		sv = NewScrollView()
		sv.PixelScroll = true
		sv.FixedViewport = true
		sv.SetContentAuto(newStripes())
		w.SetRoot(sv)
	}})
	view := NewPageView(h)
	host.SetRoot(view)

	step := func(cond func() bool, what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			host.Step()
			time.Sleep(2 * time.Millisecond)
		}
		host.Step()
	}
	composited := func() bool {
		u, _ := h.TakeFrameLayersForTest()
		return len(u) == 1 && u[0].Composited && len(u[0].Tiles) > 0
	}
	step(func() bool { return view.FrameSeq() > 0 && composited() }, "a composited scroll layer")
	px := func(y int) color.RGBA {
		host.Invalidate()
		host.Step()
		img := host.Snapshot()
		s := img.Bounds().Dx() / 300
		return img.RGBAAt(150*s, y*s+s/2)
	}
	if got, want := px(10), rgbaOf(stripeColor(0)); !nearRGBA(got, want) {
		t.Fatalf("before scrolling: %v, want band 0 %v", got, want)
	}

	release := make(chan struct{})
	blocked := make(chan struct{})
	h.Do(func(*Window) { close(blocked); <-release })
	<-blocked
	// Five notches down: 5 × 40pt = 200pt, the top of band 4.
	host.DispatchTestEvent(NewScrollEvent(150, 100, 0, -5, 0))
	if got, want := px(10), rgbaOf(stripeColor(4)); !nearRGBA(got, want) {
		t.Fatalf("while the page is blocked: %v, want band 4 %v", got, want)
	}
	if got, want := px(60), rgbaOf(stripeColor(5)); !nearRGBA(got, want) {
		t.Fatalf("while the page is blocked, 50pt lower: %v, want band 5 %v", got, want)
	}

	close(release)
	var off float32
	step(func() bool {
		got := make(chan float32, 1)
		h.Do(func(*Window) { got <- sv.ScrollY() })
		select {
		case off = <-got:
		case <-time.After(time.Second):
		}
		return off == 200
	}, "the page to apply the UI scroll")
	step(func() bool {
		ls, _ := h.TakeFrameLayersForTest()
		return len(ls) == 1 && ls[0].Offset.Y == 200
	}, "a commit at the new offset")
	if got, want := px(10), rgbaOf(stripeColor(4)); !nearRGBA(got, want) {
		t.Fatalf("after the page caught up: %v, want band 4 %v", got, want)
	}
}
