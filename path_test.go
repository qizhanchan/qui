package qui

import (
	"image"
	"image/color"
	"testing"
)

func TestAddRRectCorners(t *testing.T) {
	rect := Rect{X: 0, Y: 0, W: 100, H: 60}

	// A per-corner path spans the full rect bounds.
	p := NewPath().AddRRectCorners(rect, 10, 20, 0, 5)
	if p.IsEmpty() {
		t.Fatal("per-corner path is empty")
	}
	b := p.Bounds()
	if b.W < 99 || b.W > 101 || b.H < 59 || b.H > 61 {
		t.Errorf("bounds = %vx%v, want ~100x60", b.W, b.H)
	}

	// All-zero radii degenerate to a plain rect.
	square := NewPath().AddRRectCorners(rect, 0, 0, 0, 0)
	if square.Bounds() != rect {
		t.Errorf("zero-radius bounds = %+v, want %+v", square.Bounds(), rect)
	}

	// CSS overlap rule: radii larger than the edge scale down so corners
	// never overrun. With tl=tr=100 on a 100-wide rect they clamp so the
	// path stays within bounds.
	clamped := NewPath().AddRRectCorners(Rect{W: 100, H: 100}, 100, 100, 0, 0)
	if cb := clamped.Bounds(); cb.W > 100.5 || cb.H > 100.5 {
		t.Errorf("clamped bounds = %vx%v, want within 100x100", cb.W, cb.H)
	}
}

func TestPathBoundsIncludesAllCommandPoints(t *testing.T) {
	p := NewPath().
		MoveTo(10, 20).
		LineTo(100, 50).
		QuadTo(150, -10, 200, 30).
		CubicTo(250, 200, 50, 250, 0, 100).
		Close()

	b := p.Bounds()
	// Quad control (150,-10) and cubic controls (250,200), (50,250)
	// should expand the bounding box to (0,-10)..(250,250).
	want := Rect{X: 0, Y: -10, W: 250, H: 260}
	if b != want {
		t.Errorf("Bounds = %+v, want %+v", b, want)
	}
}

func TestPathStrokeRendersLineSegments(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := newImageCanvasForTest(img)

	p := NewPath().MoveTo(5, 20).LineTo(35, 20)
	c.DrawShape(ShapePath{Path: p},
		Paint{Color: Color{R: 1, G: 0, B: 0, A: 1}, Style: PaintStroke, StrokeWidth: 1})

	// Center of the stroked line: a few pixels should be red.
	red := 0
	for x := 5; x <= 35; x++ {
		if img.RGBAAt(x, 20).R > 0 {
			red++
		}
	}
	if red < 25 {
		t.Errorf("expected the horizontal stroke to paint ~30 pixels; got %d", red)
	}
}

func TestPathFillTriangleEvenOdd(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for i := range img.Pix {
		img.Pix[i] = 0
	}
	c := newImageCanvasForTest(img)

	// A simple right triangle with vertices (5,5), (35,5), (5,35).
	p := NewPath().MoveTo(5, 5).LineTo(35, 5).LineTo(5, 35).Close()
	c.DrawShape(ShapePath{Path: p}, Paint{Color: Color{R: 0, G: 1, B: 0, A: 1}})

	// (10, 10) is inside the triangle — should be green.
	if got := img.RGBAAt(10, 10); got.G == 0 {
		t.Errorf("interior pixel (10,10) not filled; got %+v", got)
	}
	// (30, 30) is outside the triangle — should be untouched (black).
	if got := img.RGBAAt(30, 30); got.G != 0 {
		t.Errorf("exterior pixel (30,30) leaked into fill; got %+v", got)
	}
}

func TestPathAddOvalFillsApproximateCircle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	c := newImageCanvasForTest(img)
	p := NewPath().AddOval(Rect{X: 10, Y: 10, W: 40, H: 40})
	c.DrawShape(ShapePath{Path: p}, Paint{Color: Color{R: 1, G: 0, B: 0, A: 1}})

	// Center of the circle should be filled.
	if got := img.RGBAAt(30, 30); got.R == 0 {
		t.Errorf("center of oval should be filled; got %+v", got)
	}
	// Far corner is well outside the inscribed circle.
	if got := img.RGBAAt(0, 0); got.R != 0 {
		t.Errorf("outside-circle pixel leaked into fill; got %+v", got)
	}
}

