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

// The dialog field is Google's outlined field: 14 px text, 12 px from the
// outer edge to the text (1 px border + 11 px padding), a darker outline on
// hover, a 2 px accent outline on focus and a faded one when disabled -
// all through CSS state rules on the <input>.
func TestDialogFieldStatesAndMetrics(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	h.Mount(win, ``, func() h.Node {
		return h.Div(h.Dialog(h.DialogProps{Title: "T", Body: h.Div(h.Input().Value("x"), h.Input().Disabled(true))}))
	})
	var fields []*widgets.Input
	for _, ov := range win.Overlays() {
		ov.Layout(qui.Rect{W: 800, H: 600})
		var walk func(w qui.Widget)
		walk = func(w qui.Widget) {
			if in, ok := w.(*widgets.Input); ok {
				fields = append(fields, in)
			}
			if cl, ok := w.(qui.ChildLister); ok {
				for _, c := range cl.ChildList() {
					walk(c)
				}
			}
		}
		walk(ov)
	}
	if len(fields) != 2 {
		t.Fatalf("found %d inputs, want 2", len(fields))
	}
	rgb := func(r, g, b float32) qui.Color { return qui.Color{R: r / 255, G: g / 255, B: b / 255, A: 1} }
	in := fields[0]
	base := in.States.Base
	if base.Font.Size != 14 || base.Padding.Left != 11 || base.Padding.Right != 11 || base.Padding.Top != 9 || base.BorderSize != 1 || base.Radius != 4 {
		t.Errorf("base: font %v pad %v border %v radius %v", base.Font.Size, base.Padding, base.BorderSize, base.Radius)
	}
	if base.Border != rgb(116, 119, 117) {
		t.Errorf("base border = %v, want #747775", base.Border)
	}
	if b := in.Bounds(); b.H != 40 {
		t.Errorf("field height = %v, want 40", b.H)
	}
	if got := in.States.Resolve(qui.StateHover).Border; got != rgb(31, 31, 31) {
		t.Errorf(":hover border = %v, want #1f1f1f", got)
	}
	f := in.States.Resolve(qui.StateFocused)
	if f.BorderSize != 2 || f.Border != rgb(11, 87, 208) {
		t.Errorf(":focus border = %v / %v, want 2px #0b57d0", f.Border, f.BorderSize)
	}
	// Hovering the focused field keeps the focus outline.
	if f := in.States.Resolve(qui.StateFocused | qui.StateHover); f.Border != rgb(11, 87, 208) {
		t.Errorf(":focus:hover border = %v, want the accent", f.Border)
	}
	d := fields[1].States.Resolve(qui.StateDisabled)
	if d.Border.A >= 0.2 || d.Foreground.A >= 0.5 {
		t.Errorf(":disabled border %v / text %v, want faded", d.Border, d.Foreground)
	}
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
