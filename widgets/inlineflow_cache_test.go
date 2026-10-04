package widgets

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/qizhanchan/qui"
)

// InlineBox.buildLayout caches the shaped paragraph. These tests pin the
// cache-key logic: every input that changes the shaped result must still
// force a rebuild, or text silently renders with a stale layout.

func inlineWithText(text string) *InlineBox {
	b := NewInlineBox()
	b.AddText(text, Font{Size: 14}, Color{A: 1}, TextDecoration(0), "")
	return b
}

// Re-measuring at the same width must be stable (this is the case the cache
// short-circuits — the result has to stay correct, not just fast).
func TestInlineLayoutCacheStableAcrossMeasures(t *testing.T) {
	b := inlineWithText("the quick brown fox jumps over the lazy dog")
	first := b.Measure(Size{W: 200})
	for i := 0; i < 5; i++ {
		if got := b.Measure(Size{W: 200}); got != first {
			t.Fatalf("measure %d = %+v, want the stable %+v", i, got, first)
		}
	}
}

// A different width must re-wrap.
func TestInlineLayoutCacheWidthChangeRebuilds(t *testing.T) {
	b := inlineWithText("the quick brown fox jumps over the lazy dog")
	narrow := b.Measure(Size{W: 80})
	wide := b.Measure(Size{W: 600})
	if narrow.H <= wide.H {
		t.Errorf("narrow height %v should exceed wide height %v (no re-wrap?)", narrow.H, wide.H)
	}
	// Back to the original width reproduces the original result.
	if again := b.Measure(Size{W: 80}); again != narrow {
		t.Errorf("re-measuring at 80 = %+v, want %+v", again, narrow)
	}
}

// New content must rebuild, at the same width.
func TestInlineLayoutCacheContentChangeRebuilds(t *testing.T) {
	b := inlineWithText("short")
	before := b.Measure(Size{W: 300})
	b.AddText(" plus considerably more text appended after the fact",
		Font{Size: 14}, Color{A: 1}, TextDecoration(0), "")
	after := b.Measure(Size{W: 300})
	if after.W <= before.W {
		t.Errorf("appending text did not widen the layout: %v → %v", before.W, after.W)
	}

	b.Clear()
	b.AddText("short", Font{Size: 14}, Color{A: 1}, TextDecoration(0), "")
	if cleared := b.Measure(Size{W: 300}); cleared != before {
		t.Errorf("after Clear + re-add, measure = %+v, want %+v", cleared, before)
	}
}

// Option and font changes are part of the key: they must rebuild even though
// content and width are untouched.
func TestInlineLayoutCacheOptionChangesRebuild(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"

	// Wrap off → single line, so taller-when-wrapped must collapse.
	b := inlineWithText(text)
	wrapped := b.Measure(Size{W: 80})
	b.Wrap = false
	if unwrapped := b.Measure(Size{W: 80}); unwrapped.H >= wrapped.H {
		t.Errorf("turning Wrap off did not rebuild: H %v → %v", wrapped.H, unwrapped.H)
	}

	// Base font size. It sizes EMPTY lines (see InlineLayoutOptions.BaseFont),
	// so a box of blank lines is what exercises it — a text-bearing line takes
	// its height from the run's own font.
	b2 := NewInlineBox()
	b2.AddBreak()
	b2.AddBreak()
	b2.Style().Font = Font{Size: 12}
	small := b2.Measure(Size{W: 400})
	b2.Style().Font = Font{Size: 40}
	if big := b2.Measure(Size{W: 400}); big.H <= small.H {
		t.Errorf("growing the base font did not rebuild: H %v → %v", small.H, big.H)
	}

	// line-height multiplier.
	b3 := inlineWithText(text)
	normal := b3.Measure(Size{W: 400})
	b3.LineHeightScale = 3
	if loose := b3.Measure(Size{W: 400}); loose.H <= normal.H {
		t.Errorf("line-height change did not rebuild: H %v → %v", normal.H, loose.H)
	}

	// Long-word breaking changes the wrapped width of an unbreakable run.
	b4 := NewInlineBox()
	b4.AddText(strings.Repeat("x", 120), Font{Size: 14}, Color{A: 1}, TextDecoration(0), "")
	overflowing := b4.Measure(Size{W: 60})
	b4.BreakLongWords = true
	if broken := b4.Measure(Size{W: 60}); broken.W >= overflowing.W {
		t.Errorf("BreakLongWords did not rebuild: W %v → %v", overflowing.W, broken.W)
	}
}

// BenchmarkInlineRemeasure is the scroll-path cost: layout engines call
// Measure on every child of every relayout, and a ScrollView re-lays out its
// whole content on each scroll event. Before the cache this re-ran harfbuzz
// shaping every time (~175 µs per paragraph); it should now be a key compare.
func BenchmarkInlineRemeasure(b *testing.B) {
	box := NewInlineBox()
	for i := 0; i < 8; i++ {
		box.AddText(fmt.Sprintf("fragment %d with a few words of prose ", i),
			Font{Size: 14}, Color{A: 1}, TextDecoration(0), "")
	}
	box.Measure(Size{W: 400})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		box.Measure(Size{W: 400})
	}
}
