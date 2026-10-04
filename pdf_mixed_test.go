package qui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPDFMixedFonts exercises a Latin primary (JetBrains
// Mono, glyf -> Type0 subset) plus a CJK fallback, with a single DrawText
// string spanning both. Verifies run-splitting produces two embedded fonts
// and all text (Latin + CJK) is selectable.
func TestPDFMixedFonts(t *testing.T) {
	jbm := "fonts/jetbrainsmono/ttf/JetBrainsMono-Regular.ttf"
	data, err := os.ReadFile(jbm)
	if err != nil {
		t.Skipf("JetBrains Mono not readable: %v", err)
	}
	cjkCandidates := []string{
		"/System/Library/Fonts/Supplemental/Songti.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
		"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
	}
	cjk := ""
	for _, c := range cjkCandidates {
		if _, err := os.Stat(c); err == nil {
			cjk = c
			break
		}
	}
	if cjk == "" {
		t.Skip("no CJK fallback font present")
	}

	SetAutoLoadSystemCJK(false)
	if err := SetDefaultFont("JetBrains Mono", data); err != nil {
		t.Fatalf("SetDefaultFont: %v", err)
	}
	if err := LoadFallbackFontFromFile(cjk); err != nil {
		t.Fatalf("LoadFallbackFontFromFile: %v", err)
	}

	doc := NewPDFDoc()
	cv := doc.NewPageLogical(816, 1056, 96)
	cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	cv.DrawText("数据 Data 123 表格", Rect{X: 48, Y: 44, W: 600, H: 24}, Color{A: 1}, Font{Size: 18})

	path := t.TempDir() + "/mixed.pdf"
	if err := doc.WritePDFFile(path); err != nil {
		t.Fatalf("WritePDFFile: %v", err)
	}
	if doc.fonts == nil || len(doc.fonts.order) < 2 {
		t.Errorf("expected >=2 embedded fonts, got %d", len(doc.fonts.order))
	}

	if _, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.Command("pdftotext", path, "-").Output()
		if err != nil {
			t.Fatalf("pdftotext: %v", err)
		}
		got := string(out)
		for _, want := range []string{"数据", "Data", "123", "表格"} {
			if !strings.Contains(got, want) {
				t.Errorf("pdftotext missing %q; got:\n%s", want, got)
			}
		}
	}
}
