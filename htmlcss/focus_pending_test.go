package htmlcss

import (
	"github.com/qizhanchan/qui"
	"testing"
)

// Native windows coalesce mutations until the rendering opportunity. A mount
// effect can call focus before the new input has been styled or backed by an
// editor. Model that pending state explicitly (test windows flush inline).
func TestFocusFlushesPendingControlBeforeFirstText(t *testing.T) {
	eng := NewStyleEngine("")
	root := eng.NewEl("div")
	win := qui.NewTestWindow(qui.Size{W: 400, H: 100})
	win.SetRoot(root)
	in := eng.NewEl("input")
	root.SetElementChildren([]qui.Widget{in})
	eng.SetRoot(root)
	eng.dirty = map[*El]dirtyKind{in: dirtySelf}
	if in.backing != nil {
		t.Fatal("expected pending native control creation")
	}
	in.RequestFocus()
	win.DispatchTestEvent(qui.NewCharEvent('s', 0))
	eng.FlushPendingStyles()
	win.LayoutForTest()
	in.RequestFocus()
	for _, c := range "tate" {
		win.DispatchTestEvent(qui.NewCharEvent(c, 0))
	}
	if got := in.InputValue(); got != "state" {
		t.Fatalf("first key lost across pending style flush: %q", got)
	}
}
