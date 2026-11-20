package media

import (
	"image"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
)

// hostRenderer is a CPU renderer whose canvas offers what a GPU-presenting
// one does: retained draws under the 2D layer and compositor overlays.
type hostRenderer struct {
	*qui.CPURenderer
	mu    sync.Mutex
	draws map[any]func(qui.Compositor)
}

type hostCanvas struct {
	qui.Canvas
	r *hostRenderer
}

func (c hostCanvas) ClipBounds() qui.Rect        { return c.Canvas.(qui.ClipAware).ClipBounds() }
func (c hostCanvas) ClipBoundsLogical() qui.Rect { return c.Canvas.(qui.ClipAware).ClipBoundsLogical() }
func (c hostCanvas) QueueRetainedDraw(key any, b qui.Rect, fn func(qui.Compositor)) {
	c.r.mu.Lock()
	c.r.draws[key] = fn
	c.r.mu.Unlock()
}
func (c hostCanvas) QueueRetainedOverlayDraw(any, qui.Rect, func(qui.Compositor))   {}
func (c hostCanvas) QueueCompositorOverlayDraw(any, qui.Rect, func(qui.Compositor)) {}

func (r *hostRenderer) Begin(size qui.Size) qui.Canvas {
	return hostCanvas{Canvas: r.CPURenderer.Begin(size), r: r}
}

// recComp records where a draw landed.
type recComp struct{ dst []qui.Rect }

func (c *recComp) DrawPixelBuffer(unsafe.Pointer, qui.Rect, qui.Rect) bool { return false }
func (c *recComp) DrawPixels(_ any, _ uint64, _ unsafe.Pointer, _, _, _ int, _ bool, _, dst qui.Rect) bool {
	c.dst = append(c.dst, dst)
	return true
}
func (c *recComp) DrawYUV420(_ any, _ uint64, _ [3]qui.YUVPlane, _ [9]float32, _, _ [3]float32, _, dst qui.Rect) bool {
	c.dst = append(c.dst, dst)
	return true
}
func (c *recComp) FramebufferSize() qui.Size { return qui.Size{W: 400, H: 300} }

// A FrameView on a page thread hands its picture to the host window as a
// retained draw under the frame, placed where the host shows the page, and
// the host clears the 2D layer there so it shows through (RFC 0005 P5b).
func TestFrameViewOnPageThreadCompositesInHost(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	host, err := qui.NewHeadlessWindowForTest(400, 300)
	if err != nil {
		t.Skip(err)
	}
	defer host.Destroy()
	r := &hostRenderer{CPURenderer: qui.NewCPURenderer(), draws: map[any]func(qui.Compositor){}}
	host.SetRenderer(r)

	th := qui.NewPageThread("video")
	defer th.Close()
	fv := NewFrameView()
	h := th.NewPageHost(qui.PageHostConfig{Width: 200, Height: 100, Setup: func(w *qui.Window) {
		root := qui.NewContainer(qui.AbsoluteLayout{})
		root.AddChild(fv)
		fv.SetAbsolutePosition(qui.AbsolutePosition{Left: 20, Top: 10, Width: 80, Height: 40, Anchor: qui.AnchorLeft | qui.AnchorTop})
		w.SetRoot(root)
	}})
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	fv.PushRGBA(img)
	view := qui.NewPageView(h)
	// The view sits about 100pt right, 50pt down in the host.
	root := qui.NewContainer(qui.AbsoluteLayout{})
	root.AddChild(view)
	view.SetAbsolutePosition(qui.AbsolutePosition{Left: 100, Top: 50, Width: 200, Height: 100, Anchor: qui.AnchorLeft | qui.AnchorTop})
	host.SetRoot(root)

	var fn func(qui.Compositor)
	deadline := time.Now().Add(5 * time.Second)
	for fn == nil {
		if time.Now().After(deadline) {
			t.Fatal("the host never received the page's video draw")
		}
		host.Step()
		time.Sleep(5 * time.Millisecond)
		r.mu.Lock()
		for k, d := range r.draws {
			if _, page := k.(*FrameView); !page {
				fn = d
			}
		}
		r.mu.Unlock()
	}
	host.Invalidate()
	host.Step()
	c := &recComp{}
	fn(c)
	s := host.DevicePixelRatio()
	fbc := make(chan qui.Rect, 1)
	h.Do(func(*qui.Window) { fbc <- fv.Bounds() })
	fb, vb := <-fbc, view.Bounds()
	if len(c.dst) != 1 {
		t.Fatalf("draw composited %d frames, want 1", len(c.dst))
	}
	want := qui.Rect{X: (vb.X + fb.X) * s, Y: (vb.Y + fb.Y) * s, W: fb.W * s, H: fb.H * s}
	got := c.dst[0]
	if abs32(got.X-want.X) > 1 || abs32(got.Y-want.Y) > 1 || abs32(got.W-want.W) > 1 || abs32(got.H-want.H) > 1 {
		t.Fatalf("video lands at %v, want %v", got, want)
	}
	// The host frame has a transparent hole there.
	shot := host.Snapshot()
	if a := shot.RGBAAt(int((vb.X+fb.X+fb.W/2)*s), int((vb.Y+fb.Y+fb.H/2)*s)).A; a != 0 {
		t.Fatalf("host frame alpha over the video is %d, want a hole (0)", a)
	}
	if px := shot.RGBAAt(int((vb.X+fb.X+fb.W+5)*s), int((vb.Y+fb.Y+5)*s)); px.A != 255 {
		t.Fatalf("host frame beside the video is %v, want the page's opaque frame", px)
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
