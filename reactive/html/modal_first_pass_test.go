package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	h "github.com/qizhanchan/qui/reactive/html"
)

// A modal open in the very first pass is laid out styled by the window's
// layout pass: it was first laid out while detached, before the engine
// styled the tree, and a headless window never ticks the host to fix it.
func TestModalOpenOnFirstPassIsStyled(t *testing.T) {
	css := `.row { display: flex; flex-direction: row; height: 22px; }`
	for name, modal := range map[string]func(h.Node) h.Node{
		"sheet":  func(c h.Node) h.Node { return h.ModalSheet(func() {}, c) },
		"dialog": func(c h.Node) h.Node { return h.Dialog(h.DialogProps{Body: c, OnDismiss: func() {}}) },
	} {
		t.Run(name, func(t *testing.T) {
			win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
			rt := h.Mount(win, css, func() h.Node {
				return h.Div(modal(h.Div(h.Div(h.Span("a"), h.Span("b")).Class("row"))))
			})
			win.SetRoot(rt.Root())
			win.DrainJobsForTest()
			rt.Flush()
			win.LayoutForTest()
			row := findClass(win.Overlays()[0], "row")
			if got := row.Bounds().H; got != 22 {
				t.Fatalf("row height = %v, want 22 (unstyled layout)", got)
			}
		})
	}
}
