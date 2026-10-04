package qui

import (
	"strings"
	"testing"
)

// PDF colour operators carry no alpha; without an /ExtGState everything
// exported opaque, so a half-transparent watermark printed as solid ink.

func TestPDFTranslucentFillUsesExtGState(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPage(100, 100)
	cv.FillRect(Rect{W: 10, H: 10}, Color{R: 1, A: 0.5})
	out, err := doc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	pdf := string(out)
	if !strings.Contains(pdf, "/ExtGState << /GA127") && !strings.Contains(pdf, "/ExtGState << /GA128") {
		t.Fatalf("no alpha state in the page resources:\n%s", pdf)
	}
	if !strings.Contains(pdf, "/Type /ExtGState /ca 0.5") {
		t.Errorf("the alpha state does not carry ca 0.5:\n%s", pdf)
	}
	if !strings.Contains(pdf, " gs\n") {
		t.Error("the content stream never switches graphics state")
	}
}

// Opaque-only content must not grow the machinery.
func TestPDFOpaqueContentHasNoExtGState(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPage(100, 100)
	cv.FillRect(Rect{W: 10, H: 10}, Color{A: 1})
	out, _ := doc.Bytes()
	if strings.Contains(string(out), "ExtGState") {
		t.Error("an opaque page declared an alpha state")
	}
}

// The state is sticky, so an opaque draw AFTER a translucent one has to
// reset it — otherwise the body text of a watermarked page would inherit
// the watermark's transparency.
func TestPDFOpaqueDrawAfterTranslucentResetsAlpha(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPage(100, 100)
	cv.FillRect(Rect{W: 10, H: 10}, Color{A: 0.25})
	cv.FillRect(Rect{X: 20, W: 10, H: 10}, Color{A: 1})
	out, _ := doc.Bytes()
	pdf := string(out)
	if !strings.Contains(pdf, "/GA255 gs") {
		t.Fatalf("opaque draw did not reset the alpha state:\n%s", pdf)
	}
	if !strings.Contains(pdf, "/Type /ExtGState /ca 1 ") {
		t.Errorf("no fully opaque state was emitted:\n%s", pdf)
	}
}
