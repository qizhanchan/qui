package qui

import "testing"

func TestBuildRichTextLayoutWrapAndAlign(t *testing.T) {
	baseFont := Font{Size: 14}
	baseColor := ColorBlack
	maxW, _ := TextMetrics("Hello world", baseFont)
	layout := BuildRichTextLayout([]TextSpan{
		{Text: "Hello "},
		{Text: "world world"},
	}, baseFont, baseColor, TextLayoutOptions{
		MaxWidth: maxW * 0.6,
		Wrap:     true,
		Align:    TextAlignCenter,
	})
	if len(layout.Lines) < 2 {
		t.Fatalf("expected wrapped rich text lines, got %d", len(layout.Lines))
	}
	if layout.Lines[0].Offset < 0 {
		t.Fatalf("expected non-negative offset, got %v", layout.Lines[0].Offset)
	}
}

func TestBuildRichTextLayoutKeepsSpanStyles(t *testing.T) {
	baseFont := Font{Size: 14}
	baseColor := ColorBlack
	red := Color{R: 1, A: 1}
	boldFont := Font{Size: 16, Bold: true}
	layout := BuildRichTextLayout([]TextSpan{
		{Text: "A"},
		{Text: "B", Color: &red, Font: &boldFont},
	}, baseFont, baseColor, TextLayoutOptions{})
	if len(layout.Lines) != 1 {
		t.Fatalf("expected one line, got %d", len(layout.Lines))
	}
	if len(layout.Lines[0].Runs) < 2 {
		t.Fatalf("expected at least two runs, got %d", len(layout.Lines[0].Runs))
	}
	got := layout.Lines[0].Runs[1]
	if got.Color != red {
		t.Fatalf("expected run color %+v, got %+v", red, got.Color)
	}
	if !fontStyleEqual(got.Font, boldFont) {
		t.Fatalf("expected run font %+v, got %+v", boldFont, got.Font)
	}
}

func TestRichTextLayoutLinkAt(t *testing.T) {
	baseFont := Font{Size: 14}
	spans := []TextSpan{
		{Text: "go to "},
		{Text: "here", Href: "https://example.com"},
		{Text: " now"},
	}
	origin := Rect{X: 10, Y: 20, W: 400, H: 100}
	layout := DrawTextSpans(noopCanvas{}, spans, origin, baseFont, ColorBlack, TextLayoutOptions{})
	if len(layout.Lines) != 1 || len(layout.Lines[0].Runs) < 3 {
		t.Fatalf("expected one line with 3 runs, got %d lines", len(layout.Lines))
	}

	// Walk x across the single line: the run carrying "here" is the only
	// hittable link; the surrounding runs are not.
	line := layout.Lines[0]
	x := origin.X
	widths := make([]float32, len(line.Runs))
	for i, run := range line.Runs {
		widths[i] = run.Width
	}
	// Midpoint of the "here" run (run index 1).
	linkMid := x + widths[0] + widths[1]/2
	if href, ok := layout.LinkAt(origin, Point{X: linkMid, Y: origin.Y + 5}); !ok || href != "https://example.com" {
		t.Fatalf("LinkAt over link = (%q, %v), want (%q, true)", href, ok, "https://example.com")
	}
	// Midpoint of the leading "go to " run — no link.
	plainMid := x + widths[0]/2
	if href, ok := layout.LinkAt(origin, Point{X: plainMid, Y: origin.Y + 5}); ok {
		t.Fatalf("LinkAt over plain text = (%q, true), want no link", href)
	}
	// A point far below the text — no link.
	if _, ok := layout.LinkAt(origin, Point{X: linkMid, Y: origin.Y + 500}); ok {
		t.Fatalf("LinkAt below text should not hit a link")
	}
}

func TestParagraphStyleToLayoutOptions(t *testing.T) {
	p := ParagraphStyle{
		Align:           TextAlignEnd,
		Wrap:            true,
		MaxLines:        2,
		Ellipsis:        true,
		LineHeightScale: 1.3,
	}
	opts := p.LayoutOptions(123)
	if opts.MaxWidth != 123 || opts.Align != TextAlignEnd || !opts.Wrap || !opts.Ellipsis || opts.MaxLines != 2 || opts.LineHeightScale != 1.3 {
		t.Fatalf("unexpected options conversion: %+v", opts)
	}
}

