package widgets

import (
	"image"
	"math"
	"testing"

	. "github.com/qizhanchan/qui"
)

// The native window paints at 2x, with its toolbar offset included. Test
// both full damage and a strip at the bottom of a fractional inline pill.
func TestFractionalInsetPillDamage(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		for _, strip := range []bool{false, true} {
			box := NewBox(nil)
			box.Layout(Rect{X: 20, Y: 20.34375, W: 166.6875, H: 47.375})
			box.Style().Radius = 9999
			box.Style().Shadow = ShadowStyle{Spread: 1, Inset: true, Color: Color{A: 1}}
			img := image.NewRGBA(image.Rect(0, 0, 400, 160))
			c := NewImageCanvas(img)
			c.Scale(scale, scale)
			damage := box.PaintBounds()
			if strip {
				damage.Y += damage.H - 2
				damage.H = 2
			}
			c.ClipRect(damage)
			box.Draw(c)
			r := box.Bounds()
			x := int((r.X + r.W/2) * scale)
			coverage := 0
			for y := int(math.Floor(float64((r.Y + r.H - 1) * scale))); y < int(math.Ceil(float64((r.Y+r.H)*scale))); y++ {
				coverage += int(img.RGBAAt(x, y).A)
			}
			if coverage < int(180*scale) {
				t.Fatalf("scale %v strip %v: bottom border coverage %d", scale, strip, coverage)
			}
		}
	}
}
