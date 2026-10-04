package icons_test

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/icons"
)

// TestRegistryNonEmpty asserts the //go:embed bundled at least the
// icons the showcase + golden test reference. A future fetch.sh that
// silently drops one of these would surface as a panic at package
// load (mustLoad) — this test just gives a friendlier failure.
func TestRegistryNonEmpty(t *testing.T) {
	must := []string{"settings", "search", "done", "edit", "lock", "arrow_back", "more_vert"}
	for _, n := range must {
		if icons.Get(n) == nil {
			t.Errorf("icon %q missing from registry", n)
		}
	}
	if len(icons.Names()) < len(must) {
		t.Fatalf("only %d icons registered, want at least %d", len(icons.Names()), len(must))
	}
}

// TestRasterize forces a rasterization through the qui.VectorSource
// interface so a regression in the negative-Y viewBox math (the icon set's
// Symbols use "0 -960 960 960") would fail loudly.
func TestRasterize(t *testing.T) {
	const px = 24
	img := icons.Settings.Rasterize(px, px, qui.Color{})
	if img == nil {
		t.Fatal("Rasterize returned nil")
	}
	b := img.(*image.RGBA).Bounds()
	if b.Dx() != px || b.Dy() != px {
		t.Errorf("rasterize bounds = %v, want %dx%d", b, px, px)
	}
	// Find at least one non-transparent pixel — confirms the viewBox
	// translate landed glyph ink inside the target rect, not above it.
	var inked bool
	for y := b.Min.Y; y < b.Max.Y && !inked; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.(*image.RGBA).RGBAAt(x, y).A > 0 {
				inked = true
				break
			}
		}
	}
	if !inked {
		t.Error("rasterized icon is entirely transparent — viewBox math is off")
	}
}
