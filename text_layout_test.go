package qui

import (
	"image"
	"strings"
	"testing"
)

func TestBuildTextLayoutWrap(t *testing.T) {
	fontSpec := Font{Size: 14}
	wordW, _ := TextMetrics("hello", fontSpec)
	layout := BuildTextLayout("hello world", fontSpec, TextLayoutOptions{
		MaxWidth: wordW + 1,
		Wrap:     true,
	})
	if len(layout.Lines) < 2 {
		t.Fatalf("expected wrapped lines, got %d", len(layout.Lines))
	}
}

func TestBuildTextLayoutEllipsis(t *testing.T) {
	fontSpec := Font{Size: 14}
	wordW, _ := TextMetrics("hello", fontSpec)
	layout := BuildTextLayout("hello world again", fontSpec, TextLayoutOptions{
		MaxWidth: wordW + 1,
		Wrap:     true,
		MaxLines: 1,
		Ellipsis: true,
	})
	if !layout.Truncated {
		t.Fatalf("expected truncated layout")
	}
	if len(layout.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(layout.Lines))
	}
	if !strings.HasSuffix(layout.Lines[0].Text, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", layout.Lines[0].Text)
	}
}

func TestBuildTextLayoutAlign(t *testing.T) {
	fontSpec := Font{Size: 14}
	maxW, _ := TextMetrics("much longer", fontSpec)
	layout := BuildTextLayout("short\nmuch longer", fontSpec, TextLayoutOptions{
		MaxWidth: maxW,
		Align:    TextAlignEnd,
	})
	if len(layout.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(layout.Lines))
	}
	if layout.Lines[0].Offset <= 0 {
		t.Fatalf("expected first line offset > 0, got %v", layout.Lines[0].Offset)
	}
	if layout.Lines[1].Offset != 0 {
		t.Fatalf("expected second line offset = 0, got %v", layout.Lines[1].Offset)
	}
}

// text-align: justify stretches soft-wrapped lines to the layout width via
// per-line JustifyExtra (extra advance per space); paragraph-final lines and
// lines before a forced '\n' stay natural.
func TestBuildTextLayoutJustify(t *testing.T) {
	fontSpec := Font{Size: 14}
	twoWords, _ := TextMetrics("aaaa bbbb", fontSpec)
	maxW := twoWords + 12 // fits two words + slack, not three
	layout := BuildTextLayout("aaaa bbbb cccc dddd eeee", fontSpec, TextLayoutOptions{
		MaxWidth: maxW,
		Wrap:     true,
		Align:    TextAlignJustify,
	})
	if len(layout.Lines) < 2 {
		t.Fatalf("expected wrapped lines, got %d", len(layout.Lines))
	}
	first := layout.Lines[0]
	if first.JustifyExtra <= 0 {
		t.Errorf("first (soft-wrapped) line JustifyExtra = %v, want > 0", first.JustifyExtra)
	}
	if first.Width != maxW {
		t.Errorf("justified line Width = %v, want stretched to MaxWidth %v", first.Width, maxW)
	}
	if first.Offset != 0 {
		t.Errorf("justified line Offset = %v, want 0", first.Offset)
	}
	last := layout.Lines[len(layout.Lines)-1]
	if last.JustifyExtra != 0 {
		t.Errorf("last line JustifyExtra = %v, want 0 (paragraph-final not stretched)", last.JustifyExtra)
	}

	// A line ended by '\n' is paragraph-final: no stretch even mid-text.
	hard := BuildTextLayout("aaaa bbbb\ncccc dddd eeee ffff", fontSpec, TextLayoutOptions{
		MaxWidth: maxW,
		Wrap:     true,
		Align:    TextAlignJustify,
	})
	if hard.Lines[0].JustifyExtra != 0 {
		t.Errorf("line before forced break JustifyExtra = %v, want 0", hard.Lines[0].JustifyExtra)
	}
}

func TestBuildTextLayoutLineHeightScale(t *testing.T) {
	fontSpec := Font{Size: 14}
	base := BuildTextLayout("a\nb", fontSpec, TextLayoutOptions{})
	scaled := BuildTextLayout("a\nb", fontSpec, TextLayoutOptions{LineHeightScale: 1.5})
	if want := fontSpec.Size * 1.5; scaled.LineHeight != want {
		t.Fatalf("scaled line height = %v, want %v", scaled.LineHeight, want)
	}
	if want := scaled.LineHeight * 2; scaled.Height != want {
		t.Fatalf("scaled layout height = %v, want %v (base=%v)", scaled.Height, want, base.Height)
	}
}

