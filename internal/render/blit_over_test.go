package render

import (
	"bytes"
	"image"
	stddraw "image/draw"
	"math/rand"
	"testing"
)

func randomPremul(rng *rand.Rand, r image.Rectangle) *image.RGBA {
	img := image.NewRGBA(r)
	for i := 0; i < len(img.Pix); i += 4 {
		var a uint8
		switch rng.Intn(4) {
		case 0:
			a = 0
		case 1, 2:
			a = 0xff
		default:
			a = uint8(rng.Intn(256))
		}
		img.Pix[i+3] = a
		for c := 0; c < 3; c++ {
			img.Pix[i+c] = uint8(rng.Intn(int(a) + 1))
		}
	}
	return img
}

func TestDrawOverRGBAMatchesStdlib(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 50; n++ {
		dst := randomPremul(rng, image.Rect(0, 0, 37, 23))
		src := randomPremul(rng, image.Rect(3, 5, 40, 30))
		r := image.Rect(rng.Intn(10)-3, rng.Intn(10)-3, 20+rng.Intn(25), 10+rng.Intn(20))
		sp := image.Pt(3+rng.Intn(8), 5+rng.Intn(8))
		want := image.NewRGBA(dst.Rect)
		copy(want.Pix, dst.Pix)
		stddraw.Draw(want, r, src, sp, stddraw.Over)
		drawOverRGBA(dst, r, src, sp)
		if !bytes.Equal(dst.Pix, want.Pix) {
			t.Fatalf("case %d: r=%v sp=%v differs from draw.Draw", n, r, sp)
		}
	}
}

func BenchmarkCompositeOpaque(b *testing.B) {
	dst := image.NewRGBA(image.Rect(0, 0, 2560, 740))
	src := image.NewRGBA(dst.Rect)
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 0xff
	}
	b.Run("stdlib", func(b *testing.B) {
		for range b.N {
			stddraw.Draw(dst, dst.Rect, src, image.Point{}, stddraw.Over)
		}
	})
	b.Run("runs", func(b *testing.B) {
		for range b.N {
			drawOverRGBA(dst, dst.Rect, src, image.Point{})
		}
	})
}
