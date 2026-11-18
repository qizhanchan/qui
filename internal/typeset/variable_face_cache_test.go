package typeset

import (
	_ "embed"
	"github.com/qizhanchan/qui/internal/gfx"
	"testing"
)

// Open Sans Latin variable font from the frozen layout corpus (SIL OFL;
// see testdata/variable-shaping-fixture-source.txt and its license notice).
//
//go:embed testdata/variable-shaping-fixture.ttf
var variableFaceFixture []byte

func TestVariableWeightShapingIndependentOfFirstFace(t *testing.T) {
	const family = "variable-face-cache-test"
	if err := LoadFontVariantsFromBytes(family, []gfx.FontWeight{400, 700}, false, variableFaceFixture); err != nil {
		t.Fatal(err)
	}
	measure := func(weight gfx.FontWeight, text string) float32 {
		t.Helper()
		out, ok := ShapeSingleLine([]rune(text), gfx.Font{Family: family, Size: 15.2, Weight: weight}, TextDirectionAuto)
		if !ok {
			t.Fatal("shaping failed")
		}
		return out.Advance
	}
	regular := measure(400, "Documentation")
	bold := measure(700, "Documentation")
	if regular == bold {
		t.Fatal("fixture must have different advances for regular and bold")
	}
	// Clear cached lines/fonts so each ordering exercises the shaper cache.
	invalidateShapingFontCache()
	invalidateShapeCaches()
	measure(700, "bold first")
	afterBold := measure(400, "Documentation")
	if afterBold != regular {
		t.Fatalf("regular width depends on first weight: %v vs %v", afterBold, regular)
	}
	invalidateShapingFontCache()
	invalidateShapeCaches()
	measure(400, "regular first")
	afterRegular := measure(700, "Documentation")
	if afterRegular != bold {
		t.Fatalf("bold width depends on first weight: %v vs %v", afterRegular, bold)
	}
}
