package render

import (
	"image"
	"math"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/typeset"
)

// TestDrawShadowProducesDarkPixels confirms that DrawShadow lays down
// non-zero alpha around the rect when given a non-zero blur. We don't
// pin exact pixels (those depend on the Gaussian approximation) but we
// require the visible shape:
//   - center of the rect is fully covered (alpha > 200)
//   - a few px outside the rect is partially covered (alpha > 0 and < 200)
//   - far outside is untouched (alpha == 0)
func TestDrawShadowProducesDarkPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	canvas := NewImageCanvasForTest(img)
	rect := gfx.Rect{X: 30, Y: 30, W: 40, H: 40}
	spec := gfx.ElevationSpec{X: 0, Y: 2, Blur: 6, Spread: 0, Opacity: 0.5}
	canvas.DrawShadow(rect, 8, spec, gfx.Color{R: 0, G: 0, B: 0, A: 1})

	// Center of the rect (50,50) should be solidly shadowed.
	a := img.RGBAAt(50, 52).A // y=52 because the shadow has Y=+2 offset
	if a < 60 {
		t.Errorf("center shadow alpha too low: %d", a)
	}
	// Just outside the right edge — should be partial.
	a = img.RGBAAt(72, 50).A
	if a == 0 || a > 240 {
		t.Errorf("right-edge shadow alpha out of partial range: %d", a)
	}
	// Far corner — untouched.
	a = img.RGBAAt(99, 99).A
	if a != 0 {
		t.Errorf("far corner should be untouched, got alpha %d", a)
	}
}

// TestDrawShadowZeroOpacityIsNoOp guards the early-return so widgets
// that pass Theme.Elevation[0] (no shadow) don't pay any cost.
func TestDrawShadowZeroOpacityIsNoOp(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	canvas := NewImageCanvasForTest(img)
	canvas.DrawShadow(gfx.Rect{X: 10, Y: 10, W: 30, H: 30}, 4, gfx.ElevationSpec{}, gfx.Color{R: 0, G: 0, B: 0, A: 1})
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			if img.RGBAAt(x, y).A != 0 {
				t.Fatalf("zero-opacity shadow touched pixel (%d,%d)", x, y)
			}
		}
	}
}

// TestDrawShadowTinyPillDoesNotPanic guards against rounding-mismatch
// crashes in stampRoundedAlpha. When the shadow rect's fractional width
// rounds DOWN to an integer but the radius rounds UP (e.g. W=3.4 →
// right-left=3 while radius=1.7 → rInt=2), the top/bottom strip's
// `right - rInt < left + rInt` and fillBand used to slice with x0 > x1.
// Triggered by a freshly-laid-out icon button that briefly presents a
// sub-4-px shadow during the first mouse-move tick.
func TestDrawShadowTinyPillDoesNotPanic(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	canvas := NewImageCanvasForTest(img)
	// W=3.4, H=10, radius=H/2=1.7 is the canonical reproducer — but
	// also sweep a band of fractional widths so a future rounding tweak
	// can't reintroduce the same family of off-by-ones.
	for w := float32(0.4); w <= 6.0; w += 0.1 {
		canvas.DrawShadow(
			gfx.Rect{X: 10, Y: 10, W: w, H: w * 2},
			w, // pill-style: radius == shorter side (W) when W <= H
			gfx.ElevationSpec{X: 0, Y: 1, Blur: 4, Spread: 0, Opacity: 0.5},
			gfx.Color{R: 0, G: 0, B: 0, A: 1},
		)
	}
}

// TestDrawShadowZeroBlurStillStamps verifies blur=0 falls through to a
// crisp solid stamp (useful when the caller wants a hard offset rect,
// like inset borders).
func TestDrawShadowZeroBlurStillStamps(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	canvas := NewImageCanvasForTest(img)
	canvas.DrawShadow(gfx.Rect{X: 10, Y: 10, W: 20, H: 20}, 0,
		gfx.ElevationSpec{X: 0, Y: 0, Blur: 0, Spread: 0, Opacity: 1.0},
		gfx.Color{R: 1, G: 0, B: 0, A: 1})
	if img.RGBAAt(20, 20).R == 0 {
		t.Errorf("crisp shadow stamp missing at center")
	}
}

func TestFractionalShadowRingPreservesBottom(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		img := image.NewRGBA(image.Rect(0, 0, 400, 180))
		c := NewImageCanvasForTest(img)
		c.Scale(scale, scale)
		r := gfx.Rect{X: 20, Y: 20.34375, W: 166.6875, H: 47.375}
		c.DrawShadow(r, 9999, gfx.ElevationSpec{Spread: 1, Opacity: 1, Knockout: true}, gfx.Color{A: 1})
		x := int((r.X + r.W/2) * scale)
		outerY := int(math.Ceil(float64((r.Y + r.H) * scale)))
		if a := img.RGBAAt(x, outerY).A; a < 100 {
			t.Fatalf("scale %v: fractional bottom edge disappeared: %d", scale, a)
		}
		coverage := 0
		for y := int((r.Y + r.H - 1) * scale); y < int((r.Y+r.H+2)*scale); y++ {
			coverage += int(img.RGBAAt(x, y).A)
		}
		if coverage < int(200*scale) {
			t.Fatalf("scale %v: bottom ring coverage %d", scale, coverage)
		}
	}
}

func TestUnderlineOffsetSeparatesUnderscoreInk(t *testing.T) {
	f := gfx.Font{Family: "Menlo", Size: 15.44}
	layout := typeset.BuildTextLayout("__import__()", f, typeset.TextLayoutOptions{LineHeightScale: 24 / 15.44})
	img := image.NewRGBA(image.Rect(0, 0, 200, 60))
	c := NewImageCanvasForTest(img)
	r := gfx.Rect{W: 200, H: 24}
	typeset.DrawTextLayout(c, layout, r, gfx.Color{A: 1}, f)
	typeset.DrawTextDecorationPaint(c, layout, r, gfx.Color{A: 1}, f, typeset.DecorationUnderline, typeset.DecorationPaint{Color: gfx.Color{R: 1, A: 1}, HasColor: true, UnderlineOffset: 4, HasUnderlineOffset: true})
	lastGlyph, lastLine := -1, -1
	for y := 0; y < 60; y++ {
		for x := 0; x < 200; x++ {
			p := img.RGBAAt(x, y)
			if p.A > 100 {
				if p.R < 40 && p.G < 40 && p.B < 40 {
					lastGlyph = max(lastGlyph, y)
				}
				if p.R > 150 && p.G < 40 && p.B < 40 {
					lastLine = max(lastLine, y)
				}
			}
		}
	}
	if lastGlyph < 0 || lastLine <= lastGlyph+1 {
		t.Fatalf("underline merged with glyph ink: glyph bottom %d, line bottom %d", lastGlyph, lastLine)
	}
}
