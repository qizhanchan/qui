package typeset

import (
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// A joining space from a run in another font is its own fragment in that
// font: prefixed to the next word it would be drawn in the word's (wider
// monospace) font and overrun the gap.
func TestJoiningSpaceKeepsItsFont(t *testing.T) {
	sans := gfx.Font{Size: 16}
	mono := gfx.Font{Size: 16, Family: "Courier"}
	l := BuildInlineLayout([]InlineItem{
		{Text: "A ", Font: sans},
		{Text: "tt", Font: mono, Span: 1},
		{Text: " ", Font: sans},
		{Text: "kbd", Font: mono, Span: 2},
		{Text: " x", Font: sans},
	}, InlineLayoutOptions{MaxWidth: 500, Wrap: true, BaseFont: sans})
	for _, f := range l.Lines[0].Frags {
		if len(f.Text) > 1 && f.Text[0] == ' ' && !gfx.FontStyleEqual(f.Font, sans) {
			t.Fatalf("fragment %q starts with a sans space but is drawn in %v", f.Text, f.Font)
		}
	}
	var prevEnd float32
	for i, f := range l.Lines[0].Frags {
		if i > 0 && f.Rect.X < prevEnd-0.01 {
			t.Fatalf("fragment %q overlaps the previous one (x %.2f < %.2f)", f.Text, f.Rect.X, prevEnd)
		}
		prevEnd = f.Rect.X + f.Rect.W
	}
}

// Inline element edges glue to the element's first and last tokens and
// take inline space; spans never merge fragments across elements.
func TestInlineSpanInsets(t *testing.T) {
	f := gfx.Font{Size: 16}
	plain := BuildInlineLayout([]InlineItem{{Text: "ab cd", Font: f}}, InlineLayoutOptions{BaseFont: f})
	l := BuildInlineLayout([]InlineItem{
		{Text: "ab ", Font: f},
		{Inset: 5, Span: 1},
		{Text: "cd", Font: f, Span: 1},
		{Inset: 7, InsetEnd: true, Span: 1},
	}, InlineLayoutOptions{BaseFont: f})
	if d := l.Width - plain.Width; d < 11.99 || d > 12.01 {
		t.Fatalf("insets should add 12px, got %.2f", d)
	}
	frags := l.Lines[0].Frags
	last := frags[len(frags)-1]
	if last.Span != 1 || last.Text != "cd" {
		t.Fatalf("the span's text is its own fragment: %+v", frags)
	}
	if end := last.Rect.X + last.Rect.W; end > l.Width-6.99 {
		t.Fatalf("the end inset follows the glyphs: glyphs end %.2f, width %.2f", end, l.Width)
	}
}

// A joining space between a box (or another span) and a link keeps the
// plain run's style: it is not underlined with the link.
func TestJoiningSpaceNotUnderlinedWithLink(t *testing.T) {
	f := gfx.Font{Size: 16}
	bold := gfx.Font{Size: 16, Weight: 700}
	link := DecorationUnderline
	for name, items := range map[string][]InlineItem{
		"after box": {
			{Box: true, Size: gfx.Size{W: 30, H: 16}},
			{Text: " ", Font: f},
			{Text: "link", Font: f, Decoration: link, Href: "#", Span: 1},
		},
		"after span": {
			{Text: "bold", Font: bold, Span: 1},
			{Text: " ", Font: f},
			{Text: "link", Font: f, Decoration: link, Href: "#", Span: 2},
		},
	} {
		l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 500, Wrap: true, BaseFont: f})
		for _, fr := range l.Lines[0].Frags {
			if fr.Href != "" && len(fr.Text) > 0 && fr.Text[0] == ' ' {
				t.Fatalf("%s: the link fragment %q swallowed the plain space", name, fr.Text)
			}
		}
	}
}
