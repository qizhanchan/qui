package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// labelDoc renders html into a test window so clicks go through real
// dispatch (hit testing, bubbling) rather than calling handlers directly.
func labelDoc(t *testing.T, html, css string) (RenderResult, *qui.Window) {
	t.Helper()
	res := RenderDoc(html, css, Options{})
	win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
	win.SetRoot(res.Root)
	res.Root.Measure(qui.Size{W: 400, H: 0})
	res.Root.Layout(qui.Rect{W: 400, H: 200})
	return res, win
}

// clickEl presses and releases the left button at a point inside el,
// dx from its left edge, vertically centered.
func clickEl(win *qui.Window, el *El, dx float32) {
	b := el.Bounds()
	p := qui.LocalPointToWindow(el, qui.Point{X: b.X + dx, Y: b.Y + b.H/2})
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, p.X, p.Y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, p.X, p.Y, qui.MouseButtonLeft, 0))
}

// A <label> without for= labels the first form control inside it: a click
// on its text activates that control, as in the browser.
func TestLabelImplicitControlRadio(t *testing.T) {
	res, win := labelDoc(t, `<body>
		<label id="la" class="row"><input id="a" type="radio" name="g" checked><span>Merge incoming changes</span></label>
		<label id="lb" class="row"><input id="b" type="radio" name="g"><span>Rebase the current branch</span></label>
	</body>`, `.row { display: flex; flex-direction: row; align-items: center; gap: 6px; }
		input { width: 14px; height: 14px; }`)
	a, b := res.ByID["a"].(*El), res.ByID["b"].(*El)
	a.ensureBacking()
	b.ensureBacking()
	ra, rb := a.backing.(*widgets.RadioButton), b.backing.(*widgets.RadioButton)

	clickEl(win, res.ByID["lb"].(*El), 80)
	if !rb.Checked || ra.Checked {
		t.Fatalf("after clicking label b's text: a=%v b=%v, want b selected", ra.Checked, rb.Checked)
	}
	clickEl(win, res.ByID["la"].(*El), 80)
	if !ra.Checked || rb.Checked {
		t.Fatalf("after clicking label a's text: a=%v b=%v, want a selected", ra.Checked, rb.Checked)
	}
}

// Clicking the label's text toggles a wrapped checkbox once; clicking the
// checkbox itself also toggles it exactly once (the label must not
// re-activate a control that already handled the click).
func TestLabelImplicitControlCheckbox(t *testing.T) {
	res, win := labelDoc(t, `<body>
		<label id="l"><input id="cb" type="checkbox"> I accept the terms</label>
	</body>`, `input { width: 14px; height: 14px; }`)
	cb := res.ByID["cb"].(*El)
	cb.ensureBacking()
	box := cb.backing.(*widgets.CheckBox)
	toggles := 0
	box.OnChange = func(bool) { toggles++ }

	clickEl(win, res.ByID["l"].(*El), 60)
	if !box.Checked || toggles != 1 {
		t.Fatalf("label text click: checked=%v toggles=%d, want true/1", box.Checked, toggles)
	}
	clickEl(win, cb, 7)
	if box.Checked || toggles != 2 {
		t.Fatalf("checkbox click: checked=%v toggles=%d, want false/2", box.Checked, toggles)
	}
}

// A disabled control is not activated through its label.
func TestLabelImplicitControlDisabled(t *testing.T) {
	res, win := labelDoc(t, `<body>
		<label id="l" style="display: flex"><input id="cb" type="checkbox" disabled><span>Locked option</span></label>
	</body>`, `input { width: 14px; height: 14px; }`)
	cb := res.ByID["cb"].(*El)
	cb.ensureBacking()
	box := cb.backing.(*widgets.CheckBox)
	clickEl(win, res.ByID["l"].(*El), 60)
	if box.Checked {
		t.Error("clicking the label of a disabled checkbox should not check it")
	}
}
