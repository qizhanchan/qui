package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// A large JPEG-like picture drawn rotated at a quarter of its size goes
// through one cached RGBA copy near that scale, reused by later draws, and
// lands where the original would.
func TestScaledForDrawCachesNearDrawingScale(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 800, 600), image.YCbCrSubsampleRatio420)
	for i := range src.Y {
		src.Y[i] = 200
	}
	for i := range src.Cb {
		src.Cb[i], src.Cr[i] = 128, 128
	}
	m := gfx.RotateMatrix(0.03).Concat(gfx.ScaleMatrix(0.25, 0.25))
	a, ma := scaledForDraw(src, m)
	b, _ := scaledForDraw(src, m)
	if a == image.Image(src) {
		t.Fatal("a minified YCbCr picture was not resampled")
	}
	if a != b {
		t.Fatal("the second draw did not reuse the cached copy")
	}
	if w := a.Bounds().Dx(); w < 200 || w > 240 {
		t.Fatalf("cached width %d, want just above the drawn 200", w)
	}
	// The copy covers the same device area as the original.
	full := m.TransformRect(gfx.Rect{W: 800, H: 600})
	got := ma.TransformRect(gfx.Rect{W: float32(a.Bounds().Dx()), H: float32(a.Bounds().Dy())})
	if d := full.W - got.W; d > 0.5 || d < -0.5 {
		t.Fatalf("device extent %v, want %v", got, full)
	}
	r, g, bb, _ := a.At(100, 100).RGBA()
	want := color.YCbCr{Y: 200, Cb: 128, Cr: 128}
	wr, wg, wb, _ := want.RGBA()
	if r>>8 != wr>>8 || g>>8 != wg>>8 || bb>>8 != wb>>8 {
		t.Fatalf("resampled color %v,%v,%v want %v,%v,%v", r>>8, g>>8, bb>>8, wr>>8, wg>>8, wb>>8)
	}
	// An RGBA picture drawn near 1× is used as is.
	rgba := image.NewRGBA(image.Rect(0, 0, 10, 10))
	if got, _ := scaledForDraw(rgba, gfx.RotateMatrix(0.1)); got != image.Image(rgba) {
		t.Fatal("an unscaled RGBA picture was copied")
	}
}
