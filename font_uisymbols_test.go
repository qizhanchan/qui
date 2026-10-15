package qui

import "testing"

// TestUIKeyboardSymbolsMatchTextSize: ⌘ ⇧ ⌥ in a shortcut resolve to the
// platform UI face (drawn at cap height), not the small-em general symbol
// font; other runes never resolve to the UI-symbol face.
func TestUIKeyboardSymbolsMatchTextSize(t *testing.T) {
	faces := GetFontFacesFor(Font{Size: 100})
	if uiSymbolFallback.Load() == nil {
		t.Skip("no platform UI-symbol face on this system")
	}
	hb, _, ok := faceForRune('H', faces).GlyphBounds('H')
	if !ok {
		t.Fatal("primary has no H")
	}
	capH := (hb.Max.Y - hb.Min.Y).Ceil()
	for _, r := range "⌘⇧⌥" {
		f := faceForRune(r, faces)
		if _, isUI := f.(uiSymbolFace); !isUI {
			t.Errorf("%c resolved to %T, want the UI-symbol face", r, f)
		}
		b, _, ok := f.GlyphBounds(r)
		if !ok {
			t.Fatalf("%c: no glyph", r)
		}
		h := (b.Max.Y - b.Min.Y).Ceil()
		if float64(h) < 0.85*float64(capH) {
			t.Errorf("%c height %d is visibly smaller than cap height %d", r, h, capH)
		}
	}
	for _, r := range "A‹⟳✓" {
		if _, isUI := faceForRune(r, faces).(uiSymbolFace); isUI {
			t.Errorf("%c must not resolve to the UI-symbol face", r)
		}
	}
	// The PDF chain applies the same restriction.
	chain := fontChainForSpec(Font{Size: 100})
	if f, _, _ := resolveRune(chain, '⌘', nil); f != uiSymbolFallback.Load() {
		t.Errorf("pdf chain: ⌘ not on the UI-symbol face")
	}
	if f, _, _ := resolveRune(chain, 'A', nil); f == uiSymbolFallback.Load() {
		t.Errorf("pdf chain: A on the UI-symbol face")
	}
}

func TestUIKeyboardSymbolsShapingChain(t *testing.T) {
	GetFontFacesFor(Font{Size: 14})
	ui := uiSymbolFallback.Load()
	if ui == nil {
		t.Skip("no platform UI-symbol face on this system")
	}
	p, ok := shapeParagraph([]rune("⌘B ‹"), Font{Size: 14}, TextDirectionAuto)
	if !ok {
		t.Fatal("shape failed")
	}
	for _, run := range p.runs {
		src := sourceFontForShapingFace(run.Face)
		for _, g := range run.Glyphs {
			r := p.text[g.ClusterIndex]
			if (src == ui) != isUIKeyboardSymbol(r) {
				t.Errorf("rune %c shaped with UI face=%v", r, src == ui)
			}
		}
	}
}

// inkSpan returns the first and last pixel row (and the first column) with
// ink when text is drawn at the top-left of a 120x60 canvas.
func inkSpan(t *testing.T, text string, f Font) (top, bottom, left int) {
	t.Helper()
	cv, img := NewOffscreenCanvas(120, 60)
	cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	cv.DrawText(text, Rect{X: 10, Y: 10, W: 100, H: 40}, Color{A: 1}, f)
	top, bottom, left = -1, -1, -1
	for y := 0; y < 60; y++ {
		for x := 0; x < 120; x++ {
			if img.RGBAAt(x, y).R < 160 {
				if top < 0 {
					top = y
				}
				bottom = y
				if left < 0 || x < left {
					left = x
				}
			}
		}
	}
	return
}

// A shortcut's modifier glyphs share the label's baseline and stand at
// about cap height, whichever face they came from, so "⌘Z" reads as one
// word. (⌃ is a small caret at the top of the line, so only its bottom edge
// is not compared.)
func TestUIKeyboardSymbolsShareBaselineAndSize(t *testing.T) {
	GetFontFacesFor(Font{Size: 28})
	if uiSymbolFallback.Load() == nil {
		t.Skip("no platform UI-symbol face on this system")
	}
	f := Font{Size: 28}
	zTop, zBot, _ := inkSpan(t, "Z", f)
	capH := zBot - zTop + 1
	for _, g := range []string{"⌘", "⇧", "⌥"} {
		top, bot, _ := inkSpan(t, g, f)
		if bot < 0 {
			t.Fatalf("%s drew nothing", g)
		}
		if d := bot - zBot; d < -2 || d > 2 {
			t.Errorf("%s bottom edge is %d px from the baseline (Z bottom %d, glyph bottom %d)", g, d, zBot, bot)
		}
		if h := bot - top + 1; h < capH*8/10 || h > capH*12/10 {
			t.Errorf("%s is %d px tall, cap height is %d", g, h, capH)
		}
	}
}
