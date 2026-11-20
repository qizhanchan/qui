package jetbrainsmono

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/typeset"
	"golang.org/x/image/font/sfnt"
)

// With a Latin primary, full-width CJK punctuation must come from the
// same CJK font as the characters around it — not from Apple Symbols,
// whose centred ，：are the Traditional design and a different typeface.
func TestCJKPunctuationFromCJKFont(t *testing.T) {
	Use()
	const s = "平台补贴最后：运费，商品。（是）！"
	l := qui.BuildTextLayout(s, qui.Font{Size: 13}, qui.TextLayoutOptions{})
	cjk := typeset.CJKFallback.Load()
	if cjk == nil {
		t.Skip("no system CJK font")
	}
	runes := []rune(s)
	for _, ln := range l.Lines {
		for _, run := range ln.Shaped.Runs {
			src := typeset.SourceFontForShapingFace(run.Face)
			for _, g := range run.Glyphs {
				r := runes[ln.Start+g.ClusterIndex]
				if src != cjk {
					t.Errorf("shaped %q with a non-CJK font", r)
				}
			}
		}
	}
	chain := typeset.FontChainForSpec(gfx.Font{Size: 13})
	var buf sfnt.Buffer
	for _, r := range "：，（）！" {
		if f, _, _ := typeset.ResolveRune(chain, r, &buf); f != cjk {
			t.Errorf("ResolveRune(%q) is not the CJK font", r)
		}
	}
	// ASCII punctuation stays with the primary.
	if f, _, _ := typeset.ResolveRune(chain, ':', &buf); f != chain[0] {
		t.Error("ASCII ':' left the primary font")
	}
}

// A chosen fallback family draws the Chinese a Latin-only primary lacks,
// in place of the font the OS would pick.
func TestChosenFallbackFamilyDrawsCJK(t *testing.T) {
	Use()
	const fam = "Hiragino Sans GB"
	if !qui.SetFallbackFontFamily(fam) {
		t.Skip(fam + " not installed")
	}
	t.Cleanup(func() { qui.SetFallbackFontFamily("") })
	l := qui.BuildTextLayout("中文：好", qui.Font{Size: 13}, qui.TextLayoutOptions{})
	var buf sfnt.Buffer
	for _, ln := range l.Lines {
		for _, run := range ln.Shaped.Runs {
			src := typeset.SourceFontForShapingFace(run.Face)
			name, _ := src.Name(&buf, sfnt.NameIDPostScript)
			if name != "HiraginoSansGB-W3" {
				t.Errorf("run shaped with %q, want HiraginoSansGB-W3", name)
			}
		}
	}
}
