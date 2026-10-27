package render

import (
	"image"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-text/typesetting/di"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/typeset"
)

func loadShapingTestFont(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("shaping test font %s not present; skipping", path)
		}
		t.Fatalf("read shaping test font: %v", err)
	}
	if err := typeset.LoadFontFromBytes(data); err != nil {
		t.Fatalf("load shaping test font: %v", err)
	}
	t.Cleanup(func() { _ = typeset.LoadFontFromBytes(goregular.TTF) })
}

func TestGraphemeBoundariesUnicodeClusters(t *testing.T) {
	text := []rune("a\u0301क्षि👩‍💻🇺🇳")
	want := []int{0, 2, 6, 9, 11}
	if got := typeset.GraphemeBoundaries(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("GraphemeBoundaries() = %v, want %v", got, want)
	}

	for i := 0; i+1 < len(want); i++ {
		if got := typeset.NextClusterBoundary(text, want[i]); got != want[i+1] {
			t.Fatalf("NextClusterBoundary(%d) = %d, want %d", want[i], got, want[i+1])
		}
		if got := typeset.PrevClusterBoundary(text, want[i+1]); got != want[i] {
			t.Fatalf("PrevClusterBoundary(%d) = %d, want %d", want[i+1], got, want[i])
		}
	}
}

func TestLineBreakBoundariesUseUnicodeRules(t *testing.T) {
	breaks := typeset.LineBreakBoundaries([]rune("hello world"))
	wantOffsets := []int{6, 11}
	if len(breaks) != len(wantOffsets) {
		t.Fatalf("line breaks = %+v, want offsets %v", breaks, wantOffsets)
	}
	for i, want := range wantOffsets {
		if breaks[i].Offset != want {
			t.Fatalf("break %d offset = %d, want %d", i, breaks[i].Offset, want)
		}
	}
	if breaks[0].Mandatory || !breaks[1].Mandatory {
		t.Fatalf("line break mandatory flags = %+v", breaks)
	}
}

func TestShapeParagraphProducesClustersAndSpacing(t *testing.T) {
	if err := typeset.LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	plain, ok := typeset.ShapeParagraph([]rune("office"), gfx.Font{Size: 16}, typeset.TextDirectionAuto)
	if !ok || len(plain.Runs) == 0 || plain.Advance <= 0 {
		t.Fatalf("shape plain text failed: ok=%v paragraph=%+v", ok, plain)
	}
	spaced, ok := typeset.ShapeParagraph([]rune("office"), gfx.Font{Size: 16, LetterSpacing: 2}, typeset.TextDirectionAuto)
	if !ok {
		t.Fatal("shape spaced text failed")
	}
	if spaced.Advance <= plain.Advance {
		t.Fatalf("letter spacing did not increase advance: plain=%v spaced=%v", plain.Advance, spaced.Advance)
	}

	for _, run := range plain.Runs {
		for _, glyph := range run.Glyphs {
			if glyph.ClusterIndex < 0 || glyph.ClusterIndex >= len(plain.Text) {
				t.Fatalf("glyph cluster %d outside text length %d", glyph.ClusterIndex, len(plain.Text))
			}
			if glyph.RuneCount <= 0 || glyph.GlyphCount <= 0 {
				t.Fatalf("invalid cluster counts: %+v", glyph)
			}
		}
	}
}

func TestShapeParagraphMixedBidi(t *testing.T) {
	if err := typeset.LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	paragraph, ok := typeset.ShapeParagraph([]rune("abc אבג 123"), gfx.Font{Size: 16}, typeset.TextDirectionAuto)
	if !ok {
		t.Fatal("shape mixed bidi text failed")
	}
	if paragraph.Direction != di.DirectionLTR {
		t.Fatalf("base direction = %v, want LTR", paragraph.Direction)
	}
	hasRTL := false
	for _, run := range paragraph.Runs {
		if run.Direction == di.DirectionRTL {
			hasRTL = true
			break
		}
	}
	if !hasRTL {
		t.Fatalf("mixed paragraph did not produce an RTL run: %+v", paragraph.Runs)
	}
}

