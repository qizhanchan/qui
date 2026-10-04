package htmlcss

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// input type=password masks its display while keeping the real value.
func TestInputPasswordMasks(t *testing.T) {
	res := RenderDoc(`<body><input id="p" type="password" value="secret"></body>`, ``, Options{})
	el := res.ByID["p"].(*El)
	el.ensureBacking()
	in, ok := el.backing.(*widgets.Input)
	if !ok {
		t.Fatalf("password input backing = %T, want *widgets.Input", el.backing)
	}
	if !in.Password {
		t.Error("password input should have Password=true")
	}
	if in.GetText() != "secret" {
		t.Errorf("GetText() = %q, want the real value", in.GetText())
	}
}

// input type=range renders a Slider honoring min/max/value.
func TestInputRangeSlider(t *testing.T) {
	res := RenderDoc(`<body><input id="r" type="range" min="0" max="10" value="7"></body>`, ``, Options{})
	el := res.ByID["r"].(*El)
	el.ensureBacking()
	sl, ok := el.backing.(*widgets.Slider)
	if !ok {
		t.Fatalf("range input backing = %T, want *widgets.Slider", el.backing)
	}
	if sl.Min != 0 || sl.Max != 10 {
		t.Errorf("slider range = [%v,%v], want [0,10]", sl.Min, sl.Max)
	}
	if sl.Value < 6.9 || sl.Value > 7.1 {
		t.Errorf("slider value = %v, want 7", sl.Value)
	}
	// The value is programmable and readable like any other input's, so a
	// range can be a CONTROLLED control (a scrubber following state).
	el.SetInputValue("3")
	if sl.Value < 2.9 || sl.Value > 3.1 {
		t.Errorf("after SetInputValue slider value = %v, want 3", sl.Value)
	}
	if got := el.InputValue(); got != "3" {
		t.Errorf("InputValue() = %q, want \"3\"", got)
	}
}