func TestDrawTextLayoutUsesOffset(t *testing.T) {
	rc := &recordCanvas{}
	layout := TextLayout{
		Lines: []TextLayoutLine{
			{Text: "a", Width: 10, Offset: 5},
			{Text: "b", Width: 10, Offset: 0},
		},
		LineHeight: 20,
	}
	DrawTextLayout(rc, layout, Rect{X: 100, Y: 50}, ColorBlack, Font{Size: 14})
	if len(rc.textRects) != 2 {
		t.Fatalf("expected 2 draw calls, got %d", len(rc.textRects))
	}
	if rc.textRects[0].X != 105 || rc.textRects[0].Y != 50 {
		t.Fatalf("unexpected first text rect: %+v", rc.textRects[0])
	}
	if rc.textRects[1].X != 100 || rc.textRects[1].Y != 70 {
		t.Fatalf("unexpected second text rect: %+v", rc.textRects[1])
	}
}

func TestTextMetricsItalicIncludesVisualOverhang(t *testing.T) {
	normalW, _ := TextMetrics("123", Font{Size: 20})
	italicW, _ := TextMetrics("123", Font{Size: 20, Italic: true})
	if italicW <= normalW {
		t.Fatalf("italic metrics should include overhang, normal=%v italic=%v", normalW, italicW)
	}
}

func TestBuildTextLayoutWrapRespectsMaxWidthWithItalic(t *testing.T) {
	fontSpec := Font{Size: 20, Italic: true}
	maxW, _ := TextMetrics("123", fontSpec)
	layout := BuildTextLayout("123 123 123", fontSpec, TextLayoutOptions{
		MaxWidth: maxW,
		Wrap:     true,
	})
	if len(layout.Lines) < 2 {
		t.Fatalf("expected wrapped lines for italic text, got %d", len(layout.Lines))
	}
	for i, line := range layout.Lines {
		if line.Width > maxW+0.01 {
			t.Fatalf("line %d width %v exceeds max %v", i, line.Width, maxW)
		}
	}
}

type recordCanvas struct {
	textRects []Rect
}

func (c *recordCanvas) Clear(Color)                                     {}
func (c *recordCanvas) FillRect(Rect, Color)                            {}
func (c *recordCanvas) FillRoundedRect(Rect, float32, Color)            {}
func (c *recordCanvas) StrokeRect(Rect, Color, float32)                 {}
func (c *recordCanvas) StrokeRoundedRect(Rect, float32, Color, float32) {}
func (c *recordCanvas) DrawImage(image.Image, Rect)                     {}
func (c *recordCanvas) DrawLine(Point, Point, Color, float32)           {}
func (c *recordCanvas) DrawPolyline([]Point, Color, float32)            {}
func (c *recordCanvas) DrawText(_ string, rect Rect, _ Color, _ Font) {
	c.textRects = append(c.textRects, rect)
}
func (c *recordCanvas) DrawShadow(Rect, float32, ElevationSpec, Color) {}
func (c *recordCanvas) DrawVector(VectorSource, Rect, Color)           {}
func (c *recordCanvas) DrawShape(Shape, Paint)                         {}
func (c *recordCanvas) Save() int                                      { return 0 }
func (c *recordCanvas) SaveLayer(Rect, Paint) int                      { return 0 }
func (c *recordCanvas) ClipPath(*Path)                                 {}
func (c *recordCanvas) Restore()                                       {}
func (c *recordCanvas) RestoreTo(int)                                  {}
func (c *recordCanvas) ClipRect(Rect)                                  {}
func (c *recordCanvas) Scale(float32, float32)                         {}
func (c *recordCanvas) Translate(float32, float32)                     {}
func (c *recordCanvas) Rotate(float32)                                 {}
func (c *recordCanvas) Concat(Matrix)                                  {}
func (c *recordCanvas) CurrentMatrix() Matrix                          { return IdentityMatrix() }
func (c *recordCanvas) ClipBounds() Rect                               { return Rect{X: -1e9, Y: -1e9, W: 2e9, H: 2e9} }
func TestRuneAdvanceLetterSpacing(t *testing.T) {
	base := Font{Size: 16}
	spaced := base
	spaced.LetterSpacing = 4
	b := RuneAdvance('m', base)
	s := RuneAdvance('m', spaced)
	if d := s - b; d < 3.5 || d > 4.5 {
		t.Errorf("letter-spacing added %v to advance, want ~4", d)
	}
	// word-spacing adds only to the space rune, not glyphs.
	ws := base
	ws.WordSpacing = 6
	if d := RuneAdvance(' ', ws) - RuneAdvance(' ', base); d < 5.5 || d > 6.5 {
		t.Errorf("word-spacing added %v to space advance, want ~6", d)
	}
	if RuneAdvance('m', ws) != RuneAdvance('m', base) {
		t.Error("word-spacing must not affect non-space glyphs")
	}
}

