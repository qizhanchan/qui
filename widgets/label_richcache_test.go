package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

// Label memoizes its rich-text layout (BuildRichTextLayout re-shapes through
// harfbuzz, ~128 µs for a handful of spans, and Measure / Draw / LinkAt all
// want the same answer). These tests pin the cache-key logic: every input
// that changes the shaped result must still force a rebuild, or rich text
// silently renders with a stale layout.

func richLabel(texts ...string) *Label {
	l := NewLabel("")
	spans := make([]TextSpan, 0, len(texts))
	for _, t := range texts {
		spans = append(spans, TextSpan{Text: t, Font: &Font{Size: 14}})
	}
	l.SetSpans(spans)
	// Wrap is off in DefaultParagraphStyle, and the default style carries
	// padding — both would make these tests measure something other than what
	// they mean to. Zero padding keeps the widths in the tests exact.
	l.Paragraph.Wrap = true
	l.Style().Padding = Insets{}
	return l
}

const richProse = "the quick brown fox jumps over the lazy dog and keeps running "

// layoutID identifies a built layout by the address of its first line, so a
// rebuild (which allocates a fresh Lines slice) is observable. Comparing the
// layout structs themselves is impossible — they contain slices.
func layoutID(l RichTextLayout) *RichTextLine {
	if len(l.Lines) == 0 {
		return nil
	}
	return &l.Lines[0]
}

func (l *Label) testLayout(maxW float32) RichTextLayout {
	return l.richTextLayout(l.Paragraph.LayoutOptions(maxW), l.resolveForeground())
}

// The whole point: repeated builds with unchanged inputs reuse the layout.
func TestLabelRichCacheHitsOnUnchangedInputs(t *testing.T) {
	l := richLabel(richProse, "and a second span")
	first := l.testLayout(300)
	for i := 0; i < 5; i++ {
		if layoutID(l.testLayout(300)) != layoutID(first) {
			t.Fatalf("build %d re-shaped despite unchanged inputs", i)
		}
	}
	// Measure goes through the same cache.
	before := layoutID(first)
	l.Measure(Size{W: 300})
	if layoutID(l.richLayout) != before {
		t.Error("Measure re-shaped instead of reusing the cached layout")
	}
}

// A width change must re-wrap.
func TestLabelRichCacheWidthChangeRebuilds(t *testing.T) {
	l := richLabel(richProse)
	narrow := l.testLayout(90)
	narrowH := narrow.Height
	wide := l.testLayout(600)
	if layoutID(wide) == layoutID(narrow) {
		t.Fatal("width change did not rebuild")
	}
	if narrowH <= wide.Height {
		t.Errorf("narrow height %v should exceed wide height %v (no re-wrap?)", narrowH, wide.Height)
	}
}

// Replacing content must rebuild — via either setter.
func TestLabelRichCacheContentChangeRebuilds(t *testing.T) {
	l := richLabel("short")
	before := l.testLayout(400)
	beforeW := before.Width

	l.SetSpans([]TextSpan{{Text: "considerably more text than before", Font: &Font{Size: 14}}})
	after := l.testLayout(400)
	if layoutID(after) == layoutID(before) {
		t.Fatal("SetSpans did not invalidate the layout")
	}
	if after.Width <= beforeW {
		t.Errorf("wider content measured %v, want more than %v", after.Width, beforeW)
	}

	// Same span text, set again: still a new revision, so a rebuild — content
	// identity is not something SetSpans can cheaply prove unchanged.
	same := l.Spans()
	cp := append([]TextSpan(nil), same...)
	l.SetSpans(cp)
	if layoutID(l.testLayout(400)) == layoutID(after) {
		t.Error("SetSpans did not bump the content revision")
	}

	// SetText drops spans entirely; the label leaves rich mode.
	l.SetText("plain now")
	if len(l.Spans()) != 0 {
		t.Error("SetText should clear spans")
	}
	if l.haveRich {
		t.Error("SetText should invalidate the rich-text cache")
	}
}

