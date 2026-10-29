package typeset

import (
	"os"
	"slices"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

func glyphIDs(t *testing.T, s string, f gfx.Font) []uint32 {
	t.Helper()
	p, ok := ShapeParagraph([]rune(s), f, TextDirectionAuto)
	if !ok {
		t.Fatalf("shaping %q failed", s)
	}
	var ids []uint32
	for _, run := range p.Runs {
		for _, g := range run.Glyphs {
			ids = append(ids, uint32(g.GlyphID))
		}
	}
	return ids
}

// JetBrains Mono's contextual alternates draw "!=" as ≠. Default features
// switch that off for every Font, a Font's own Features still win, and the
// change reaches already-cached shapes.
func TestDefaultFontFeaturesTurnOffCodeLigatures(t *testing.T) {
	data, err := os.ReadFile("../../fonts/jetbrainsmono/ttf/JetBrainsMono-Regular.ttf")
	if err != nil {
		t.Skip(err)
	}
	if err := LoadFontVariantFromBytes("JBM Features Test", gfx.FontWeightNormal, false, data); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetDefaultFontFeatures(nil) })
	f := gfx.Font{Family: "JBM Features Test", Size: 14}
	plain := append(glyphIDs(t, "!", f), glyphIDs(t, "=", f)...)

	lig := glyphIDs(t, "!=", f)
	if slices.Equal(lig, plain) {
		t.Fatalf("test font shaped \"!=\" without its ligature (%v); calt not applied?", lig)
	}
	if !SetDefaultFontFeatures([]string{"-calt", "-liga", "-dlig"}) {
		t.Fatal("first set reported no change")
	}
	if SetDefaultFontFeatures([]string{"-calt", "-liga", "-dlig"}) {
		t.Fatal("same set reported a change")
	}
	if got := glyphIDs(t, "!=", f); !slices.Equal(got, plain) {
		t.Fatalf("with defaults off, \"!=\" = %v, want the plain glyphs %v", got, plain)
	}
	on := f
	on.Features = []string{"calt"}
	if got := glyphIDs(t, "!=", on); !slices.Equal(got, lig) {
		t.Fatalf("a Font's own calt should win over the default: %v, want %v", got, lig)
	}
}
