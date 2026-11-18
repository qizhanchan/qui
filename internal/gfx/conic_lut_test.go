package gfx

import (
	"math"
	"testing"
)

// The LUT path must match direct stop sampling to within one 8-bit step
// everywhere around the circle, for any start angle.
func TestConicLUTMatchesStops(t *testing.T) {
	stops := []GradientStop{
		{Offset: 0, Color: Color{R: 1, A: 1}},
		{Offset: 0.3, Color: Color{G: 1, A: 1}},
		{Offset: 0.7, Color: Color{B: 1, A: 0.5}},
		{Offset: 1, Color: Color{R: 1, A: 1}},
	}
	for _, angle := range []float32{0, 1.2, -4, 9} {
		direct := ConicGradient{Center: Point{X: 50, Y: 50}, Angle: angle, Stops: stops}
		lut := direct
		lut.LUT = NewConicLUT(stops)
		for i := 0; i < 720; i++ {
			th := float64(i) * math.Pi / 360
			x, y := float32(50+40*math.Sin(th)), float32(50-40*math.Cos(th))
			a, b := direct.ColorAt(x, y), lut.ColorAt(x, y)
			for _, d := range []float32{a.R - b.R, a.G - b.G, a.B - b.B, a.A - b.A} {
				if d > 1.0/255 || d < -1.0/255 {
					t.Fatalf("angle %v step %d: direct %+v lut %+v", angle, i, a, b)
				}
			}
		}
	}
}

func TestAtan2fMatchesMath(t *testing.T) {
	for i := -200; i <= 200; i++ {
		for j := -200; j <= 200; j += 7 {
			y, x := float32(i)*0.37, float32(j)*0.53
			got := float64(atan2f(y, x))
			want := math.Atan2(float64(y), float64(x))
			if x == 0 && y == 0 {
				continue
			}
			d := got - want
			if d > math.Pi {
				d -= 2 * math.Pi
			} else if d < -math.Pi {
				d += 2 * math.Pi
			}
			if math.Abs(d) > 2e-5 {
				t.Fatalf("atan2f(%v, %v) = %v, want %v", y, x, got, want)
			}
		}
	}
}
