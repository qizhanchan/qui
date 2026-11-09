//go:build darwin && cgo && !qui_nometal

package render

import (
	"image"
	"testing"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
)

// newOffscreenMetal binds a MetalRenderer to an offscreen layer, or skips
// when the machine has no Metal device.
func newOffscreenMetal(t *testing.T, w, h int) *MetalRenderer {
	t.Helper()
	layer := newTestMetalLayer(w, h)
	t.Cleanup(func() { releaseTestMetalLayer(layer) })
	r := NewMetalRenderer()
	if !r.SetMetalLayer(layer) {
		t.Skip("no Metal device")
	}
	t.Cleanup(r.Close)
	return r
}

func near(a, b uint8) bool { return a-b < 3 || b-a < 3 }

func TestMetalCompositesVideoUnderTheHole(t *testing.T) {
	r := newOffscreenMetal(t, 64, 64)
	c := r.Begin(gfx.Size{W: 64, H: 64})
	c.FillRect(gfx.Rect{W: 64, H: 64}, gfx.Color{B: 1, A: 1})
	c.ClearRect(gfx.Rect{X: 16, Y: 16, W: 32, H: 32})
	y := make([]byte, 32*32)
	u := make([]byte, 16*16)
	v := make([]byte, 16*16)
	for i := range y {
		y[i] = 200
	}
	for i := range u {
		u[i], v[i] = 100, 50
	}
	planes := [3]YUVPlane{
		{Pix: unsafe.Pointer(&y[0]), Stride: 32, W: 32, H: 32},
		{Pix: unsafe.Pointer(&u[0]), Stride: 16, W: 16, H: 16},
		{Pix: unsafe.Pointer(&v[0]), Stride: 16, W: 16, H: 16},
	}
	identity := [9]float32{1, 0, 0, 0, 1, 0, 0, 0, 1}
	drew := false
	c.(RetainedMetalCanvas).QueueRetainedMetalDraw("video", gfx.Rect{X: 16, Y: 16, W: 32, H: 32}, func(mc MetalCompositor) {
		drew = mc.DrawYUV420("video", planes, identity, [3]float32{}, [3]float32{1, 1, 1},
			gfx.Rect{W: 32, H: 32}, gfx.Rect{X: 16, Y: 16, W: 32, H: 32})
	})
	r.End()
	if !drew {
		t.Fatal("DrawYUV420 failed")
	}
	got := r.readShown()
	if got == nil {
		t.Fatal("no presented frame to read")
	}
	px := func(x, y int) [4]uint8 {
		i := got.PixOffset(x, y)
		return [4]uint8{got.Pix[i], got.Pix[i+1], got.Pix[i+2], got.Pix[i+3]}
	}
	// Inside the hole: the converted video colour (identity conversion).
	if p := px(32, 32); !near(p[0], 200) || !near(p[1], 100) || !near(p[2], 50) {
		t.Errorf("video pixel = %v, want ~(200,100,50)", p)
	}
	// Outside: the 2D layer over it.
	if p := px(4, 4); p[0] != 0 || p[1] != 0 || p[2] != 255 {
		t.Errorf("2D pixel = %v, want (0,0,255)", p)
	}
}

// stripeFrame paints rows whose colour encodes their content row, so a
// pixel moved to the wrong place shows.
func stripeFrame(c gfx.Canvas, off, y0, y1 int) {
	for y := y0; y < y1; y++ {
		n := y + off
		c.FillRect(gfx.Rect{Y: float32(y), W: 64, H: 1},
			gfx.Color{R: float32(n%17) / 16, G: float32(n%7) / 6, B: float32(n%5) / 4, A: 1})
	}
}

func TestMetalScrollPixelsOnTheGPU(t *testing.T) {
	r := newOffscreenMetal(t, 64, 64)
	c := r.Begin(gfx.Size{W: 64, H: 64})
	stripeFrame(c, 0, 0, 64)
	r.End()
	t.Logf("IOSurface frame buffer: %v", r.surfaceBase != nil)

	// Content scrolls by 10 rows: move the pixels up, paint the 10 new
	// rows at the bottom, upload only those.
	if !r.ScrollPixels(image.Rect(0, 0, 64, 64), 0, -10) {
		t.Fatal("ScrollPixels declined")
	}
	c = r.Begin(gfx.Size{W: 64, H: 64})
	c.ClipRect(gfx.Rect{Y: 54, W: 64, H: 10})
	stripeFrame(c, 10, 54, 64)
	r.SetDamageRects([]image.Rectangle{image.Rect(0, 54, 64, 64)})
	r.End()

	got := r.readShown()
	if got == nil {
		t.Fatal("no presented frame to read")
	}
	// Reference: the scrolled content painted from scratch. (With an
	// IOSurface the CPU frame and the texture are one buffer, so comparing
	// them alone would not catch a move that never happened.)
	ref := newOffscreenMetal(t, 64, 64)
	stripeFrame(ref.Begin(gfx.Size{W: 64, H: 64}), 10, 0, 64)
	ref.End()
	want := ref.readShown()
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x += 16 {
			i := got.PixOffset(x, y)
			if string(got.Pix[i:i+3]) != string(want.Pix[i:i+3]) {
				t.Fatalf("presented (%d,%d) = %v, want %v", x, y, got.Pix[i:i+4], want.Pix[i:i+4])
			}
			if string(got.Pix[i:i+3]) != string(r.img.Pix[i:i+3]) {
				t.Fatalf("presented (%d,%d) = %v, CPU frame %v", x, y, got.Pix[i:i+4], r.img.Pix[i:i+4])
			}
		}
	}
}