func TestTextMetricsLetterSpacingWidensRun(t *testing.T) {
	base := Font{Size: 16}
	spaced := base
	spaced.LetterSpacing = 3
	wb, _ := TextMetrics("abcde", base)
	ws, _ := TextMetrics("abcde", spaced)
	// 5 glyphs → ~5×3 = 15px wider (letter-spacing after each).
	if d := ws - wb; d < 13 || d > 17 {
		t.Errorf("letter-spacing widened 5-glyph run by %v, want ~15", d)
	}
}

func TestBuildTextLayoutFirstIndent(t *testing.T) {
	fontSpec := Font{Size: 14}
	const indent = 24
	layout := BuildTextLayout("indented paragraph text", fontSpec, TextLayoutOptions{
		MaxWidth:    400,
		Wrap:        true,
		FirstIndent: indent,
	})
	if len(layout.Lines) == 0 {
		t.Fatal("no lines")
	}
	if o := layout.Lines[0].Offset; o < indent-0.5 || o > indent+0.5 {
		t.Errorf("first line Offset = %v, want ~%v (text-indent)", o, indent)
	}
}

func TestBuildTextLayoutFirstIndentWrapsEarlier(t *testing.T) {
	fontSpec := Font{Size: 14}
	// Width fits the whole phrase without indent, but the indent should push
	// the first line to wrap sooner so the first line holds fewer words.
	full := BuildTextLayout("one two three four", fontSpec, TextLayoutOptions{
		MaxWidth: 150, Wrap: true,
	})
	indented := BuildTextLayout("one two three four", fontSpec, TextLayoutOptions{
		MaxWidth: 150, Wrap: true, FirstIndent: 80,
	})
	if len(indented.Lines) <= len(full.Lines) {
		t.Errorf("indent should force earlier wrap: full=%d indented=%d lines",
			len(full.Lines), len(indented.Lines))
	}
}

// The two text engines must agree on long-word handling. BuildTextLayout
// breaks a too-long word only with BreakLongWords (what native widgets set by
// default); without it the word forms one overflowing line, matching CSS
// overflow-wrap:normal and BuildInlineLayout's default.
func TestTextLayoutBreakLongWordsToggle(t *testing.T) {
	f := Font{Size: 16}
	long := "1231212312123121231212312123121231212312123121231212312"
	maxW := float32(120)

	whole := BuildTextLayout(long, f, TextLayoutOptions{MaxWidth: maxW, Wrap: true})
	if len(whole.Lines) != 1 {
		t.Errorf("overflow-wrap:normal should keep one overflowing line, got %d", len(whole.Lines))
	}
	if whole.Width <= maxW {
		t.Errorf("expected overflow, width=%v maxW=%v", whole.Width, maxW)
	}

	broken := BuildTextLayout(long, f, TextLayoutOptions{MaxWidth: maxW, Wrap: true, BreakLongWords: true})
	if len(broken.Lines) < 2 {
		t.Fatalf("BreakLongWords should split the run, got %d lines", len(broken.Lines))
	}
	var joined string
	for i, ln := range broken.Lines {
		joined += ln.Text
		if ln.Width > maxW+0.5 {
			t.Errorf("line %d width %v exceeds MaxWidth %v", i, ln.Width, maxW)
		}
	}
	if joined != long {
		t.Errorf("text after breaking = %q, want %q", joined, long)
	}
	// Line ranges stay contiguous either way — selection depends on it.
	for _, l := range []TextLayout{whole, broken} {
		for i := 1; i < len(l.Lines); i++ {
			if l.Lines[i-1].End != l.Lines[i].Start {
				t.Errorf("line ranges not contiguous at %d: %d != %d", i, l.Lines[i-1].End, l.Lines[i].Start)
			}
		}
	}

	// Ordinary words still wrap at spaces with breaking off, and the long
	// word overflows on its own line without swallowing its neighbours.
	mixed := BuildTextLayout("ab "+long+" cd", f, TextLayoutOptions{MaxWidth: maxW, Wrap: true})
	if len(mixed.Lines) != 3 {
		t.Fatalf("expected ab / longword / cd on three lines, got %d", len(mixed.Lines))
	}
	if mixed.Lines[0].Text != "ab" || mixed.Lines[1].Text != long || mixed.Lines[2].Text != "cd" {
		t.Errorf("unexpected lines: %q / %q / %q", mixed.Lines[0].Text, mixed.Lines[1].Text, mixed.Lines[2].Text)
	}
}
