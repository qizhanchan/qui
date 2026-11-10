package render

import (
	"image"
	"unsafe"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Optional renderer capabilities.
//
// Window talks to its renderer through Renderer (Begin/End) plus these
// narrow interfaces, never through a concrete type, so the window code is
// the same whichever renderers a build contains (CPU everywhere; WebGPU
// with cgo — see docs/rfc/0003-webgpu.zh.md).

// FrameInfoRenderer receives the per-frame facts Window knows before Begin.
type FrameInfoRenderer interface {
	SetLogicalSize(s gfx.Size)
	SetDamage(rect image.Rectangle)
}

// TransparentRenderer keeps the framebuffer's alpha (overlay panels).
type TransparentRenderer interface {
	SetTransparent(on bool)
}

// FrameDamageSource reports which part of a CPUFrameSource's image the
// last frame changed, so a surface can composite only that.
type FrameDamageSource interface {
	FrameDamage() image.Rectangle
}

// MetalLayerRenderer draws into a CAMetalLayer the window hands it before
// each frame (SetMetalLayer, nil to detach). SetMetalLayer reports whether
// the renderer can use the layer; false means no usable GPU device, and
// the window falls back to another renderer.
type MetalLayerRenderer interface {
	SetMetalLayer(layer unsafe.Pointer) bool
}

// NativeWindowRenderer presents into a native window the window hands it
// (glfw: HWND / X11 window / NSView's layer) — see SurfaceTarget.
type NativeWindowRenderer interface {
	SetSurfaceTarget(t SurfaceTarget) bool
}

// SurfaceTarget names what a GPU surface is created on. Exactly one of
// MetalLayer, HWND or XWindow is set.
type SurfaceTarget struct {
	MetalLayer unsafe.Pointer // CAMetalLayer*
	HInstance  unsafe.Pointer // Windows
	HWND       unsafe.Pointer
	XDisplay   unsafe.Pointer // Display*
	XWindow    uint64
}

// IsZero reports an empty target.
func (t SurfaceTarget) IsZero() bool {
	return t.MetalLayer == nil && t.HWND == nil && t.XWindow == 0
}

// SelfPresentingRenderer presents its own frames (the WebGPU renderer
// presents the drawable it drew), so Window must not present
// again.
type SelfPresentingRenderer interface {
	PresentsItself() bool
}

// PixelScroller moves pixels of the frame it keeps between frames, so
// content that only moved need not be rasterized again
// (Window.ScrollPixels).
type PixelScroller interface {
	// CanScrollPixels reports whether the renderer currently keeps its
	// frame in memory (false before the first frame).
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

// AutoRenderer asks the window for the best presentation it has: the
// window replaces it with a WebGPURenderer where the surface offers a GPU
// target and Dawn loads, or keeps using it as the CPURenderer it embeds. It is what NewGLRenderer used to request.
type AutoRenderer struct {
	CPURenderer
}

// NewAutoRenderer returns a renderer request the window resolves.
func NewAutoRenderer() *AutoRenderer { return &AutoRenderer{} }
