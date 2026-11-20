package jetbrainsmono

import (
	"runtime"
	"testing"

	"github.com/qizhanchan/qui"
)

// notdefIn reports the runes of s that shaped to glyph 0 (a box).
func notdefIn(s string) []rune {
	var bad []rune
	runes := []rune(s)
	l := qui.BuildTextLayout(s, qui.Font{Size: 13}, qui.TextLayoutOptions{})
	for _, ln := range l.Lines {
		if ln.Shaped == nil {
			continue
		}
		for _, run := range ln.Shaped.Runs {
			for _, g := range run.Glyphs {
				if g.GlyphID == 0 && g.ClusterIndex < len(runes) {
					bad = append(bad, runes[ln.Start+g.ClusterIndex])
				}
			}
		}
	}
	return bad
}

// JetBrains Mono has no glyph for U+0009 nor any control: a tab in plain
// text (a label, a tooltip quoting a compiler error) shapes as a space,
// the other controls as nothing — never .notdef.
func TestControlsAreNotTofu(t *testing.T) {
	Use()
	for _, s := range []string{"a\tb", "a\rb", "a\fb", "a\x1b[0mb", "a\x00b", "a\u0085b", "a\x7fb"} {
		if bad := notdefIn(s); len(bad) > 0 {
			t.Errorf("%q: .notdef for %q", s, bad)
		}
	}
}

// What a terminal and an editor show every day, over JetBrains Mono (the
// go-code setup): TUI status glyphs, box drawing, arrows, CJK and other
// scripts, math. Every one must find a font — bundled, probed, or the
// system fallback.
func TestEverydayTextHasNoTofu(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("relies on the macOS system fonts")
	}
	Use()
	for _, s := range []string{
		"⏺ Reading ⎿ done ✻ Thinking ✢ ⏵⏵ auto mode ❯ ● ○ ◐ ⏸ ⏹",    // Claude Code
		"┌─┬─┐│ │╞═╡└─┴─┘ ▀▄█▌▐░▒▓ ⠋⠙⠹⠸",                            // boxes, blocks, spinners
		"→ ← ↑ ↓ ⇒ ⇐ ↻ ⟳ ✓ ✗ ➜ ★ ☆ ♠ ⚠ ⌘ ⌥ ⇧",                       // arrows, marks
		"中文 日本語 한국어 ひらがな カタカナ",                                      // CJK
		"ไทย العربية עברית हिन्दी ქართული ትግርኛ ᏣᎳᎩ བོད ខ្មែរ සිංහල", // scripts
		"∀∃∑∏√∞≠≤≥∈∉⊂⊃∧∨ ℝℕℤ αβγ Ωμ",                                // math
	} {
		if bad := notdefIn(s); len(bad) > 0 {
			t.Errorf("%q: .notdef for %q", s, string(bad))
		}
	}
}
