package render

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// The elided (nine-patch) cached mask must draw exactly what the
// uncached, whole-buffer path draws.
func TestShadowMaskElisionExact(t *testing.T) {
	cases := []struct {
		rect   gfx.Rect
		radius float32
		spec   gfx.ElevationSpec
	}{
		{gfx.Rect{X: 20.25, Y: 30.5, W: 300, H: 180}, 12, gfx.ElevationSpec{Blur: 16, Y: 4, Opacity: 1}},
		{gfx.Rect{X: 10, Y: 10, W: 420, H: 90}, 24, gfx.ElevationSpec{Blur: 30, Spread: 3, X: -2, Y: 8, Opacity: 0.6, Knockout: true}},
		{gfx.Rect{X: 7.75, Y: 12.125, W: 200, H: 260}, 0, gfx.ElevationSpec{Blur: 0, Spread: 2, Opacity: 1, Knockout: true}},
		{gfx.Rect{X: 40, Y: 40, W: 120, H: 400}, 60, gfx.ElevationSpec{Blur: 8, Opacity: 1}}, // pill across x
		{gfx.Rect{X: 15.5, Y: 22, W: 500, H: 300}, 8, gfx.ElevationSpec{Blur: 50, Y: 25, Spread: -6, Opacity: 0.3, Knockout: true}},
	}
	for i, c := range cases {
		col := gfx.Color{R: 0.1, G: 0.2, B: 0.3, A: 0.8}
		bounds := image.Rect(0, 0, 640, 560)
		draw := func(off bool) *image.RGBA {
			shadowMaskCacheOff = off
			defer func() { shadowMaskCacheOff = false }()
			img := image.NewRGBA(bounds)
			for j := range img.Pix {
				img.Pix[j] = uint8(j * 7)
			}
			// A clip that cuts the shadow, as a tile would.
			drawShadowImplRaw(img, c.rect, c.radius, c.spec, col, image.Rect(5, 50, 600, 520))
			return img
		}
		want, got := draw(true), draw(false)
		for j := range want.Pix {
			if want.Pix[j] != got.Pix[j] {
				p := j / 4
				t.Fatalf("case %d: pixel (%d,%d) channel %d: elided %d, whole %d", i, p%bounds.Dx(), p/bounds.Dx(), j%4, got.Pix[j], want.Pix[j])
			}
		}
	}
	// The cached masks are small: corners and one middle row and column.
	shadowMasks.Lock()
	defer shadowMasks.Unlock()
	if shadowMasks.bytes > 300<<10 {
		t.Errorf("cached masks hold %d bytes; elision should keep them small", shadowMasks.bytes)
	}
}
