package gfx

import (
	"image"
	"math/rand"
	"testing"
)

// A downsampled wide blur matches the full-resolution three-pass box blur
// closely: small mean error, bounded worst case, same bounds.
func TestWideBlurDownsampleMatchesFull(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	src := image.NewRGBA(image.Rect(10, 20, 10+300, 20+90))
	// Blocks of random premultiplied colour plus noise: hard edges are the
	// worst case for the reduced-resolution path.
	for y := 0; y < 90; y++ {
		for x := 0; x < 300; x++ {
			a := uint8(255)
			if (x/37+y/23)%3 == 0 {
				a = 128
			}
			c := uint8((x/37*70 + y/23*40 + rng.Intn(20)) % 256)
			if c > a {
				c = a
			}
			o := src.PixOffset(10+x, 20+y)
			src.Pix[o], src.Pix[o+1], src.Pix[o+2], src.Pix[o+3] = c, c/2, a-c/3, a
		}
	}
	for _, r := range []int{6, 16, 32} {
		got := BlurImageFilter{Radius: float32(r)}.ApplyImage(src)
		// Reference: force the full-resolution passes.
		b := src.Bounds()
		a, want := image.NewRGBA(b), image.NewRGBA(b)
		boxBlurPass(src, a, r, true)
		boxBlurPass(a, want, r, false)
		for pass := 1; pass < 3; pass++ {
			boxBlurPass(want, a, r, true)
			boxBlurPass(a, want, r, false)
		}
		if got.Bounds() != b {
			t.Fatalf("r=%d: bounds %v, want %v", r, got.Bounds(), b)
		}
		var sum, worst int
		for i := range got.Pix {
			d := int(got.Pix[i]) - int(want.Pix[i])
			if d < 0 {
				d = -d
			}
			sum += d
			worst = max(worst, d)
		}
		mean := float64(sum) / float64(len(got.Pix))
		if mean > 2 || worst > 24 {
			t.Fatalf("r=%d: mean error %.2f, worst %d", r, mean, worst)
		}
	}
}
