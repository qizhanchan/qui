//go:build darwin && cgo

package qui

import (
	"image"

	"github.com/qizhanchan/qui/internal/render"
)

// RenderWithWebGPUForTest lays root out in a test window of size (physical
// = logical pixels) and renders frames frames of it with a WebGPURenderer
// on an offscreen layer, returning the last presented picture — 2D layer
// and GPU content together. Errors when WebGPU is unavailable (callers
// skip). For tests of GPU widgets.
func RenderWithWebGPUForTest(root Widget, size Size, frames int) (*image.RGBA, error) {
	r, release, err := render.NewOffscreenWebGPURenderer(int(size.W), int(size.H))
	if err != nil {
		return nil, err
	}
	defer release()
	w := NewTestWindow(size)
	w.initUIThreadOwnership()
	w.renderer = r
	w.SetRoot(root)
	if frames < 1 {
		frames = 1
	}
	for i := 0; i < frames; i++ {
		w.layoutPass()
		r.SetLogicalSize(size)
		c := r.Begin(size)
		w.paintClip(c, Rect{W: size.W, H: size.H})
		r.End()
	}
	return r.PresentedFrame(), nil
}
