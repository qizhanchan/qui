package qui

import (
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui/internal/render"
)

// The compositor thread (RFC 0004 R4).
//
// A layer whose motion is a curve of time (a CSS marquee composited on the
// GPU, its overlay draw reading only a committed snapshot) is animated
// here, as Chromium's impl thread animates transform layers: once per
// refresh period this thread presents the last frame with the retained
// draws replayed at its own frame time. The UI thread then presents only
// when it has new content, and a script blocking it for 100 ms does not
// stop the layer. The thread runs only while every retained draw is
// compositor-safe (render.CompositorRenderer.CompositorWanted), and skips
// a period in which the UI thread holds the renderer (it is painting —
// its own present shows the layer at the UI frame time).
//
// QUI_COMPOSITOR_THREAD=0 turns it off.

var compositorThreadOff = os.Getenv("QUI_COMPOSITOR_THREAD") == "0"

type compositorThread struct {
	quit   chan struct{}
	frames atomic.Int64 // frames it presented
}

// notePresented records that the UI thread presented a frame now.
func (w *Window) notePresented(now time.Time) {
	w.lastPresent.Store(now.UnixNano())
}

// syncCompositorThread starts or stops the compositor thread after a
// Step, as the renderer's retained draws allow.
func (w *Window) syncCompositorThread() {
	cr, ok := w.renderer.(render.CompositorRenderer)
	want := ok && !compositorThreadOff && cr.CompositorWanted()
	switch {
	case want && w.comp == nil:
		w.comp = &compositorThread{quit: make(chan struct{})}
		go w.runCompositor(cr, w.comp)
	case !want && w.comp != nil:
		w.stopCompositorThread()
	}
}

func (w *Window) stopCompositorThread() {
	if w.comp != nil {
		close(w.comp.quit)
		w.comp = nil
	}
}

func (w *Window) runCompositor(cr render.CompositorRenderer, ct *compositorThread) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var clock frameClock
	for {
		select {
		case <-ct.quit:
			return
		case <-timer.C:
		}
		period := time.Duration(w.refreshPeriod.Load())
		if period <= 0 {
			period = defaultRefreshPeriod
		}
		start := time.Now()
		clock.period = period
		if since := time.Duration(start.UnixNano() - w.lastPresent.Load()); since < period*5/4 {
			// The UI thread is producing frames (painting a scroll, say):
			// it shows the layer itself. Presenting in between would only
			// double the frame rate and hold the renderer it waits for.
			timer.Reset(max(period*5/4-since, time.Millisecond))
			continue
		}
		t := start
		if !frameClockOff {
			t = clock.advance(start)
		}
		if cr.RedrawFromCompositor(t) {
			ct.frames.Add(1)
			if debugIdle {
				idleCompFrames.Add(1)
			}
		}
		// Present blocks for a drawable when the display is behind, which
		// paces this loop on vsync; the sleep paces it when it does not.
		timer.Reset(max(period-time.Since(start), time.Millisecond))
	}
}

// CompositorDriving reports whether the compositor thread animates this
// window's compositor-safe layers: their owners need not request GPU
// redraws for them. UI thread.
func (w *Window) CompositorDriving() bool { return w != nil && w.comp != nil }

// CompositorFrames reports how many frames the compositor thread presented
// for this window (tests, diagnostics).
func (w *Window) CompositorFrames() int64 {
	if w == nil || w.comp == nil {
		return 0
	}
	return w.comp.frames.Load()
}

var idleCompFrames atomic.Int64