func TestShapeArabicJoins(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	text := []rune("سلام")
	paragraph, ok := typeset.ShapeParagraph(text, gfx.Font{Size: 24}, typeset.TextDirectionAuto)
	if !ok || len(paragraph.Runs) != 1 {
		t.Fatalf("shape Arabic failed: ok=%v runs=%d", ok, len(paragraph.Runs))
	}
	run := paragraph.Runs[0]
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
	if err := typeset.LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	stops := typeset.TextCaretStops("a\u0301b", gfx.Font{Size: 18}, typeset.TextDirectionAuto)
	for _, stop := range stops {
		if stop.Offset == 1 {
			t.Fatalf("caret stop landed inside combining grapheme: %+v", stops)
		}
	}
	if got := typeset.TextOffsetAtX("a\u0301b", gfx.Font{Size: 18}, typeset.TextDirectionAuto, stops[1].X); got != 2 {
		t.Fatalf("caret round trip = %d, want 2", got)
	}
}

func TestRTLCaretAndVisualNavigation(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	const text = "سلام"
	font := gfx.Font{Size: 24}
	startX := typeset.TextXForOffset(text, font, typeset.TextDirectionAuto, 0)
	endX := typeset.TextXForOffset(text, font, typeset.TextDirectionAuto, len([]rune(text)))
	if startX <= endX {
		t.Fatalf("RTL logical start should be visually right: start=%v end=%v", startX, endX)
	}
	left := typeset.TextVisualNeighbor(text, font, typeset.TextDirectionAuto, 0, -1)
	if left <= 0 {
		t.Fatalf("left from RTL logical start did not advance logically: %d", left)
	}
}

func TestShapedArabicDrawsOutlinePixels(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	img := image.NewRGBA(image.Rect(0, 0, 180, 60))
	canvas := NewImageCanvas(img)
	canvas.DrawText("سلام", gfx.Rect{X: 8, Y: 8, W: 160, H: 40}, gfx.ColorBlack, gfx.Font{Size: 30})
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

	font := gfx.Font{Size: 20}
	layout := typeset.BuildInlineLayout([]typeset.InlineItem{{Text: "سلام عالم", Font: font, Color: gfx.ColorBlack}},
		typeset.InlineLayoutOptions{BaseFont: font, Direction: typeset.TextDirectionAuto})
	if len(layout.Lines) != 1 || len(layout.Lines[0].Frags) < 2 {
		t.Fatalf("unexpected RTL inline layout: %+v", layout.Lines)
	}
	if strings.TrimSpace(layout.Lines[0].Frags[0].Text) != "عالم" {
		t.Fatalf("leftmost RTL word = %q, want %q", layout.Lines[0].Frags[0].Text, "عالم")
	}
}

func TestRichTextArabicKeepsContextAcrossColorBoundary(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/Amiri-Regular.ttf")

	red := gfx.Color{R: 1, A: 1}
	font := gfx.Font{Size: 24}
	layout := typeset.BuildRichTextLayout([]typeset.TextSpan{
		{Text: "سل", Font: &font, Color: &red},
		{Text: "ام", Font: &font},
	}, font, gfx.ColorBlack, typeset.TextLayoutOptions{Direction: typeset.TextDirectionRTL})
	if len(layout.Lines) != 1 || len(layout.Lines[0].Runs) < 2 {
		t.Fatalf("unexpected styled Arabic layout: %+v", layout.Lines)
	}
	for _, run := range layout.Lines[0].Runs {
		if run.Shaped == nil || len(run.Shaped.Runs) == 0 || len(run.Shaped.Runs[0].Glyphs) == 0 {
			t.Fatalf("styled run lost shaped glyphs: %+v", run)
		}
	}
	joined, ok := typeset.ShapeParagraph([]rune("سلام"), font, typeset.TextDirectionRTL)
	if !ok {
		t.Fatal("shape comparison paragraph failed")
	}
	var styledAdvance float32
	for _, run := range layout.Lines[0].Runs {
		styledAdvance += run.Width
	}
	if diff := typeset.AbsFloat(styledAdvance - joined.Advance); diff > 1.0 {
		t.Fatalf("style boundary changed contextual advance by %v px", diff)
	}
}

