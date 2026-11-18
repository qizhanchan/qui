package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestInlineSpanInsetsAndRects(t *testing.T) {
	font := Font{Size: 16}
	plain := NewInlineBox()
	plain.AddText("ab cd", font, Color{A: 1}, 0, "")
	want := plain.MaxContentWidth()

	b := NewInlineBox()
	b.AddText("ab ", font, Color{A: 1}, 0, "")
	src := new(int)
	b.OpenSpan(InlineSpanStyle{
		Background: Color{R: 1, A: 1},
		Padding:    Insets{Left: 4, Right: 6, Top: 2, Bottom: 2},
		Margin:     Insets{Left: 3},
		Source:     src,
	})
	b.AddText("cd", font, Color{A: 1}, 0, "")
	b.CloseSpan()
	if got := b.MaxContentWidth(); absDiff(got, want+13) > 0.01 {
		t.Fatalf("span insets should add 3+4+6 px: width %.2f, plain %.2f", got, want)
	}
	b.Measure(Size{W: 500})
	b.Layout(Rect{W: 500, H: 40})
	rects := b.SpanRects(src)
	if len(rects) != 1 {
		t.Fatalf("want one fragment rect, got %v", rects)
	}
	r := rects[0]
	// The border box starts after the margin and covers padding + glyphs.
	if absDiff(r.X+r.W, b.MaxContentWidth()) > 0.01 {
		t.Fatalf("span box should end at the run's end: %v (run %.2f)", r, b.MaxContentWidth())
	}
	if absDiff(r.W, 4+plainWidth("cd", font)+6) > 0.01 {
		t.Fatalf("span box width %v", r)
	}
	if got, ok := b.SpanSourceAt(Point{X: r.X + r.W/2, Y: r.Y + r.H/2}); !ok || got != any(src) {
		t.Fatalf("SpanSourceAt inside the span = %v, %v", got, ok)
	}
}

// An end inset with no content of its own glues to the element's last
// word: a break never separates the padding from the text.
func TestInlineSpanInsetDoesNotBreak(t *testing.T) {
	font := Font{Size: 16}
	b := NewInlineBox()
	b.OpenSpan(InlineSpanStyle{Padding: Insets{Left: 20, Right: 20}})
	b.AddText("word", font, Color{A: 1}, 0, "")
	b.CloseSpan()
	w := b.MaxContentWidth()
	sz := b.Measure(Size{W: w - 1})
	one := NewInlineBox()
	one.AddText("word", font, Color{A: 1}, 0, "")
	if lineH := one.Measure(Size{W: 500}).H; sz.H > lineH*1.5 {
		t.Fatalf("a glued inset must not wrap onto its own line: %.1f vs line %.1f", sz.H, lineH)
	}
}

func plainWidth(s string, f Font) float32 {
	b := NewInlineBox()
	b.AddText(s, f, Color{A: 1}, 0, "")
	return b.MaxContentWidth()
}

func absDiff(a, b float32) float32 {
	if a > b {
		return a - b
	}
	return b - a
}
