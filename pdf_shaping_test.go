package qui

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPDFArabicShapingRemainsSelectable(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	doc := NewPDFDoc()
	canvas := doc.NewPageLogical(400, 200, 96)
	canvas.DrawText("سلام", Rect{X: 40, Y: 40, W: 300, H: 50}, ColorBlack, Font{Size: 30})
	outPath := t.TempDir() + "/arabic.pdf"
	if err := doc.WritePDFFile(outPath); err != nil {
		t.Fatalf("write PDF: %v", err)
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return
	}
	out, err := exec.Command("pdftotext", outPath, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext: %v", err)
	}
	if !strings.Contains(string(out), "سلام") {
		t.Fatalf("Arabic ToUnicode extraction = %q, want سلام", out)
	}
}
