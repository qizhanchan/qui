package qui

import (
	"image"
	"image/color"

	"github.com/qizhanchan/qui/internal/render"
)

// The rasterizers and renderers live in internal/render. The aliases and
// wrappers below are their public face; the WebGPU entry points (GPU
// widgets, custom shaders) are in render_gpu.go.

type (
	// CPUReadback is the optional ability to produce a CPU-side copy of the
	// finished frame when the backend's pixels do NOT live in system memory.
	CPUReadback = render.CPUReadback

	// CPURenderer is the renderer for surfaces without a GPU path: the
	// browser canvas, X11, the headless backend. It stops at the finished
	// *image.RGBA and lets the platform surface composite it
	// (platformSurface.presentCPU).
	CPURenderer = render.CPURenderer

	// AutoRenderer asks the window for its best presentation: MetalRenderer
	// on cocoa, WebGPURenderer on glfw, CPURenderer elsewhere — see
	// window_renderer.go and docs/rfc/0003-webgpu.zh.md.
	AutoRenderer = render.AutoRenderer

	// GLRenderer is the old name of AutoRenderer. There is no OpenGL in qui
	// any more.
	//
	// Deprecated: use AutoRenderer (NewAutoRenderer).
	GLRenderer = render.AutoRenderer

	// MetalRenderer rasterizes on the CPU and presents through a
	// CAMetalLayer (macOS). It is what AutoRenderer resolves to on cocoa;
	// it is CPURenderer in builds without Metal.
	MetalRenderer = render.MetalRenderer

	// RasterBackend is the abstraction below the Canvas frontend. Everything
	// that reads state (matrix, clip stack, layer bookkeeping) and dispatches
	// on Shape lives on the ImageCanvas frontend. Everything that TURNS
	// primitives INTO PIXELS lives on a RasterBackend implementation.
	RasterBackend = render.RasterBackend

	// Compositor composites prepared pixels (video frames, pushed RGBA
	// frames) into the frame being presented — see
	// RetainedCompositorCanvas.
	Compositor = render.Compositor

	// RetainedCompositorCanvas is implemented by the canvases of the GPU-
	// presenting renderers (Metal and WebGPU): retained draws that composite
	// video-like content under the 2D layer, shown through the holes
	// Canvas.ClearRect opened. Type-assert the canvas passed to Draw.
	RetainedCompositorCanvas = render.RetainedCompositorCanvas

	// YUVPlane is one 8-bit plane for Compositor.DrawYUV420.
	YUVPlane = render.YUVPlane
)

// NewAutoRenderer returns a renderer request the window resolves to its
// best presentation (see AutoRenderer).
func NewAutoRenderer() *AutoRenderer { return render.NewAutoRenderer() }

// NewGLRenderer is the old name of NewAutoRenderer.
//
// Deprecated: use NewAutoRenderer.
func NewGLRenderer() *AutoRenderer { return render.NewAutoRenderer() }

// NewCPURenderer creates a renderer that rasterizes on the CPU and hands
// finished frames to the platform compositor.
func NewCPURenderer() *CPURenderer { return render.NewCPURenderer() }

// NewMetalRenderer creates a Metal-presenting renderer (a CPURenderer in
// builds without Metal).
func NewMetalRenderer() *MetalRenderer { return render.NewMetalRenderer() }

// MetalRendererAvailable reports whether this build contains the Metal
// renderer.
const MetalRendererAvailable = render.MetalRendererAvailable

// ColorToRGBA converts a qui.Color into the standard color.RGBA used by
// Go's image package — handy when callers need to fill an arbitrary
// *image.RGBA without going through the Canvas surface (e.g. to paint
// a background before handing the canvas to a widget for snapshotting).
func ColorToRGBA(c Color) color.RGBA { return render.ColorToRGBA(c) }

// NewImageCanvas wraps an *image.RGBA into a Canvas suitable for headless
// rendering — same rasterizer as the production CPU path, but with no
// Window or GPU dependency. Use this in tests and golden-image
// snapshot harnesses. The returned canvas comes pre-initialized with a
// state stack whose bottom frame's clip equals the image's full bounds,
// so test code can immediately FillRect / DrawText / Save / ClipRect
// without manual setup.
func NewImageCanvas(img *image.RGBA) Canvas { return render.NewImageCanvas(img) }
