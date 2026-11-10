//go:build cgo && !js

package render

import (
	"image"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// GPU content — 3D viewports, custom shaders, anything beyond compositing
// prepared pixels — goes through WebGPU (docs/rfc/0003-webgpu.zh.md).
//
// A widget type-asserts the canvas passed to Draw:
//
//	if g, ok := canvas.(qui.GPUCanvas); ok {
//	    g.QueueRetainedGPUDraw(v, physBox, v.drawGPU)
//	}
//
// The draw runs while the renderer builds the frame, after the CPU raster
// of that frame is finished. It may record its own render passes on
// f.Encoder (into its own textures) and then composite the result with
// f.DrawTexture, or draw straight into the frame with f.RenderPass. All of
// it lands UNDER the 2D layer, which shows it through the holes
// Canvas.ClearRect opened. Hooks installed with Window.SetPostRender land
// over the 2D layer instead.
//
// Only WebGPURenderer runs these draws. A MetalRenderer canvas accepts
// them only to notice that the window needs WebGPU: it drops the draw and
// the window switches renderers at the end of the frame (the next frame
// has the content). Other canvases do not implement GPUCanvas, and the
// widget should draw a CPU placeholder.

// GPUCanvas is implemented by canvases of GPU-presenting renderers.
type GPUCanvas interface {
	gfx.Canvas
	// QueueGPUDraw runs fn while building this frame only.
	QueueGPUDraw(fn func(*GPUFrame))
	// QueueRetainedGPUDraw runs fn on this frame and on later frames by
	// the retained-draw rule (retained.go): physBounds (physical pixels,
	// top-left origin) is the area fn paints; re-queuing under key
	// replaces the previous registration.
	QueueRetainedGPUDraw(key any, physBounds gfx.Rect, fn func(*GPUFrame))
}

// GPUHookRenderer runs whole-frame GPU hooks: pre under the 2D layer (a
// shader background), post over it (an overlay, a capture).
type GPUHookRenderer interface {
	SetPreRender(fn func(*GPUFrame))
	SetPostRender(fn func(*GPUFrame))
}

// TextureOpts tunes GPUFrame.DrawTexture.
type TextureOpts struct {
	// Opacity scales the result (premultiplied). Zero means 1.
	Opacity float32
	// Clip limits drawing to this physical rectangle; empty means none.
	Clip gfx.Rect
	// Nearest samples without filtering (pixel art, 1:1 blits).
	Nearest bool
	// Straight says the texture holds straight (not premultiplied)
	// alpha; it is premultiplied while drawing.
	Straight bool
}

// GPUFrame is the frame being built, handed to GPU draws and hooks. Valid
// only during the call.
type GPUFrame struct {
	// Device is the process-wide WebGPU device.
	Device *wgpu.Device
	// Encoder records this frame's GPU work, in order: passes a draw opens
	// here run before the frame's composite pass.
	Encoder *wgpu.CommandEncoder
	// Format is the frame target's format: pipelines that draw through
	// RenderPass must use it as their color target.
	Format wgpu.TextureFormat
	// Size is the frame's physical size; LogicalSize the window's.
	Size, LogicalSize gfx.Size

	r *WebGPURenderer
}

// DrawTexture composites the src part (texels) of view at dst (physical
// pixels), premultiplied source-over, in queue order.
func (f *GPUFrame) DrawTexture(view *wgpu.TextureView, src, dst gfx.Rect, opts TextureOpts) {
	if f == nil || f.r == nil || view == nil {
		return
	}
	f.r.addTextureOp(view, src, dst, opts)
}

// RenderPass runs fn inside the frame's composite pass, in queue order,
// drawing straight into the frame target (whose content so far is kept).
// The viewport covers the whole frame; restore any scissor you set.
func (f *GPUFrame) RenderPass(fn func(*wgpu.RenderPass)) {
	if f == nil || f.r == nil || fn == nil {
		return
	}
	f.r.ops = append(f.r.ops, compositeOp{pass: fn})
}

// Capture asks for the finished frame — 2D layer and GPU content as
// presented — to be read back after this frame is submitted; fn gets it
// as RGBA on the UI thread. For debugging and tests: it waits for the GPU.
func (f *GPUFrame) Capture(fn func(*image.RGBA)) {
	if f == nil || f.r == nil || fn == nil {
		return
	}
	f.r.capture = append(f.r.capture, fn)
}

// Compositor offers the video-style compositing primitives for this frame.
func (f *GPUFrame) Compositor() Compositor {
	if f == nil || f.r == nil {
		return nil
	}
	return webgpuCompositor{r: f.r}
}
