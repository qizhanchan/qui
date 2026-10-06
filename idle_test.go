package qui

import (
	"image"
	"testing"
	"time"
)

type countingAnimator struct{ ticks int }

func (a *countingAnimator) Tick(time.Time) (Rect, bool) { a.ticks++; return Rect{}, false }
func (a *countingAnimator) Stop()                       {}

// The point of on-demand pacing: once a frame has settled, the loop may
// sleep until an event instead of waking 60 times a second.
func TestIdleWindowSleepsUntilAnEvent(t *testing.T) {
	w := newWindowOnFake(newFakePlatformWindow())
	now := time.Now()

	w.Invalidate()
	if got := w.nextFrameWait(now); got != frameInterval {
		t.Fatalf("dirty window: wait = %v, want one frame", got)
	}
	w.Step()
	// A frame that painted buys one more, so a Tick that animates by
	// returning a dirty rect each frame keeps running unasked.
	if got := w.nextFrameWait(now); got != frameInterval {
		t.Fatalf("right after a paint: wait = %v, want one frame", got)
	}
	w.Step()
	if got := w.nextFrameWait(now); got != pumpForever {
		t.Fatalf("quiet window: wait = %v, want to block until an event", got)
	}
}

func TestWorkInFlightKeepsFramesComing(t *testing.T) {
	now := time.Now()
	cases := map[string]func(w *Window){
		"animator":     func(w *Window) { w.animators = append(w.animators, &countingAnimator{}) },
		"pending job":  func(w *Window) { w.PostJob(func() {}) },
		"after layout": func(w *Window) { w.AfterLayout(func() {}) },
		"dirty layout": func(w *Window) {
			root := NewContainer(FlexLayout{})
			w.SetRoot(root)
			root.InvalidateLayout()
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWindowOnFake(newFakePlatformWindow())
			setup(w)
			w.dirtyRegion = Rect{} // isolate the condition under test
			if got := w.nextFrameWait(now); got != frameInterval {
				t.Errorf("wait = %v, want one frame", got)
			}
		})
	}
}

func TestRequestTickAtWakesAtTheEarliestDeadline(t *testing.T) {
	w := newWindowOnFake(newFakePlatformWindow())
	now := time.Now()

	w.RequestTickAt(now.Add(400 * time.Millisecond))
	w.RequestTickAt(now.Add(250 * time.Millisecond))
	w.RequestTickAt(now.Add(900 * time.Millisecond))
	if got := w.nextFrameWait(now); got != 250*time.Millisecond {
		t.Fatalf("wait = %v, want the earliest request (250ms)", got)
	}

	// A Step at or past the deadline spends it; nothing re-requested, so
	// the window goes back to sleeping until an event.
	w.tickAt = now.Add(-time.Millisecond)
	w.Step()
	if !w.tickAt.IsZero() {
		t.Fatalf("reached deadline survived the Step: %v", w.tickAt)
	}
	if got := w.nextFrameWait(time.Now()); got != pumpForever {
		t.Fatalf("wait = %v after the deadline was spent, want forever", got)
	}

	// A deadline still in the future survives a Step: it may have been
	// requested from an event handler, not re-requested by a Tick.
	future := time.Now().Add(time.Hour)
	w.RequestTickAt(future)
	w.Step()
	if !w.tickAt.Equal(future) {
		t.Fatalf("future deadline was dropped by a Step")
	}
}

func TestTooltipDeadlinesWakeTheLoop(t *testing.T) {
	w := newWindowOnFake(newFakePlatformWindow())
	now := time.Now()
	w.tooltipShowAt = now.Add(600 * time.Millisecond)
	if got := w.nextFrameWait(now); got != 600*time.Millisecond {
		t.Fatalf("wait = %v, want the tooltip delay", got)
	}
	// A close deadline only counts while a tooltip is showing.
	w.tooltipShowAt = time.Time{}
	w.tooltipCloseAt = now.Add(200 * time.Millisecond)
	if got := w.nextFrameWait(now); got != pumpForever {
		t.Fatalf("close deadline without a tooltip: wait = %v, want forever", got)
	}
	w.tooltipView = &tooltipView{}
	if got := w.nextFrameWait(now); got != 200*time.Millisecond {
		t.Fatalf("wait = %v, want the close deadline", got)
	}
	// Overdue but still pending (a selection drag holds the tooltip open):
	// recheck at frame cadence, never spin on a zero timeout.
	if got := w.nextFrameWait(now.Add(time.Second)); got != frameInterval {
		t.Fatalf("overdue deadline: wait = %v, want one frame", got)
	}
}

