package pdf

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

func TestPDFArabicShapingRemainsSelectable(t *testing.T) {
	testLoadShapingFont(t, "testdata/fonts/Amiri-Regular.ttf")

	doc := NewDoc()
	canvas := doc.NewPageLogical(400, 200, 96)
	canvas.DrawText("سلام", gfx.Rect{X: 40, Y: 40, W: 300, H: 50}, gfx.ColorBlack, gfx.Font{Size: 30})
	outPath := t.TempDir() + "/arabic.pdf"
	if err := doc.WriteFile(outPath); err != nil {
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
