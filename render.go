package qui

import (
	"image"
	"image/color"

	"github.com/qizhanchan/qui/internal/render"
)

// The rasterizers — the CPU and GPU raster backends, CPURenderer and
// GLRenderer — live in internal/render. The aliases and wrappers below are
// their public face; GL-only entry points are in render_gl.go.

type (
	// CPUReadback is the optional ability to produce a CPU-side copy of the
	// finished frame when the backend's pixels do NOT live in system memory.
	CPUReadback = render.CPUReadback

	// CPURenderer is the renderer for surfaces without a GPU context: the
	// browser canvas, X11 without GLX, the headless backend. It runs the same
	// ImageCanvas frontend and cpuBackend rasterizer as GLRenderer's default
	// path, but stops at the finished *image.RGBA and lets the platform
	// surface composite it (platformSurface.presentCPU) instead of uploading
	// it to a GL texture.
	CPURenderer = render.CPURenderer

	// GLRenderer is CPURenderer in builds without OpenGL.
	GLRenderer = render.GLRenderer

	// GLState carries framebuffer/window sizing info to raw GL callbacks.
	// It deliberately doesn't expose the *Window or *GLRenderer so
	// callbacks stay decoupled from the Window's internal state — they
	// just need physical pixel dimensions to set glViewport/glScissor.
	GLState = render.GLState

	// GPUCanvas is an optional extension to Canvas for canvases backed
	// by a GPU pipeline. Widgets that need direct OpenGL access do a
	// type assertion on the canvas passed to Draw:
	GPUCanvas = render.GPUCanvas

	// RasterBackend is the abstraction below the Canvas frontend. Everything
	// that reads state (matrix, clip stack, layer bookkeeping) and dispatches
	// on Shape lives on the ImageCanvas frontend. Everything that TURNS
	// primitives INTO PIXELS lives on a RasterBackend implementation.
	RasterBackend = render.RasterBackend

	// RetainedGLCanvas is implemented by GPU canvases that support retained GL
	// draws (the GLRenderer's canvas does). Type-assert the canvas passed to
	// Draw, like GPUCanvas.
	RetainedGLCanvas = render.RetainedGLCanvas

	// TextureOpts tunes a DrawTexture call.
	TextureOpts = render.TextureOpts
)

// ActiveGLRenderer is always nil without OpenGL.
func ActiveGLRenderer() *GLRenderer { return render.ActiveGLRenderer() }

// NewCPURenderer creates a renderer that rasterizes on the CPU and hands
// finished frames to the platform compositor.
func NewCPURenderer() *CPURenderer { return render.NewCPURenderer() }

// NewGLRenderer returns a CPURenderer in builds without OpenGL.
func NewGLRenderer() *GLRenderer { return render.NewGLRenderer() }

// PhysicalScissor converts a logical-pixel clip rectangle into the
// physical-pixel, bottom-origin GL scissor box that glScissor expects.
// Returns (x, y, w, h, true) ready for `gl.Scissor(x, y, w, h)`, or
// (0,0,0,0,false) when the clip has zero area.
func PhysicalScissor(state GLState, clip Rect) (int32, int32, int32, int32, bool) {
	return render.PhysicalScissor(state, clip)
}

// ColorToRGBA converts a qui.Color into the standard color.RGBA used by
// Go's image package — handy when callers need to fill an arbitrary
// *image.RGBA without going through the Canvas surface (e.g. to paint
// a background before handing the canvas to a widget for snapshotting).
func ColorToRGBA(c Color) color.RGBA { return render.ColorToRGBA(c) }

// NewImageCanvas wraps an *image.RGBA into a Canvas suitable for headless
// rendering — same rasterizer as the production CPU path, but with no
// Window / GLFW / OpenGL dependency. Use this in tests and golden-image
// snapshot harnesses. The returned canvas comes pre-initialized with a
// state stack whose bottom frame's clip equals the image's full bounds,
// so test code can immediately FillRect / DrawText / Save / ClipRect
// without manual setup.
func NewImageCanvas(img *image.RGBA) Canvas { return render.NewImageCanvas(img) }
