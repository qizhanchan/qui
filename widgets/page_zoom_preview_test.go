package widgets

import (
	"runtime"
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// A zoom preview scales the committed frame on the UI thread while the
// page thread is busy, and gives way to the page's frame once the page has
// applied the change.
func TestPageViewZoomPreview(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, err := NewHeadlessWindowForTest(300, 200)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	th := NewPageThread("test")
	defer th.Close()
	h := th.NewPageHost(PageHostConfig{Width: 300, Height: 200, Setup: func(w *Window) {
		sv := NewScrollView()
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
	step(func() bool { return view.FrameSeq() > 0 }, "a first frame")
	px := func(y int) Color {
		host.Invalidate()
		host.Step()
		img := host.Snapshot()
		s := img.Bounds().Dx() / 300
		c := img.RGBAAt(150*s, y*s+s/2)
		return Color{R: float32(c.R) / 255, G: float32(c.G) / 255, B: float32(c.B) / 255, A: 1}
	}
	near := func(a, b Color) bool { return nearRGBA(rgbaOf(a), rgbaOf(b)) }
	if got := px(60); !near(got, stripeColor(1)) {
		t.Fatalf("before: y=60 shows %v, want band 1", got)
	}

	release := make(chan struct{})
	seq := view.FrameSeq()
	view.PreviewZoom(2, func(*Window) { <-release })
	if !view.ZoomPreviewing() {
		t.Fatal("no preview after PreviewZoom")
	}
	// Scaled 2×: band 0 (0–50pt) now covers 0–100pt.
	if got := px(60); !near(got, stripeColor(0)) {
		t.Fatalf("previewing 200%%: y=60 shows %v, want band 0", got)
	}
	close(release)
	step(func() bool { return !view.ZoomPreviewing() }, "the page to commit after the zoom")
	if view.FrameSeq() == seq {
		t.Fatal("preview ended without a new commit")
	}
	// apply changed nothing, so the page's frame is unscaled again.
	if got := px(60); !near(got, stripeColor(1)) {
		t.Fatalf("after: y=60 shows %v, want band 1", got)
	}
}
