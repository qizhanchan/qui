//go:build cgo && !js

package qui

import "github.com/qizhanchan/qui/internal/render"

func init() {
	newWebGPURenderer = func() Renderer { return render.NewWebGPURenderer() }
	webgpuUsable = render.WebGPUAvailable
}

// SetPreRender installs a whole-window GPU hook run every frame under the
// widget tree, after the frame target is cleared (a shader background
// that shows through transparent widgets or ClearRect holes). Pass nil to
// clear. The hooks run only while the window presents with WebGPU.
func (w *Window) SetPreRender(fn func(*GPUFrame)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPreRender")
	w.gpuPre = fn
	if fn == nil {
		w.gpuPre = nil
	}
	w.applyGPUHooks()
	w.Invalidate()
}

// SetPostRender installs a whole-window GPU hook run every frame over the
// widget tree (an overlay, a post-process). Pass nil to clear.
func (w *Window) SetPostRender(fn func(*GPUFrame)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPostRender")
	w.gpuPost = fn
	if fn == nil {
		w.gpuPost = nil
	}
	w.applyGPUHooks()
	w.Invalidate()
}

// applyGPUHooks hands the window's GPU hooks to its renderer (again, after
// the renderer changed).
func (w *Window) applyGPUHooks() {
	hr, ok := w.renderer.(render.GPUHookRenderer)
	if !ok {
		return
	}
	pre, _ := w.gpuPre.(func(*GPUFrame))
	post, _ := w.gpuPost.(func(*GPUFrame))
	if pre != nil || post != nil || w.gpuHooksSet {
		hr.SetPreRender(pre)
		hr.SetPostRender(post)
		w.gpuHooksSet = pre != nil || post != nil
	}
}
