package qui

import (
	"image"
	"testing"
	"time"
)

// rasterTestRenderer is a Renderer that really rasterizes — into a retained
// CPU image, like GLRenderer's default path — without a GL context, so a
// frame's pixels can be checked through Window.Snapshot.
type rasterTestRenderer struct {
	img    *image.RGBA
	frames int
}

func (r *rasterTestRenderer) Begin(size Size) Canvas {
	w, h := int(size.W), int(size.H)
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	return NewImageCanvas(r.img)
}

func (r *rasterTestRenderer) End() { r.frames++ }

func (r *rasterTestRenderer) Snapshot() *image.RGBA {
	if r.img == nil {
		return nil
	}
	dst := image.NewRGBA(r.img.Rect)
	copy(dst.Pix, r.img.Pix)
	return dst
}

// swatch fills its bounds with one color — the stand-in for "the model
// changed, and this paint shows it".
type swatch struct {
	BaseWidget
	color Color
}

func (s *swatch) Measure(avail Size) Size { return avail }
func (s *swatch) Draw(c Canvas)           { c.FillRect(s.Bounds(), s.color) }

var (
	swatchBefore = Color{R: 1, A: 1}
	swatchAfter  = Color{G: 1, A: 1}
)

// newPaintedSwatchWindow is a platform-backed window whose first frame
// (swatchBefore) is already on screen and settled.
func newPaintedSwatchWindow(t *testing.T) (*Window, *fakePlatformWindow, *swatch) {
	t.Helper()
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	w.renderer = &rasterTestRenderer{}
	sw := &swatch{color: swatchBefore}
	w.SetRoot(sw)
	settle(t, w)
	if got := centerPixel(t, w.Snapshot()); got != colorKey(swatchBefore) {
		t.Fatalf("precondition: first frame center = %v, want %v", got, colorKey(swatchBefore))
	}
	return w, f, sw
}

func centerPixel(t *testing.T, img *image.RGBA) [4]uint8 {
	t.Helper()
	if img == nil {
		t.Fatal("no frame captured")
	}
	c := img.RGBAAt(img.Rect.Dx()/2, img.Rect.Dy()/2)
	return [4]uint8{c.R, c.G, c.B, c.A}
}

// shootWhileLooping takes an agent-style capture from another goroutine
// (the HTTP handler's position) while this goroutine plays App.step: it
// runs a Step whenever the window is due one. The loop starts only once
// the capture's own job is queued (the pending count rose past
// queuedBefore), so what that Step drains is fixed.
func shootWhileLooping(t *testing.T, w *Window, queuedBefore int, due func() bool, shoot func() *image.RGBA) *image.RGBA {
	t.Helper()
	ch := make(chan *image.RGBA, 1)
	go func() { ch <- shoot() }()
	deadline := time.Now().Add(3 * time.Second)
	for w.PendingJobs() <= queuedBefore {
		if time.Now().After(deadline) {
			t.Fatal("capture never queued its job")
		}
		time.Sleep(time.Millisecond)
	}
	for {
		select {
		case img := <-ch:
			return img
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("capture never completed")
		}
		if due() {
			w.Step()
		} else {
			time.Sleep(time.Millisecond)
		}
	}
}

func colorKey(c Color) [4]uint8 {
	v := ColorToRGBA(c)
	return [4]uint8{v.R, v.G, v.B, v.A}
}

