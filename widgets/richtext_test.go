package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// RichText must satisfy the window-level selection contracts so it can
// participate in cross-widget drag-select + copy exactly like Label.
var (
	_ TextSelectable     = (*RichText)(nil)
	_ RichTextSelectable = (*RichText)(nil)
)

func TestRichTextSelectionAndLink(t *testing.T) {
	blue := Color{R: 0, G: 0, B: 1, A: 1}
	rt := NewRichText(
		TextSpan{Text: "go to "},
		TextSpan{Text: "here", Color: &blue, Href: "https://example.com"},
	).Wrap(false)

	w := NewTestWindow(Size{W: 300, H: 60})
	w.SetRoot(rt)
	rt.Layout(Rect{X: 0, Y: 0, W: 300, H: 40})
	rt.ClearLayoutDirty()

	if got := rt.SelectableLength(); got != len("go to here") {
		t.Fatalf("SelectableLength = %d, want %d", got, len("go to here"))
	}

	// Select the whole content and confirm the plain text round-trips.
	rt.SetSelectionRange(0, rt.SelectableLength())
	if got := rt.SelectedText(); got != "go to here" {
		t.Fatalf("SelectedText = %q, want %q", got, "go to here")
	}
	rt.ClearTextSelection()
	if got := rt.SelectedText(); got != "" {
		t.Fatalf("after clear SelectedText = %q, want empty", got)
	}

	// The link span is discoverable via LinkAt at its glyph region.
	content := rt.Bounds()
	leadW, _ := TextMetrics("go to ", rt.Style().Font)
	linkW, _ := TextMetrics("here", rt.Style().Font)
	mid := Point{X: content.X + leadW + linkW/2, Y: content.Y + content.H/2}
	if href, ok := rt.LinkAt(mid); !ok || href != "https://example.com" {
		t.Fatalf("LinkAt over link = (%q, %v), want the example.com href", href, ok)
	}
}
