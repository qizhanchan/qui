package qui

import (
	"image"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-text/typesetting/di"
	"golang.org/x/image/font/gofont/goregular"
)

func loadShapingTestFont(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shaping test font: %v", err)
	}
	if err := LoadFontFromBytes(data); err != nil {
		t.Fatalf("load shaping test font: %v", err)
	}
	t.Cleanup(func() { _ = LoadFontFromBytes(goregular.TTF) })
}

func TestGraphemeBoundariesUnicodeClusters(t *testing.T) {
	text := []rune("a\u0301क्षि👩‍💻🇺🇳")
	want := []int{0, 2, 6, 9, 11}
	if got := GraphemeBoundaries(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("GraphemeBoundaries() = %v, want %v", got, want)
	}

	for i := 0; i+1 < len(want); i++ {
		if got := NextClusterBoundary(text, want[i]); got != want[i+1] {
			t.Fatalf("NextClusterBoundary(%d) = %d, want %d", want[i], got, want[i+1])
		}
		if got := PrevClusterBoundary(text, want[i+1]); got != want[i] {
			t.Fatalf("PrevClusterBoundary(%d) = %d, want %d", want[i+1], got, want[i])
		}
	}
}

func TestLineBreakBoundariesUseUnicodeRules(t *testing.T) {
	breaks := lineBreakBoundaries([]rune("hello world"))
	wantOffsets := []int{6, 11}
	if len(breaks) != len(wantOffsets) {
		t.Fatalf("line breaks = %+v, want offsets %v", breaks, wantOffsets)
	}
	for i, want := range wantOffsets {
		if breaks[i].offset != want {
			t.Fatalf("break %d offset = %d, want %d", i, breaks[i].offset, want)
		}
	}
	if breaks[0].mandatory || !breaks[1].mandatory {
		t.Fatalf("line break mandatory flags = %+v", breaks)
	}
}

func TestShapeParagraphProducesClustersAndSpacing(t *testing.T) {
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	plain, ok := shapeParagraph([]rune("office"), Font{Size: 16}, TextDirectionAuto)
	if !ok || len(plain.runs) == 0 || plain.advance <= 0 {
		t.Fatalf("shape plain text failed: ok=%v paragraph=%+v", ok, plain)
	}
	spaced, ok := shapeParagraph([]rune("office"), Font{Size: 16, LetterSpacing: 2}, TextDirectionAuto)
	if !ok {
		t.Fatal("shape spaced text failed")
	}
	if spaced.advance <= plain.advance {
		t.Fatalf("letter spacing did not increase advance: plain=%v spaced=%v", plain.advance, spaced.advance)
	}

	for _, run := range plain.runs {
		for _, glyph := range run.Glyphs {
			if glyph.ClusterIndex < 0 || glyph.ClusterIndex >= len(plain.text) {
				t.Fatalf("glyph cluster %d outside text length %d", glyph.ClusterIndex, len(plain.text))
			}
			if glyph.RuneCount <= 0 || glyph.GlyphCount <= 0 {
				t.Fatalf("invalid cluster counts: %+v", glyph)
			}
		}
	}
}

func TestShapeParagraphMixedBidi(t *testing.T) {
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	paragraph, ok := shapeParagraph([]rune("abc אבג 123"), Font{Size: 16}, TextDirectionAuto)
	if !ok {
		t.Fatal("shape mixed bidi text failed")
	}
	if paragraph.direction != di.DirectionLTR {
		t.Fatalf("base direction = %v, want LTR", paragraph.direction)
	}
	hasRTL := false
	for _, run := range paragraph.runs {
		if run.Direction == di.DirectionRTL {
			hasRTL = true
			break
		}
	}
	if !hasRTL {
		t.Fatalf("mixed paragraph did not produce an RTL run: %+v", paragraph.runs)
	}
}

func TestShapeArabicJoins(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	text := []rune("سلام")
	paragraph, ok := shapeParagraph(text, Font{Size: 24}, TextDirectionAuto)
	if !ok || len(paragraph.runs) != 1 {
		t.Fatalf("shape Arabic failed: ok=%v runs=%d", ok, len(paragraph.runs))
	}
	run := paragraph.runs[0]
	if run.Direction != di.DirectionRTL {
		t.Fatalf("Arabic direction = %v, want RTL", run.Direction)
	}
	contextual := false
	for _, glyph := range run.Glyphs {
		nominal, ok := run.Face.NominalGlyph(text[glyph.ClusterIndex])
		if ok && nominal != glyph.GlyphID {
			contextual = true
			break
		}
	}
	if !contextual {
		t.Fatalf("Arabic shaping did not apply contextual substitutions: %+v", run.Glyphs)
	}
}

func TestTextCaretStopsRespectGraphemeClusters(t *testing.T) {
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	stops := TextCaretStops("a\u0301b", Font{Size: 18}, TextDirectionAuto)
	for _, stop := range stops {
		if stop.Offset == 1 {
			t.Fatalf("caret stop landed inside combining grapheme: %+v", stops)
		}
	}
	if got := TextOffsetAtX("a\u0301b", Font{Size: 18}, TextDirectionAuto, stops[1].X); got != 2 {
		t.Fatalf("caret round trip = %d, want 2", got)
	}
}

