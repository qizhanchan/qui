package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func mountForLayout(t *testing.T, css, tag, text string) (*El, *qui.Window) {
	t.Helper()
	eng := NewStyleEngine(css)
	root := eng.NewEl(tag)
	root.SetTextContent(text)
	eng.SetRoot(root)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(root)
	eng.Restyle()
	qui.MeasureChild(root, qui.Size{W: 400, H: 300})
	root.Layout(qui.Rect{W: 400, H: 300})
	root.ClearLayoutDirty()
	win.ClearDirtyRegion()
	return root, win
}

// A text element keeps its box model on the element, not on its internal
// label. Restyling one must not bounce the label's padding through the
// CSS value and back to zero: that round trip invalidated layout on every
// restyle, so a clock ticking in a header relaid out the whole page.
func TestRestyleOfUnchangedTextElementKeepsLayoutClean(t *testing.T) {
	root, win := mountForLayout(t,
		`h1 { margin: 8px; padding: 6px } .a { color: #ff0000 } .b { color: #0000ff }`,
		"h1", "Title")

	root.SetClass("a") // paint-only: a scoped restyle that moves nothing
	if root.IsLayoutDirty() {
		t.Fatal("color-only restyle of a padded text element dirtied layout")
	}
	if win.DirtyRegion().IsEmpty() {
		t.Fatal("color change did not repaint")
	}
}

// white-space reaches the label as a paragraph flag, not a Style field, so
// it must invalidate on its own or the label's cached size goes stale.
func TestWhiteSpaceToggleRemeasuresText(t *testing.T) {
	root, _ := mountForLayout(t,
		`.nw { white-space: nowrap }`,
		"p", "several words that need more than one line at this width")
	avail := qui.Size{W: 120, H: 0}
	wrapped := qui.MeasureChild(root, avail).H

	root.SetClass("nw")
	if !root.IsLayoutDirty() {
		t.Fatal("white-space change did not dirty layout")
	}
	if single := qui.MeasureChild(root, avail).H; single >= wrapped {
		t.Fatalf("nowrap height %v, want less than wrapped %v (stale cached measure?)", single, wrapped)
	}
}
