package htmlcss

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// overlayRenderer is a CPU renderer whose canvas can also take retained
// overlay draws, as WebGPURenderer's does: enough to exercise the GPU
// layer path without a GPU.
type overlayRenderer struct {
	*qui.CPURenderer
	fn      func(qui.Compositor)
	bounds  qui.Rect
	queued  int
	redraws int
}

type overlayCanvas struct {
	qui.Canvas
	r *overlayRenderer
}

func (c overlayCanvas) ClipBounds() qui.Rect { return c.Canvas.(qui.ClipAware).ClipBounds() }
func (c overlayCanvas) ClipBoundsLogical() qui.Rect {
	return c.Canvas.(qui.ClipAware).ClipBoundsLogical()
}
func (c overlayCanvas) QueueRetainedOverlayDraw(key any, b qui.Rect, fn func(qui.Compositor)) {
	c.r.fn, c.r.bounds = fn, b
	c.r.queued++
}

func (r *overlayRenderer) Begin(size qui.Size) qui.Canvas {
	return overlayCanvas{Canvas: r.CPURenderer.Begin(size), r: r}
}
func (r *overlayRenderer) ForgetRetainedDraw(key any) {
	if _, isView := key.(*widgets.ScrollView); isView {
		r.fn = nil
	}
}
func (r *overlayRenderer) RedrawRetained() bool { r.redraws++; return r.fn != nil }

// fakeCompositor records where the textures land.
type fakeCompositor struct{ dst []qui.Rect }

func (f *fakeCompositor) DrawPixelBuffer(unsafe.Pointer, qui.Rect, qui.Rect) bool { return false }
func (f *fakeCompositor) DrawPixels(key any, version uint64, pix unsafe.Pointer, stride, w, h int, bgra bool, src, dst qui.Rect) bool {
	f.dst = append(f.dst, dst)
	return true
}
func (f *fakeCompositor) DrawYUV420(any, uint64, [3]qui.YUVPlane, [9]float32, [3]float32, [3]float32, qui.Rect, qui.Rect) bool {
	return false
}
func (f *fakeCompositor) FramebufferSize() qui.Size { return qui.Size{} }

func (r *overlayRenderer) replay() []qui.Rect {
	if r.fn == nil {
		return nil
	}
	f := &fakeCompositor{}
	r.fn(f)
	return f.dst
}

// On a renderer that composites over the 2D layer, a marquee row is not
// rasterized while it moves: its card bitmaps are textures placed at the
// row's current offset by a retained overlay draw, and the animation's
// frames are GPU redraws that paint nothing. A fixed box over the row
// keeps its area: the 2D frame draws the row there, under the box.
func TestMarqueeGPULayer(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`<!doctype html><html><head><style>
@keyframes marquee { from { transform: translateX(0) } to { transform: translateX(-100%) } }
body { margin: 0; background: #f6f7f9 }
nav { position: fixed; top: 0; left: 0; right: 0; height: 40px; background: white; z-index: 10 }
.clip { position: relative; display: flex; overflow-x: clip }
.row { display: flex; padding: 30px 0; animation: marquee 4s linear infinite }
.card { min-width: 160px; display: flex; justify-content: center }
.pic { width: 120px; height: 90px; border-radius: 12px; transform: rotate(3deg) }
</style></head><body><nav id=nav>nav</nav><div style="height:60px"></div><div class=clip><div id=row class=row>`)
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&sb, `<div class=card><div class=pic style="background:#%02x%02x%02x"></div></div>`,
			(i*61)%256, (i*97)%256, (i*29)%256)
	}
	sb.WriteString(`</div></div><div style="height:1200px">text below</div></body></html>`)

	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win, err := qui.NewHeadlessWindowForTest(500, 360)
	if err != nil {
		t.Skip(err)
	}
	r := &overlayRenderer{CPURenderer: qui.NewCPURenderer()}
	win.SetRenderer(r)
	view := widgets.NewScrollView()
	view.FixedViewport = true
	view.OverlayBar = true
	view.PixelScroll = true
	view.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}
	win.SetRoot(view)
	rd := RenderDoc(sb.String(), "", Options{Window: win, ViewportWidth: 500, ViewportHeight: 360, DocumentRoot: true})
	view.SetContentAuto(rd.Root)
	row := rd.ByID["row"].(*El)
	step := func() { time.Sleep(20 * time.Millisecond); win.Step() }
	row.SetAttr("style", "color: black")
	rd.Engine.FlushPendingStyles()
	win.Invalidate()
	for i := 0; i < 4; i++ {
		step()
	}
	if !row.Box.GPULayerFollows() || r.fn == nil {
		t.Fatalf("the marquee row is not composited on the GPU (queued %d, follows %v, cpu layer %v)",
			r.queued, row.Box.GPULayerFollows(), row.Box.CompositeLayerActive())
	}
	first := r.replay()
	if len(first) == 0 {
		t.Fatal("the overlay draw placed no card textures")
	}
	clip := r.bounds
	for _, d := range first {
		if d.X < clip.X-0.5 || d.Y < clip.Y-0.5 || d.X+d.W > clip.X+clip.W+0.5 || d.Y+d.H > clip.Y+clip.H+0.5 {
			t.Fatalf("texture %v drawn outside the layer's clip %v", d, clip)
		}
	}

	// Animation frames repaint nothing and only redraw on the GPU.
	seq, redraws := win.PaintSeq(), r.redraws
	for i := 0; i < 5; i++ {
		step()
	}
	if win.PaintSeq() != seq {
		t.Fatalf("animation frames rasterized: paint seq %d -> %d", seq, win.PaintSeq())
	}
	if r.redraws == redraws {
		t.Fatal("the animation asked for no GPU redraw")
	}
	moved := r.replay()
	// The first card is cut at the clip's edge; the second shows the move.
	if len(first) < 2 || len(moved) < 2 || moved[1].X >= first[1].X {
		t.Fatalf("textures did not follow the row leftwards: %v -> %v", first, moved)
	}

	// A fixed box over part of the row: the row stays on the GPU, the
	// textures leave the box's area out, and that area alone is repainted
	// in the 2D frame each animation frame, so the box stays on top.
	rd.ByID["nav"].(*El).SetAttr("style", "height: 110px")
	rd.Engine.FlushPendingStyles()
	for i := 0; i < 3; i++ {
		step()
	}
	covered := row.Box.GPULayerCovered()
	if r.fn == nil || !row.Box.GPULayerFollows() || len(covered) == 0 {
		t.Fatalf("a partly covered row should stay on the GPU with covered parts (fn %v, follows %v, covered %v)",
			r.fn != nil, row.Box.GPULayerFollows(), covered)
	}
	scale := float32(1)
	if sn := r.Snapshot(); sn != nil {
		scale = float32(sn.Rect.Dx()) / 500
	}
	navBottom := 110 * scale
	for _, d := range r.replay() {
		if d.Y < navBottom-0.5 {
			t.Fatalf("texture %v drawn over the fixed box (bottom %v)", d, navBottom)
		}
	}
	seq = win.PaintSeq()
	for i := 0; i < 3; i++ {
		step()
	}
	if win.PaintSeq() == seq {
		t.Fatal("the covered part of the row is not repainted while it animates")
	}
	// The box covering everything still works the same way.
	rd.ByID["nav"].(*El).SetAttr("style", "height: 300px")
	rd.Engine.FlushPendingStyles()
	for i := 0; i < 3; i++ {
		step()
	}
	if got := r.replay(); len(got) != 0 {
		t.Fatalf("textures drawn under a box that covers the whole row: %v", got)
	}
}
