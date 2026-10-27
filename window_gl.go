package qui

import (
	"log"
	"sync"

	"github.com/qizhanchan/qui/internal/render"
)

// ForgetRetainedGLDraw drops key's retained draw from this window's
// renderer (see GLRenderer.ForgetRetainedGLDraw). UI thread.
func (w *Window) ForgetRetainedGLDraw(key any) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ForgetRetainedGLDraw")
	if rr, ok := w.renderer.(render.RetainedGLRenderer); ok {
		rr.ForgetRetainedGLDraw(key)
	}
}

// RequestGLRedraw asks for a frame that re-runs this window's retained GL
// draws (RetainedGLCanvas) without repainting any widget — the cheap way to
// show new content in a GL-backed widget. Safe to call from any goroutine;
// requests made before the frame runs coalesce into one.
func (w *Window) RequestGLRedraw() {
	if w == nil {
		return
	}
	if w.glRedraw.Swap(true) {
		return // already pending; the loop has been woken
	}
	w.wakeLoop()
}

// glRedrawPending reports whether RequestGLRedraw is waiting for a frame.
func (w *Window) glRedrawPending() bool { return w != nil && w.glRedraw.Load() }

// redrawGLOnly runs a RequestGLRedraw frame. Step calls it when nothing
// is dirty.
func (w *Window) redrawGLOnly() {
	rr, ok := w.renderer.(render.RetainedGLRenderer)
	if !ok {
		return
	}
	if fi, ok := w.renderer.(render.FrameInfoRenderer); ok {
		fi.SetLogicalSize(w.lastSize)
	}
	if !rr.RedrawRetained() {
		return
	}
	w.present()
	w.framePainted = true
}

var cpuFallbackLogOnce sync.Once

// rendererForPlatform adapts the renderer an app asked for to what the
// window's surface can run. Apps written for the desktop call
// SetRenderer(NewGLRenderer()) unconditionally; on a surface with no GPU
// context (browser canvas, X11 without GLX, headless) a GL renderer would
// draw nothing, so it is replaced by a CPURenderer — same rasterizer, same
// pixels, presented by the compositor instead of a buffer swap. Raw-GL
// content (QueueGLDraw, scene3d) is what is lost, and widgets already
// degrade when the canvas lacks GPUCanvas.
func rendererForPlatform(pw platformWindow, r Renderer) Renderer {
	if pw == nil || pw.caps().GPUContext {
		return r
	}
	if g, ok := r.(render.GPUContextRenderer); ok && g.RequiresGPUContext() {
		cpuFallbackLogOnce.Do(func() {
			log.Printf("qui: this window has no GPU context; rendering with CPURenderer instead of GLRenderer")
		})
		return NewCPURenderer()
	}
	return r
}

// defaultRenderer is the renderer a platform window starts with, so an app
// that never calls SetRenderer still shows its UI.
func defaultRenderer(pw platformWindow) Renderer {
	if pw != nil && pw.caps().GPUContext && render.GLRendererAvailable {
		return NewGLRenderer()
	}
	return NewCPURenderer()
}
