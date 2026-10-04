package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A scroll host (overflow:auto) must follow a descendant that changes size
// WITHOUT a width change or a child-list change — here an inline-style
// height, the shape of a live preview image being swapped for a taller one.
// Before the fix the ScrollView kept its stale content size, so the child
// stayed laid out at its old height (and the scroll range never grew).
func TestScrollHostFollowsChildHeightChange(t *testing.T) {
	eng := NewStyleEngine(
		`.canvas { display:flex; flex-direction:column; width:300px; height:200px; overflow-y:auto }`)
	root := eng.NewEl("div")
	root.SetClass("canvas")
	child := eng.NewEl("div")
	child.SetAttr("style", "width:100px;height:150px")
	root.SetElementChildren([]qui.Widget{child})
	eng.SetRoot(root)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 400})
	win.SetRoot(root)
	eng.Restyle()
	root.Layout(qui.Rect{W: 300, H: 200})

	if got := child.Bounds().H; got != 150 {
		t.Fatalf("initial child height = %v, want 150", got)
	}

	child.SetAttr("style", "width:100px;height:400px") // taller than the viewport
	root.Layout(qui.Rect{W: 300, H: 200})

	if got := child.Style().Height; got != 400 {
		t.Fatalf("restyled height = %v, want 400", got)
	}
	if got := child.Bounds().H; got != 400 {
		t.Fatalf("child laid out at %v, want 400", got)
	}
}
