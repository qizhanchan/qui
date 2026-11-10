package qui

import (
	"log"
	"os"
	"strings"
	"sync/atomic"

	"github.com/qizhanchan/qui/internal/platform/contract"
	"github.com/qizhanchan/qui/internal/render"
)

// Renderer selection — see docs/rfc/0003-webgpu.zh.md.
//
// An app asks for NewAutoRenderer (or the deprecated NewGLRenderer, or sets
// no renderer at all) and the window resolves it. There are two answers:
// a WebGPURenderer (Dawn: Metal on macOS, Vulkan on Linux, D3D12 on
// Windows) on surfaces that offer a GPU target — cocoa's CAMetalLayer, a
// native window — when Dawn loads; otherwise a CPURenderer whose frames
// the platform compositor shows (CALayer contents, XPutImage, <canvas>).
// A WebGPU window whose device is lost drops to the CPU path for good.
//
// QUI_RENDERER overrides the choice: "webgpu" or "cpu" ("auto" or unset:
// the default).

// rendererPreference is QUI_RENDERER, lower-cased: "", "webgpu" or "cpu".
// Anything else is reported once and ignored.
func rendererPreference() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("QUI_RENDERER")))
	switch v {
	case "", "auto":
		return ""
	case "webgpu", "cpu":
		return v
	}
	if !badRendererLogged.Swap(true) {
		hint := ""
		if v == "metal" || v == "gl" || v == "opengl" {
			hint = " (that renderer was removed; WebGPU presents through Metal on macOS)"
		}
		log.Printf("qui: QUI_RENDERER=%q is not a renderer%s; use webgpu or cpu", v, hint)
	}
	return ""
}

var badRendererLogged atomic.Bool

// newWebGPURenderer builds a WebGPURenderer, or is nil in builds without
// WebGPU (no cgo, js). Set by window_gpu.go.
var newWebGPURenderer func() Renderer

// webgpuUsable reports whether WebGPU can run in this process (loads Dawn
// on first use).
var webgpuUsable func() bool

func webgpuOK() bool { return newWebGPURenderer != nil && webgpuUsable != nil && webgpuUsable() }

// metalSurfaceOf returns the window's CAMetalLayer presentation path, if
// its surface has one.
func metalSurfaceOf(pw platformWindow) (contract.MetalSurface, bool) {
	if pw == nil {
		return nil, false
	}
	s := pw.surface()
	if cs, ok := s.(contractSurface); ok {
		ms, ok := cs.s.(contract.MetalSurface)
		return ms, ok
	}
	ms, ok := s.(contract.MetalSurface)
	return ms, ok
}

// nativeSurfaceOf returns the window's native-window presentation path
// (glfw), if its surface has one.
func nativeSurfaceOf(pw platformWindow) (contract.NativeSurface, bool) {
	if pw == nil {
		return nil, false
	}
	s := pw.surface()
	if cs, ok := s.(contractSurface); ok {
		ns, ok := cs.s.(contract.NativeSurface)
		return ns, ok
	}
	ns, ok := s.(contract.NativeSurface)
	return ns, ok
}

// preferredRenderer resolves an AutoRenderer request into the renderer
// this window should run. Any other renderer is the app's explicit choice
// and passes through.
func (w *Window) preferredRenderer(r Renderer) Renderer {
	if _, auto := r.(*AutoRenderer); !auto {
		return r
	}
	pref := rendererPreference()
	if pref == "cpu" {
		return NewCPURenderer()
	}
	_, hasMetal := metalSurfaceOf(w.plat)
	_, hasNative := nativeSurfaceOf(w.plat)
	if (hasMetal || hasNative) && !w.noWebGPU && webgpuOK() {
		return newWebGPURenderer()
	}
	if (hasMetal || hasNative) && !w.noWebGPU {
		logWebGPUMissing()
	}
	return NewCPURenderer()
}

// bindSurface hands a GPU-presenting renderer its window's surface before
// a frame, and resolves a pending AutoRenderer. When the renderer cannot
// use the surface the window falls back to the CPU path.
func (w *Window) bindSurface() {
	if _, auto := w.renderer.(*AutoRenderer); auto {
		w.SetRenderer(w.renderer)
	}
	if mr, ok := w.renderer.(render.MetalLayerRenderer); ok {
		if ms, ok := metalSurfaceOf(w.plat); ok {
			if layer := ms.MetalLayer(); layer != nil && mr.SetMetalLayer(layer) {
				w.logRenderer()
				return
			}
			w.rendererFailed()
			return
		}
	}
	if nr, ok := w.renderer.(render.NativeWindowRenderer); ok {
		if ns, ok := nativeSurfaceOf(w.plat); ok {
			t := ns.NativeSurfaceTarget()
			st := render.SurfaceTarget{MetalLayer: t.MetalLayer, HInstance: t.HInstance, HWND: t.HWND,
				XDisplay: t.XDisplay, XWindow: t.XWindow}
			if !st.IsZero() && nr.SetSurfaceTarget(st) {
				w.logRenderer()
				return
			}
			w.rendererFailed()
		}
	}
}

