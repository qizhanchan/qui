package qui

import (
	"encoding/xml"
	"image"
	"image/color"
	"io"
	"strings"
	"testing"
)

// SVGCanvas is an export backend, so the tests assert on the SERIALIZED
// document: that it parses, that geometry and paint land in the attributes a
// consumer reads, and that the state stack (transform, nested clips) is
// reflected per element rather than lost.

// svgDoc draws through f and returns the document text.
func svgDoc(t *testing.T, w, h float32, f func(cv Canvas)) string {
	t.Helper()
	cv := NewSVGCanvas(w, h)
	f(cv)
	out := string(cv.Bytes())
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("malformed SVG: %v\n%s", err, out)
		}
	}
	return out
}

func TestSVGCanvasSatisfiesCanvas(t *testing.T) {
	var _ Canvas = NewSVGCanvas(100, 100)
	var _ ClipAware = NewSVGCanvas(100, 100)
}

func TestSVGCanvasRootAttributes(t *testing.T) {
	cv := NewSVGCanvas(320, 180)
	cv.SetTitle("Slide 1 & 2")
	out := string(cv.Bytes())
	for _, want := range []string{
		`width="320"`, `height="180"`, `viewBox="0 0 320 180"`,
		`<title>Slide 1 &amp; 2</title>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
}

func TestSVGCanvasShapes(t *testing.T) {
	out := svgDoc(t, 200, 200, func(cv Canvas) {
		cv.FillRect(Rect{X: 1, Y: 2, W: 30, H: 40}, Color{R: 1, A: 1})
		cv.FillRoundedRect(Rect{X: 5, Y: 5, W: 20, H: 20}, 4, Color{G: 1, A: 1})
		cv.StrokeRect(Rect{X: 0, Y: 0, W: 10, H: 10}, Color{A: 1}, 2)
		cv.DrawLine(Point{X: 1, Y: 1}, Point{X: 9, Y: 9}, Color{B: 1, A: 1}, 3)
		cv.DrawPolyline([]Point{{X: 0, Y: 0}, {X: 5, Y: 5}, {X: 10, Y: 0}}, Color{A: 1}, 1)
		cv.DrawShape(ShapePath{Path: NewPath().MoveTo(0, 0).QuadTo(5, 10, 10, 0).
			CubicTo(12, 2, 14, 4, 16, 0).Close()}, Paint{Color: Color{A: 1}})
	})
	for _, want := range []string{
		`<rect x="1" y="2" width="30" height="40" fill="#ff0000"`,
		`rx="4"`,
		`stroke="#000000" stroke-width="2"`,
		`<line x1="1" y1="1" x2="9" y2="9"`,
		`<polyline points="0,0 5,5 10,0"`,
		`d="M0 0 Q5 10 10 0 C12 2 14 4 16 0 Z"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// TestSVGCanvasBakesTransform pins the central design choice: the matrix is
// written per element, so a rotated subtree survives export.
func TestSVGCanvasBakesTransform(t *testing.T) {
	out := svgDoc(t, 100, 100, func(cv Canvas) {
		depth := cv.Save()
		cv.Translate(50, 50)
		cv.Rotate(1.5707963) // 90°
		cv.FillRect(Rect{W: 10, H: 10}, Color{A: 1})
		cv.RestoreTo(depth)
		// Back at identity: the second rect must carry no transform.
		cv.FillRect(Rect{X: 1, Y: 1, W: 2, H: 2}, Color{A: 1})
	})
	if !strings.Contains(out, `transform="matrix(`) {
		t.Errorf("rotated rect lost its transform:\n%s", out)
	}
	if !strings.Contains(out, `<rect x="1" y="1" width="2" height="2" fill="#000000"/>`) {
		t.Errorf("identity-matrix rect should carry no transform attribute:\n%s", out)
	}
}

func TestSVGCanvasNestedClips(t *testing.T) {
	out := svgDoc(t, 100, 100, func(cv Canvas) {
		depth := cv.Save()
		cv.ClipRect(Rect{W: 50, H: 50})
		cv.ClipPath(NewPath().AddCircle(25, 25, 20))
		cv.FillRect(Rect{W: 100, H: 100}, Color{A: 1})
		cv.RestoreTo(depth)
		cv.FillRect(Rect{X: 60, Y: 60, W: 10, H: 10}, Color{A: 1})
	})
	if strings.Count(out, "<clipPath") != 2 {
		t.Errorf("expected two clipPath defs, got:\n%s", out)
	}
	// The inner clip must chain to the outer one, and the clipped rect must
	// reference the inner clip.
	if !strings.Contains(out, `<clipPath id="clip2" clip-path="url(#clip1)">`) {
		t.Errorf("inner clip not chained to outer:\n%s", out)
	}
	if !strings.Contains(out, `width="100" height="100" fill="#000000" clip-path="url(#clip2)"`) {
		t.Errorf("clipped rect does not reference the inner clip:\n%s", out)
	}
	// After the restore the clip is gone again.
	if !strings.Contains(out, `<rect x="60" y="60" width="10" height="10" fill="#000000"/>`) {
		t.Errorf("clip leaked past RestoreTo:\n%s", out)
	}
}

func TestSVGCanvasGradient(t *testing.T) {
	out := svgDoc(t, 100, 100, func(cv Canvas) {
		cv.DrawShape(ShapeRect(Rect{W: 100, H: 100}), Paint{Shader: LinearGradient{
			Start: Point{}, End: Point{X: 100},
			Stops: []GradientStop{
				{Offset: 0, Color: Color{R: 1, A: 1}},
				{Offset: 1, Color: Color{B: 1, A: 0.5}},
			},
		}})
		cv.DrawShape(ShapeRect(Rect{W: 10, H: 10}), Paint{Shader: RadialGradient{
			Center: Point{X: 5, Y: 5}, Radius: 5,
			Stops: []GradientStop{{Offset: 0, Color: Color{A: 1}}},
		}})
	})
	for _, want := range []string{
		`<linearGradient id="grad1" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="100" y2="0">`,
		`<stop offset="0" stop-color="#ff0000"/>`,
		`stop-opacity="0.5"`,
		`fill="url(#grad1)"`,
		`<radialGradient id="grad2"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestSVGCanvasText(t *testing.T) {
	out := svgDoc(t, 200, 100, func(cv Canvas) {
		cv.DrawText("a < b & \"c\"", Rect{X: 10, Y: 10, W: 100, H: 20},
			Color{A: 1}, Font{Size: 14})
		cv.DrawText("two\nlines", Rect{X: 0, Y: 40, W: 100, H: 40}, Color{A: 1}, Font{Size: 12})
	})
	if !strings.Contains(out, `a &lt; b &amp; &quot;c&quot;`) {
		t.Errorf("text not escaped:\n%s", out)
	}
	if strings.Count(out, "<text ") != 3 {
		t.Errorf("expected 3 text elements (1 + 2 lines), got:\n%s", out)
	}
	if !strings.Contains(out, `font-size="14"`) {
		t.Errorf("missing font size:\n%s", out)
	}
}

func TestSVGCanvasImageIsSelfContained(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	out := svgDoc(t, 50, 50, func(cv Canvas) {
		cv.DrawImage(img, Rect{X: 1, Y: 1, W: 10, H: 10})
	})
	if !strings.Contains(out, `xlink:href="data:image/png;base64,`) {
		t.Errorf("image is not embedded:\n%s", out)
	}
}

// TestSVGCanvasSaveLayerKeepsContent guards the degradation contract: an
// effect layer loses its effect, never its content.
func TestSVGCanvasSaveLayerKeepsContent(t *testing.T) {
	out := svgDoc(t, 100, 100, func(cv Canvas) {
		depth := cv.SaveLayer(Rect{W: 100, H: 100}, Paint{
			ImageFilter: DropShadowImageFilter{Blur: 4, Color: Color{A: 1}},
		})
		cv.FillRect(Rect{X: 10, Y: 10, W: 20, H: 20}, Color{R: 1, A: 1})
		cv.RestoreTo(depth)
	})
	if !strings.Contains(out, `fill="#ff0000"`) {
		t.Errorf("SaveLayer swallowed its content:\n%s", out)
	}
}
