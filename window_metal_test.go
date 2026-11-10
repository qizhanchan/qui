package qui

import (
	"testing"
	"unsafe"

	"github.com/qizhanchan/qui/internal/render"
)

// fakeMetalSurface is a surface with a Metal path that never hands out a
// layer (there is no compositor in a test).
type fakeMetalSurface struct {
	*fakeSurface
}

func (s *fakeMetalSurface) MetalLayer() unsafe.Pointer { return nil }

// metalFakeWindow is a fake platform window whose surface offers Metal.
type metalFakeWindow struct {
	*fakePlatformWindow
	ms *fakeMetalSurface
}

func (f *metalFakeWindow) surface() platformSurface { return f.ms }

func newMetalFakeWindow(t *testing.T) (*Window, *metalFakeWindow) {
	t.Helper()
	if !render.MetalRendererAvailable {
		t.Skip("no Metal renderer in this build")
	}
	f := newFakePlatformWindow()
	f.capsVal.GPUSurface = true
	mf := &metalFakeWindow{fakePlatformWindow: f, ms: &fakeMetalSurface{fakeSurface: &fakeSurface{owner: f}}}
	w := &Window{plat: mf, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	w.initUIThreadOwnership()
	return w, mf
}

// withWebGPU pretends WebGPU is (or is not) usable in this process.
func withWebGPU(t *testing.T, ok bool) {
	t.Helper()
	oldNew, oldOK := newWebGPURenderer, webgpuUsable
	t.Cleanup(func() { newWebGPURenderer, webgpuUsable = oldNew, oldOK })
	webgpuUsable = func() bool { return ok }
	if newWebGPURenderer == nil {
		// Builds without WebGPU: a stand-in type the selection can return.
		newWebGPURenderer = func() Renderer { return &fakeWebGPURenderer{} }
	}
}

type fakeWebGPURenderer struct{ CPURenderer }

func isWebGPU(r Renderer) bool {
	if _, ok := r.(*fakeWebGPURenderer); ok {
		return true
	}
	_, ok := r.(render.NativeWindowRenderer)
	return ok
}

func TestPreferredRendererPicksMetal(t *testing.T) {
	w, _ := newMetalFakeWindow(t)
	withWebGPU(t, true)
	t.Setenv("QUI_RENDERER", "")
	if _, ok := w.preferredRenderer(NewAutoRenderer()).(*MetalRenderer); !ok {
		t.Fatalf("AutoRenderer on a Metal surface should become a MetalRenderer")
	}
	// An explicit renderer is the app's choice.
	cpu := NewCPURenderer()
	if got := w.preferredRenderer(cpu); got != Renderer(cpu) {
		t.Fatalf("CPURenderer replaced by %T", got)
	}
	t.Setenv("QUI_RENDERER", "webgpu")
	if r := w.preferredRenderer(NewAutoRenderer()); !isWebGPU(r) {
		t.Fatalf("QUI_RENDERER=webgpu gave %T", r)
	}
	t.Setenv("QUI_RENDERER", "cpu")
	if _, ok := w.preferredRenderer(NewAutoRenderer()).(*CPURenderer); !ok {
		t.Fatalf("QUI_RENDERER=cpu must give a CPURenderer")
	}
	t.Setenv("QUI_RENDERER", "")
	w.wantWebGPU = true
	if r := w.preferredRenderer(NewAutoRenderer()); !isWebGPU(r) {
		t.Fatalf("a window that needs WebGPU got %T", r)
	}
	withWebGPU(t, false)
	if _, ok := w.preferredRenderer(NewAutoRenderer()).(*MetalRenderer); !ok {
		t.Fatalf("without Dawn a WebGPU window stays on Metal")
	}
}

func TestPreferredRendererWithoutGPUSurface(t *testing.T) {
	f := newFakePlatformWindow()
	w := &Window{plat: f, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	withWebGPU(t, true)
	if _, ok := w.preferredRenderer(NewAutoRenderer()).(*CPURenderer); !ok {
		t.Fatalf("a surface without a GPU path gets a CPURenderer")
	}
}

func TestBindSurfaceFallsBackWithoutLayer(t *testing.T) {
	w, _ := newMetalFakeWindow(t)
	withWebGPU(t, false)
	t.Setenv("QUI_RENDERER", "")
	w.SetRenderer(NewAutoRenderer())
	if _, ok := w.renderer.(*MetalRenderer); !ok {
		t.Fatalf("renderer = %T, want MetalRenderer", w.renderer)
	}
	w.bindSurface()
	if _, ok := w.renderer.(*CPURenderer); !ok || !w.noMetal {
		t.Fatalf("no layer: renderer = %T noMetal=%v, want CPU", w.renderer, w.noMetal)
	}
}

func TestWebGPURequestSwitchesMetalWindow(t *testing.T) {
	w, _ := newMetalFakeWindow(t)
	withWebGPU(t, true)
	t.Setenv("QUI_RENDERER", "")
	w.SetRenderer(NewAutoRenderer())
	mr, ok := w.renderer.(*MetalRenderer)
	if !ok {
		t.Fatalf("renderer = %T, want MetalRenderer", w.renderer)
	}
	canvas := mr.Begin(Size{W: 100, H: 100})
	// FrameView-style widgets take the compositor path...
	if _, ok := canvas.(RetainedCompositorCanvas); !ok {
		t.Fatalf("Metal canvas must offer RetainedCompositorCanvas")
	}
	w.checkWebGPUSwitch()
	if _, ok := w.renderer.(*MetalRenderer); !ok {
		t.Fatalf("no WebGPU request yet, but the window left Metal")
	}
	// ...a GPU widget asks for WebGPU.
	requestWebGPU(t, canvas)
	mr.End()
	w.checkWebGPUSwitch()
	if !isWebGPU(w.renderer) || !w.wantWebGPU {
		t.Fatalf("after a WebGPU request: renderer = %T wantWebGPU=%v", w.renderer, w.wantWebGPU)
	}
}
