package svg_test

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
)

func rasterAlpha(t *testing.T, src string, size int) func(x, y int) uint32 {
	t.Helper()
	icon, err := svg.ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img := icon.Rasterize(size, size, qui.Color{A: 1}).(*image.RGBA)
	return func(x, y int) uint32 {
		_, _, _, a := img.At(x, y).RGBA()
		return a
	}
}

// TestArcSweepPicksCorrectCenter is a regression for small arcs in icon
// paths (a rounded-rect corner "a2 2 0 0 1 2 2"): the sweep flag used to
// be handed to rasterx with the wrong polarity, so every non-semicircle
// arc took the other centre and bulged outwards as a 3/4 circle — the
// corners of small icons rendered as dots.
func TestArcSweepPicksCorrectCenter(t *testing.T) {
	// Filled rounded rect 18x18 at (3,3) with r=2 corners.
	a := rasterAlpha(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z" fill="black"/></svg>`, 24)
	for _, p := range [][2]int{{0, 0}, {23, 0}, {0, 23}, {23, 23}, {1, 1}, {22, 22}, {12, 1}, {1, 12}} {
		if v := a(p[0], p[1]); v != 0 {
			t.Errorf("pixel %v outside the rounded rect is covered (a=%#x)", p, v)
		}
	}
	for _, p := range [][2]int{{12, 12}, {4, 4}, {19, 4}, {4, 19}, {19, 19}} {
		if v := a(p[0], p[1]); v < 0xc000 {
			t.Errorf("pixel %v inside the rounded rect not covered (a=%#x)", p, v)
		}
	}

	// Large clockwise (sweep=1) arc from top to left: a pie covering the
	// TR, BR and BL quadrants; TL stays empty.
	big := rasterAlpha(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 12L12 4A8 8 0 1 1 4 12Z" fill="black"/></svg>`, 24)
	if big(8, 8) != 0 {
		t.Errorf("large arc pie: TL quadrant covered")
	}
	for _, p := range [][2]int{{15, 8}, {15, 15}, {8, 15}} {
		if big(p[0], p[1]) < 0xc000 {
			t.Errorf("large arc pie: %v not covered", p)
		}
	}

	// Small counter-clockwise (sweep=0) arc from top to left: TL only.
	small := rasterAlpha(t, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 12l0-8a8 8 0 0 0-8 8z" fill="black"/></svg>`, 24)
	if small(8, 8) < 0xc000 {
		t.Errorf("small arc pie: TL quadrant not covered")
	}
	for _, p := range [][2]int{{15, 8}, {15, 15}, {8, 15}} {
		if small(p[0], p[1]) != 0 {
			t.Errorf("small arc pie: %v covered", p)
		}
	}
}
