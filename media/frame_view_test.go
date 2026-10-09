package media

import (
	"image"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/qizhanchan/qui"
)

// fakePayload counts releases so tests can assert every pushed frame is
// released exactly once.
type fakePayload struct {
	w, h     int
	released atomic.Int32
	drawn    int
}

func (f *fakePayload) size() (int, int)      { return f.w, f.h }
func (f *fakePayload) cpuImage() image.Image { return image.NewRGBA(image.Rect(0, 0, f.w, f.h)) }
func (f *fakePayload) release()              { f.released.Add(1) }
func (f *fakePayload) draw(*FrameView, qui.GLState, qui.Rect, qui.Rect) bool {
	f.drawn++
	return true
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func TestFrameViewMapToFrameContain(t *testing.T) {
	// 400x300 view, 1920x1080 frame: letterboxed vertically.
	bounds := qui.Rect{X: 10, Y: 20, W: 400, H: 300}
	content := fitDstRect(FitContain, bounds, 1920, 1080)
	if !approx(float64(content.W), 400) || !approx(float64(content.H), 225) || !approx(float64(content.Y), 20+37.5) {
		t.Fatalf("contain rect = %+v", content)
	}
	x, y, inside := mapToFrame(content, bounds, 1920, 1080, qui.Point{X: 10, Y: 20 + 37.5})
	if !inside || !approx(x, 0) || !approx(y, 0) {
		t.Fatalf("top-left of picture -> (%v,%v,%v)", x, y, inside)
	}
	x, y, inside = mapToFrame(content, bounds, 1920, 1080, qui.Point{X: 210, Y: 20 + 37.5 + 112.5})
	if !inside || !approx(x, 960) || !approx(y, 540) {
		t.Fatalf("centre -> (%v,%v,%v)", x, y, inside)
	}
	// In the letterbox band: outside, coordinates extrapolated (negative y).
	x, y, inside = mapToFrame(content, bounds, 1920, 1080, qui.Point{X: 110, Y: 25})
	if inside || y >= 0 || !approx(x, 480) {
		t.Fatalf("letterbox -> (%v,%v,%v)", x, y, inside)
	}
}

func TestFrameViewMapToFrameStretchAndCover(t *testing.T) {
	bounds := qui.Rect{W: 200, H: 200}
	stretch := fitDstRect(FitStretch, bounds, 100, 50)
	x, y, inside := mapToFrame(stretch, bounds, 100, 50, qui.Point{X: 100, Y: 100})
	if !inside || !approx(x, 50) || !approx(y, 25) {
		t.Fatalf("stretch -> (%v,%v,%v)", x, y, inside)
	}
	cover := fitDstRect(FitCover, bounds, 100, 50) // 400x200, x from -100
	if !approx(float64(cover.X), -100) || !approx(float64(cover.W), 400) {
		t.Fatalf("cover rect = %+v", cover)
	}
	x, y, inside = mapToFrame(cover, bounds, 100, 50, qui.Point{X: 0, Y: 0})
	if !inside || !approx(x, 25) || !approx(y, 0) {
		t.Fatalf("cover left edge -> (%v,%v,%v)", x, y, inside)
	}
	// A point outside the view is never inside even if on the picture.
	if _, _, inside := mapToFrame(cover, bounds, 100, 50, qui.Point{X: -50, Y: 10}); inside {
		t.Fatal("point outside bounds reported inside")
	}
}

func TestFrameViewCropToVisible(t *testing.T) {
	// FitCover overflow: 400-wide picture in a 200-wide box, centred.
	src, dst, ok := cropToVisible(qui.Rect{X: -100, W: 400, H: 200}, qui.Rect{W: 200, H: 200}, 100, 50)
	if !ok || dst != (qui.Rect{W: 200, H: 200}) {
		t.Fatalf("dst = %+v ok=%v", dst, ok)
	}
	if !approx(float64(src.X), 25) || !approx(float64(src.W), 50) || !approx(float64(src.H), 50) {
		t.Fatalf("src = %+v", src)
	}
	if _, _, ok := cropToVisible(qui.Rect{X: 300, W: 10, H: 10}, qui.Rect{W: 200, H: 200}, 10, 10); ok {
		t.Fatal("fully clipped picture must report !ok")
	}
}

func TestFrameViewLatestWinsAndRefcountBalance(t *testing.T) {
	v := NewFrameView()
	a := &fakePayload{w: 64, h: 48}
	b := &fakePayload{w: 64, h: 48}
	c := &fakePayload{w: 64, h: 48}
	v.push(a)
	v.push(b) // a never drawn: released + dropped
	if a.released.Load() != 1 {
		t.Fatalf("replaced pending frame not released: %d", a.released.Load())
	}
	if got := v.promote(); got != b {
		t.Fatalf("promote returned %v, want newest", got)
	}
	v.notePresented(b)
	v.push(c)
	if b.released.Load() != 0 {
		t.Fatal("current frame released before its replacement was promoted")
	}
	if got := v.promote(); got != c || b.released.Load() != 1 {
		t.Fatalf("promote must swap and release the previous current (b released=%d)", b.released.Load())
	}
	// Promote with nothing pending keeps showing the current frame.
	if got := v.promote(); got != c {
		t.Fatal("idle promote dropped the current frame")
	}
	v.Destroy()
	for i, f := range []*fakePayload{a, b, c} {
		if n := f.released.Load(); n != 1 {
			t.Fatalf("frame %d released %d times, want exactly 1", i, n)
		}
	}
	st := v.Stats()
	if st.Pushed != 3 || st.Dropped != 1 || st.Presented != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestFrameViewConcurrentPushesBalance(t *testing.T) {
	v := NewFrameView()
	const n = 500
	frames := make([]*fakePayload, n)
	for i := range frames {
		frames[i] = &fakePayload{w: 8, h: 8}
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := g; i < n; i += 4 {
				v.push(frames[i])
			}
		}(g)
	}
	// The "UI thread" promotes concurrently.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			v.promote()
		}
		close(done)
	}()
	wg.Wait()
	<-done
	v.promote()
	v.Destroy()
	for i, f := range frames {
		if c := f.released.Load(); c != 1 {
			t.Fatalf("frame %d released %d times", i, c)
		}
	}
	if st := v.Stats(); st.Pushed != n {
		t.Fatalf("pushed = %d", st.Pushed)
	}
}

