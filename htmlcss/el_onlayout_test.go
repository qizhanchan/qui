package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// SetOnLayout fires after the element's own layout with its final bounds and
// with its children (in-flow and absolutely positioned) already laid out, and
// fires again on every later layout — a window resize included.
func TestElOnLayoutFiresWithFinalBounds(t *testing.T) {
	root := Render(
		`<body><div class="row"><div class="a"></div><div class="abs"></div></div></body>`,
		`body { margin: 0; padding: 0; }
		 .row { display: flex; position: relative; padding: 0 10px; height: 40px; }
		 .a { width: 50px; height: 20px; }
		 .abs { position: absolute; left: 0; top: 0; width: 70px; height: 10px; }`,
		Options{},
	)
	row := findElByClassInTree(root, "row")
	a := findElByClassInTree(root, "a")
	abs := findElByClassInTree(root, "abs")
	if row == nil || a == nil || abs == nil {
		t.Fatalf("missing els")
	}
	calls := 0
	var rowW, aW, absW float32
	row.SetOnLayout(func(el *El) {
		calls++
		if el != row {
			t.Errorf("onLayout got %p, want row %p", el, row)
		}
		rowW, aW, absW = el.Bounds().W, a.Bounds().W, abs.Bounds().W
	})
	win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	win.SetRoot(root)
	win.LayoutForTest()
	if calls == 0 {
		t.Fatalf("onLayout did not fire")
	}
	if rowW != 400 || aW != 50 || absW != 70 {
		t.Errorf("bounds at callback: row=%v a=%v abs=%v, want 400/50/70", rowW, aW, absW)
	}
	before := calls
	win.ResizeForTest(qui.Size{W: 300, H: 200})
	win.LayoutForTest()
	if calls == before {
		t.Fatalf("onLayout did not fire after resize")
	}
	if rowW != 300 {
		t.Errorf("row width after resize = %v, want 300", rowW)
	}
	row.SetOnLayout(nil)
	before = calls
	win.LayoutForTest()
	if calls != before {
		t.Errorf("onLayout fired after removal")
	}
}