// A <label for=cb> click toggles the associated checkbox.
func TestLabelForTogglesCheckbox(t *testing.T) {
	res := RenderDoc(`<body>
		<input id="cb" type="checkbox">
		<label for="cb">Accept</label>
	</body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	cb := res.ByID["cb"].(*El)
	cb.ensureBacking()
	box := cb.backing.(*widgets.CheckBox)
	if box.Checked {
		t.Fatal("checkbox should start unchecked")
	}
	// Find the label and fire its click handler.
	var label *El
	walkEls(res.Root.(*El), func(e *El) {
		if e.tag == "label" {
			label = e
		}
	})
	if label == nil || label.onClick == nil {
		t.Fatal("label[for] should have a click handler")
	}
	label.onClick()
	if !box.Checked {
		t.Error("clicking label[for=cb] should check the checkbox")
	}
}

// A label[for] acts as a control surface: its text must not start a drag
// text-selection (otherwise clicking to toggle the checkbox then moving the
// mouse highlighted the label prose). A plain label stays selectable.
func TestLabelForNotSelectable(t *testing.T) {
	res := RenderDoc(`<body>
		<input id="cb" type="checkbox">
		<label id="lf" for="cb">I accept the terms</label>
		<label id="plain">just a caption</label>
	</body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	lf := res.ByID["lf"].(*El)
	if lf.textLabel == nil {
		t.Fatal("label[for] should fold to a text label")
	}
	if lf.textLabel.Selectable {
		t.Error("label[for] text should be non-selectable (acts as a control)")
	}
	if lf.textLabel.TextSelectionEnabled() {
		t.Error("label[for] must opt out of window-driven selection")
	}
	// A label WITHOUT a for= target stays normal selectable prose.
	plain := res.ByID["plain"].(*El)
	if plain.textLabel != nil && !plain.textLabel.Selectable {
		t.Error("a plain <label> (no for=) should remain selectable")
	}
}

// A push-button <input> (submit/reset/button) renders as a button (box +
// text label = value), NOT an editable text field.
func TestInputButtonTypesRenderAsButtons(t *testing.T) {
	cases := []struct{ html, wantLabel string }{
		{`<input id="b" type="submit">`, "Submit"},
		{`<input id="b" type="reset">`, "Reset"},
		{`<input id="b" type="submit" value="Send it">`, "Send it"},
		{`<input id="b" type="button" value="Poke">`, "Poke"},
	}
	for _, c := range cases {
		res := RenderDoc(`<body>`+c.html+`</body>`, ``, Options{})
		el := res.ByID["b"].(*El)
		if el.isControl() {
			t.Errorf("%s: should not be a backing control", c.html)
		}
		if el.backing != nil {
			t.Errorf("%s: unexpected editable backing %T", c.html, el.backing)
		}
		if el.text != c.wantLabel {
			t.Errorf("%s: label = %q, want %q", c.html, el.text, c.wantLabel)
		}
		// Button-type inputs borrow the <button> UA default. That default is
		// minimal (borderless, no fill) — the borrowed chrome shows up as the
		// rounded corner + padding, not a background box.
		if el.lastCS == nil || el.lastCS.Radius <= 0 {
			t.Errorf("%s: expected borrowed button chrome (border-radius)", c.html)
		}
		if el.lastCS != nil && el.lastCS.HasBackground {
			t.Errorf("%s: minimal button default should have no background box", c.html)
		}
		res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
		if el.textLabel == nil || el.textLabel.Selectable {
			t.Errorf("%s: button label should be a non-selectable face", c.html)
		}
	}
}

// A submit <input> click submits the enclosing form; a reset <input> restores
// controls to their defaults.
func TestInputSubmitAndReset(t *testing.T) {
	res := RenderDoc(`<body>
		<form id="f">
			<input id="name" name="name" type="text" value="alice">
			<input name="ok" type="checkbox" value="yes" checked>
			<input id="send" type="submit" value="Send">
			<input id="clear" type="reset" value="Clear">
		</form>
	</body>`, ``, Options{})

	form := res.ByID["f"].(*El)
	var got map[string]string
	form.SetOnFormSubmit(func(m map[string]string) { got = m })

	// Submit/reset behavior is a built-in click action (runBuiltinClick),
	// invoked at dispatch — NOT wired into e.onClick, which the reconciler
	// would overwrite on each re-render.
	send := res.ByID["send"].(*El)
	if !send.runBuiltinClick() {
		t.Fatal("submit input did not perform its built-in click action")
	}
	if got == nil {
		t.Fatal("submit input did not fire the form handler")
	}
	if got["name"] != "alice" || got["ok"] != "yes" {
		t.Errorf("submitted data = %v, want name=alice ok=yes", got)
	}
	if _, leaked := got[""]; leaked {
		t.Error("push button leaked into form serialization")
	}

	// Mutate the text field, then reset should restore its default value.
	nameEl := res.ByID["name"].(*El)
	nameEl.SetInputValue("bob")
	clear := res.ByID["clear"].(*El)
	if !clear.runBuiltinClick() {
		t.Fatal("reset input did not perform its built-in click action")
	}
	if in := nameEl.backing.(*widgets.Input); in.GetText() != "alice" {
		t.Errorf("after reset, name = %q, want the default alice", in.GetText())
	}
}

func walkEls(e *El, fn func(*El)) {
	fn(e)
	for _, k := range e.elementKids {
		if ke, ok := k.(*El); ok {
			walkEls(ke, fn)
		}
	}
}

// The HTML disabled attribute disables the backing control.
func TestInputDisabledAttr(t *testing.T) {
	res := RenderDoc(`<body><input id="d" disabled><input id="e"></body>`, ``, Options{})
	d := res.ByID["d"].(*El)
	d.ensureBacking()
	if in, ok := d.backing.(*widgets.Input); !ok || in.Enabled() {
		t.Error("input with disabled attr should be disabled")
	}
	e := res.ByID["e"].(*El)
	e.ensureBacking()
	if in, ok := e.backing.(*widgets.Input); !ok || !in.Enabled() {
		t.Error("input without disabled attr should be enabled")
	}
}

// outline sets OutlineWidth/Color on the computed style (drawn as a ring).
func TestOutlineComputed(t *testing.T) {
	res := RenderDoc(`<body><div id="o" style="outline:3px solid red">x</div></body>`, ``, Options{})
	el := res.ByID["o"].(*El)
	if el.lastCS == nil || !el.lastCS.HasOutline {
		t.Fatal("outline should set HasOutline")
	}
	if el.lastCS.OutlineWidth != 3 {
		t.Errorf("outline width = %v, want 3", el.lastCS.OutlineWidth)
	}
	if el.lastCS.OutlineColor.R < 0.9 || el.lastCS.OutlineColor.G > 0.1 {
		t.Errorf("outline color = %+v, want red", el.lastCS.OutlineColor)
	}
}

// <input type=file> renders as a push button (not an editable field). Its
// default label is the "no file" placeholder, and it serializes its
// (initially empty) chosen path when named.
func TestInputFileRendersAsButton(t *testing.T) {
	res := RenderDoc(`<body><input id="f" type="file" name="doc"></body>`, ``, Options{})
	el := res.ByID["f"].(*El)
	if el.isControl() {
		t.Error("file input should not be a backing editable control")
	}
	if el.backing != nil {
		t.Errorf("file input should have no editable backing, got %T", el.backing)
	}
	if !el.isFileInput() {
		t.Error("isFileInput should report true")
	}
	if el.text != "Choose File…" {
		t.Errorf("default label = %q, want %q", el.text, "Choose File…")
	}
	name, val, ok := el.controlNameValue()
	if !ok || name != "doc" || val != "" {
		t.Errorf("controlNameValue = (%q,%q,%v), want (doc,\"\",true)", name, val, ok)
	}
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
	if el.textLabel == nil || el.textLabel.Selectable {
		t.Error("file button label should be a non-selectable face")
	}
}

// A chosen file sets the label to the base name and the serialized value
// to the full path.
func TestInputFileChosenLabelAndValue(t *testing.T) {
	res := RenderDoc(`<body><input id="f" type="file" name="doc"></body>`, ``, Options{})
	el := res.ByID["f"].(*El)
	el.fileValue = "/tmp/photos/holiday.png"
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
	// applyComputed recomputes the label from fileValue on the next restyle.
	el.applyComputed(el.lastCS)
	if el.fileInputLabel() != "holiday.png" {
		t.Errorf("label = %q, want holiday.png", el.fileInputLabel())
	}
	_, val, _ := el.controlNameValue()
	if val != "/tmp/photos/holiday.png" {
		t.Errorf("serialized value = %q, want full path", val)
	}
}

// acceptExtensions keeps extension tokens and drops MIME/wildcard tokens.
func TestAcceptExtensions(t *testing.T) {
	got := acceptExtensions(".PNG, .jpg, image/*, .gif")
	want := []string{"png", "jpg", "gif"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ext[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// <input type=color> renders as a filled swatch button: not editable, its
// box background reflects the value, and a picked color updates it.
func TestInputColorRendersAsSwatch(t *testing.T) {
	res := RenderDoc(`<body><input id="c" type="color" value="#3366ff"></body>`, ``, Options{})
	el := res.ByID["c"].(*El)
	if el.isControl() || el.backing != nil {
		t.Error("color input should not be an editable backing control")
	}
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
	want, _ := parseColor("#3366ff")
	if got := el.Style().Background; got != want {
		t.Errorf("swatch background = %+v, want %+v (value color)", got, want)
	}
	// Picking a new color updates the stored value + repaints on restyle.
	el.setColorValue("#e6194b")
	el.applyComputed(el.lastCS)
	want2, _ := parseColor("#e6194b")
	if got := el.Style().Background; got != want2 {
		t.Errorf("after pick, background = %+v, want %+v", got, want2)
	}
}

// stubIcon is a trivial VectorSource for exercising the icon-decorated
// color-input path.
type stubIcon struct{}

func (stubIcon) Rasterize(w, h int, _ qui.Color) image.Image {
	return image.NewRGBA(image.Rect(0, 0, w, h))
}

// A color input carrying an icon (SetIcon / h.Icon) renders Google-Sheets
// style: the box keeps a neutral background and the picked color shows as a
// bottom border bar under the icon — NOT as a full-box swatch.
func TestInputColorWithIconShowsBottomBar(t *testing.T) {
	res := RenderDoc(`<body><input id="c" type="color" value="#3366ff"></body>`, ``, Options{})
	el := res.ByID["c"].(*El)
	el.SetIcon(stubIcon{})
	el.applyComputed(el.lastCS)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	want, _ := parseColor("#3366ff")
	if got := el.Style().Background; got == want {
		t.Errorf("icon color input should not fill its background with the color, got %+v", got)
	}
	if got := el.Style().BorderColors.Bottom; got != want {
		t.Errorf("bottom border color = %+v, want %+v (picked value)", got, want)
	}
	if el.Style().BorderWidths.Bottom <= 0 {
		t.Errorf("bottom border width = %v, want > 0", el.Style().BorderWidths.Bottom)
	}
}

// A date input (no native picker) gets a format-hint placeholder when the
// author didn't supply one.
func TestInputDateFormatHint(t *testing.T) {
	res := RenderDoc(`<body><input id="d" type="date"></body>`, ``, Options{})
	el := res.ByID["d"].(*El)
	el.ensureBacking()
	in, ok := el.backing.(*widgets.Input)
	if !ok {
		t.Fatalf("date input backing = %T, want *widgets.Input", el.backing)
	}
	if in.Placeholder != "YYYY-MM-DD" {
		t.Errorf("date placeholder = %q, want YYYY-MM-DD", in.Placeholder)
	}
}

// colorToHex round-trips a parsed hex, and the palette grid produces a
// grayscale row (black→white) plus the vivid base row.
func TestColorGridGeneration(t *testing.T) {
	if got := colorToHex(qui.Color{R: 1, G: 0, B: 0, A: 1}); got != "#ff0000" {
		t.Errorf("colorToHex red = %q, want #ff0000", got)
	}
	rows := colorGridRows()
	if len(rows) < 3 {
		t.Fatalf("expected several palette rows, got %d", len(rows))
	}
	gray := rows[0]
	if gray[0] != "#000000" || gray[len(gray)-1] != "#ffffff" {
		t.Errorf("grayscale row = %v, want black→white ends", gray)
	}
	if rows[1][1] != "#ff0000" {
		t.Errorf("vivid row col1 = %q, want #ff0000", rows[1][1])
	}
	// A tint row is lighter than the base; the last shade row is darker.
	lum := func(c qui.Color) float32 { return c.R + c.G + c.B }
	base, _ := parseColor(colorHueBases[1]) // pure red
	tint, _ := parseColor(rows[2][1])       // first tint row
	shade, _ := parseColor(rows[len(rows)-1][1])
	if lum(tint) <= lum(base) {
		t.Errorf("tint %v should be lighter than base %v", tint, base)
	}
	if lum(shade) >= lum(base) {
		t.Errorf("shade %v should be darker than base %v", shade, base)
	}
}

// anchoredPopupPos flips a popup above the anchor when it would overflow
// the bottom, clamps into the viewport otherwise, and keeps X on-screen.
func TestAnchoredPopupPos(t *testing.T) {
	win := qui.Size{W: 800, H: 600}
	pop := qui.Size{W: 300, H: 400}

	// Room below: opens just under the anchor.
	if _, y := anchoredPopupPos(win, pop, qui.Rect{X: 10, Y: 20, W: 48, H: 26}); y != 20+26+4 {
		t.Errorf("room-below y = %v, want %v", y, 20+26+4)
	}
	// Near the bottom (no room below) but room above → flips above.
	anchor := qui.Rect{X: 10, Y: 500, W: 48, H: 26}
	if _, y := anchoredPopupPos(win, pop, anchor); y != 500-400-4 {
		t.Errorf("flip-above y = %v, want %v", y, 500-400-4)
	}
	// Taller than the window from either side → pinned so bottom shows.
	tall := qui.Size{W: 300, H: 590}
	if _, y := anchoredPopupPos(win, tall, qui.Rect{X: 10, Y: 300, W: 48, H: 26}); y != 600-590 {
		t.Errorf("pinned y = %v, want %v", y, 600-590)
	}
	// X overflow on the right is clamped to the window width.
	if x, _ := anchoredPopupPos(win, pop, qui.Rect{X: 700, Y: 20, W: 48, H: 26}); x != 800-300 {
		t.Errorf("clamped x = %v, want %v", x, 800-300)
	}
}

// The color swatch's hover/active feedback must be BORDER-ONLY: changing
// its background would misrepresent the picked color. Real buttons and the
// file picker still get the background-darkening chrome.
func TestColorSwatchStatesBorderOnly(t *testing.T) {
	res := RenderDoc(`<body>
		<input id="c" type="color" value="#3366ff">
		<input id="f" type="file">
	</body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	c := res.ByID["c"].(*El)
	if c.Box.Hover == nil || c.Box.Active == nil {
		t.Fatal("color swatch should have hover/active feedback")
	}
	if c.Box.Hover.Background.A != 0 || c.Box.Active.Background.A != 0 {
		t.Error("color swatch hover/active must NOT set a background (misrepresents the color)")
	}
	if c.Box.Hover.BorderSize <= 0 {
		t.Error("color swatch hover should change the border")
	}
	// The swatch background stays the picked color regardless of state.
	want, _ := parseColor("#3366ff")
	if c.Style().Background != want {
		t.Errorf("swatch bg = %+v, want the value color %+v", c.Style().Background, want)
	}
	// The file picker (a real button) keeps the darkening background chrome.
	f := res.ByID["f"].(*El)
	if f.Box.Hover == nil || f.Box.Hover.Background.A == 0 {
		t.Error("file picker hover should darken the background (button chrome)")
	}
}

// Palette swatch buttons must keep their fill across hover/press/focus —
// only the border ring may change. widgets.NewButton's default states
// darken the background, which would misrepresent the swatch color.
func TestColorPaletteSwatchesKeepFill(t *testing.T) {
	content := colorPaletteContent("#4a86e8", func(string) {})
	var buttons []*widgets.Button
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if b, ok := w.(*widgets.Button); ok {
			buttons = append(buttons, b)
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, k := range c.ChildList() {
				walk(k)
			}
		}
	}
	walk(content)
	if len(buttons) < 10 {
		t.Fatalf("expected many swatch buttons, found %d", len(buttons))
	}
	for _, b := range buttons {
		base := b.States.Base.Background
		if base.A == 0 {
			continue // (shouldn't happen — every swatch has a fill)
		}
		for name, s := range map[string]*qui.Style{"hover": b.States.Hover, "pressed": b.States.Pressed, "focused": b.States.Focused} {
			if s == nil {
				continue
			}
			if s.Background != base {
				t.Errorf("swatch %s state changed fill %+v → %+v (must keep the color)", name, base, s.Background)
			}
		}
		// There IS still a feedback: hover border is thicker than base.
		if b.States.Hover != nil && b.States.Hover.BorderSize <= b.States.Base.BorderSize {
			t.Error("swatch hover should thicken the border ring")
		}
		if b.StateLayerColor.A != 0 {
			t.Error("swatch must have no hover/focus tint overlay")
		}
	}
}
