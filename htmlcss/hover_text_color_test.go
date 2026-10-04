package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// P0.1: a :hover rule that only changes `color` must recolor the element's
// text (which lives on a child Label, not the Box), not silently no-op.
func TestHoverChangesTextColor(t *testing.T) {
	res := RenderDoc(
		`<body><div class="row"><a class="lnk" href="#">Link</a></div></body>`,
		`.row { display: flex; } .lnk { color: #000000; } .lnk:hover { color: #ff0000; }`,
		Options{},
	)
	els := res.ByClass["lnk"]
	if len(els) == 0 {
		t.Fatal("no .lnk element found")
	}
	e := els[0].(*El)
	if !e.textStateActive {
		t.Fatal("textStateActive not set for a text-only :hover color rule")
	}

	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 80})

	var rec qui.RecordingCanvas
	e.Draw(&rec) // resting pass
	if e.textLabel == nil {
		t.Fatal("link produced no text label")
	}
	black := qui.Color{A: 1}
	if got := e.textLabel.Style().Foreground; got != black {
		t.Fatalf("resting foreground = %+v, want black (hover leaked?)", got)
	}

	// Enter hover, redraw: the label recolors to red.
	b := e.Bounds()
	e.Handle(qui.NewMouseEvent(qui.EventMouseEnter, b.X+1, b.Y+1, 0, 0))
	e.Draw(&rec)
	red := qui.Color{R: 1, A: 1}
	if got := e.textLabel.Style().Foreground; got != red {
		t.Errorf("hovered foreground = %+v, want red", got)
	}

	// Leave hover, redraw: back to resting black.
	e.Handle(qui.NewMouseEvent(qui.EventMouseLeave, b.X-5, b.Y-5, 0, 0))
	e.Draw(&rec)
	if got := e.textLabel.Style().Foreground; got != black {
		t.Errorf("after leave foreground = %+v, want black", got)
	}
}
