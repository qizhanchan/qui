package html_test

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	h "github.com/qizhanchan/qui/reactive/html"
	"github.com/qizhanchan/qui/widgets"
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
// control's box (no hidden inset) and padding grows the field.
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

// Fields in a dialog body keep the native control look: the framework
// dialog CSS styles the shell, not the controls inside it.
func TestDialogFieldsKeepNativeLook(t *testing.T) {
	height := func(win *qui.Window, el *htmlcss.El) float32 {
		for _, c := range el.ChildList() {
			switch c.(type) {
			case *widgets.Input, *widgets.Select:
				return c.Measure(qui.Size{W: 400, H: 600}).H
			}
		}
		t.Fatalf("<%s> has no backing control", el.Tag())
		return 0
	}
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	h.Mount(win, ``, func() h.Node {
		return h.Div(h.Input(), h.Select("A", "B"),
			h.Dialog(h.DialogProps{Title: "Rename", Body: h.Div(h.Input(), h.Select("A", "B"))}))
	})
	win.Root().Layout(qui.Rect{W: 800, H: 600})
	for _, tag := range []string{"input", "select"} {
		page := findTag(win.Root(), tag)
		dlg := dlgField(t, win, tag)
		if page == nil {
			t.Fatalf("no page <%s>", tag)
		}
		if hp, hd := height(win, page), height(win, dlg); hp != hd {
			t.Errorf("dialog <%s> height = %v, page one %v — the dialog restyled a control", tag, hd, hp)
		}
		if bp, bd := page.Style().Border, dlg.Style().Border; bp != bd {
			t.Errorf("dialog <%s> border = %+v, page one %+v", tag, bd, bp)
		}
	}
}

func dlgField(t *testing.T, win *qui.Window, tag string) *htmlcss.El {
	t.Helper()
	for _, ov := range win.Overlays() {
		ov.Layout(qui.Rect{W: 800, H: 600})
		if el := findTag(ov, tag); el != nil {
			return el
		}
	}
	t.Fatalf("no <%s> in the dialog", tag)
	return nil
}

func backingInput(el *htmlcss.El) *widgets.Input {
	for _, c := range el.ChildList() {
		if in, ok := c.(*widgets.Input); ok {
			return in
		}
	}
	return nil
}

// Text sits at border + padding from the field's edge, and the box keeps
// its CSS height with the line centred in it (the r64 container rule,
// applied to a control).
func TestInputTextInsetIsBorderPlusPadding(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	css := `input { border: 3px solid #000000; border-radius: 4px; padding: 0 9px; height: 40px; box-sizing: border-box; font-size: 14px; }`
	rt := h.Mount(win, css, func() h.Node { return h.Div(h.Input().Value("H")) })
	root := rt.Root().(*htmlcss.El)
	root.Layout(qui.Rect{W: 400, H: 200})
	el := findTag(root, "input")
	b := el.Bounds()
	if b.H != 40 {
		t.Fatalf("input height = %v, want 40", b.H)
	}
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	cv := qui.NewImageCanvas(img)
	cv.Clear(qui.Color{R: 1, G: 1, B: 1, A: 1})
	root.Draw(cv)
	left, top, bot := 1<<30, 1<<30, -1
	for y := int(b.Y) + 4; y < int(b.Y+b.H)-4; y++ {
		for x := int(b.X) + 4; x < int(b.X+b.W)-4; x++ {
			c := img.RGBAAt(x, y)
			if c.R < 128 {
				if x-int(b.X) < left {
					left = x - int(b.X)
				}
				if y-int(b.Y) < top {
					top = y - int(b.Y)
				}
				if y-int(b.Y) > bot {
					bot = y - int(b.Y)
				}
			}
		}
	}
	if left < 12 || left > 14 {
		t.Errorf("text starts %d px from the edge, want border 3 + padding 9 = 12 (+ bearing)", left)
	}
	if mid := float32(top+bot) / 2; mid < 18 || mid > 22 {
		t.Errorf("text is centred at y=%v in a 40 px field", mid)
	}
}
