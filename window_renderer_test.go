package qui

import (
	"testing"
	"unsafe"

	"github.com/qizhanchan/qui/internal/render"
)

// fakeLayerSurface is a surface with a CAMetalLayer path that never hands
// out a layer (there is no compositor in a test).
type fakeLayerSurface struct {
	*fakeSurface
	layer unsafe.Pointer // nil unless a test sets one
}

func (s *fakeLayerSurface) MetalLayer() unsafe.Pointer { return s.layer }

// layerFakeWindow is a fake platform window whose surface offers a GPU
// target (like cocoa).
type layerFakeWindow struct {
	*fakePlatformWindow
	ms *fakeLayerSurface
}

func (f *layerFakeWindow) surface() platformSurface { return f.ms }

func newLayerFakeWindow(t *testing.T) *Window {
	t.Helper()
	f := newFakePlatformWindow()
	f.capsVal.GPUSurface = true
	mf := &layerFakeWindow{fakePlatformWindow: f, ms: &fakeLayerSurface{fakeSurface: &fakeSurface{owner: f}}}
	w := &Window{plat: mf, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	w.initUIThreadOwnership()
	return w
}

// withWebGPU pretends WebGPU is (or is not) usable in this process.
func withWebGPU(t *testing.T, ok bool) {
	t.Helper()
	oldNew, oldOK := newWebGPURenderer, webgpuUsable
	t.Cleanup(func() { newWebGPURenderer, webgpuUsable = oldNew, oldOK })
	webgpuUsable = func() bool { return ok }
	// A stand-in WebGPU renderer (also in builds without WebGPU) that, like
	// the real one with no layer or a lost device, cannot bind a surface.
	newWebGPURenderer = func() Renderer { return &fakeWebGPURenderer{} }
}

type fakeWebGPURenderer struct{ CPURenderer }

func (*fakeWebGPURenderer) SetMetalLayer(unsafe.Pointer) bool          { return false }
func (*fakeWebGPURenderer) SetSurfaceTarget(render.SurfaceTarget) bool { return false }

func isWebGPU(r Renderer) bool {
	if _, ok := r.(*fakeWebGPURenderer); ok {
		return true
	}
	_, ok := r.(render.NativeWindowRenderer)
	return ok
}

func isCPU(r Renderer) bool {
	switch r.(type) {
	case *CPURenderer, *AutoRenderer:
		return true
	}
	return false
}

func TestPreferredRendererPicksWebGPU(t *testing.T) {
	w := newLayerFakeWindow(t)
	withWebGPU(t, true)
	t.Setenv("QUI_RENDERER", "")
	if r := w.preferredRenderer(NewAutoRenderer()); !isWebGPU(r) {
		t.Fatalf("GPU surface + Dawn gave %T, want WebGPU", r)
	}
	t.Setenv("QUI_RENDERER", "cpu")
	if r := w.preferredRenderer(NewAutoRenderer()); !isCPU(r) {
		t.Fatalf("QUI_RENDERER=cpu gave %T", r)
	}
	// A removed renderer's name is reported and ignored.
	t.Setenv("QUI_RENDERER", "metal")
	if r := w.preferredRenderer(NewAutoRenderer()); !isWebGPU(r) {
		t.Fatalf("QUI_RENDERER=metal gave %T, want the default (WebGPU)", r)
	}
}

func TestPreferredRendererWithoutDawn(t *testing.T) {
	w := newLayerFakeWindow(t)
	withWebGPU(t, false)
	t.Setenv("QUI_RENDERER", "")
	if r := w.preferredRenderer(NewAutoRenderer()); !isCPU(r) {
		t.Fatalf("without Dawn: %T, want the CPU path", r)
	}
}

func TestPreferredRendererWithoutGPUSurface(t *testing.T) {
	f := newFakePlatformWindow()
	w := &Window{plat: f, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	withWebGPU(t, true)
	if r := w.preferredRenderer(NewAutoRenderer()); !isCPU(r) {
		t.Fatalf("a surface without a GPU path gets the CPU path, got %T", r)
	}
}

// A WebGPU renderer that cannot use the window's surface (no layer here;
// a lost device in practice) leaves the window on the CPU path for good.
func TestBindSurfaceFallsBackToCPU(t *testing.T) {
	w := newLayerFakeWindow(t)
	withWebGPU(t, true)
	t.Setenv("QUI_RENDERER", "")
	w.SetRenderer(NewAutoRenderer())
	if !isWebGPU(w.renderer) {
		t.Fatalf("renderer = %T, want WebGPU", w.renderer)
	}
	w.bindSurface()
	if !isCPU(w.renderer) || !w.noWebGPU {
		t.Fatalf("no layer: renderer = %T noWebGPU=%v, want CPU", w.renderer, w.noWebGPU)
	}
	w.SetRenderer(NewAutoRenderer())
	if !isCPU(w.renderer) {
		t.Fatalf("after the failure AutoRenderer gave %T again", w.renderer)
	}
}

// flakyWebGPURenderer binds its surface until fail is set (a device lost
// mid-run).
type flakyWebGPURenderer struct {
	CPURenderer
	fail *bool
}

func (r *flakyWebGPURenderer) SetMetalLayer(unsafe.Pointer) bool          { return !*r.fail }
func (r *flakyWebGPURenderer) SetSurfaceTarget(render.SurfaceTarget) bool { return !*r.fail }

// Falling back to the CPU path mid-run must repaint the whole window, even
// when the frame that noticed only had a small dirty rect: the new
// renderer's frame starts empty.
func TestRendererFailureRepaintsEverything(t *testing.T) {
	w := newLayerFakeWindow(t)
	w.plat.(*layerFakeWindow).ms.layer = unsafe.Pointer(&w)
	fail := false
	w.SetRenderer(&flakyWebGPURenderer{fail: &fail})
	red := Color{R: 1, A: 1}
	w.SetRoot(&fillWidget{c: red})
	w.Step()
	if _, ok := w.renderer.(*flakyWebGPURenderer); !ok {
		t.Fatalf("first frame: renderer = %T, want the GPU stand-in", w.renderer)
	}

	fail = true
	webgpuOld := webgpuUsable
	t.Cleanup(func() { webgpuUsable = webgpuOld })
	webgpuUsable = func() bool { return false }
	w.resetDirty()
	w.InvalidateRect(Rect{X: 10, Y: 10, W: 4, H: 4})
	w.Step()

	src, ok := w.renderer.(CPUFrameSource)
	if !ok || !isCPU(w.renderer) {
		t.Fatalf("renderer = %T, want the CPU path", w.renderer)
	}
	img := src.FrameImage()
	if img == nil {
		t.Fatal("no CPU frame")
	}
	b := img.Bounds()
	c := img.RGBAAt(b.Max.X-5, b.Max.Y-5)
	if c.R != 255 || c.A != 255 {
		t.Fatalf("far corner = %v after the fallback, want red (whole window repainted)", c)
	}
}