// Paragraph is a public struct callers mutate in place; each option that
// reaches the layout builder must be part of the key.
func TestLabelRichCacheParagraphOptionsRebuild(t *testing.T) {
	t.Run("wrap", func(t *testing.T) {
		l := richLabel(richProse)
		wrapped := l.testLayout(90)
		wrappedH := wrapped.Height
		l.Paragraph.Wrap = false
		un := l.testLayout(90)
		if layoutID(un) == layoutID(wrapped) {
			t.Fatal("Wrap change did not rebuild")
		}
		if un.Height >= wrappedH {
			t.Errorf("unwrapped height %v should be less than wrapped %v", un.Height, wrappedH)
		}
	})

	t.Run("align", func(t *testing.T) {
		// Alignment shifts line offsets without changing the measured size,
		// so the offset is what proves the rebuild landed.
		l := richLabel(richProse)
		start := l.testLayout(600)
		startOff := start.Lines[0].Offset
		l.Paragraph.Align = TextAlignCenter
		centered := l.testLayout(600)
		if layoutID(centered) == layoutID(start) {
			t.Fatal("Align change did not rebuild")
		}
		if centered.Lines[0].Offset <= startOff {
			t.Errorf("centered offset %v should exceed start-aligned %v",
				centered.Lines[0].Offset, startOff)
		}
	})

	t.Run("lineHeightScale", func(t *testing.T) {
		l := richLabel(richProse)
		normal := l.testLayout(400)
		normalH := normal.Height
		l.Paragraph.LineHeightScale = 3
		loose := l.testLayout(400)
		if layoutID(loose) == layoutID(normal) {
			t.Fatal("LineHeightScale change did not rebuild")
		}
		if loose.Height <= normalH {
			t.Errorf("loose height %v should exceed normal %v", loose.Height, normalH)
		}
	})

	t.Run("maxLines", func(t *testing.T) {
		l := richLabel(richProse)
		full := l.testLayout(90)
		fullLines := len(full.Lines)
		if fullLines < 2 {
			t.Skipf("fixture did not wrap (%d line)", fullLines)
		}
		l.Paragraph.MaxLines = 1
		clamped := l.testLayout(90)
		if layoutID(clamped) == layoutID(full) {
			t.Fatal("MaxLines change did not rebuild")
		}
		if len(clamped.Lines) != 1 {
			t.Errorf("MaxLines=1 produced %d lines", len(clamped.Lines))
		}
	})

	t.Run("ellipsis", func(t *testing.T) {
		l := richLabel(richProse)
		l.Paragraph.MaxLines = 1
		plain := l.testLayout(90)
		l.Paragraph.Ellipsis = true
		ell := l.testLayout(90)
		if layoutID(ell) == layoutID(plain) {
			t.Fatal("Ellipsis change did not rebuild")
		}
	})
}

// Font identity and the resolved foreground are baked into the layout's runs.
func TestLabelRichCacheFontAndColorRebuild(t *testing.T) {
	l := richLabel(richProse)
	base := l.testLayout(400)
	l.Style().Font.LetterSpacing = 4
	spaced := l.testLayout(400)
	if layoutID(spaced) == layoutID(base) {
		t.Fatal("letter-spacing change did not rebuild")
	}

	l2 := richLabel(richProse)
	c1 := l2.testLayout(400)
	l2.Style().Foreground = Color{R: 1, A: 1}
	if layoutID(l2.testLayout(400)) == layoutID(c1) {
		t.Error("foreground change did not rebuild (color is baked into runs)")
	}
}

// BenchmarkLabelRichRemeasure is the scroll-path cost: Measure runs for every
// child of every relayout, and a ScrollView re-lays out its whole content on
// each scroll event.
func BenchmarkLabelRichRemeasure(b *testing.B) {
	l := richLabel(richProse, "second span ", "third span ", "fourth span ")
	l.Measure(Size{W: 400})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Measure(Size{W: 400})
	}
}

// A font-REGISTRY change (SetDefaultFont / fallback swap) leaves the font
// spec identical while changing the faces behind it, so the shaped layout
// moves. The global text memo is cleared on such a change; this per-widget
// cache watches qui.FontRegistryGeneration instead.
//
// Asserted white-box (the key carries the live generation) rather than by
// actually swapping the font: a swap is process-global and would change text
// metrics for every later test in this package, including the golden ones.
// qui's own TestFontRegistryGenerationChanges covers the counter itself.
func TestLabelRichCacheKeyTracksFontGeneration(t *testing.T) {
	l := richLabel(richProse)
	opts := l.Paragraph.LayoutOptions(400)
	key := l.layoutKey(opts, l.resolveForeground())
	if key.fontGen != FontRegistryGeneration() {
		t.Fatalf("key fontGen = %d, want the live generation %d",
			key.fontGen, FontRegistryGeneration())
	}
	// A stale generation in the cached key must force a rebuild.
	before := l.testLayout(400)
	l.richKey.fontGen--
	if layoutID(l.testLayout(400)) == layoutID(before) {
		t.Error("a stale font generation did not invalidate the cached layout")
	}
}

// The cache must not change what is PAINTED. Draw reads the same memo as
// Measure, so a stale entry would show up as the previous text still on
// screen — exactly the failure a state-only test cannot see.
func TestLabelRichCacheDrawsCurrentContent(t *testing.T) {
	render := func(l *Label) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 300, 60))
		cv := NewImageCanvas(img)
		l.Layout(Rect{W: 300, H: 60})
		l.Measure(Size{W: 300, H: 60}) // populates the cache, as a real frame does
		l.Draw(cv)
		return img
	}
	ink := func(img *image.RGBA) int {
		n := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					n++
				}
			}
		}
		return n
	}

	l := richLabel("iii")
	thin := ink(render(l))
	if thin == 0 {
		t.Fatal("nothing painted for the first content")
	}
	// Repainting with no change must produce identical pixels.
	if again := ink(render(l)); again != thin {
		t.Errorf("repaint changed ink %d → %d with no input change", thin, again)
	}
	// New content must actually reach the screen.
	l.SetSpans([]TextSpan{{Text: "WWWWWWWWWWWW", Font: &Font{Size: 14}}})
	if wide := ink(render(l)); wide == thin {
		t.Error("painted ink unchanged after SetSpans — stale cached layout drawn")
	}
}
