package htmlcss

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// RFC 0005 P3: a CSS marquee in a page running on a page thread keeps
// moving while that thread is stuck in a long script. The page commits the
// marquee's compositor-safe overlay draw; the host window's compositor
// thread replays it from the animation curve, with no page involvement.
func TestMarqueeOnPageThreadKeepsMovingWhilePageBlocked(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><style>
@keyframes marquee { from { transform: translateX(0) } to { transform: translateX(-100%) } }
body { margin: 0 }
.clip { display: flex; overflow-x: clip }
.row { display: flex; padding: 20px 0; animation: marquee 3s linear infinite }
.card { min-width: 150px; height: 80px; margin: 0 10px; border-radius: 10px }
</style></head><body><div class=clip><div id=row class=row>`)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, `<div class=card style="background:#%02x%02x%02x"></div>`, (i*61)%256, (i*97)%256, (i*29)%256)
	}
	sb.WriteString(`</div></div></body></html>`)
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })

	host, err := qui.NewHeadlessWindowForTest(500, 200)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	r := &compRenderer{overlayRenderer: &overlayRenderer{CPURenderer: qui.NewCPURenderer()}}
	host.SetRenderer(r)

	th := qui.NewPageThread("marquee")
	defer th.Close()
	h := th.NewPageHost(qui.PageHostConfig{Width: 500, Height: 200, Setup: func(w *qui.Window) {
		view := widgets.NewScrollView()
		view.FixedViewport = true
		view.PixelScroll = true
		w.SetRoot(view)
		rd := RenderDoc(sb.String(), "", Options{Window: w, ViewportWidth: 500, ViewportHeight: 200, DocumentRoot: true})
		view.SetContentAuto(rd.Root)
		row := rd.ByID["row"].(*El)
		row.SetAttr("style", "color: black") // restyle in the window: the animator registers
		rd.Engine.FlushPendingStyles()
		w.Invalidate()
	}})
	host.SetRoot(qui.NewPageView(h))

	deadline := time.Now().Add(10 * time.Second)
	for !r.CompositorWanted() {
		if time.Now().After(deadline) {
			t.Fatal("the host never received the page's compositor-safe marquee draw")
		}
		host.Step()
		time.Sleep(10 * time.Millisecond)
	}
	host.Step()

	release := make(chan struct{})
	blocked := make(chan struct{})
	h.Do(func(*qui.Window) { close(blocked); <-release })
	<-blocked
	defer close(release)

	t0 := time.Now()
	a, b := r.replayAt(t0), r.replayAt(t0.Add(500*time.Millisecond))
	if len(a) < 2 || len(b) < 2 || b[1].X >= a[1].X {
		t.Fatalf("the marquee draw does not move with time: %v -> %v", a, b)
	}

	// The host's compositor thread presents frames on its own meanwhile.
	r.mu.Lock()
	r.frames, r.at = nil, nil
	r.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	r.mu.Lock()
	frames := append([][]qui.Rect(nil), r.frames...)
	r.mu.Unlock()
	if len(frames) < 5 {
		t.Fatalf("the host compositor presented %d frames during a 150 ms page stall, want several", len(frames))
	}
	first, last := frames[0], frames[len(frames)-1]
	if len(first) < 2 || len(last) < 2 || last[1].X >= first[1].X {
		t.Fatalf("the marquee did not move during the page stall: %v -> %v", first, last)
	}
}