// TestLineHeightIsCSSSemantics pins the unit at the framework boundary: a
// LineHeightScale is a CSS `line-height`, a multiple of the FONT SIZE.
//
// It used to multiply the FACE's natural line height (≈1.16 em for the
// common text faces), which every caller then compounded: htmlcss feeds it
// the computed `line-height` property verbatim, so `line-height: 1.5`
// rendered as 1.74; q-word feeds it Docs' 1.38 and got 1.60.
func TestLineHeightIsCSSSemantics(t *testing.T) {
	font := Font{Size: 20}
	for _, scale := range []float32{1, 1.38, 1.5, 2} {
		if got, want := scaledLineHeight(font, scale), font.Size*scale; got != want {
			t.Errorf("scale %v → %v px, want %v (× font size)", scale, got, want)
		}
	}
	// <= 0 is CSS `line-height: normal`: the face's own height, which for a
	// real text face is taller than the em.
	normal := scaledLineHeight(font, 0)
	if normal <= font.Size {
		t.Errorf("normal line height %v should exceed the em (%v)", normal, font.Size)
	}
	if scaledLineHeight(font, -1) != normal {
		t.Error("a negative scale must resolve to normal too")
	}
}

// A layout's height must follow the same rule end to end, not just the
// helper — the plain-text and rich-text builders each compute it.
func TestLayoutHeightsFollowCSSLineHeight(t *testing.T) {
	font := Font{Size: 20}
	const scale = 1.5
	plain := BuildTextLayout("one\ntwo\nthree", font, TextLayoutOptions{LineHeightScale: scale})
	if got, want := plain.Height, font.Size*scale*3; got != want {
		t.Errorf("plain 3-line height = %v, want %v", got, want)
	}
	rich := BuildRichTextLayout([]TextSpan{{Text: "one\ntwo\nthree"}}, font, Color{},
		TextLayoutOptions{LineHeightScale: scale})
	if got, want := rich.Height, font.Size*scale*3; got != want {
		t.Errorf("rich 3-line height = %v, want %v", got, want)
	}
}

// An unset Font.Size means 14 px (GetFontFacesFor's default) — including in
// the font-size-relative line-height branch. That is also what makes the
// memo safe: BuildTextLayout's cache key normalizes Size 0 to 14, so the two
// spellings share one entry and MUST produce one result. lineBox once read
// the raw Size, ignored the scale (0 × scale = 0), and the Size-0 result
// poisoned the shared entry for later Size-14 callers.
func TestLineHeightAppliesDefaultFontSize(t *testing.T) {
	opts := TextLayoutOptions{LineHeightScale: 2}
	const text = "size-zero-cache-key-probe"
	zero := BuildTextLayout(text, Font{}, opts)             // computes + caches
	fourteen := BuildTextLayout(text, Font{Size: 14}, opts) // same key: cache hit
	if want := float32(14 * 2); zero.LineHeight != want || fourteen.LineHeight != want {
		t.Errorf("LineHeight = %v (Size 0) / %v (Size 14), want %v for both",
			zero.LineHeight, fourteen.LineHeight, want)
	}
}

func TestRichTextSmallCapsKeepsSourceText(t *testing.T) {
	baseFont := Font{Size: 20}
	capsFont := baseFont
	capsFont.SmallCaps = true

	upper := BuildRichTextLayout([]TextSpan{{Text: "OFFICE"}}, baseFont, ColorBlack, TextLayoutOptions{})
	caps := BuildRichTextLayout([]TextSpan{{Text: "office", Font: &capsFont}}, baseFont, ColorBlack, TextLayoutOptions{})
	if len(upper.Lines) != 1 || len(caps.Lines) != 1 {
		t.Fatalf("expected one line each, got %d / %d", len(upper.Lines), len(caps.Lines))
	}
	// Capitals drawn small: the same letters as OFFICE, narrower.
	if caps.Lines[0].Width >= upper.Lines[0].Width {
		t.Fatalf("small caps width %v is not below full caps %v",
			caps.Lines[0].Width, upper.Lines[0].Width)
	}
	var text string
	for _, run := range caps.Lines[0].Runs {
		text += run.Text
	}
	if text != "office" {
		t.Fatalf("small caps run text = %q, want the source's own case", text)
	}
}
