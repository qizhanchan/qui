//go:build darwin && cgo && !qui_nometal

package render

import (
	"image"
	"testing"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// newOffscreenWebGPU binds a WebGPURenderer to an offscreen CAMetalLayer,
// or skips when Dawn is not installed. shared selects the IOSurface frame
// (when the device supports it) or the upload path.
func newOffscreenWebGPU(t *testing.T, w, h int, shared bool) *WebGPURenderer {
	t.Helper()
	if _, err := wgpu.Shared(); err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	wgpu.SetErrorHandler(func(kind int, msg string) { t.Errorf("WebGPU error %d: %s", kind, msg) })
	t.Cleanup(func() { wgpu.SetErrorHandler(nil) })
	old := webgpuNoShared
	webgpuNoShared = !shared
	t.Cleanup(func() { webgpuNoShared = old })
	layer := newTestMetalLayer(w, h)
	t.Cleanup(func() { releaseTestMetalLayer(layer) })
	r := NewWebGPURenderer()
	r.keepShown = true
	if !r.SetMetalLayer(layer) {
		t.Fatal("SetMetalLayer failed")
	}
	t.Cleanup(r.Close)
	return r
}

func forBothFrames(t *testing.T, f func(t *testing.T, shared bool)) {
	t.Run("shared", func(t *testing.T) { f(t, true) })
	t.Run("upload", func(t *testing.T) { f(t, false) })
}

func pixelAt(img *image.RGBA, x, y int) [4]uint8 {
	i := img.PixOffset(x, y)
	return [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

// The presented frame is the CPU frame, pixel for pixel.
func TestWebGPUPresentsTheCPUFrame(t *testing.T) {
	forBothFrames(t, func(t *testing.T, shared bool) {
		const w, h = 37, 23
		r := newOffscreenWebGPU(t, w, h, shared)
		c := r.Begin(gfx.Size{W: w, H: h})
		if dev, _ := wgpu.Shared(); shared && r.shared == nil {
			t.Errorf("IOSurface frame not used (device import support: %v)", dev.CanImportIOSurface())
		}
		stripeFrame(c, 3, 0, h)
		c.FillRect(gfx.Rect{X: 5, Y: 5, W: 9, H: 7}, gfx.Color{R: 1, G: 0.5, A: 0.5})
		r.End()
		got := r.readShown()
		if got == nil {
			t.Fatal("nothing presented")
		}
		cpu := r.Snapshot()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if a, b := pixelAt(got, x, y), pixelAt(cpu, x, y); a != b {
					t.Fatalf("(%d,%d): presented %v, CPU %v", x, y, a, b)
				}
			}
		}
	})
}

func TestWebGPUCompositesVideoUnderTheHole(t *testing.T) {
	r := newOffscreenWebGPU(t, 64, 64, true)
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
	c.(RetainedCompositorCanvas).QueueRetainedDraw("video", gfx.Rect{X: 16, Y: 16, W: 32, H: 32}, func(mc Compositor) {
		drew = mc.DrawYUV420("video", planes, identity, [3]float32{}, [3]float32{1, 1, 1},
			gfx.Rect{W: 32, H: 32}, gfx.Rect{X: 16, Y: 16, W: 32, H: 32})
	})
	r.End()
	if !drew {
		t.Fatal("DrawYUV420 failed")
	}
	got := r.readShown()
	if p := pixelAt(got, 32, 32); !near(p[0], 200) || !near(p[1], 100) || !near(p[2], 50) {
		t.Errorf("video pixel = %v, want ~(200,100,50)", p)
	}
	if p := pixelAt(got, 4, 4); p != [4]uint8{0, 0, 255, 255} {
		t.Errorf("2D pixel = %v, want (0,0,255,255)", p)
	}

	// A RequestGPURedraw frame replays the video with new content and
	// keeps the 2D layer.
	for i := range y {
		y[i] = 90
	}
	if !r.RedrawRetained() {
		t.Fatal("RedrawRetained declined")
	}
	got = r.readShown()
	if p := pixelAt(got, 32, 32); !near(p[0], 90) {
		t.Errorf("redrawn video pixel = %v, want R ~90", p)
	}
	if p := pixelAt(got, 4, 4); p != [4]uint8{0, 0, 255, 255} {
		t.Errorf("redrawn 2D pixel = %v", p)
	}
}

func TestWebGPUDrawPixels(t *testing.T) {
	r := newOffscreenWebGPU(t, 32, 32, true)
	c := r.Begin(gfx.Size{W: 32, H: 32})
	c.FillRect(gfx.Rect{W: 32, H: 32}, gfx.Color{G: 1, A: 1})
	c.ClearRect(gfx.Rect{X: 8, Y: 8, W: 16, H: 16})
	bgra := make([]byte, 4*4*4)
	for i := 0; i < len(bgra); i += 4 {
		bgra[i], bgra[i+1], bgra[i+2], bgra[i+3] = 10, 20, 230, 255 // B G R A
	}
	c.(RetainedCompositorCanvas).QueueRetainedDraw("px", gfx.Rect{X: 8, Y: 8, W: 16, H: 16}, func(mc Compositor) {
		mc.DrawPixels("px", unsafe.Pointer(&bgra[0]), 16, 4, 4, true, gfx.Rect{W: 4, H: 4}, gfx.Rect{X: 8, Y: 8, W: 16, H: 16})
	})
	r.End()
	got := r.readShown()
	if p := pixelAt(got, 16, 16); p != [4]uint8{230, 20, 10, 255} {
		t.Errorf("pixels = %v, want (230,20,10,255)", p)
	}
	r.ForgetRetainedDraw("px")
	if len(r.pixels) != 0 {
		t.Error("ForgetRetainedDraw kept the texture")
	}
}

func TestWebGPUScrollPixels(t *testing.T) {
	forBothFrames(t, func(t *testing.T, shared bool) {
		r := newOffscreenWebGPU(t, 64, 64, shared)
		stripeFrame(r.Begin(gfx.Size{W: 64, H: 64}), 0, 0, 64)
		r.End()
		if !r.ScrollPixels(image.Rect(0, 0, 64, 64), 0, -10) {
			t.Fatal("ScrollPixels declined")
		}
		c := r.Begin(gfx.Size{W: 64, H: 64})
		c.ClipRect(gfx.Rect{Y: 54, W: 64, H: 10})
		stripeFrame(c, 10, 54, 64)
		// A repaint inside the moved area must survive the move.
		c.ClipRect(gfx.Rect{W: 64, H: 64})
		r.SetDamageRects([]image.Rectangle{image.Rect(0, 54, 64, 64)})
		r.End()

		got := r.readShown()
		ref := newOffscreenWebGPU(t, 64, 64, shared)
		stripeFrame(ref.Begin(gfx.Size{W: 64, H: 64}), 10, 0, 64)
		ref.End()
		want := ref.readShown()
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x += 16 {
				if a, b := pixelAt(got, x, y), pixelAt(want, x, y); a != b {
					t.Fatalf("(%d,%d) = %v, want %v", x, y, a, b)
				}
			}
		}
	})
}

