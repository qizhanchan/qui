package qui

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// TestPDFBasicText renders a page with Latin text and shapes, then verifies
// (a) the PDF is structurally valid, (b) pdftotext recovers the text (proving
// selectable/searchable output), and (c) a font is embedded.
func TestPDFBasicText(t *testing.T) {
	// Use a single-file bundled font so this test exercises the vector+text+
	// embed pipeline independent of the platform default (which may be a
	// .ttc collection — that path is covered by the subsetting tests).
	SetAutoLoadSystemCJK(false)
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("LoadFontFromBytes: %v", err)
	}

	doc := NewPDFDoc()
	cv := doc.NewPageLogical(816, 1056, 96) // US Letter @96dpi
	cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	// A filled rect + a border.
	cv.FillRect(Rect{X: 40, Y: 40, W: 200, H: 30}, Color{R: 0.9, G: 0.9, B: 0.95, A: 1})
	cv.StrokeRect(Rect{X: 40, Y: 40, W: 200, H: 30}, Color{R: 0, G: 0, B: 0, A: 1}, 1)
	// Text.
	black := Color{A: 1}
	cv.DrawText("Hello PDF 123", Rect{X: 48, Y: 44, W: 180, H: 22}, black, Font{Size: 14})
	cv.DrawText("Second line here", Rect{X: 48, Y: 100, W: 300, H: 22}, black, Font{Size: 12})

	path := t.TempDir() + "/basic.pdf"
	if err := doc.WritePDFFile(path); err != nil {
		t.Fatalf("WritePDFFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "%PDF-1.") {
		t.Fatalf("missing PDF header")
	}
	if !strings.Contains(string(data), "startxref") {
		t.Fatalf("missing startxref")
	}

	// pdftotext must recover the drawn text (selectable-text proof).
	if _, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.Command("pdftotext", path, "-").Output()
		if err != nil {
			t.Fatalf("pdftotext: %v", err)
		}
		got := string(out)
		for _, want := range []string{"Hello PDF 123", "Second line here"} {
			if !strings.Contains(got, want) {
				t.Errorf("pdftotext output missing %q; got:\n%s", want, got)
			}
		}
	}

	// pdffonts must show an embedded font.
	if _, err := exec.LookPath("pdffonts"); err == nil {
		out, err := exec.Command("pdffonts", path).Output()
		if err != nil {
			t.Fatalf("pdffonts: %v", err)
		}
		s := string(out)
		if !strings.Contains(s, "yes") {
			t.Errorf("expected an embedded font in pdffonts output:\n%s", s)
		}
		if !strings.Contains(strings.ToLower(s), "cid") {
			t.Logf("note: expected a CID/Type0 font:\n%s", s)
		}
	}
}
