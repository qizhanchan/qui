package svg_test

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
)

const filledSquareSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect x="0" y="0" width="16" height="16" fill="black"/></svg>`

func TestRasterizeProducesExactSize(t *testing.T) {
	icon, err := svg.ParseBytes([]byte(filledSquareSVG))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img := icon.Rasterize(28, 28, qui.Color{R: 1, A: 1})
	if img == nil {
		t.Fatal("nil image")
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 28, 28) {
		t.Errorf("bounds = %v, want 0,0,28,28", got)
	}
	rgba := img.(*image.RGBA)
	// Center pixel should be red & opaque (full coverage + tint).
	r, g, b, a := rgba.RGBAAt(14, 14).RGBA()
	if a == 0 {
		t.Fatalf("expected covered pixel, got transparent (%d %d %d %d)", r, g, b, a)
	}
	if r < 0xfe00 || g > 0x0200 || b > 0x0200 {
		t.Errorf("center pixel not pure red: r=%x g=%x b=%x a=%x", r, g, b, a)
	}
}

func TestRasterizeCachesPerSizeAndTint(t *testing.T) {
	icon, err := svg.ParseBytes([]byte(filledSquareSVG))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	red := qui.Color{R: 1, A: 1}
	a := icon.Rasterize(16, 16, red)
	b := icon.Rasterize(16, 16, red)
	if a != b {
		t.Error("expected cache hit for same size + tint")
	}
	c := icon.Rasterize(16, 16, qui.Color{G: 1, A: 1})
	if a == c {
		t.Error("different tint should produce a different raster")
	}
	d := icon.Rasterize(32, 32, red)
	if a == d {
		t.Error("different size should produce a different raster")
	}
}

func TestRasterizeNonPositiveSize(t *testing.T) {
	icon, err := svg.ParseBytes([]byte(filledSquareSVG))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := icon.Rasterize(0, 16, qui.Color{}); got != nil {
		t.Errorf("zero width: want nil, got %v", got)
	}
	if got := icon.Rasterize(16, -1, qui.Color{}); got != nil {
		t.Errorf("negative height: want nil, got %v", got)
	}
}
