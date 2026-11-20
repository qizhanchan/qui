package typeset

import (
	"runtime"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Scripts no hand-kept fallback covers: only the system lookup can find
// fonts for them.
var systemOnlyRunes = []rune{
	0x1200, // ሀ Ethiopic
	0x13A0, // Ꭰ Cherokee
	0x0F40, // ཀ Tibetan
	0x10D0, // ა Georgian
	0x0D9A, // ක Sinhala
	0x1780, // ក Khmer
}

func TestSystemFallbackFindsInstalledFonts(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs the macOS system fonts")
	}
	// Tibetan: Core Text names Kokonor, which sfnt cannot parse — the
	// installed-font index must supply another font.
	PreloadSystemFontIndex()
	for _, r := range systemOnlyRunes {
		EnsureRunesCovered([]rune{r})
		if !TextFacesHaveGlyph(r) {
			t.Errorf("%c %U: no font after the system lookup", r, r)
			continue
		}
		l, ok := ShapeSingleLine([]rune{'a', r}, gfx.Font{Size: 13}, TextDirectionLTR)
		if !ok {
			t.Fatalf("%U: shaping failed", r)
		}
		for _, run := range l.Runs {
			for _, g := range run.Glyphs {
				if g.GlyphID == 0 {
					t.Errorf("%c %U shaped .notdef", r, r)
				}
			}
		}
	}
}

// LastResort and colour emoji are not text fallbacks; controls are never
// looked up.
func TestSystemFallbackRejects(t *testing.T) {
	for _, n := range []string{"LastResort", ".LastResort", "AppleColorEmoji", ".AppleColorEmojiUI", ""} {
		if usableFallbackName(n) {
			t.Errorf("%q accepted", n)
		}
	}
	for _, r := range []rune{'a', '\t', 0x200D, 0xFE0F, 0x1F600, 0x0301} {
		if !skipSystemFallback(r) {
			t.Errorf("%U looked up", r)
		}
	}
	if skipSystemFallback(0x4E2D) || skipSystemFallback(0xE0B0) {
		t.Error("a drawable rune skipped")
	}
}