func TestRTLCaretAndVisualNavigation(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	const text = "سلام"
	font := Font{Size: 24}
	startX := TextXForOffset(text, font, TextDirectionAuto, 0)
	endX := TextXForOffset(text, font, TextDirectionAuto, len([]rune(text)))
	if startX <= endX {
		t.Fatalf("RTL logical start should be visually right: start=%v end=%v", startX, endX)
	}
	left := TextVisualNeighbor(text, font, TextDirectionAuto, 0, -1)
	if left <= 0 {
		t.Fatalf("left from RTL logical start did not advance logically: %d", left)
	}
}

func TestShapedArabicDrawsOutlinePixels(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	img := image.NewRGBA(image.Rect(0, 0, 180, 60))
	canvas := NewImageCanvas(img)
	canvas.DrawText("سلام", Rect{X: 8, Y: 8, W: 160, H: 40}, ColorBlack, Font{Size: 30})
	nonZero := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			nonZero++
		}
	}
	if nonZero < 20 {
		t.Fatalf("shaped Arabic produced only %d non-transparent pixels", nonZero)
	}
}

func TestInlineRTLWordsUseVisualOrder(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	font := Font{Size: 20}
	layout := BuildInlineLayout([]InlineItem{{Text: "سلام عالم", Font: font, Color: ColorBlack}},
		InlineLayoutOptions{BaseFont: font, Direction: TextDirectionAuto})
	if len(layout.Lines) != 1 || len(layout.Lines[0].Frags) < 2 {
		t.Fatalf("unexpected RTL inline layout: %+v", layout.Lines)
	}
	if strings.TrimSpace(layout.Lines[0].Frags[0].Text) != "عالم" {
		t.Fatalf("leftmost RTL word = %q, want %q", layout.Lines[0].Frags[0].Text, "عالم")
	}
}

func TestRichTextArabicKeepsContextAcrossColorBoundary(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	red := Color{R: 1, A: 1}
	font := Font{Size: 24}
	layout := BuildRichTextLayout([]TextSpan{
		{Text: "سل", Font: &font, Color: &red},
		{Text: "ام", Font: &font},
	}, font, ColorBlack, TextLayoutOptions{Direction: TextDirectionRTL})
	if len(layout.Lines) != 1 || len(layout.Lines[0].Runs) < 2 {
		t.Fatalf("unexpected styled Arabic layout: %+v", layout.Lines)
	}
	for _, run := range layout.Lines[0].Runs {
		if run.shaped == nil || len(run.shaped.runs) == 0 || len(run.shaped.runs[0].Glyphs) == 0 {
			t.Fatalf("styled run lost shaped glyphs: %+v", run)
		}
	}
	joined, ok := shapeParagraph([]rune("سلام"), font, TextDirectionRTL)
	if !ok {
		t.Fatal("shape comparison paragraph failed")
	}
	var styledAdvance float32
	for _, run := range layout.Lines[0].Runs {
		styledAdvance += run.Width
	}
	if diff := absFloat(styledAdvance - joined.advance); diff > 1.0 {
		t.Fatalf("style boundary changed contextual advance by %v px", diff)
	}
}

func TestDevanagariClusterShapesAndDraws(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/NotoSansDevanagari-Regular.ttf")

	const text = "क्षि"
	runes := []rune(text)
	paragraph, ok := shapeParagraph(runes, Font{Size: 30}, TextDirectionAuto)
	if !ok || len(paragraph.runs) == 0 {
		t.Fatal("shape Devanagari failed")
	}
	for _, stop := range TextCaretStops(text, Font{Size: 30}, TextDirectionAuto) {
		if stop.Offset != 0 && stop.Offset != len(runes) {
			t.Fatalf("caret landed inside Devanagari grapheme: %+v", TextCaretStops(text, Font{Size: 30}, TextDirectionAuto))
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 160, 70))
	NewImageCanvas(img).DrawText(text, Rect{X: 8, Y: 8, W: 140, H: 50}, ColorBlack, Font{Size: 30})
	ink := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			ink++
		}
	}
	if ink < 20 {
		t.Fatalf("Devanagari outline produced only %d pixels", ink)
	}
}

func TestMixedBidiCaretRoundTrip(t *testing.T) {
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	const text = "abc אבג 123"
	font := Font{Size: 18}
	stops := TextCaretStops(text, font, TextDirectionLTR)
	if len(stops) < len(GraphemeBoundaries([]rune(text))) {
		t.Fatalf("mixed bidi caret stops incomplete: %+v", stops)
	}
	for _, stop := range stops {
		got := TextOffsetAtX(text, font, TextDirectionLTR, stop.X)
		valid := false
		for _, peer := range stops {
			if absFloat(peer.X-stop.X) < 0.01 && peer.Offset == got {
				valid = true
				break
			}
		}
		if !valid {
			t.Fatalf("caret hit at x=%v returned invalid offset %d; stops=%+v", stop.X, got, stops)
		}
	}
	segments := TextSelectionSegments(text, font, TextDirectionLTR, 1, 9)
	if len(segments) < 2 {
		t.Fatalf("mixed bidi selection should split visually, got %+v", segments)
	}
}

