package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// A `:hover` rule must NOT leak into the resting style, and must produce
// a hover style on the built Box that only takes effect while hovered.
func TestHoverStyleGatedAndApplied(t *testing.T) {
	// .ib has a width, so it builds as a Box (not a text Label); its
	// child is an inner block so it stays a real box.
	root := Render(
		`<body><div class="ib"><div class="inner"></div></div></body>`,
		`.ib { width: 30px; height: 24px; padding: 4px; } .ib:hover { background: #dde3ee; }`,
		Options{},
	)

	var box *widgets.Box
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if b, ok := w.(*El); ok && b.Hover != nil {
			box = &b.Box
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(root)

	if box == nil {
		t.Fatal("no Box with a hover style was produced for .ib:hover")
	}
	// Resting style must not carry the hover background (no leak).
	if box.Style().Background.A > 0 {
		t.Errorf("resting background = %+v, want transparent (hover leaked into base)", box.Style().Background)
	}
	// Hover style carries the highlight.
	want := qui.Color{R: 0xdd / 255.0, G: 0xe3 / 255.0, B: 0xee / 255.0, A: 1}
	if box.Hover.Background != want {
		t.Errorf("hover background = %+v, want %+v", box.Hover.Background, want)
	}

	box.Layout(qui.Rect{X: 0, Y: 0, W: 40, H: 30})

	// Not hovered: no fill recorded.
	var rest qui.RecordingCanvas
	box.Draw(&rest)
	restFills := len(rest.Fills)

	// Hovered: the highlight fill appears.
	bd := box.Bounds()
	box.Handle(qui.NewMouseEvent(qui.EventMouseEnter, bd.X+2, bd.Y+2, 0, 0))
	var hov qui.RecordingCanvas
	box.Draw(&hov)
	if len(hov.Fills) <= restFills {
		t.Errorf("hover did not add a highlight fill (rest=%d hover=%d)", restFills, len(hov.Fills))
	}
}
