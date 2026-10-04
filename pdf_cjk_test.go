package qui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPDFCJKSubset exercises the .ttc face-0 extraction + glyph subsetting
// path using the system PingFang collection: a mixed CJK+Latin string must
// (a) round-trip through pdftotext (selectable Chinese), (b) validate under
// mutool, and (c) produce a SMALL file — proving the font was subset, not
// embedded whole (PingFang.ttc is tens of MB).
func TestPDFCJKSubset(t *testing.T) {
	candidates := []string{
		"/System/Library/Fonts/Supplemental/Songti.ttc",
		"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		"/System/Library/Fonts/PingFang.ttc",
	}
	ttc := ""
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			ttc = c
			break
		}
	}
	if ttc == "" {
		t.Skip("no CJK .ttc collection present")
	}
	if err := LoadFontFromFile(ttc); err != nil {
		t.Fatalf("LoadFontFromFile(%s): %v", ttc, err)
	}
	SetAutoLoadSystemCJK(true)

	doc := NewPDFDoc()
	cv := doc.NewPageLogical(816, 1056, 96)
	cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	cv.DrawText("你好，世界 Hello 123", Rect{X: 48, Y: 44, W: 500, H: 24}, Color{A: 1}, Font{Size: 18})

	path := t.TempDir() + "/cjk.pdf"
	if err := doc.WritePDFFile(path); err != nil {
		t.Fatalf("WritePDFFile: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 2_000_000 {
		t.Errorf("PDF is %d bytes — subsetting likely failed (full PingFang is tens of MB)", fi.Size())
	}

	if _, err := exec.LookPath("pdftotext"); err == nil {
		out, err := exec.Command("pdftotext", path, "-").Output()
		if err != nil {
			t.Fatalf("pdftotext: %v", err)
		}
		got := string(out)
		for _, want := range []string{"你好", "世界", "Hello", "123"} {
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
		if strings.Contains(strings.ToLower(string(out)), "error") {
			t.Errorf("mutool reported errors:\n%s", out)
		}
	}
}
