package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/widgets"
	h "github.com/qizhanchan/qui/reactive/html"
)

func findTag(w qui.Widget, tag string) *htmlcss.El {
	if el, ok := w.(*htmlcss.El); ok && el.Tag() == tag {
		return el
	}
	if cl, ok := w.(qui.ChildLister); ok {
		for _, c := range cl.ChildList() {
			if f := findTag(c, tag); f != nil {
				return f
			}
		}
	}
	return nil
}

// Padding and height on <input> / <select> are honoured: the element is the
// control's box (no hidden inset), padding grows the field, and a dialog's
// fields are GM3 40 px outlined fields.
func TestControlPaddingAndHeightAreCSS(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	css := `.tb select { height: 28px; padding: 0 8px; box-sizing: border-box; }
	        .pad { padding: 8px 12px; font-size: 14px; }`
	rt := h.Mount(win, css, func() h.Node {
		return h.Div(
			h.Div(h.Select("Arial", "Times")).Class("tb"),
			h.Input().Class("pad"),
		)
	})
	root := rt.Root().(*htmlcss.El)
	root.Layout(qui.Rect{W: 800, H: 600})
	sel := findTag(root, "select")
	if got := sel.Bounds().H; got != 28 {
		t.Errorf("toolbar select height = %v, want 28 (no built-in inset)", got)
	}
	in := findTag(root, "input")
	// 1px border + 8px padding each side + one 14px line (1.3em).
	if got := in.Bounds().H; got < 36 || got > 37 {
		t.Errorf("padded input height = %v, want ~36.2", got)
	}
}

func TestDialogFieldsAreGM3Height(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	h.Mount(win, ``, func() h.Node {
		return h.Div(h.Dialog(h.DialogProps{Title: "Rename", Body: h.Div(h.Input(), h.Select("A", "B"))}))
	})
	var dlgInput, dlgSelect *htmlcss.El
	for _, ov := range win.Overlays() {
		ov.Layout(qui.Rect{W: 800, H: 600})
		if dlgInput == nil {
			dlgInput = findTag(ov, "input")
		}
		if dlgSelect == nil {
			dlgSelect = findTag(ov, "select")
		}
	}
	if dlgInput == nil || dlgSelect == nil {
		t.Fatal("dialog fields not found")
	}
	for name, el := range map[string]*htmlcss.El{"input": dlgInput, "select": dlgSelect} {
		var bh float32
		for _, c := range el.ChildList() {
			switch c.(type) {
			case *widgets.Input, *widgets.Select:
				bh = c.Measure(qui.Size{W: 400, H: 600}).H
			}
		}
		if bh != 40 {
			t.Errorf("dialog %s field height = %v, want 40", name, bh)
		}
	}
}
