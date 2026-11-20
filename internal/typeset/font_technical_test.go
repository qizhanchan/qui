package typeset

import (
	"runtime"
	"testing"
)

// Claude Code prints ⏺ as its bullet and ⏵⏵ in its mode line; both must
// come from a text face (foreground colour) — on macOS STIX Two Math, which
// the system fallback finds — not tofu or colour emoji.
func TestMediaSymbolsHaveTextGlyphs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("relies on the macOS system fonts")
	}
	for _, r := range []rune{0x23FA, 0x23F5, 0x23F8} {
		EnsureRunesCovered([]rune{r})
		if !TextFacesHaveGlyph(r) {
			t.Errorf("no text glyph for %U", r)
		}
		if IsEmojiRune(r) {
			t.Errorf("%U routed to colour emoji", r)
		}
	}
}