func TestAppIdleTimeoutTakesTheSoonestWindow(t *testing.T) {
	now := time.Now()
	idle := newWindowOnFake(newFakePlatformWindow())
	waiting := newWindowOnFake(newFakePlatformWindow())
	waiting.RequestTickAt(now.Add(300 * time.Millisecond))

	a := &App{windows: []*Window{idle, waiting}}
	if got := a.idleTimeout(now); got != 300*time.Millisecond {
		t.Fatalf("wait = %v, want the waiting window's deadline", got)
	}

	a.SetMaxIdleWait(100 * time.Millisecond)
	if got := a.idleTimeout(now); got != 100*time.Millisecond {
		t.Fatalf("wait = %v, want the SetMaxIdleWait cap", got)
	}

	a = &App{windows: []*Window{idle}}
	if got := a.idleTimeout(now); got != pumpForever {
		t.Fatalf("all idle: wait = %v, want forever", got)
	}

	// A close requested from inside a frame must be acted on now, not after
	// the next input event.
	idle.plat.setShouldClose(true)
	if got := a.idleTimeout(now); got != 0 {
		t.Fatalf("closing window: wait = %v, want an immediate poll", got)
	}
}

func TestTextureUploadIsScopedToDamage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	r := &GLRenderer{}

	// Nothing mirrored yet: the first frame uploads everything.
	r.damage = image.Rect(10, 10, 20, 20)
	if got := r.textureUploadRect(img); got != img.Rect {
		t.Fatalf("first frame uploads %v, want the whole image", got)
	}

	r.texSrc = img
	if got, want := r.textureUploadRect(img), image.Rect(9, 9, 21, 21); got != want {
		t.Fatalf("damaged frame uploads %v, want %v (damage + 1px guard)", got, want)
	}
	r.damage = image.Rect(195, 95, 260, 140)
	if got, want := r.textureUploadRect(img), image.Rect(194, 94, 200, 100); got != want {
		t.Fatalf("damage past the edge uploads %v, want %v", got, want)
	}
	// Unknown damage (a renderer driven outside Window.Step) is everything.
	r.damage = image.Rectangle{}
	if got := r.textureUploadRect(img); got != img.Rect {
		t.Fatalf("unknown damage uploads %v, want the whole image", got)
	}
	// A different image (resize reallocated it) is not what the texture holds.
	r.damage = image.Rect(10, 10, 20, 20)
	if got := r.textureUploadRect(image.NewRGBA(img.Rect)); got != img.Rect {
		t.Fatalf("new image uploads %v, want the whole image", got)
	}
}

func TestPhysicalRectRoundsOutward(t *testing.T) {
	got := physicalRect(Rect{X: 10.25, Y: 3.5, W: 20.5, H: 7.25}, 2, 2)
	if want := image.Rect(20, 7, 62, 22); got != want {
		t.Fatalf("physicalRect = %v, want %v", got, want)
	}
}

// fadeWidget stands in for a widget mid-transition (Button's hover fade):
// its Tick reports a dirty rect while frames remain, then nothing.
type fadeWidget struct {
	BaseWidget
	framesLeft int
}

func (f *fadeWidget) Tick(time.Time) Rect {
	if f.framesLeft == 0 {
		return Rect{}
	}
	f.framesLeft--
	return Rect{W: 10, H: 10}
}

// An animating Tickable needs no RequestTickAt: each frame it dirties
// buys the next, and the loop goes idle once the animation settles.
func TestAnimatingTickableRunsUntilItSettles(t *testing.T) {
	w := newWindowOnFake(newFakePlatformWindow())
	fade := &fadeWidget{framesLeft: 3}
	w.SetRoot(fade)
	w.Step() // settle the initial layout + paint

	frames := 0
	for w.nextFrameWait(time.Now()) == frameInterval {
		w.Step()
		if frames++; frames > 10 {
			t.Fatal("loop never went idle")
		}
	}
	if fade.framesLeft != 0 {
		t.Fatalf("loop went idle with %d animation frames left", fade.framesLeft)
	}
	if got := w.nextFrameWait(time.Now()); got != pumpForever {
		t.Fatalf("settled window: wait = %v, want forever", got)
	}
}
