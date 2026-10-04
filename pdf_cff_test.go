package qui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPDFCFFType3 exercises the Type3 outline-font path using a CFF/OpenType
// CJK font (Hiragino Sans GB). CFF fonts can't be CIDFontType2, so glyphs are
// embedded as vector outlines; the text must still be selectable via
// ToUnicode, the file must be small (subset outlines only, not the whole
// font), and mutool must accept it.
func TestPDFCFFType3(t *testing.T) {
	const cff = "/System/Library/Fonts/Hiragino Sans GB.ttc"
	if _, err := os.Stat(cff); err != nil {
		t.Skip("Hiragino Sans GB.ttc not present")
	}
	if err := LoadFontFromFile(cff); err != nil {
		t.Fatalf("LoadFontFromFile: %v", err)
	}
	SetAutoLoadSystemCJK(true)

	doc := NewPDFDoc()
	cv := doc.NewPageLogical(816, 1056, 96)
	cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	cv.DrawText("你好世界 数据表 Hello", Rect{X: 48, Y: 44, W: 600, H: 24}, Color{A: 1}, Font{Size: 20})

	path := t.TempDir() + "/cff.pdf"
	if err := doc.WritePDFFile(path); err != nil {
		t.Fatalf("WritePDFFile: %v", err)
	}

	fi, _ := os.Stat(path)
	if fi.Size() > 500_000 {
		t.Errorf("Type3 PDF is %d bytes — expected a small outline subset", fi.Size())
	}

	if _, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.Command("pdftotext", path, "-").Output()
		if err != nil {
			t.Fatalf("pdftotext: %v", err)
		}
		got := string(out)
		for _, want := range []string{"你好", "世界", "数据", "Hello"} {
			if !strings.Contains(got, want) {
				t.Errorf("pdftotext missing %q; got:\n%s", want, got)
			}
		}
	}
	if _, err := exec.LookPath("mutool"); err == nil {
		out, err := exec.Command("mutool", "info", path).CombinedOutput()
		if err != nil {
			t.Fatalf("mutool info failed: %v\n%s", err, out)
		}
	}
}