func TestDevanagariClusterShapesAndDraws(t *testing.T) {
	loadShapingTestFont(t, "testdata/fonts/NotoSansDevanagari-Regular.ttf")

	const text = "क्षि"
	runes := []rune(text)
	paragraph, ok := typeset.ShapeParagraph(runes, gfx.Font{Size: 30}, typeset.TextDirectionAuto)
	if !ok || len(paragraph.Runs) == 0 {
		t.Fatal("shape Devanagari failed")
	}
	for _, stop := range typeset.TextCaretStops(text, gfx.Font{Size: 30}, typeset.TextDirectionAuto) {
		if stop.Offset != 0 && stop.Offset != len(runes) {
			t.Fatalf("caret landed inside Devanagari grapheme: %+v", typeset.TextCaretStops(text, gfx.Font{Size: 30}, typeset.TextDirectionAuto))
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 160, 70))
	NewImageCanvas(img).DrawText(text, gfx.Rect{X: 8, Y: 8, W: 140, H: 50}, gfx.ColorBlack, gfx.Font{Size: 30})
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
	if err := typeset.LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	const text = "abc אבג 123"
	font := gfx.Font{Size: 18}
	stops := typeset.TextCaretStops(text, font, typeset.TextDirectionLTR)
	if len(stops) < len(typeset.GraphemeBoundaries([]rune(text))) {
		t.Fatalf("mixed bidi caret stops incomplete: %+v", stops)
	}
	for _, stop := range stops {
		got := typeset.TextOffsetAtX(text, font, typeset.TextDirectionLTR, stop.X)
		valid := false
		for _, peer := range stops {
			if typeset.AbsFloat(peer.X-stop.X) < 0.01 && peer.Offset == got {
				valid = true
				break
			}
		}
		if !valid {
			t.Fatalf("caret hit at x=%v returned invalid offset %d; stops=%+v", stop.X, got, stops)
		}
	}
	segments := typeset.TextSelectionSegments(text, font, typeset.TextDirectionLTR, 1, 9)
	if len(segments) < 2 {
		t.Fatalf("mixed bidi selection should split visually, got %+v", segments)
	}
}

