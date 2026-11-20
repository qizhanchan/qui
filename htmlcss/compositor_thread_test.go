package htmlcss

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// compRenderer is overlayRenderer with a compositor thread entry: frames
// it presents off the UI thread record where the textures landed.
type compRenderer struct {
	*overlayRenderer
	mu     sync.Mutex
	safe   bool
	frames [][]qui.Rect
	at     []time.Time
}

type compCanvas struct {
	overlayCanvas
	cr *compRenderer
}

func (c compCanvas) QueueCompositorOverlayDraw(key any, b qui.Rect, fn func(qui.Compositor)) {
	c.cr.mu.Lock()
	defer c.cr.mu.Unlock()
	c.QueueRetainedOverlayDraw(key, b, fn)
	c.cr.safe = true
}

func (r *compRenderer) Begin(size qui.Size) qui.Canvas {
	r.mu.Lock()
	r.safe = false
	r.mu.Unlock()
	return compCanvas{overlayCanvas{Canvas: r.CPURenderer.Begin(size), r: r.overlayRenderer}, r}
}

func (r *compRenderer) CompositorWanted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.safe && r.fn != nil
}
func (r *compRenderer) SetFrameTime(time.Time) {}
func (r *compRenderer) RedrawFromCompositor(t time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.safe || r.fn == nil {
		return false
	}
	f := &timedCompositor{t: t}
	r.fn(f)
	r.frames = append(r.frames, f.dst)
	r.at = append(r.at, t)
	return true
}

type timedCompositor struct {
	fakeCompositor
	t time.Time
}

func (c *timedCompositor) FrameTime() time.Time { return c.t }

// replayAt places the textures as a frame at t would.
func (r *compRenderer) replayAt(t time.Time) []qui.Rect {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := &timedCompositor{t: t}
	r.fn(f)
	return f.dst
}

// A marquee composited on the GPU is animated by the compositor thread,
// from the animation's curve, at the positions the UI thread would draw —
// and keeps moving while the UI thread is blocked (a long script).
func TestMarqueeCompositorThread(t *testing.T) {
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
	win, err := qui.NewHeadlessWindowForTest(500, 200)
	if err != nil {
		t.Skip(err)
	}
	r := &compRenderer{overlayRenderer: &overlayRenderer{CPURenderer: qui.NewCPURenderer()}}
	win.SetRenderer(r)
	view := widgets.NewScrollView()
	view.FixedViewport = true
	view.PixelScroll = true
	win.SetRoot(view)
	rd := RenderDoc(sb.String(), "", Options{Window: win, ViewportWidth: 500, ViewportHeight: 200, DocumentRoot: true})
	view.SetContentAuto(rd.Root)
	row := rd.ByID["row"].(*El)
	row.SetAttr("style", "color: black") // restyle in the window: the animator registers
	rd.Engine.FlushPendingStyles()
	win.Invalidate()
	for i := 0; i < 6; i++ {
		time.Sleep(17 * time.Millisecond)
		win.Step()
	}
	if !row.Box.GPULayerFollows() || !r.CompositorWanted() {
		t.Fatalf("the marquee is not a compositor-safe GPU layer (follows %v, wanted %v)", row.Box.GPULayerFollows(), r.CompositorWanted())
	}
	// The curve agrees with the UI thread: at the UI's last frame time it
	// puts the layer where the UI thread's own animation tick put the box.
	live, curve, ok := row.Box.GPULayerOffsetsForTest(win.LastFrameTimeForTest())
	if !ok || abs32(live[0]-curve[0]) > 0.01 || abs32(live[1]-curve[1]) > 0.01 {
		t.Fatalf("curve offset %v, UI thread offset %v (ok %v)", curve, live, ok)
	}
	if n := len(r.replayAt(time.Now())); n == 0 {
		t.Fatal("no textures placed")
	}

	// Block the UI thread for 150 ms.
	r.mu.Lock()
	r.frames, r.at = nil, nil
	r.mu.Unlock()
	time.Sleep(150 * time.Millisecond)
	r.mu.Lock()
	frames := append([][]qui.Rect(nil), r.frames...)
	r.mu.Unlock()
	if len(frames) < 5 {
		t.Fatalf("the compositor thread presented %d frames during a 150 ms stall, want several", len(frames))
	}
	first, last := frames[0], frames[len(frames)-1]
	if len(first) < 2 || len(last) < 2 || last[1].X >= first[1].X {
		t.Fatalf("textures did not move during the stall: %v -> %v", first, last)
	}
	// While the compositor thread drives the layer, the UI thread asks for
	// no redraw-only frames of its own.
	if !win.CompositorDriving() {
		t.Fatal("the compositor thread is not driving the layer")
	}
	redraws := r.redraws
	for i := 0; i < 4; i++ {
		time.Sleep(17 * time.Millisecond)
		win.Step()
	}
	if r.redraws != redraws {
		t.Fatalf("the UI thread presented %d redraw-only frames for a layer the compositor drives", r.redraws-redraws)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
