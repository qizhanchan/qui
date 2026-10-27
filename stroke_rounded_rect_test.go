package qui

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui/internal/render"
)

// A stroke wider than the corner radius is a ring with square inner
// corners, not a solid block (3px border on a 2px radius).
func TestStrokeRoundedRectThickerThanRadiusIsRing(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	canvas := render.NewImageCanvasForTest(img)
	black := Color{A: 1}
	canvas.StrokeRoundedRect(Rect{X: 5, Y: 5, W: 30, H: 20}, 2, black, 3)

	opaque := func(x, y int) bool { return img.RGBAAt(x, y).A > 250 }
	// Ring: the 3px band along each edge is painted...
	for _, p := range [][2]int{{20, 5}, {20, 7}, {20, 22}, {20, 24}, {5, 15}, {7, 15}, {32, 15}, {34, 15}} {
		if !opaque(p[0], p[1]) {
			t.Errorf("edge band pixel %v not painted", p)
		}
	}
	// ...and the inside is untouched, right up to the square inner corners.
	for _, p := range [][2]int{{20, 15}, {8, 8}, {31, 8}, {8, 21}, {31, 21}, {9, 9}} {
		if img.RGBAAt(p[0], p[1]).A != 0 {
			t.Errorf("interior pixel %v painted (alpha %d); stroke filled the whole rect", p, img.RGBAAt(p[0], p[1]).A)
		}
	}
	// The outer corner is still rounded: the very corner pixel stays clear.
	if img.RGBAAt(5, 5).A > 128 {
		t.Errorf("outer corner pixel not rounded (alpha %d)", img.RGBAAt(5, 5).A)
	}
}

// A box narrower than twice the stroke has no hole; it is simply filled.
func TestStrokeRoundedRectThickStrokeNoHole(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 30, 30))
	canvas := render.NewImageCanvasForTest(img)
	canvas.StrokeRoundedRect(Rect{X: 5, Y: 5, W: 6, H: 20}, 1, Color{A: 1}, 3)
	if a := img.RGBAAt(8, 15).A; a < 250 {
		t.Errorf("center of a thin box = alpha %d, want solid", a)
	}
}

// A translucent thick stroke must not double up where its bands meet.
func TestStrokeRoundedRectThickTranslucentUniform(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	canvas := render.NewImageCanvasForTest(img)
	canvas.StrokeRoundedRect(Rect{X: 5, Y: 5, W: 30, H: 20}, 2, Color{A: 0.5}, 3)
	want := img.RGBAAt(20, 6).A
	for _, p := range [][2]int{{6, 15}, {20, 23}, {33, 15}, {7, 7}, {20, 7}} {
		if got := img.RGBAAt(p[0], p[1]).A; got != want {
			t.Errorf("pixel %v alpha %d, want %d everywhere on the ring", p, got, want)
		}
	}
}

// A stroke no thicker than the radius keeps its round inner corner.
func TestStrokeRoundedRectThinStrokeKeepsRoundInner(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	canvas := render.NewImageCanvasForTest(img)
	canvas.StrokeRoundedRect(Rect{X: 5, Y: 5, W: 50, H: 50}, 12, Color{A: 1}, 2)
	if a := img.RGBAAt(20, 6).A; a < 250 {
		t.Errorf("top edge alpha %d, want solid", a)
	}
	if a := img.RGBAAt(30, 30).A; a != 0 {
		t.Errorf("center alpha %d, want 0", a)
	}
	// Inside the inner arc's corner pocket, between ring and square corner.
	if a := img.RGBAAt(6, 6).A; a != 0 {
		t.Errorf("pixel (6,6) alpha %d, want 0 (outside the rounded corner)", a)
	}
}