func TestCJKLineBreakingUsesUAX14(t *testing.T) {
	font := gfx.Font{Size: 16}
	text := "天地玄黄宇宙洪荒"
	one, _ := typeset.TextMetrics("天地", font)
	layout := typeset.BuildTextLayout(text, font, typeset.TextLayoutOptions{
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
	font := gfx.Font{Size: 18}
	maxWidth := float32(240)
	start := typeset.BuildTextLayout("אבג", font, typeset.TextLayoutOptions{
		MaxWidth:  maxWidth,
		Align:     typeset.TextAlignStart,
		Direction: typeset.TextDirectionRTL,
	})
	end := typeset.BuildTextLayout("אבג", font, typeset.TextLayoutOptions{
		MaxWidth:  maxWidth,
		Align:     typeset.TextAlignEnd,
		Direction: typeset.TextDirectionRTL,
	})
	if len(start.Lines) != 1 || start.Lines[0].Offset <= 0 {
		t.Fatalf("RTL start alignment should offset right: %+v", start.Lines)
	}
	if len(end.Lines) != 1 || end.Lines[0].Offset != 0 {
		t.Fatalf("RTL end alignment should stay left: %+v", end.Lines)
	}
}

func TestShapeParagraphSmallCaps(t *testing.T) {
	if err := typeset.LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	text := []rune("Office")
	base := gfx.Font{Size: 20}
	small := base
	small.SmallCaps = true

	plain, ok := typeset.ShapeParagraph(text, base, typeset.TextDirectionAuto)
	if !ok {
		t.Fatal("shape plain failed")
	}
	caps, ok := typeset.ShapeParagraph(text, small, typeset.TextDirectionAuto)
	if !ok {
		t.Fatal("shape small caps failed")
	}
	upper, ok := typeset.ShapeParagraph([]rune("OFFICE"), base, typeset.TextDirectionAuto)
	if !ok {
		t.Fatal("shape upper failed")
	}

	// Small capitals are capitals drawn small, so the word is narrower than
	// the same word set in full capitals — and, at Docs' 0.7, narrower than
	// the lowercase original too (a capital's advance is wider than a
	// lowercase letter's, but not by enough to survive the scale).
	if caps.Advance >= upper.Advance {
		t.Fatalf("small caps advance %v is not below full caps %v", caps.Advance, upper.Advance)
	}
	if caps.Advance == plain.Advance {
		t.Fatalf("small caps measured identically to the lowercase original (%v)", caps.Advance)
	}
	_ = plain
	// The source text is untouched — caret offsets, selection and copy all
	// index it.
	if string(caps.Text) != "Office" {
		t.Fatalf("small caps rewrote the source text: %q", string(caps.Text))
	}
	if string(caps.ShapeText) != "OFFICE" {
		t.Fatalf("shaped text = %q, want OFFICE", string(caps.ShapeText))
	}
	// The leading 'O' was already a capital, so it keeps the nominal size
	// while the mapped letters shape smaller.
	sizes := map[float32]bool{}
	for _, run := range caps.Runs {
		sizes[typeset.FixedToPx(run.Size)] = true
		for _, glyph := range run.Glyphs {
			if glyph.ClusterIndex < 0 || glyph.ClusterIndex >= len(text) {
				t.Fatalf("glyph cluster %d outside source text", glyph.ClusterIndex)
			}
		}
	}
	if !sizes[20] || !sizes[20*typeset.SmallCapsSizeFactor] {
		t.Fatalf("run sizes = %v, want both 20 and %v", sizes, 20*typeset.SmallCapsSizeFactor)
	}
}

func TestShapeParagraphSmallCapsKeepsRuneMapping(t *testing.T) {
	if err := typeset.LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load goregular: %v", err)
	}
	// ß has no single-rune capital (strings.ToUpper would expand it to SS
	// and slide every later cluster index by one).
	text := []rune("straße")
	caps, ok := typeset.ShapeParagraph(text, gfx.Font{Size: 16, SmallCaps: true}, typeset.TextDirectionAuto)
	if !ok {
		t.Fatal("shape failed")
	}
	if len(caps.ShapeText) != len(text) {
		t.Fatalf("shaped text length %d != source length %d", len(caps.ShapeText), len(text))
	}
	if caps.ShapeText[4] != 'ß' {
		t.Fatalf("ß was case-mapped to %q", caps.ShapeText[4])
	}
}

func TestShapingFallbacksParseLazily(t *testing.T) {
	spec := gfx.Font{Size: 14}
	chain := typeset.FontChainForSpec(spec)
	if len(chain) < 2 {
		t.Skip("no fallback fonts on this system")
	}
	typeset.TextShapingState.Lock()
	defer typeset.TextShapingState.Unlock()

	m := typeset.FacesForSpecLocked(chain, spec)
	if len(m) == 0 || m[0].Face == nil {
		t.Fatal("primary face not loaded up front")
	}
	if got := m.ResolveFace('a'); got != m[0].Face {
		t.Fatal("'a' did not resolve to the primary face")
	}
	for i, e := range m[1:] {
		if e.Face != nil {
			t.Fatalf("fallback %d parsed for a rune the primary covers", i+1)
		}
	}

	// A rune the primary lacks still reaches a fallback that has it.
	for _, r := range []rune{'中', '→', '✓', 'ا', 'ก'} {
		if typeset.SourceHasGlyph(chain[0], r) {
			continue
		}
		for i := 1; i < len(chain); i++ {
			if typeset.FallbackSkipsRune(chain[i], r) || !typeset.SourceHasGlyph(chain[i], r) {
				continue
			}
			face := m.ResolveFace(r)
			if face == m[0].Face {
				t.Fatalf("%q fell back to the primary face", r)
			}
			if _, ok := face.NominalGlyph(r); !ok {
				t.Fatalf("%q resolved to a face without the glyph", r)
			}
			return
		}
	}
	t.Skip("no rune outside the primary with fallback coverage")
}