func TestFrameViewClearDropsFrames(t *testing.T) {
	v := NewFrameView()
	a := &fakePayload{w: 4, h: 4}
	b := &fakePayload{w: 4, h: 4}
	v.push(a)
	v.promote()
	v.push(b)
	v.Clear()
	if b.released.Load() != 1 {
		t.Fatal("Clear must release the pending frame")
	}
	if w, h := v.FrameSize(); w != 0 || h != 0 {
		t.Fatalf("FrameSize after Clear = %dx%d", w, h)
	}
	if got := v.promote(); got != nil || a.released.Load() != 1 {
		t.Fatalf("UI-side promote after Clear must drop current (got %v, a released %d)", got, a.released.Load())
	}
}

func TestFrameViewDrawCPUFallbackAndPresentHook(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 320, H: 240})
	v := NewFrameView()
	w.SetRoot(v)
	v.Layout(qui.Rect{W: 320, H: 240})
	var got []PresentInfo
	v.OnPresent(func(p PresentInfo) { got = append(got, p) })

	img := image.NewRGBA(image.Rect(0, 0, 160, 120))
	v.PushRGBA(img)
	// Not retained (never drawn on a GPU canvas): the push falls back to a
	// posted repaint, deduplicated.
	v.PushRGBA(image.NewRGBA(image.Rect(0, 0, 160, 120)))
	if n := w.PendingJobs(); n != 1 {
		t.Fatalf("pushes must post exactly one repaint job, have %d", n)
	}
	w.DrainJobsForTest()

	rc := &qui.RecordingCanvas{}
	v.Draw(rc)
	if len(got) != 1 || got[0].Seq != 2 || got[0].Width != 160 {
		t.Fatalf("present hook = %+v", got)
	}
	if st := v.Stats(); st.Presented != 1 || st.Dropped != 1 {
		t.Fatalf("stats = %+v", st)
	}
	// Redrawing the same frame does not count it again.
	v.Draw(rc)
	if len(got) != 1 {
		t.Fatalf("same frame reported twice: %+v", got)
	}
	if r := v.ContentRect(); r != (qui.Rect{W: 320, H: 240}) {
		t.Fatalf("ContentRect = %+v", r)
	}
	if x, y, in := v.MapToFrame(qui.Point{X: 160, Y: 120}); !in || !approx(x, 80) || !approx(y, 60) {
		t.Fatalf("MapToFrame = %v,%v,%v", x, y, in)
	}
}

func TestFrameViewIsRawKeyboardTargetAndFocusable(t *testing.T) {
	v := NewFrameView()
	if !v.Focusable() {
		t.Fatal("FrameView should be focusable by default")
	}
	var target interface{ WantsRawKeyboard() bool } = v
	if target.WantsRawKeyboard() {
		t.Fatal("raw keyboard must be opt-in")
	}
	v.SetRawKeyboard(true)
	if !target.WantsRawKeyboard() {
		t.Fatal("SetRawKeyboard(true) not reflected")
	}
}
