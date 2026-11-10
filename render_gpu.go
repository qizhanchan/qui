package qui

import "github.com/qizhanchan/qui/internal/render"

// WebGPU — the GPU API of qui (docs/rfc/0003-webgpu.zh.md). The device,
// textures, pipelines and passes themselves are in package
// github.com/qizhanchan/qui/gpu.

type (
	// WebGPURenderer rasterizes on the CPU and presents through WebGPU
	// (Dawn: Metal, Vulkan, D3D12). It is what AutoRenderer resolves to
	// wherever Dawn loads and the surface offers a GPU target.
	WebGPURenderer = render.WebGPURenderer

	// GPUCanvas is implemented by canvases of GPU-presenting renderers.
	// Widgets with GPU content (3D, custom shaders) type-assert the canvas
	// passed to Draw and queue draws that run while the frame is built:
	//
	//	if g, ok := canvas.(qui.GPUCanvas); ok {
	//	    g.QueueRetainedGPUDraw(v, physBox, v.drawGPU)
	//	}
	//
	// Canvases without GPU presentation (the CPU path: no Dawn, a lost
	// device, x11 / web / headless) do not implement it, and the widget
	// should draw a CPU placeholder.
	GPUCanvas = render.GPUCanvas

	// GPUFrame is the frame being built, handed to GPU draws and hooks:
	// the device, the frame's command encoder, the target format and size,
	// plus DrawTexture / RenderPass / Compositor.
	GPUFrame = render.GPUFrame

	// TextureOpts tunes GPUFrame.DrawTexture.
	TextureOpts = render.TextureOpts
)

// NewWebGPURenderer creates a WebGPU-presenting renderer. Apps normally
// leave the choice to the window (NewAutoRenderer) or set
// QUI_RENDERER=webgpu.
func NewWebGPURenderer() *WebGPURenderer { return render.NewWebGPURenderer() }

// WebGPUAvailable reports whether WebGPU can run in this process: the
// Dawn library loads and a GPU device can be made (done on first call).
func WebGPUAvailable() bool { return render.WebGPUAvailable() }
