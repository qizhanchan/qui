package qui

import "image"

// NewOffscreenCanvas allocates a wpx×hpx premultiplied-RGBA surface and
// returns a Canvas that renders into it plus the backing image. It is the
// production entry point for headless rendering (print preview thumbnails,
// image export) — the same CPU rasterizer as the on-screen path, with no
// Window / GLFW / OpenGL dependency.
//
// The canvas comes pre-initialized with a state stack whose bottom frame's
// clip equals the image's full bounds, so callers can immediately
// FillRect / DrawText / Save / ClipRect. To render at a target DPI, Scale
// by dpi/96 before drawing in logical (96 px/inch) units.
//
// Returns (nil, nil) if wpx <= 0 or hpx <= 0.
func NewOffscreenCanvas(wpx, hpx int) (Canvas, *image.RGBA) {
	if wpx <= 0 || hpx <= 0 {
		return nil, nil
	}
	img := image.NewRGBA(image.Rect(0, 0, wpx, hpx))
	return NewImageCanvas(img), img
}
