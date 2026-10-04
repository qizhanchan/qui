package qui

import (
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// The PDF backend gets rotation for free: pdfCanvas mirrors every matrix
// mutation as a `cm` operator and emits draw coordinates verbatim in the
// resulting user space, so a rotated CTM rotates text and shapes natively —
// no offscreen detour like the raster path needs.
//
// This test pins that down so a future refactor can't silently un-rotate
// exported text. The invariant is one of ORDER: the rotation `cm` has to be
// emitted before the text object, because a glyph's final placement is
// (text space → Tm → user space → CTM → device) and only the CTM carries
// the rotation. The emitter's own `Tm` is just a Y-flip plus translation —
// it composes with the CTM rather than replacing it, so it is not a
// problem, but a `cm` emitted after `BT` would be.
func TestPDFRotatedTextEmitsRotationMatrix(t *testing.T) {
	SetAutoLoadSystemCJK(false)
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("LoadFontFromBytes: %v", err)
	}

	doc := NewPDFDoc()
	cv := doc.NewPageLogical(400, 300, 96)
	pc, ok := cv.(*pdfCanvas)
	if !ok {
		t.Fatalf("NewPageLogical returned %T, want *pdfCanvas", cv)
	}

	depth := cv.Save()
	cv.Translate(200, 150)
	cv.Rotate(1.5707963) // 90°
	cv.Translate(-200, -150)
	cv.DrawText("Rotated", Rect{X: 150, Y: 140, W: 100, H: 20}, Color{A: 1}, Font{Size: 14})
	cv.RestoreTo(depth)

	stream := pc.page.buf.String()

	// A 90° rotation has zero on the diagonal and ±1 off it. pdfNum trims
	// trailing zeros, so match on the operator shape rather than exact text.
	rotIdx := strings.Index(stream, "0 1 -1 0 0 0 cm")
	if rotIdx < 0 {
		rotIdx = strings.Index(stream, "0 -1 1 0 0 0 cm")
	}
	if rotIdx < 0 {
		t.Fatalf("content stream has no 90° rotation cm operator:\n%s", stream)
	}
	// Text must still be shown inside a text object.
	btIdx := strings.Index(stream, "BT")
	if btIdx < 0 || !strings.Contains(stream, "ET") {
		t.Fatalf("content stream has no text object:\n%s", stream)
	}
	if !strings.Contains(stream, "Tj") && !strings.Contains(stream, "TJ") {
		t.Errorf("text object shows no glyphs:\n%s", stream)
	}
	// The ordering invariant: rotation established before the text object.
	if rotIdx > btIdx {
		t.Errorf("rotation cm emitted AFTER BT (%d > %d) — exported text would "+
			"not be rotated:\n%s", rotIdx, btIdx, stream)
	}
}

// Sibling guard for what is LEFT of the old v1 gap. DrawImage is now a real
// image XObject (pdf_image.go, pdf_image_test.go); DrawVector is still a
// no-op, so an icon or an SVG is absent from PDF export whether or not it is
// rotated. This documents the behaviour rather than blessing it.
func TestPDFVectorIsStillUnimplemented(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPageLogical(200, 200, 96)
	pc := cv.(*pdfCanvas)

	cv.DrawImage(halvesImage(10, 10), Rect{X: 10, Y: 10, W: 50, H: 50})
	if !strings.Contains(pc.page.buf.String(), "Do") {
		t.Error("pdfCanvas.DrawImage stopped emitting an XObject placement")
	}

	before := pc.page.buf.Len()
	cv.DrawVector(nil, Rect{X: 10, Y: 10, W: 50, H: 50}, Color{A: 1})
	if pc.page.buf.Len() != before {
		t.Log("pdfCanvas.DrawVector now emits content — the v1 stub has been " +
			"implemented; update this test and the q-ppt PDF-export notes")
	}
}
