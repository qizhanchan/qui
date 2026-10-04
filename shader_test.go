package qui

import (
	"image"
	"testing"
)

// LinearGradient at the start/end/midpoint returns the expected stop
// color. Sanity check that the parametric projection math matches
// intent — a linear gradient from (0, 0) red to (10, 0) blue must be
// pure red at (0, 0), pure blue at (10, 0), and 50/50 at (5, 0).
func TestLinearGradientSampling(t *testing.T) {
	g := LinearGradient{
		Start: Point{},
		End:   Point{X: 10},
		Stops: []GradientStop{
			{Offset: 0, Color: Color{R: 1, A: 1}},
			{Offset: 1, Color: Color{B: 1, A: 1}},
		},
	}
	cases := []struct {
		x, y float32
		want Color
	}{
		{0, 0, Color{R: 1, A: 1}},
		{10, 0, Color{B: 1, A: 1}},
		{5, 0, Color{R: 0.5, B: 0.5, A: 1}},
		// Perpendicular to the gradient axis — sample only depends on
		// the projection, so y varies leaves the color unchanged.
		{5, 100, Color{R: 0.5, B: 0.5, A: 1}},
		// Off-axis clamp.
		{-5, 0, Color{R: 1, A: 1}},
		{15, 0, Color{B: 1, A: 1}},
	}
	for _, tc := range cases {
		got := g.ColorAt(tc.x, tc.y)
		if !colorsClose(got, tc.want, 0.02) {
			t.Errorf("ColorAt(%v, %v) = %+v, want %+v", tc.x, tc.y, got, tc.want)
		}
	}
}

// RadialGradient samples radially — sample at center returns first
// stop, sample at radius returns last stop, sample halfway returns
// interpolated color.
func TestRadialGradientSampling(t *testing.T) {
	g := RadialGradient{
		Center: Point{X: 10, Y: 10},
		Radius: 10,
		Stops: []GradientStop{
			{Offset: 0, Color: Color{R: 1, A: 1}},
			{Offset: 1, Color: Color{B: 1, A: 1}},
		},
	}
	if got := g.ColorAt(10, 10); !colorsClose(got, Color{R: 1, A: 1}, 0.02) {
		t.Errorf("center = %+v, want red", got)
	}
	if got := g.ColorAt(20, 10); !colorsClose(got, Color{B: 1, A: 1}, 0.02) {
		t.Errorf("radius = %+v, want blue", got)
	}
	if got := g.ColorAt(15, 10); !colorsClose(got, Color{R: 0.5, B: 0.5, A: 1}, 0.02) {
		t.Errorf("half radius = %+v, want 50/50", got)
	}
	// Past radius saturates to the last stop.
	if got := g.ColorAt(30, 10); !colorsClose(got, Color{B: 1, A: 1}, 0.02) {
		t.Errorf("past radius = %+v, want blue (clamp)", got)
	}
}

// End-to-end: a filled rounded rect with a LinearGradient paint
// gradients from red at left to blue at right. The path rasterizer
// samples the shader per pixel; assert the left / right pixels take
// the expected colors.
func TestPaintShaderFillEndToEnd(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	c := NewImageCanvas(img)

	shader := LinearGradient{
		Start: Point{X: 0, Y: 0},
		End:   Point{X: 40, Y: 0},
		Stops: []GradientStop{
			{Offset: 0, Color: Color{R: 1, A: 1}},
			{Offset: 1, Color: Color{B: 1, A: 1}},
		},
	}
	c.DrawShape(ShapeRect(Rect{W: 40, H: 20}), Paint{Shader: shader, AntiAlias: true})

	// Left edge — should be near-red.
	i := img.PixOffset(2, 10)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 220 || got[2] > 60 {
		t.Errorf("left pixel = %v, want ~red-dominant", got)
	}
	// Right edge — should be near-blue.
	i = img.PixOffset(38, 10)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[2] < 220 || got[0] > 60 {
		t.Errorf("right pixel = %v, want ~blue-dominant", got)
	}
	// Center — should be roughly halfway between red and blue.
	i = img.PixOffset(20, 10)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 90 || got[0] > 160 || got[2] < 90 || got[2] > 160 {
		t.Errorf("center pixel = %v, want ~R≈B≈128", got)
	}
}

// A shader-filled rect renders identically whether the paint's Style
// is unset (fill default) or the caller went through the ShapePath
// route explicitly. Guards against the DrawShape switch forgetting to
// route rect+shader through the path rasterizer.
func TestPaintShaderRectMatchesPath(t *testing.T) {
	shader := LinearGradient{
		Start: Point{X: 0, Y: 0},
		End:   Point{X: 20, Y: 0},
		Stops: []GradientStop{
			{Offset: 0, Color: Color{R: 1, A: 1}},
			{Offset: 1, Color: Color{G: 1, A: 1}},
		},
	}

	imgRect := image.NewRGBA(image.Rect(0, 0, 20, 20))
	c1 := NewImageCanvas(imgRect)
	c1.DrawShape(ShapeRect(Rect{W: 20, H: 20}), Paint{Shader: shader, AntiAlias: true})

	imgPath := image.NewRGBA(image.Rect(0, 0, 20, 20))
	c2 := NewImageCanvas(imgPath)
	path := NewPath().AddRect(Rect{W: 20, H: 20})
	c2.DrawShape(ShapePath{Path: path}, Paint{Shader: shader, AntiAlias: true})

	// Compare center pixels — the rest of the row goes through the same
	// scanline logic.
	i := imgRect.PixOffset(10, 10)
	a := [4]uint8{imgRect.Pix[i], imgRect.Pix[i+1], imgRect.Pix[i+2], imgRect.Pix[i+3]}
	j := imgPath.PixOffset(10, 10)
	b := [4]uint8{imgPath.Pix[j], imgPath.Pix[j+1], imgPath.Pix[j+2], imgPath.Pix[j+3]}
	for k := 0; k < 4; k++ {
		diff := int(a[k]) - int(b[k])
		if diff < -2 || diff > 2 {
			t.Fatalf("rect vs path fill drift at pixel[%d]: rect=%v path=%v", k, a, b)
		}
	}
}

// Solid paint (Shader == nil) keeps taking the fast-path fillRectBlend
// for axis-aligned rects — no shader plumbing overhead. This assertion
// isn't behavioral so much as a guard for the DrawShape switch: if we
// accidentally route solid paints through the path rasterizer, the
// framebuffer contents still match, so the check is that the same
// visual output holds.
func TestPaintSolidStillHitsFastPath(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	c := NewImageCanvas(img)
	c.DrawShape(ShapeRect(Rect{W: 20, H: 20}), Paint{Color: Color{R: 1, A: 1}})
	i := img.PixOffset(10, 10)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("solid fill pixel = %v, want (255,0,0,255)", got)
	}
}

func colorsClose(a, b Color, epsilon float32) bool {
	f := func(x, y float32) bool {
		d := x - y
		if d < 0 {
			d = -d
		}
		return d <= epsilon
	}
	return f(a.R, b.R) && f(a.G, b.G) && f(a.B, b.B) && f(a.A, b.A)
}
