package render

import (
	"image"

	"github.com/qizhanchan/qui/internal/gfx"
)

// CPURenderer is the renderer for surfaces without a GPU context: the
// browser canvas, X11 without GLX, the headless backend. It runs the same
// ImageCanvas frontend and cpuBackend rasterizer as GLRenderer's default
// path, but stops at the finished *image.RGBA and lets the platform
// surface composite it (platformSurface.presentCPU) instead of uploading
// it to a GL texture.
//
// It is pure Go and compiles everywhere, which is what makes it the
// renderer for builds without cgo (GOOS=js, CGO_ENABLED=0) — there,
// NewGLRenderer returns one too (renderer_nogl.go).
//
// Raw-GL affordances (GPUCanvas, RetainedGLCanvas, SetPreRender) are
// absent: its canvas does not implement them, so widgets that type-assert
// for them take their CPU fallback, exactly as under NoopRenderer.
type CPURenderer struct {
	img         *image.RGBA
	backend     *CPUBackend
	logicalSize gfx.Size
	// damage is the physical-pixel area the frame being rendered changed
	// (SetDamage); empty means unknown, i.e. the whole frame.
	damage      image.Rectangle
	transparent bool
}

// NewCPURenderer creates a renderer that rasterizes on the CPU and hands
// finished frames to the platform compositor.
func NewCPURenderer() *CPURenderer { return &CPURenderer{} }

// Begin starts a frame of the given physical size.
func (r *CPURenderer) Begin(windowSize gfx.Size) gfx.Canvas {
	w := int(maxFloat(windowSize.W, 1))
	h := int(maxFloat(windowSize.H, 1))
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
		// A reallocated buffer holds nothing from the previous frame, so
		// whatever damage the window computed, all of it must be shown.
		r.damage = image.Rectangle{}
	}
	if r.logicalSize.W == 0 || r.logicalSize.H == 0 {
		r.logicalSize = windowSize
	}
	if r.backend == nil {
		r.backend = newCPUBackend(r.img)
	} else {
		r.backend.SetBase(r.img)
	}
	r.backend.Begin(windowSize)
	state := gfx.NewCanvasState(gfx.Rect{W: float32(w), H: float32(h)})
	return imageCanvas{CanvasState: state, Backend: r.backend, Layers: newFrontendLayerTracker()}
}

// End finishes the frame. The pixels stay in the renderer's buffer for
// FrameImage / Snapshot.
func (r *CPURenderer) End() {
	if r.backend != nil {
		r.backend.End()
	}
}

// FrameImage implements CPUFrameSource.
func (r *CPURenderer) FrameImage() *image.RGBA {
	if r == nil {
		return nil
	}
	return r.img
}

// FrameDamage reports the area of FrameImage the last frame changed, or
// an empty rectangle when the whole frame must be treated as new.
func (r *CPURenderer) FrameDamage() image.Rectangle {
	if r == nil || r.img == nil {
		return image.Rectangle{}
	}
	return r.damage.Intersect(r.img.Rect)
}

// SetLogicalSize records the window's logical (point) size for the next
// frame. Window.Step calls it before Begin.
func (r *CPURenderer) SetLogicalSize(s gfx.Size) {
	if r != nil {
		r.logicalSize = s
	}
}

// SetDamage records the physical-pixel area the coming frame repaints.
func (r *CPURenderer) SetDamage(rect image.Rectangle) {
	if r != nil {
		r.damage = rect
	}
}

// SetTransparent records whether untouched pixels should stay transparent
// (overlay panels). The CPU rasterizer already preserves alpha; the flag
// is kept for the presenting surface.
func (r *CPURenderer) SetTransparent(on bool) {
	if r != nil {
		r.transparent = on
	}
}

// IsTransparent reports whether SetTransparent turned transparency on.
func (r *CPURenderer) IsTransparent() bool { return r != nil && r.transparent }

// Snapshot returns a copy of the last rendered frame, or nil before the
// first frame.
func (r *CPURenderer) Snapshot() *image.RGBA {
	if r == nil || r.img == nil {
		return nil
	}
	dst := image.NewRGBA(r.img.Rect)
	copy(dst.Pix, r.img.Pix)
	return dst
}
