//go:build darwin && cgo

package typeset

import (
	"testing"

	"golang.org/x/image/font/sfnt"

	"github.com/qizhanchan/qui/internal/gfx"
)

func TestInstalledFontFamiliesMarksMenlo(t *testing.T) {
	var menlo *FontFamilyInfo
	fams := InstalledFontFamilies()
	for i := range fams {
		if fams[i].Name == "Menlo" {
			menlo = &fams[i]
		}
		if fams[i].Name != "" && fams[i].Name[0] == '.' {
			t.Errorf("hidden family %q listed", fams[i].Name)
		}
	}
	if menlo == nil {
		t.Skip("Menlo not installed")
	}
	if !menlo.Monospace || menlo.Han {
		t.Errorf("Menlo = %+v, want monospaced without Han", *menlo)
	}
}

// PingFang lives in macOS's asset directories, which the font directory
// index never scans; the family must still register and serve as the
// fallback font.
func TestFallbackFamilyFromAssetFont(t *testing.T) {
	if len(macosFamilyFacesForTest("PingFang SC")) == 0 {
		t.Skip("PingFang SC not installed")
	}
	t.Cleanup(func() { SetFallbackFamily("") })
	if !SetFallbackFamily("PingFang SC") {
		t.Fatal("PingFang SC did not load")
	}
	chain := FontChainForSpec(gfx.Font{Size: 13})
	cjk := CJKFallback.Load()
	var buf sfnt.Buffer
	if cjk == nil {
		t.Fatal("no CJK fallback")
	}
	if name, _ := cjk.Name(&buf, sfnt.NameIDPostScript); name != "PingFangSC-Regular" {
		t.Errorf("CJK fallback = %q, want PingFangSC-Regular", name)
	}
	// It leads the automatic fallbacks (after the keyboard-symbol face).
	inChain := false
	for _, f := range chain {
		inChain = inChain || f == cjk
	}
	if !inChain || (AutoFallbacks[0] != cjk && (len(AutoFallbacks) < 2 || AutoFallbacks[1] != cjk)) {
		t.Error("the fallback family does not lead the fallback chain")
	}
	if SetFallbackFamily("No Such Family 123") {
		t.Error("an unknown family reported as loaded")
	}
	if FallbackFamily() != "" {
		t.Errorf("unknown family left FallbackFamily = %q", FallbackFamily())
	}
}

func macosFamilyFacesForTest(family string) []systemFace { return platformFamilyFaces(family) }
