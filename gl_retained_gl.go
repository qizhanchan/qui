//go:build cgo && !js && !qui_nogl

package qui

import (
	"image"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// QueueRetainedGLDraw implements RetainedGLCanvas.
func (c gpuImageCanvas) QueueRetainedGLDraw(key any, physBounds Rect, fn func(GLState)) {
	if c.renderer == nil || fn == nil || key == nil {
		return
	}
	c.renderer.retained.put(key, physBounds, fn)
	c.renderer.deferredGL = append(c.renderer.deferredGL, fn)
}

// ForgetRetainedGLDraw drops a retained draw registered under key, for an
// owner that stops showing GL content without its area being repainted
// (it is otherwise dropped automatically). UI thread only.
func (r *GLRenderer) ForgetRetainedGLDraw(key any) {
	if r == nil {
		return
	}
	r.retained.forget(key)
}

// redrawRetained produces a frame that replays the retained GL draws and
// re-composites the CPU image left by the last painted frame, without
// rasterizing or uploading any CPU pixels. Returns false when there is
// nothing to redraw (no frame painted yet, no retained draws).
func (r *GLRenderer) redrawRetained() bool {
	if r == nil || !r.initialized || r.img == nil || r.backend == nil || r.retained.len() == 0 {
		return false
	}
	lastActiveGLRenderer = r
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, int32(r.img.Rect.Dx()), int32(r.img.Rect.Dy()))
	r.damage = image.Rectangle{}
	r.damageNone = true
	r.composite()
	return true
}