// The collaborative-edit shape: a goroutine PostJobs a model change and an
// agent asks for a screenshot before the loop has drained it. The shot's
// job used to run on the priority lane — ahead of the edit, and before the
// Step painted — so it returned the frame from before the edit.
func TestShotIncludesJobsQueuedAheadOfIt(t *testing.T) {
	captures := map[string]func(w *Window) *image.RGBA{
		"SnapshotScaled": func(w *Window) *image.RGBA { return w.SnapshotScaled(1) },
		"SnapshotRegion": func(w *Window) *image.RGBA {
			return w.SnapshotRegion(Rect{X: 300, Y: 200, W: 200, H: 200}, 1)
		},
		"SnapshotAnnotated": func(w *Window) *image.RGBA {
			return w.SnapshotAnnotated(AnnotateOptions{Scale: 1, HideUnhighlighted: true, Highlight: "#none"})
		},
	}
	for name, capture := range captures {
		t.Run(name, func(t *testing.T) {
			w, _, sw := newPaintedSwatchWindow(t)
			w.PostJob(func() {
				sw.color = swatchAfter
				w.InvalidateRect(sw.Bounds())
			})
			img := shootWhileLooping(t, w, w.PendingJobs(), func() bool { return frameDue(w) },
				func() *image.RGBA { return capture(w) })
			if got, want := centerPixel(t, img), colorKey(swatchAfter); got != want {
				t.Errorf("shot center = %v, want %v: the capture returned the frame from before the queued edit", got, want)
			}
		})
	}
}

// Input the platform dispatched during the event wait (a real key press, a
// window refresh) dirties the window between Steps. A shot taken then must
// show it, not the frame before it.
func TestShotIncludesChangesDirtiedSinceTheLastFrame(t *testing.T) {
	w, _, sw := newPaintedSwatchWindow(t)
	sw.color = swatchAfter
	w.InvalidateRect(sw.Bounds())

	img := shootWhileLooping(t, w, 0, func() bool { return frameDue(w) },
		func() *image.RGBA { return w.SnapshotScaled(1) })
	if got, want := centerPixel(t, img), colorKey(swatchAfter); got != want {
		t.Errorf("shot center = %v, want %v: the capture skipped the pending repaint", got, want)
	}
}

// A display-paced surface stops signalling frames while its window is
// occluded, so dirty pixels wait for a frame that does not come. The shot
// must still see current state: its job forces a Step, and that Step paints
// before the capture reads the buffer.
func TestShotOfAnOccludedWindowIsFresh(t *testing.T) {
	w, f, sw := newPaintedSwatchWindow(t)
	f.surface().(*fakeSurface).readyCh = make(chan struct{}) // never fires
	sw.color = swatchAfter
	w.InvalidateRect(sw.Bounds())
	if frameDue(w) {
		t.Fatal("precondition: an occluded display-paced window is not due a frame")
	}

	img := shootWhileLooping(t, w, 0, func() bool { return frameDue(w) },
		func() *image.RGBA { return w.SnapshotScaled(1) })
	if got, want := centerPixel(t, img), colorKey(swatchAfter); got != want {
		t.Errorf("shot center = %v, want %v: an occluded window's shot returned a stale frame", got, want)
	}
}

// AfterFrame work queued while a Step runs belongs to that Step; any queued
// from outside one keeps the loop from sleeping past it.
func TestAfterFrameRunsAtTheEndOfTheStep(t *testing.T) {
	w, _, sw := newPaintedSwatchWindow(t)
	r := w.renderer.(*rasterTestRenderer)
	var framesAtRun int
	ran := false
	w.PostJob(func() {
		sw.color = swatchAfter
		w.InvalidateRect(sw.Bounds())
		w.afterFrame = append(w.afterFrame, func() {
			ran = true
			framesAtRun = r.frames
		})
	})
	before := r.frames
	w.Step()
	if !ran {
		t.Fatal("after-frame callback did not run in the Step that queued it")
	}
	if framesAtRun != before+1 {
		t.Errorf("after-frame callback saw %d frames, want %d (after this Step's paint)", framesAtRun, before+1)
	}

	settle(t, w)
	w.afterFrame = append(w.afterFrame, func() {})
	if got := w.nextFrameWait(time.Now()); got != frameInterval {
		t.Errorf("pending after-frame work: wait = %v, want one frame", got)
	}
	w.Step()
	if len(w.afterFrame) != 0 {
		t.Errorf("after-frame queue not drained: %d left", len(w.afterFrame))
	}
}
