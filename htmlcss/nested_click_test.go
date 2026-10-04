package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A clickable element inside a clickable element must behave like the DOM:
// the INNER handler runs and, because it consumes the release, the outer one
// does not. The browser-tab shape depends on it — a tab row that activates
// on click, holding a ✕ that closes it. Firing the outer handler first (and
// swallowing the release on its way down) makes the ✕ dead.
func TestNestedClickTargetsTheInnermostHandler(t *testing.T) {
	res := RenderDoc(
		`<body><div id="row"><span id="label">Tab</span>`+
			`<button id="close">x</button></div></body>`,
		`#row { display: flex; gap: 8px; padding: 6px; }
		 #close { width: 20px; height: 20px; }`,
		Options{})

	root := res.Root
	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	win.SetRoot(root)
	root.Measure(qui.Size{W: 300, H: 0})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	row := res.ByID["row"].(*El)
	closeBtn := res.ByID["close"].(*El)
	var rowClicks, closeClicks int
	row.SetOnClick(func() { rowClicks++ })
	closeBtn.SetOnClick(func() { closeClicks++ })

	click := func(w qui.Widget) {
		b := w.Bounds()
		x, y := b.X+b.W/2, b.Y+b.H/2
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
		win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	}

	click(closeBtn)
	if closeClicks != 1 {
		t.Errorf("close handler fired %d times, want 1", closeClicks)
	}
	if rowClicks != 0 {
		t.Errorf("row handler fired %d times for a click on the close button, want 0", rowClicks)
	}

	// A click on the row's own content still reaches the row.
	click(res.ByID["label"])
	if rowClicks != 1 {
		t.Errorf("row handler fired %d times for a click on its label, want 1", rowClicks)
	}
	if closeClicks != 1 {
		t.Errorf("close handler fired %d times total, want 1", closeClicks)
	}
}
