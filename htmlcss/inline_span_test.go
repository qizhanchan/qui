package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// webInlineBoxes renders a web-compat document and returns its inline runs.
func webInlineBoxes(t *testing.T, src string, q QuirksMode) []*widgets.InlineBox {
	t.Helper()
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(false) })
	win := qui.NewTestWindow(qui.Size{W: 400, H: 600})
	res := RenderDoc(src, "", Options{Window: win, ViewportWidth: 400, DocumentRoot: true, Quirks: q})
	win.SetRoot(res.Root)
	sz := res.Root.Measure(qui.Size{W: 400})
	res.Root.Layout(qui.Rect{W: 400, H: sz.H})
	var out []*widgets.InlineBox
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if ib, ok := w.(*widgets.InlineBox); ok {
			out = append(out, ib)
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, k := range c.ChildList() {
				walk(k)
			}
		}
	}
	walk(res.Root)
	return out
}

// A link's own line-height sets its lines; the block's strut is a floor in
// standards mode only (quirks: lines without direct text ignore it).
func TestInlineSpanLineHeightAndQuirksStrut(t *testing.T) {
	src := `<html><body style="margin:0;font-size:14.4px;line-height:1.6"><div><a href="#" style="line-height:1.45">Chrome is the official web browser from Google, built to be fast, secure, and customizable. Download now and make it yours. More words here to wrap again and again.</a></div></body></html>`
	std := webInlineBoxes(t, src, NoQuirksMode)
	quirks := webInlineBoxes(t, src, LimitedQuirksMode)
	if len(std) != 1 || len(quirks) != 1 {
		t.Fatalf("want one inline run each, got %d / %d", len(std), len(quirks))
	}
	hs, hq := std[0].Bounds().H, quirks[0].Bounds().H
	lines := hs / (14.4 * 1.6)
	if lines < 2 || absf(lines-float32(int(lines+0.5))) > 0.01 {
		t.Fatalf("standards lines should be strut-tall (23.04px): height %.2f", hs)
	}
	if want := float32(int(lines+0.5)) * 14.4 * 1.45; absf(hq-want) > 0.5 {
		t.Fatalf("quirks lines should follow the link's line-height: height %.2f, want %.2f", hq, want)
	}
}

// Hidden and empty inline elements do not split or add lines; a link
// with an id and a :hover rule still wraps inside the paragraph.
func TestInlineSpanFolding(t *testing.T) {
	src := `<html><head><style>a:hover{text-decoration:underline}</style></head><body style="margin:0;font:16px/20px sans-serif"><a id="top"></a><p style="margin:0">Before <span>As of 2026</span><sup style="display:none">[update]</sup>, then <a id="l" href="#">a link that is long enough to wrap onto the next line of text</a> after.</p></body></html>`
	boxes := webInlineBoxes(t, src, NoQuirksMode)
	if len(boxes) != 1 {
		t.Fatalf("want a single inline run (no empty-anchor line, no split at the hidden sup), got %d", len(boxes))
	}
	if n := boxes[0].ChildCount(); n != 0 {
		t.Fatalf("the link should fold into the run, not stay an atomic box (%d children)", n)
	}
	if h := boxes[0].Bounds().H; h < 40 {
		t.Fatalf("expected the text to wrap onto several lines, height %.1f", h)
	}
}
