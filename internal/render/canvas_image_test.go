package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Helpers for pixel inspection.
func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = c.A
	}
	return img
}

func TestFillRoundedRectFillsInteriorFully(t *testing.T) {
	// A point well inside the rounded rect should be fully filled.
	img := solidImage(100, 100, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.FillRoundedRect(gfx.Rect{X: 10, Y: 10, W: 80, H: 80}, 10, gfx.Color{R: 1, G: 0, B: 0, A: 1})

	// Center pixel (50, 50) — deep inside, should be pure red.
	px := img.RGBAAt(50, 50)
	if px.R != 255 || px.G != 0 || px.B != 0 {
		t.Errorf("center pixel not filled; got %+v", px)
	}
}

func TestFillRoundedRectLeavesOutsideCornerAlone(t *testing.T) {
	// Pixel (0,0) is outside the rounded rect (corner cut off by radius).
	// It should retain the original black color.
	img := solidImage(100, 100, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.FillRoundedRect(gfx.Rect{X: 10, Y: 10, W: 80, H: 80}, 20, gfx.Color{R: 1, G: 0, B: 0, A: 1})

	// (10, 10) is the top-left corner of the rect's bounding box, but the
	// rounded corner's circle center is (30, 30) with radius 20, so
	// (10, 10) is sqrt(800)≈28.28 away — well outside. Should stay black.
	px := img.RGBAAt(10, 10)
	if px.R != 0 || px.G != 0 || px.B != 0 {
		t.Errorf("outside-corner pixel should be untouched; got %+v", px)
	}
}

func TestFillRoundedRectHasAntiAliasedCornerTransition(t *testing.T) {
	// The 1-pixel transition band forms an arc — pixels on the corner
	// diagonal happen to fall just outside the band due to float32
	// rounding. Scan the whole top-left corner box (not just the
	// diagonal) and assert that AT LEAST SOME pixels have partial
	// coverage — that proves the AA path ran and produced mid-range
	// alpha values, not just binary fill.
	img := solidImage(100, 100, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.FillRoundedRect(gfx.Rect{X: 10, Y: 10, W: 80, H: 80}, 20, gfx.Color{R: 1, G: 0, B: 0, A: 1})

	partial := 0
	for y := 10; y < 30; y++ {
		for x := 10; x < 30; x++ {
			px := img.RGBAAt(x, y)
			if px.R > 0 && px.R < 255 {
				partial++
			}
		}
	}
	if partial == 0 {
		t.Errorf("expected AA transition pixels in top-left corner box; found none")
	}
}

func TestFillRoundedRectDegenerateFallsBackToFillRect(t *testing.T) {
	// radius <= 0.5 should paint the full rectangle with no AA.
	img := solidImage(20, 20, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.FillRoundedRect(gfx.Rect{X: 0, Y: 0, W: 20, H: 20}, 0.1, gfx.Color{R: 1, G: 1, B: 1, A: 1})

	// All pixels should be white (full fill).
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			px := img.RGBAAt(x, y)
			if px.R != 255 || px.G != 255 || px.B != 255 {
				t.Errorf("pixel (%d,%d) not fully filled; got %+v", x, y, px)
				return
			}
		}
	}
}

func TestFillRoundedRectRadiusClamp(t *testing.T) {
	// Radius larger than half the shorter side should be clamped to
	// exactly that half-side, producing a capsule / fully-rounded shape.
	// For a 20×20 rect at (10,10)..(30,30) with radius=50, clamp to 10.
	// Circle centers land at (20,20) in all four corners (they merge).
	// The rect interior is then a filled disc of radius 10.
	img := solidImage(40, 40, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.FillRoundedRect(gfx.Rect{X: 10, Y: 10, W: 20, H: 20}, 50, gfx.Color{R: 1, G: 0, B: 0, A: 1})

	// Center (20, 20) — circle-center itself — must be fully filled.
	px := img.RGBAAt(20, 20)
	if px.R != 255 {
		t.Errorf("center should be red; got %+v", px)
	}
	// Pick a pixel inside the circle: (18, 20), distance to (20,20) is
	// sqrt((18.5-20)² + 0.5²) = sqrt(2.25+0.25) ≈ 1.58, well inside.
	px = img.RGBAAt(18, 20)
	if px.R == 0 {
		t.Errorf("pixel inside clamped circle should be filled; got %+v", px)
	}
	// And pixel outside the corner: (10, 10) — distance sqrt(182.5)≈13.5,
	// outside the radius-10 circle, must stay black.
	px = img.RGBAAt(10, 10)
	if px.R != 0 {
		t.Errorf("pixel outside clamped circle should stay black; got %+v", px)
	}
}

func rasterizeSampleText(spec gfx.Font) *image.RGBA {
	img := solidImage(160, 48, color.RGBA{0, 0, 0, 0})
	c := NewImageCanvasForTest(img)
	c.DrawText("qui", gfx.Rect{X: 12, Y: 10, W: 120, H: 28}, gfx.Color{R: 1, G: 1, B: 1, A: 1}, spec)
	return img
}

func TestDrawTextBoldProducesDifferentRaster(t *testing.T) {
	normal := rasterizeSampleText(gfx.Font{Size: 20})
	bold := rasterizeSampleText(gfx.Font{Size: 20, Bold: true})
	if bytes.Equal(normal.Pix, bold.Pix) {
		t.Fatalf("bold text raster should differ from normal raster")
	}
}

func TestDrawTextItalicProducesDifferentRaster(t *testing.T) {
	normal := rasterizeSampleText(gfx.Font{Size: 20})
	italic := rasterizeSampleText(gfx.Font{Size: 20, Italic: true})
	if bytes.Equal(normal.Pix, italic.Pix) {
		t.Fatalf("italic text raster should differ from normal raster")
	}
}

func TestDrawLineHorizontalWidth1PaintsRow(t *testing.T) {
	img := solidImage(40, 20, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.DrawLine(gfx.Point{X: 5, Y: 10}, gfx.Point{X: 35, Y: 10}, gfx.Color{R: 1, G: 0, B: 0, A: 1}, 1)

	// The capsule's centerline sits at y=10. Pixel centers at y=9 and y=10
	// are within halfW=0.5 (clamped) + 0.5 transition of the line, so they
	// should pick up some red. Verify at least the mid-line pixels flipped.
	for x := 10; x < 30; x++ {
		px := img.RGBAAt(x, 10)
		if px.R == 0 {
			t.Fatalf("pixel (%d,10) should have been painted; got %+v", x, px)
		}
	}
	// Pixel far from the line should remain untouched.
	px := img.RGBAAt(20, 0)
	if px.R != 0 {
		t.Errorf("pixel far from line should remain black; got %+v", px)
	}
}

func TestDrawLineClippedRespectsClipRect(t *testing.T) {
	img := solidImage(40, 20, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.CanvasState = gfx.NewCanvasState(gfx.Rect{W: 40, H: 20})
	// Clip to the left half only; a full-width line should only paint
	// pixels in columns 0..19.
	c.ClipRect(gfx.Rect{X: 0, Y: 0, W: 20, H: 20})
	c.DrawLine(gfx.Point{X: 0, Y: 10}, gfx.Point{X: 40, Y: 10}, gfx.Color{R: 1, G: 0, B: 0, A: 1}, 2)

	// Inside clip: should be painted.
	if img.RGBAAt(10, 10).R == 0 {
		t.Errorf("expected pixel inside clip to be painted")
	}
	// Outside clip: must remain untouched.
	if img.RGBAAt(30, 10).R != 0 {
		t.Errorf("expected pixel outside clip to remain black; got %+v", img.RGBAAt(30, 10))
	}
}

// ClearRect is how GL-compositing widgets (VideoView / scene3d.Viewport /
// WebView) open the 2D layer: paint an opaque letterbox background, then
// clear the pixels the GL texture should show through.
func TestClearRectMakesTransparent(t *testing.T) {
	img := solidImage(8, 8, color.RGBA{255, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	c.ClearRect(gfx.Rect{X: 2, Y: 2, W: 4, H: 4})
	if got := img.RGBAAt(3, 3); got != (color.RGBA{}) {
		t.Errorf("cleared pixel (3,3) = %+v, want fully transparent", got)
	}
	if got := img.RGBAAt(0, 0); got.R != 255 || got.A != 255 {
		t.Errorf("untouched pixel (0,0) = %+v, want opaque red", got)
	}
}

func TestClearRectFollowsTransformAndClip(t *testing.T) {
	img := solidImage(20, 20, color.RGBA{0, 0, 255, 255})
	c := NewImageCanvasForTest(img)
	c.Save()
	c.Translate(10, 0)
	c.ClipRect(gfx.Rect{X: 0, Y: 0, W: 4, H: 20})
	c.ClearRect(gfx.Rect{X: 0, Y: 0, W: 8, H: 8}) // translated to x 10..18, clipped to 10..14
	c.Restore()
	for _, p := range []struct {
		x, y  int
		clear bool
	}{{11, 3, true}, {13, 7, true}, {3, 3, false}, {15, 3, false}, {11, 9, false}} {
		got := img.RGBAAt(p.x, p.y)
		if (got.A == 0) != p.clear {
			t.Errorf("(%d,%d) = %+v, want cleared=%v", p.x, p.y, got, p.clear)
		}
	}
}

// A zero-alpha color paints nothing, whatever its RGB and whatever the
// primitive — as on a web canvas. Color{} used to erase (the old
// hole-punch idiom), so an unset or CSS-transparent color wiped pixels:
// a diagram's stroke "none" left transparent outlines.
func TestZeroAlphaPaintsNothing(t *testing.T) {
	orig := color.RGBA{120, 80, 200, 255}
	img := solidImage(40, 40, orig)
	c := NewImageCanvasForTest(img)
	for _, col := range []gfx.Color{{}, {R: 1, G: 1, B: 1}, {R: 0.5, A: 0.0008}} {
		c.FillRect(gfx.Rect{X: 2, Y: 2, W: 10, H: 10}, col)
		c.StrokeRect(gfx.Rect{X: 14, Y: 2, W: 10, H: 10}, col, 2)
		c.FillRoundedRect(gfx.Rect{X: 26, Y: 2, W: 10, H: 10}, 3, col)
		c.StrokeRoundedRect(gfx.Rect{X: 2, Y: 14, W: 10, H: 10}, 3, col, 2)
		c.DrawLine(gfx.Point{X: 14, Y: 20}, gfx.Point{X: 36, Y: 20}, col, 3)
		c.DrawShape(gfx.ShapePath{Path: gfx.NewPath().AddCircle(20, 32, 6)}, gfx.Paint{Color: col, AntiAlias: true})
		c.DrawShape(gfx.ShapePath{Path: gfx.NewPath().AddCircle(32, 32, 6)}, gfx.Paint{Color: col, Style: gfx.PaintStroke, StrokeWidth: 2, AntiAlias: true})
	}
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			if got := img.RGBAAt(x, y); got != orig {
				t.Fatalf("pixel (%d,%d) = %+v after zero-alpha paints, want untouched %+v", x, y, got, orig)
			}
		}
	}
}

// Clear replaces the clip's pixels: transparent clears them (an overlay
// panel's corners), translucent sets them rather than blending, opaque
// fills.
func TestClearReplacesInsideClip(t *testing.T) {
	for _, tc := range []struct {
		name string
		col  gfx.Color
		want color.RGBA
	}{
		{"transparent", gfx.ColorTransparent, color.RGBA{}},
		{"translucent", gfx.Color{R: 1, A: 0.5}, color.RGBA{128, 0, 0, 128}}, // premultiplied, not blended over blue
		{"opaque", gfx.Color{G: 1, A: 1}, color.RGBA{0, 255, 0, 255}},
	} {
		img := solidImage(10, 10, color.RGBA{0, 0, 255, 255})
		c := NewImageCanvasForTest(img)
		c.Save()
		c.ClipRect(gfx.Rect{X: 0, Y: 0, W: 5, H: 10})
		c.Clear(tc.col)
		c.Restore()
		if got := img.RGBAAt(2, 2); got != tc.want {
			t.Errorf("%s: inside clip = %+v, want %+v", tc.name, got, tc.want)
		}
		if got := img.RGBAAt(7, 2); got != (color.RGBA{0, 0, 255, 255}) {
			t.Errorf("%s: outside clip = %+v, want untouched blue", tc.name, got)
		}
	}
}

// Regression: DrawStateLayer's hover overlay computes c.A = layerA *
// opacity in float space; for the first ~1.6 ms of a fast hover, that
// ends up at e.g. 0.0008 — non-zero in float, but ToByte rounds it to
// 0. Before the fix, fillRectBlend matched on rgba.A == 0 alone and
// hole-punched the underlying button to (0,0,0,0), letting gl.Clear's
// black background through inside the rounded-rect strips. Only the
// rounded corners (drawCornerAA, which no-ops on a==0) survived,
// producing a "black rectangle in the button's center, corners
// intact" flash mid-hover. The fix tightened the hole-punch trigger
// to (R,G,B,A) all zero — degenerate translucent paints now skip
// instead of erasing.
func TestFillRectTranslucentNonZeroRGBDoesNotHolePunch(t *testing.T) {
	img := solidImage(8, 8, color.RGBA{120, 80, 200, 255})
	c := NewImageCanvasForTest(img)
	// White overlay with effectively-zero alpha (rounds to 0 byte).
	c.FillRect(gfx.Rect{X: 2, Y: 2, W: 4, H: 4}, gfx.Color{R: 1, G: 1, B: 1, A: 0.0008})

	// Underlying purple must survive — no hole punch, no overlay
	// either (alpha rounded to 0 in byte space = invisible).
	if got := img.RGBAAt(3, 3); got.R != 120 || got.G != 80 || got.B != 200 || got.A != 255 {
		t.Errorf("translucent paint with rounded-to-zero alpha and non-zero RGB hole-punched: got %+v, want original purple", got)
	}
}

func TestDrawPolylinePaintsAllSegments(t *testing.T) {
	img := solidImage(40, 40, color.RGBA{0, 0, 0, 255})
	c := NewImageCanvasForTest(img)
	// Zig-zag that touches three distinct rows.
	pts := []gfx.Point{{X: 5, Y: 5}, {X: 35, Y: 5}, {X: 5, Y: 35}, {X: 35, Y: 35}}
	c.DrawPolyline(pts, gfx.Color{R: 1, G: 0, B: 0, A: 1}, 1)

	// Check one pixel per segment.
	checks := []struct{ x, y int }{
		{20, 5},  // top edge
		{20, 35}, // bottom edge
	}
	for _, ch := range checks {
		if img.RGBAAt(ch.x, ch.y).R == 0 {
			t.Errorf("expected polyline to paint (%d,%d); got %+v", ch.x, ch.y, img.RGBAAt(ch.x, ch.y))
		}
	}
}
