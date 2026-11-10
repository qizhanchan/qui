//go:build !cgo || js

package scene3d

import "github.com/qizhanchan/qui"

// Without WebGPU (no cgo, wasm) a Viewport draws a placeholder.

// Drawable is the WebGPU escape hatch of a Viewport; unused here.
type Drawable interface{}

type meshGPU struct{}

func (g *meshGPU) release() {}

type viewportGPU struct{}

func (v *Viewport) Draw(canvas qui.Canvas) {
	canvas.FillRect(v.Bounds(), qui.Color{R: 0.10, G: 0.10, B: 0.12, A: 1})
}

func (v *Viewport) Destroy() {}
