package render

import (
	"image"
	"image/color"

	"github.com/qizhanchan/qui/internal/gfx"
)

// NewImageCanvas wraps an *image.RGBA into a Canvas suitable for headless
// rendering — same rasterizer as the production CPU path, but with no
// Window / GLFW / OpenGL dependency. Use this in tests and golden-image
// snapshot harnesses. The returned canvas comes pre-initialized with a
// state stack whose bottom frame's clip equals the image's full bounds,
// so test code can immediately FillRect / DrawText / Save / ClipRect
// without manual setup.
func NewImageCanvas(img *image.RGBA) gfx.Canvas {
	b := img.Bounds()
	backend := newCPUBackend(img)
	return imageCanvas{
		CanvasState: gfx.NewCanvasState(gfx.Rect{
			X: float32(b.Min.X), Y: float32(b.Min.Y),
			W: float32(b.Dx()), H: float32(b.Dy()),
		}),
		Backend: backend,
		Layers:  newFrontendLayerTracker(),
	}
}

// NewImageCanvasForTest is the unexported sibling so internal tests can
// build a state-initialized ImageCanvas without going through the
// Canvas interface boxing.
func NewImageCanvasForTest(img *image.RGBA) imageCanvas {
	b := img.Bounds()
	backend := newCPUBackend(img)
	return imageCanvas{
		CanvasState: gfx.NewCanvasState(gfx.Rect{
			X: float32(b.Min.X), Y: float32(b.Min.Y),
			W: float32(b.Dx()), H: float32(b.Dy()),
		}),
		Backend: backend,
		Layers:  newFrontendLayerTracker(),
	}
}

// ColorToRGBA converts a qui.Color into the standard color.RGBA used by
// Go's image package — handy when callers need to fill an arbitrary
// *image.RGBA without going through the Canvas surface (e.g. to paint
// a background before handing the canvas to a widget for snapshotting).
func ColorToRGBA(c gfx.Color) color.RGBA { return toRGBA(c) }