func (w *Window) logRenderer() {
	if w.rendererLogged || os.Getenv("QUI_DEBUG_RENDERER") == "" {
		return
	}
	w.rendererLogged = true
	log.Printf("qui: window %q presents with %T", w.title, w.renderer)
}

// rendererFailed drops the GPU renderer that could not use the surface
// (or whose device was lost) for the CPU path and repaints everything.
func (w *Window) rendererFailed() {
	w.noWebGPU = true
	log.Printf("qui: WebGPU presentation unavailable for window %q; presenting CPU frames", w.title)
	w.closeRenderer()
	w.renderer = NoopRenderer{}
	w.SetRenderer(NewAutoRenderer())
	w.rendererLogged = false
	w.Invalidate()
	w.InvalidateLayout()
}

var webgpuMissingLogged atomic.Bool

// logWebGPUMissing says once why a window with a GPU surface presents CPU
// frames: Dawn is not installed (GPU-content widgets stay blank too).
func logWebGPUMissing() {
	if webgpuMissingLogged.Swap(true) {
		return
	}
	log.Printf("qui: Dawn (WebGPU) is not available; presenting CPU frames, GPU content will not show " +
		"(install it with `go run github.com/qizhanchan/qui/cmd/qui-dawn`)")
}

// closeRenderer releases a renderer's native resources (a GPU context)
// when the window stops using it.
func (w *Window) closeRenderer() {
	if c, ok := w.renderer.(interface{ Close() }); ok {
		c.Close()
	}
}

// rendererPresentsItself reports whether the renderer already presented
// the frame (the WebGPU renderer presents its own drawables).
func (w *Window) rendererPresentsItself() bool {
	sp, ok := w.renderer.(render.SelfPresentingRenderer)
	return ok && sp.PresentsItself()
}

// ForgetRetainedDraw drops key's retained draw (compositor or GPU) from
// this window's renderer, for an owner that stops showing GPU content
// without its area being repainted. UI thread.
func (w *Window) ForgetRetainedDraw(key any) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.ForgetRetainedDraw")
	if rr, ok := w.renderer.(render.RetainedRenderer); ok {
		rr.ForgetRetainedDraw(key)
	}
}

// ForgetRetainedGLDraw is the old name of ForgetRetainedDraw.
//
// Deprecated: use ForgetRetainedDraw.
func (w *Window) ForgetRetainedGLDraw(key any) { w.ForgetRetainedDraw(key) }

// RequestGPURedraw asks for a frame that re-runs this window's retained
// draws (RetainedCompositorCanvas, GPUCanvas) without repainting any widget
// — the cheap way to show new content in a GPU-backed widget. Safe to call
// from any goroutine; requests made before the frame runs coalesce.
func (w *Window) RequestGPURedraw() {
	if w == nil {
		return
	}
	if w.gpuRedraw.Swap(true) {
		return // already pending; the loop has been woken
	}
	w.wakeLoop()
}

// RequestGLRedraw is the old name of RequestGPURedraw.
//
// Deprecated: use RequestGPURedraw.
func (w *Window) RequestGLRedraw() { w.RequestGPURedraw() }

// gpuRedrawPending reports whether RequestGPURedraw is waiting for a frame.
func (w *Window) gpuRedrawPending() bool { return w != nil && w.gpuRedraw.Load() }

// redrawRetainedOnly runs a RequestGPURedraw frame. Step calls it when
// nothing is dirty.
func (w *Window) redrawRetainedOnly() {
	rr, ok := w.renderer.(render.RetainedRenderer)
	if !ok {
		return
	}
	if fi, ok := w.renderer.(render.FrameInfoRenderer); ok {
		fi.SetLogicalSize(w.lastSize)
	}
	w.bindSurface()
	if !rr.RedrawRetained() {
		return
	}
	w.present()
	w.framePainted = true
	w.retryDroppedFrame()
}

// retryDroppedFrame asks for a redraw-only frame when the renderer could
// not present the one just made (no surface texture this time): its
// pixels are in the renderer's frame already, so the next frame shows them
// without repainting — otherwise the screen would keep the previous frame
// until something else invalidates.
func (w *Window) retryDroppedFrame() {
	if d, ok := w.renderer.(render.DroppedFrameRenderer); ok && d.TakeDroppedFrame() {
		w.RequestGPURedraw()
	}
}

// defaultRenderer is the renderer a platform window starts with, so an app
// that never calls SetRenderer still shows its UI: resolved on the first
// frame (bindSurface).
func defaultRenderer(platformWindow) Renderer { return NewAutoRenderer() }
