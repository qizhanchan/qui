package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// findElByClassInTree returns the first *El carrying class in the widget
// tree (as opposed to RenderResult.ByClass, which also indexes folded
// elements that never became widgets).
func findElByClassInTree(w qui.Widget, class string) *El {
	var found *El
	var walk func(qui.Widget)
	walk = func(w qui.Widget) {
		if found != nil {
			return
		}
		if e, ok := w.(*El); ok && e.node.hasClass(class) {
			found = e
			return
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(w)
	return found
}

// P0.1 residual: a link folded into a paragraph's inline run still recolors
// on :hover. The fix keeps such a link as a real (atomic) inline El — not a
// folded text span — so it can track hover and recolor via El.Draw.
func TestFoldedLinkHoverRecolors(t *testing.T) {
	res := RenderDoc(
		`<body><p>Read the <a class="lnk" href="#">docs</a> now.</p></body>`,
		`.lnk { color: #0000ff; } .lnk:hover { color: #ff0000; }`,
		Options{},
	)
	// The link must exist as a real widget in the tree (atomic inline box),
	// not be folded away into a text span.
	e := findElByClassInTree(res.Root, "lnk")
	if e == nil {
		t.Fatal("hover-colored link was folded away; expected it to stay an atomic inline El")
	}
	if !e.textStateActive {
		t.Fatal("link with :hover color did not mark text-state active")
	}

	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 100})
	var rec qui.RecordingCanvas
	e.Draw(&rec)
	blue := qui.Color{B: 1, A: 1}
	if got := e.textLabel.Style().Foreground; got != blue {
		t.Fatalf("resting link foreground = %+v, want blue", got)
	}
	b := e.Bounds()
	e.Handle(qui.NewMouseEvent(qui.EventMouseEnter, b.X+1, b.Y+1, 0, 0))
	e.Draw(&rec)
	red := qui.Color{R: 1, A: 1}
	if got := e.textLabel.Style().Foreground; got != red {
		t.Errorf("hovered link foreground = %+v, want red", got)
	}
}

// A folded link WITHOUT a state rule still folds (no regression / no needless
// atomic boxes).
func TestPlainFoldedLinkStaysFolded(t *testing.T) {
	res := RenderDoc(
		`<body><p>Read the <a class="plain" href="#">docs</a> now.</p></body>`,
		`.plain { color: #0000ff; }`,
		Options{},
	)
	if e := findElByClassInTree(res.Root, "plain"); e != nil {
		t.Error("a plain link (no state rule) should fold into the text run, not stay a widget")
	}
}
