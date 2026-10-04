package htmlcss

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A selection drag can start on blank space (page margin, gap between
// blocks): the anchor snaps to the nearest document position and the
// drag selects the text it sweeps over — browser behavior. Regression
// guard for "must press exactly on text to start selecting".
func TestBlankPressStartsSelectionDrag(t *testing.T) {
	body := Render(
		`<body><p>first paragraph text</p><p>second paragraph text</p></body>`,
		`body{padding:24px;} p{margin:0 0 16px 0;}`,
		Options{},
	)
	viewportW := float32(500)
	h := body.Measure(qui.Size{W: viewportW, H: 0})
	win := qui.NewTestWindow(qui.Size{W: viewportW, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: viewportW, H: h.H})

	var labels []*widgets.Label
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if l, ok := w.(*widgets.Label); ok && l.SelectableLength() > 0 {
			labels = append(labels, l)
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if len(labels) < 2 {
		t.Fatalf("want 2 paragraph labels, got %d", len(labels))
	}
	first, second := labels[0], labels[1]

	// Press in the left margin (x=4 — inside body padding, left of the
	// first paragraph), then drag to the middle of the second paragraph.
	startY := first.Bounds().Y + 4
	endB := second.Bounds()
	endX, endY := endB.X+endB.W/2, endB.Y+endB.H/2
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 4, startY, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, endX, endY, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, endX, endY, qui.MouseButtonLeft, 0))

	if got := first.SelectedText(); got != "first paragraph text" {
		t.Errorf("first paragraph selection = %q, want the whole text", got)
	}
	if got := second.SelectedText(); got == "" || !strings.HasPrefix("second paragraph text", got) {
		t.Errorf("second paragraph selection = %q, want a leading slice of it", got)
	}

	// A plain click on blank space still deselects everything.
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 4, startY, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, 4, startY, qui.MouseButtonLeft, 0))
	if first.SelectedText() != "" || second.SelectedText() != "" {
		t.Error("blank click should clear the selection")
	}
}

// A press inside an interactive control's padding (an htmlcss <button>)
// must NOT arm a blank-press selection drag — the control owns the
// gesture, matching browsers.
func TestBlankPressInsideButtonDoesNotSelect(t *testing.T) {
	body := Render(
		`<body><p>some copyable text</p><button id="b">OK</button></body>`,
		`button{padding:12px 24px;} button:focus{outline:auto;}`,
		Options{},
	)
	viewportW := float32(500)
	h := body.Measure(qui.Size{W: viewportW, H: 0})
	win := qui.NewTestWindow(qui.Size{W: viewportW, H: 400})
	win.SetRoot(body)
	body.Layout(qui.Rect{X: 0, Y: 0, W: viewportW, H: h.H})

	var para *widgets.Label
	var btn qui.Widget
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if l, ok := w.(*widgets.Label); ok && strings.Contains(l.Text(), "copyable") {
			para = l
		}
		if el, ok := w.(*El); ok && el.Tag() == "button" {
			btn = el
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(body)
	if para == nil || btn == nil {
		t.Fatal("missing paragraph or button")
	}

	// Press inside the button's padding (its left edge, away from the
	// label glyphs) and drag up into the paragraph.
	bb := btn.Bounds()
	px, py := bb.X+4, bb.Y+bb.H/2
	pb := para.Bounds()
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, px, py, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, pb.X+pb.W/2, pb.Y+4, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, pb.X+pb.W/2, pb.Y+4, qui.MouseButtonLeft, 0))

	if got := para.SelectedText(); got != "" {
		t.Errorf("drag from inside a button selected %q, want no selection", got)
	}
}
