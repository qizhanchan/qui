package render

import (
	"image"
	"math"
	"testing"
)

// The YCbCr fast path (decoded JPEGs) matches the At/RGBA interface it
// replaces to within a level, without allocating per texel.
func TestPremulSamplerYCbCr(t *testing.T) {
	img := image.NewYCbCr(image.Rect(3, 5, 23, 17), image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = uint8(i * 7)
	}
	for i := range img.Cb {
		img.Cb[i], img.Cr[i] = uint8(i*13), uint8(255-i*5)
	}
	s := newPremulSampler(img)
	if s.ycc == nil {
		t.Fatal("YCbCr image not on the fast path")
	}
	for y := 0; y < 12; y++ {
		for x := 0; x < 20; x++ {
			r, g, b, a := s.texel(x, y)
			cr, cg, cb, ca := img.At(3+x, 5+y).RGBA()
			for i, p := range [][2]float32{{r, float32(cr) / 257}, {g, float32(cg) / 257}, {b, float32(cb) / 257}, {a, float32(ca) / 257}} {
				if math.Abs(float64(p[0]-p[1])) > 1.01 {
					t.Fatalf("texel (%d,%d) channel %d = %v, At gives %v", x, y, i, p[0], p[1])
				}
			}
		}
	}
	if n := testing.AllocsPerRun(10, func() { s.texel(4, 4) }); n != 0 {
		t.Fatalf("texel allocates %v times", n)
	}
}