// Smoke test: a circle, ellipse, and triangle all fill without panicking
// or producing all-zero outputs. The earlier scanline rasterizer had a
// degenerate path when the bounding box was at sub-pixel coordinates;
// pinning that here so future tweaks don't regress.
func TestPathFillSubpixelBoundsStillRenders(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	c := newImageCanvasForTest(img)
	p := NewPath().AddCircle(30.7, 30.3, 12)
	c.DrawShape(ShapePath{Path: p}, Paint{Color: Color{R: 1, G: 1, B: 1, A: 1}})

	any := false
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i+3] != 0 {
			any = true
			break
		}
	}
	if !any {
		t.Errorf("subpixel-positioned circle rendered nothing")
	}
}

// Contains is the hit-testing counterpart of the fill rasterizer: a click
// inside the painted region must report inside, one in the bounding box but
// outside the geometry must not.
func TestPathContainsMatchesGeometryNotBounds(t *testing.T) {
	// Triangle with apex at the top center of a 100x100 box: the top-left
	// corner is inside Bounds() but outside the shape.
	tri := NewPath().MoveTo(50, 0).LineTo(100, 100).LineTo(0, 100).Close()
	cases := []struct {
		pt   Point
		want bool
	}{
		{Point{X: 50, Y: 50}, true},   // center
		{Point{X: 50, Y: 95}, true},   // near the base
		{Point{X: 2, Y: 2}, false},    // corner of the bounding box
		{Point{X: 98, Y: 2}, false},   // other corner
		{Point{X: 50, Y: 105}, false}, // below the shape
	}
	for _, c := range cases {
		if got := tri.Contains(c.pt); got != c.want {
			t.Errorf("Contains(%v) = %v, want %v", c.pt, got, c.want)
		}
	}
}

// A hole punched by a reversed inner subpath is outside under the non-zero
// winding rule — the same rule DrawShape fills with.
func TestPathContainsNonZeroWindingHole(t *testing.T) {
	p := NewPath().
		MoveTo(0, 0).LineTo(100, 0).LineTo(100, 100).LineTo(0, 100).Close().
		// Inner square wound the other way.
		MoveTo(30, 30).LineTo(30, 70).LineTo(70, 70).LineTo(70, 30).Close()
	if !p.Contains(Point{X: 10, Y: 50}) {
		t.Error("point in the outer ring should be inside")
	}
	if p.Contains(Point{X: 50, Y: 50}) {
		t.Error("point in the reversed inner square should be outside (hole)")
	}
}

// Curves flatten before the test, so an oval hit-tests as an oval.
func TestPathContainsFollowsCurves(t *testing.T) {
	oval := NewPath().AddOval(Rect{W: 100, H: 50})
	if !oval.Contains(Point{X: 50, Y: 25}) {
		t.Error("oval center should be inside")
	}
	if oval.Contains(Point{X: 3, Y: 3}) {
		t.Error("oval bounding-box corner should be outside")
	}
}

// Polygons exposes one polyline per subpath with curves already sampled.
func TestPolygonsSplitsSubpaths(t *testing.T) {
	p := NewPath().
		MoveTo(0, 0).LineTo(10, 0).LineTo(10, 10).Close().
		MoveTo(20, 20).LineTo(30, 20)
	subs := p.Polygons()
	if len(subs) != 2 {
		t.Fatalf("Polygons() = %d subpaths, want 2", len(subs))
	}
	if got := subs[0][len(subs[0])-1]; got != (Point{}) {
		t.Errorf("closed subpath should end back at its start, ends at %v", got)
	}
	if len(subs[1]) != 2 {
		t.Errorf("open subpath = %d points, want 2", len(subs[1]))
	}
}

var _ = color.RGBA{} // keep image/color import used; tests above use the underlying types.
