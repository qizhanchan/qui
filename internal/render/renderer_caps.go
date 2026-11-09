package render

import (
	"image"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Optional renderer capabilities.
//
// Window talks to its renderer through Renderer (Begin/End) plus these
// narrow interfaces, never through a concrete type. That keeps window.go
// compiling in builds where GLRenderer is not a GL renderer at all (no cgo,
// GOOS=js — see renderer_nogl.go), and lets CPURenderer take part in the
// per-frame bookkeeping it does understand (sizes, damage) while skipping
// the GL hooks it cannot honor.

// FrameInfoRenderer receives the per-frame facts Window knows before Begin.
type FrameInfoRenderer interface {
	SetLogicalSize(s gfx.Size)
	SetDamage(rect image.Rectangle)
}

// TransparentRenderer keeps the framebuffer's alpha (overlay panels).
type TransparentRenderer interface {
	SetTransparent(on bool)
}

// glHookRenderer runs whole-framebuffer raw-GL hooks around the 2D layer.
type GLHookRenderer interface {
	SetPreRender(fn func(GLState))
	SetPostRender(fn func(GLState))
}

// gpuRasterRenderer can switch to the GPU raster backend.
type GPURasterRenderer interface {
	SetGPURaster(on bool)
}

// RetainedGLRenderer replays retained GL draws (gl_retained.go).
type RetainedGLRenderer interface {
	ForgetRetainedGLDraw(key any)
	RedrawRetained() bool
}

// gpuContextRenderer is implemented by renderers that cannot produce a
// frame without a current GPU context (GLRenderer in GL builds).
type GPUContextRenderer interface {
	RequiresGPUContext() bool
}

// FrameDamageSource reports which part of a CPUFrameSource's image the
// last frame changed, so a surface can composite only that.
type FrameDamageSource interface {
	FrameDamage() image.Rectangle
}

// MetalLayerRenderer draws into a CAMetalLayer the window hands it before
// each frame (SetMetalLayer, nil to detach). SetMetalLayer reports whether
// the renderer can use the layer; false means no Metal device, and the
// window falls back to another renderer.
type MetalLayerRenderer interface {
	SetMetalLayer(layer unsafe.Pointer) bool
}

// SelfPresentingRenderer presents its own frames (a Metal renderer
// presents the drawable it drew), so Window must not present again.
type SelfPresentingRenderer interface {
	PresentsItself() bool
}

// GLFallbackRenderer reports that something drawn into it asked for raw
// GL (GPUCanvas / RetainedGLCanvas), which it cannot honor; the window
// replaces it with a GLRenderer.
type GLFallbackRenderer interface {
	WantsGL() bool
}

// PixelScroller moves pixels of the frame it keeps between frames, so
// content that only moved need not be rasterized again
// (Window.ScrollPixels).
type PixelScroller interface {
	// CanScrollPixels reports whether the renderer currently keeps its
	// frame in memory (false under a GPU raster backend, or before the
	// first frame).
	CanScrollPixels() bool
	// ScrollPixels moves the pixels inside rect (physical) by (dx, dy);
	// pixels moved out of rect are dropped, the area they leave keeps its
	// old content (the caller repaints it). False when it could not.
	ScrollPixels(rect image.Rectangle, dx, dy int) bool
}

// DamageRectsRenderer takes the frame's painted rectangles one by one
// (physical pixels), in addition to their bounds (SetDamage), so it can
// upload just those. Window calls it between Begin and End.
type DamageRectsRenderer interface {
	SetDamageRects(rects []image.Rectangle)
}
