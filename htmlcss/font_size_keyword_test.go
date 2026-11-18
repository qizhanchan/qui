package htmlcss

import "testing"

// Absolute-size keywords and smaller / larger, and Chrome's 13px default
// for a font-family of just `monospace` (web-compat), which a length size
// or a second family name turns off.
func TestFontSizeKeywords(t *testing.T) {
	sheet := ParseCSS(``)
	root := computeNode(&Node{Type: ElementNode, Tag: "html"}, sheet, nil)
	size := func(style string, parent *ComputedStyle) *ComputedStyle {
		return computeNode(&Node{Type: ElementNode, Tag: "span", Attrs: map[string]string{"style": style}}, sheet, parent)
	}
	for _, c := range []struct {
		style string
		want  float32
	}{
		{"font-size: small", 13},
		{"font-size: x-large", 24},
		{"font-size: smaller", 16 / 1.2},
		{"font-size: larger", 16 * 1.2},
		{"font-size: 20px", 20},
	} {
		if got := size(c.style, root).FontSize; absf(got-c.want) > 0.01 {
			t.Errorf("%s: %.2fpx, want %.2f", c.style, got, c.want)
		}
	}
	SetWebCompat(true)
	defer SetWebCompat(false)
	root = computeNode(&Node{Type: ElementNode, Tag: "html"}, sheet, nil)
	if got := size("font-family: monospace", root).FontSize; got != 13 {
		t.Errorf("monospace alone: %.1fpx, want 13", got)
	}
	if got := size("font-family: monospace, monospace", root).FontSize; got != 16 {
		t.Errorf("monospace, monospace: %.1fpx, want 16", got)
	}
	px := size("font-size: 14px", root)
	if got := size("font-family: monospace", px).FontSize; got != 14 {
		t.Errorf("monospace under a 14px parent: %.1fpx, want 14", got)
	}
	mono := size("font-family: monospace", root)
	if got := size("font-family: sans-serif", mono).FontSize; got != 16 {
		t.Errorf("back to sans-serif under a keyword-sized monospace parent: %.1fpx, want 16", got)
	}
}

// rem is the root element's font size (html { font-size: 62.5% } → 10px),
// and the root's own font-size rem is the initial 16px.
func TestRemFollowsRootFontSize(t *testing.T) {
	sheet := ParseCSS(`html { font-size: 62.5%; padding: 1rem } body { font-size: 1.6rem; margin: 2rem }`)
	html := computeNode(&Node{Type: ElementNode, Tag: "html"}, sheet, nil)
	if html.FontSize != 10 || html.Padding.Top != 10 {
		t.Fatalf("html: font %.1f padding %.1f, want 10 / 10", html.FontSize, html.Padding.Top)
	}
	body := computeNode(&Node{Type: ElementNode, Tag: "body"}, sheet, html)
	if body.FontSize != 16 || body.Margin.Top != 20 {
		t.Fatalf("body: font %.1f margin %.1f, want 16 / 20", body.FontSize, body.Margin.Top)
	}
	sheet2 := ParseCSS(`html { font-size: 2rem }`)
	if h := computeNode(&Node{Type: ElementNode, Tag: "html"}, sheet2, nil); h.FontSize != 32 {
		t.Fatalf("the root's own rem is the initial 16px: %.1f", h.FontSize)
	}
}
