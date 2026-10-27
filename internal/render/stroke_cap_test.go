package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// A round cap must paint past the endpoint; a butt cap must not.
func TestDrawLineIntoCaps(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	for _, tc := range []struct {
		cap     gfx.StrokeCap
		painted bool
	}{{gfx.CapRound, true}, {gfx.CapSquare, true}, {gfx.CapButt, false}} {
		img := image.NewRGBA(image.Rect(0, 0, 40, 20))
		drawLineInto(img, img.Bounds(), gfx.Point{X: 10, Y: 10}, gfx.Point{X: 20, Y: 10}, 6, red, tc.cap)
		// (8,9) is 1.5px past p1 along the axis — inside a 3px-radius cap.
		if got := img.RGBAAt(8, 9).A > 0; got != tc.painted {
			t.Errorf("cap %v: pixel past endpoint painted=%v, want %v", tc.cap, got, tc.painted)
		}
	}
}

// Interior polyline ends are butt, so a bevel join stays beveled
// instead of being rounded over by the segments' own caps.
func TestStrokePolylineInteriorEndsAreButt(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	pts := []gfx.Point{{X: 5, Y: 20}, {X: 20, Y: 20}, {X: 20, Y: 35}}
	strokePolyline(img, img.Bounds(), pts, gfx.IdentityMatrix(), 10, red, gfx.CapRound, gfx.JoinBevel, 10, false)
	// Outer corner of the turn at (20,20) is toward (+x,-y). The bevel
	// cuts it off; a round cap would have filled (23,16).
	if a := img.RGBAAt(23, 16).A; a != 0 {
		t.Errorf("outer corner pixel painted (alpha %d); interior end leaked a cap past the bevel", a)
	}
	// The real start cap is still round.
	if img.RGBAAt(2, 19).A == 0 {
		t.Error("start cap missing")
	}
}
