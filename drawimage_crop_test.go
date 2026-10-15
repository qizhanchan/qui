package qui

import (
	"image"
	"image/color"
	"testing"
)

// A picture drawn partly off the canvas is cropped at its own scale, not
// squeezed into the visible part (a zoomed / scrolled image viewer).
func TestDrawImageOffCanvasCrops(t *testing.T) {
	// 4×1 source: red, green, blue, white columns.
	src := image.NewRGBA(image.Rect(0, 0, 4, 1))
	cols := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}, {255, 255, 255, 255}}
	for x, c := range cols {
		src.SetRGBA(x, 0, c)
	}
	target := image.NewRGBA(image.Rect(0, 0, 20, 10))
	cv := NewImageCanvas(target)
	// Each source column becomes 10px wide; only the first two fit.
	cv.DrawImage(src, Rect{X: 0, Y: 0, W: 40, H: 10})
	at := func(x int) color.RGBA { return target.RGBAAt(x, 5) }
	if c := at(2); c.R < 200 || c.G > 60 {
		t.Errorf("x=2: %v, want red (column 0)", c)
	}
	if c := at(15); c.G < 200 || c.R > 60 || c.B > 60 {
		t.Errorf("x=15: %v, want green (column 1) — the picture was squeezed", c)
	}

	// Scrolled left by 15px: column 1 starts at -5, column 2 at 5.
	target2 := image.NewRGBA(image.Rect(0, 0, 20, 10))
	NewImageCanvas(target2).DrawImage(src, Rect{X: -15, Y: 0, W: 40, H: 10})
	if c := target2.RGBAAt(10, 5); c.B < 200 || c.R > 60 || c.G > 60 {
		t.Errorf("scrolled x=10: %v, want blue (column 2)", c)
	}
}