const solidWGSL = `
@vertex fn vs_main(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    var p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    return vec4f(p[i], 0.0, 1.0);
}
@fragment fn fs_main() -> @location(0) vec4f { return vec4f(1.0, 0.0, 1.0, 1.0); }
`

// A GPU draw renders offscreen and composites with DrawTexture; hooks draw
// straight into the frame, the pre hook under the 2D layer and the post
// hook over it.
func TestWebGPUDrawsAndHooks(t *testing.T) {
	r := newOffscreenWebGPU(t, 40, 40, true)
	dev, _ := wgpu.Shared()
	mod, err := dev.CreateShaderModule("solid", solidWGSL)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	off, err := dev.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{Module: mod,
		Targets: []wgpu.ColorTarget{{Format: wgpu.FormatRGBA8Unorm}}})
	if err != nil {
		t.Fatal(err)
	}
	defer off.Release()
	onFrame, err := dev.CreateRenderPipeline(wgpu.RenderPipelineDescriptor{Module: mod,
		Targets: []wgpu.ColorTarget{{Format: wgpu.FormatBGRA8Unorm}}})
	if err != nil {
		t.Fatal(err)
	}
	defer onFrame.Release()
	tex := dev.CreateTexture(wgpu.TextureDescriptor{Width: 8, Height: 8, Format: wgpu.FormatRGBA8Unorm,
		Usage: wgpu.UsageRenderAttachment | wgpu.UsageTextureBinding})
	defer tex.Destroy()
	view := tex.CreateView()
	defer view.Release()

	r.SetPreRender(func(f *GPUFrame) {
		f.RenderPass(func(p *wgpu.RenderPass) {
			p.SetPipeline(onFrame) // whole frame magenta, under everything
			p.Draw(3, 1, 0, 0)
		})
	})
	postRan := false
	r.SetPostRender(func(f *GPUFrame) {
		postRan = true
		f.RenderPass(func(p *wgpu.RenderPass) {
			p.SetScissorRect(0, 36, 40, 4) // bottom strip, over the 2D layer
			p.SetPipeline(onFrame)
			p.Draw(3, 1, 0, 0)
		})
	})
	c := r.Begin(gfx.Size{W: 40, H: 40})
	c.FillRect(gfx.Rect{W: 40, H: 40}, gfx.Color{G: 1, A: 1})
	c.ClearRect(gfx.Rect{X: 0, Y: 0, W: 10, H: 10})   // pre hook shows here
	c.ClearRect(gfx.Rect{X: 20, Y: 20, W: 10, H: 10}) // GPU draw shows here
	c.(GPUCanvas).QueueRetainedGPUDraw("g", gfx.Rect{X: 20, Y: 20, W: 10, H: 10}, func(f *GPUFrame) {
		p := f.Encoder.BeginRenderPass(wgpu.RenderPassDescriptor{Color: []wgpu.ColorAttachment{{View: view}}})
		p.SetPipeline(off)
		p.Draw(3, 1, 0, 0)
		p.End()
		f.DrawTexture(view, gfx.Rect{W: 8, H: 8}, gfx.Rect{X: 20, Y: 20, W: 10, H: 10}, TextureOpts{Opacity: 0.5})
	})
	r.End()
	if !postRan {
		t.Fatal("post hook did not run")
	}
	got := r.readShown()
	checks := []struct {
		x, y int
		want [4]uint8
	}{
		{5, 5, [4]uint8{255, 0, 255, 255}},   // pre hook through the hole
		{15, 15, [4]uint8{0, 255, 0, 255}},   // 2D layer
		{25, 25, [4]uint8{255, 0, 255, 255}}, // GPU texture at half opacity over the magenta pre hook
		{5, 38, [4]uint8{255, 0, 255, 255}},  // post hook over the 2D layer
	}
	for _, ck := range checks {
		if p := pixelAt(got, ck.x, ck.y); p != ck.want {
			t.Errorf("(%d,%d) = %v, want %v", ck.x, ck.y, p, ck.want)
		}
	}
}

func TestWebGPUTransparentFrame(t *testing.T) {
	r := newOffscreenWebGPU(t, 16, 16, true)
	r.SetTransparent(true)
	c := r.Begin(gfx.Size{W: 16, H: 16})
	c.FillRect(gfx.Rect{W: 8, H: 16}, gfx.Color{R: 1, A: 0.5})
	r.End()
	got := r.readShown()
	if p := pixelAt(got, 12, 8); p != [4]uint8{} {
		t.Errorf("uncovered pixel = %v, want transparent", p)
	}
	if p := pixelAt(got, 4, 8); !near(p[0], 128) || !near(p[3], 128) {
		t.Errorf("half-transparent pixel = %v, want ~(128,0,0,128)", p)
	}
}
