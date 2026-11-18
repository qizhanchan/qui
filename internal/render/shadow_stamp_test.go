package render

import (
	"image"
	"math"
	"math/rand"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// The analytic rounded-rect stamp used for shadows must agree with the
// path rasterizer: along straight edges within the rasterizer's own row
// quantization (4 sample rows per pixel, so up to 1/8 coverage), closely
// in the corners, and with the same total coverage.
func TestStampShadowAlphaMatchesRasterizer(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 200; n++ {
		rect := gfx.Rect{
			X: 3 + rng.Float32()*5, Y: 3 + rng.Float32()*5,
			W: 0.3 + rng.Float32()*40, H: 0.3 + rng.Float32()*40,
		}
		radius := rng.Float32() * 15
		if n%4 == 0 {
			radius = 0
		}
		const w, h = 60, 60
		got := make([]uint8, w*h)
		stampShadowAlpha(got, w, h, 0, 0, rect, radius)
		rr := max(0, min(radius, min(rect.W, rect.H)/2))
		want := rasterizePathToAlpha(gfx.NewPath().AddRRect(rect, rr).Flatten(func(p gfx.Point) gfx.Point { return p }), image.Rect(0, 0, w, h)).Pix
		var sumGot, sumWant int
		for i := range got {
			sumGot += int(got[i])
			sumWant += int(want[i])
			d := int(got[i]) - int(want[i])
			if d < 0 {
				d = -d
			}
			px, py := float32(i%w)+0.5, float32(i/w)+0.5
			corner := rr > 0 && (px < rect.X+rr || px > rect.X+rect.W-rr) && (py < rect.Y+rr || py > rect.Y+rect.H-rr)
			limit := 34
			if corner {
				limit = 48
			}
			if d > limit {
				t.Fatalf("case %d rect %+v r %v: pixel (%d,%d) got %d want %d", n, rect, radius, i%w, i/w, got[i], want[i])
			}
		}
		// Total coverage (what the blur spreads) matches the exact area:
		// within 2% or 0.4 px² (tiny pills), whichever is larger.
		exact := float64(rect.W*rect.H-(4-math.Pi)*rr*rr) * 255
		if diff := math.Abs(float64(sumGot) - exact); diff > max(0.02*exact, 102) {
			t.Fatalf("case %d rect %+v r %v: total coverage %d, exact %.0f (rasterizer %d)", n, rect, radius, sumGot, exact, sumWant)
		}
	}
}

// Windowed stamps (x, y offsets) see the same pixels as a full stamp.
func TestStampShadowAlphaWindow(t *testing.T) {
	rect := gfx.Rect{X: 10.3, Y: 7.6, W: 80.2, H: 50.5}
	full := make([]uint8, 120*80)
	stampShadowAlpha(full, 120, 80, 0, 0, rect, 9)
	win := make([]uint8, 30*20)
	stampShadowAlpha(win, 30, 20, 70, 48, rect, 9)
	for j := 0; j < 20; j++ {
		for i := 0; i < 30; i++ {
			if win[j*30+i] != full[(48+j)*120+70+i] {
				t.Fatalf("window pixel (%d,%d) differs", i, j)
			}
		}
	}
}
