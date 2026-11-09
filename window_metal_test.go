package qui

import (
	"testing"
	"unsafe"

	"github.com/qizhanchan/qui/internal/render"
)

// fakeMetalSurface is a surface with a Metal path that never hands out a
// layer (there is no compositor in a test), recording ExitMetal.
type fakeMetalSurface struct {
	*fakeSurface
	exits int
}

func (s *fakeMetalSurface) MetalLayer() unsafe.Pointer { return nil }
func (s *fakeMetalSurface) ExitMetal()                 { s.exits++ }

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
	f.capsVal.GPUContext = true // cocoa windows have a GL context to fall back to
	mf := &metalFakeWindow{fakePlatformWindow: f, ms: &fakeMetalSurface{fakeSurface: &fakeSurface{owner: f}}}
	w := &Window{plat: mf, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	w.initUIThreadOwnership()
	return w, mf
}

func TestPreferredRendererPicksMetal(t *testing.T) {
	w, _ := newMetalFakeWindow(t)
	t.Setenv("QUI_RENDERER", "")
	if _, ok := w.preferredRenderer(NewGLRenderer()).(*MetalRenderer); !ok {
		t.Fatalf("GLRenderer on a Metal surface should become a MetalRenderer")
	}
	// An explicit non-GL renderer is the app's choice.
	cpu := NewCPURenderer()
	if got := w.preferredRenderer(cpu); got != Renderer(cpu) {
		t.Fatalf("CPURenderer replaced by %T", got)
	}
	t.Setenv("QUI_RENDERER", "gl")
	if _, ok := w.preferredRenderer(NewGLRenderer()).(*GLRenderer); !ok {
		t.Fatalf("QUI_RENDERER=gl must keep GL")
	}
	t.Setenv("QUI_RENDERER", "cpu")
	if _, ok := w.preferredRenderer(NewGLRenderer()).(*CPURenderer); !ok {
		t.Fatalf("QUI_RENDERER=cpu must give a CPURenderer")
	}
	t.Setenv("QUI_RENDERER", "")
	w.forceGL = true
	if _, ok := w.preferredRenderer(NewGLRenderer()).(*GLRenderer); !ok {
		t.Fatalf("a window that fell back to GL must stay GL")
	}
}

func TestPreferredRendererWithoutMetalSurface(t *testing.T) {
	if !render.MetalRendererAvailable {
		t.Skip("no Metal renderer in this build")
	}
	f := newFakePlatformWindow()
	w := &Window{plat: f, platformBacked: true, renderer: NoopRenderer{}, lastSize: Size{W: 800, H: 600}}
	if _, ok := w.preferredRenderer(NewGLRenderer()).(*GLRenderer); !ok {
		t.Fatalf("a surface without Metal keeps GL")
	}
}

func TestBindMetalFallsBackWithoutLayer(t *testing.T) {
	w, mf := newMetalFakeWindow(t)
	t.Setenv("QUI_RENDERER", "")
	w.SetRenderer(NewGLRenderer())
	if _, ok := w.renderer.(*MetalRenderer); !ok {
		t.Fatalf("renderer = %T, want MetalRenderer", w.renderer)
	}
	w.bindMetal()
	if _, ok := w.renderer.(*GLRenderer); !ok || !w.forceGL {
		t.Fatalf("no layer: renderer = %T forceGL=%v, want GL", w.renderer, w.forceGL)
	}
	if mf.ms.exits != 1 {
		t.Fatalf("ExitMetal calls = %d, want 1", mf.ms.exits)
	}
}

func TestGLRequestSwitchesMetalWindowToGL(t *testing.T) {
	w, mf := newMetalFakeWindow(t)
	t.Setenv("QUI_RENDERER", "")
	w.SetRenderer(NewGLRenderer())
	mr, ok := w.renderer.(*MetalRenderer)
	if !ok {
		t.Fatalf("renderer = %T, want MetalRenderer", w.renderer)
	}
	canvas := mr.Begin(Size{W: 100, H: 100})
	// FrameView-style widgets take the Metal path...
	if _, ok := canvas.(RetainedMetalCanvas); !ok {
		t.Fatalf("Metal canvas must offer RetainedMetalCanvas")
	}
	w.checkGLFallback()
	if _, ok := w.renderer.(*MetalRenderer); !ok {
		t.Fatalf("no GL request yet, but the window left Metal")
	}
	// ...a raw-GL widget asks for GL.
	gpu, ok := canvas.(GPUCanvas)
	if !ok {
		t.Fatalf("Metal canvas must answer GPUCanvas to detect GL widgets")
	}
	gpu.QueueGLDraw(func(GLState) {})
	mr.End()
	w.checkGLFallback()
	if _, ok := w.renderer.(*GLRenderer); !ok || !w.forceGL {
		t.Fatalf("after a GL request: renderer = %T forceGL=%v, want GL", w.renderer, w.forceGL)
	}
	if mf.ms.exits != 1 {
		t.Fatalf("ExitMetal calls = %d, want 1", mf.ms.exits)
	}
}