func TestCJKLineBreakingUsesUAX14(t *testing.T) {
	font := Font{Size: 16}
	text := "天地玄黄宇宙洪荒"
	one, _ := TextMetrics("天地", font)
	layout := BuildTextLayout(text, font, TextLayoutOptions{
		MaxWidth:       one + 0.5,
		Wrap:           true,
		BreakLongWords: false,
	})
	if len(layout.Lines) < 3 {
		t.Fatalf("CJK text did not wrap at UAX#14 opportunities: %d lines", len(layout.Lines))
	}
	var joined strings.Builder
	for _, line := range layout.Lines {
		joined.WriteString(line.Text)
	}
	if joined.String() != text {
		t.Fatalf("CJK wrap changed text: %q", joined.String())
	}
}

func TestRTLStartAndEndAlignment(t *testing.T) {
	font := Font{Size: 18}
	maxWidth := float32(240)
	start := BuildTextLayout("אבג", font, TextLayoutOptions{
		MaxWidth:  maxWidth,
		Align:     TextAlignStart,
		Direction: TextDirectionRTL,
	})
	end := BuildTextLayout("אבג", font, TextLayoutOptions{
		MaxWidth:  maxWidth,
		Align:     TextAlignEnd,
		Direction: TextDirectionRTL,
	})
	if len(start.Lines) != 1 || start.Lines[0].Offset <= 0 {
		t.Fatalf("RTL start alignment should offset right: %+v", start.Lines)
	}
	if len(end.Lines) != 1 || end.Lines[0].Offset != 0 {
		t.Fatalf("RTL end alignment should stay left: %+v", end.Lines)
	}
}

func TestShapeParagraphSmallCaps(t *testing.T) {
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	text := []rune("Office")
	base := Font{Size: 20}
	small := base
	small.SmallCaps = true

	plain, ok := shapeParagraph(text, base, TextDirectionAuto)
	if !ok {
		t.Fatal("shape plain failed")
	}
	caps, ok := shapeParagraph(text, small, TextDirectionAuto)
	if !ok {
		t.Fatal("shape small caps failed")
	}
	upper, ok := shapeParagraph([]rune("OFFICE"), base, TextDirectionAuto)
	if !ok {
		t.Fatal("shape upper failed")
	}

	// Small capitals are capitals drawn small, so the word is narrower than
	// the same word set in full capitals — and, at Docs' 0.7, narrower than
	// the lowercase original too (a capital's advance is wider than a
	// lowercase letter's, but not by enough to survive the scale).
	if caps.advance >= upper.advance {
		t.Fatalf("small caps advance %v is not below full caps %v", caps.advance, upper.advance)
	}
	if caps.advance == plain.advance {
		t.Fatalf("small caps measured identically to the lowercase original (%v)", caps.advance)
	}
	_ = plain
	// The source text is untouched — caret offsets, selection and copy all
	// index it.
	if string(caps.text) != "Office" {
		t.Fatalf("small caps rewrote the source text: %q", string(caps.text))
	}
	if string(caps.shapeText) != "OFFICE" {
		t.Fatalf("shaped text = %q, want OFFICE", string(caps.shapeText))
	}
	// The leading 'O' was already a capital, so it keeps the nominal size
	// while the mapped letters shape smaller.
	sizes := map[float32]bool{}
	for _, run := range caps.runs {
		sizes[fixedToPx(run.Size)] = true
		for _, glyph := range run.Glyphs {
			if glyph.ClusterIndex < 0 || glyph.ClusterIndex >= len(text) {
				t.Fatalf("glyph cluster %d outside source text", glyph.ClusterIndex)
			}
		}
	}
	if !sizes[20] || !sizes[20*smallCapsSizeFactor] {
		t.Fatalf("run sizes = %v, want both 20 and %v", sizes, 20*smallCapsSizeFactor)
	}
}

func TestShapeParagraphSmallCapsKeepsRuneMapping(t *testing.T) {
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	// ß has no single-rune capital (strings.ToUpper would expand it to SS
	// and slide every later cluster index by one).
	text := []rune("straße")
	caps, ok := shapeParagraph(text, Font{Size: 16, SmallCaps: true}, TextDirectionAuto)
	if !ok {
		t.Fatal("shape failed")
	}
	if len(caps.shapeText) != len(text) {
		t.Fatalf("shaped text length %d != source length %d", len(caps.shapeText), len(text))
	}
	if caps.shapeText[4] != 'ß' {
		t.Fatalf("ß was case-mapped to %q", caps.shapeText[4])
	}
}
