package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// <input type="checkbox" switch> renders a widgets.Switch with checkbox
// semantics: `checked` maps to On, toggling relinks :checked, accent-color
// tints the on-state track, and form serialization treats it as a checkbox.
func TestInputSwitchRendersSwitch(t *testing.T) {
	css := `input { accent-color: #ff0000; }`
	html := `<body>
		<input id="sw" type="checkbox" switch checked name="wifi">
		<input id="cb" type="checkbox">
	</body>`
	res := RenderDoc(html, css, Options{})

	el := res.ByID["sw"].(*El)
	el.ensureBacking()
	sw, ok := el.backing.(*widgets.Switch)
	if !ok {
		t.Fatalf("switch backing = %T, want *widgets.Switch", el.backing)
	}
	if !sw.On {
		t.Error("checked switch should start On")
	}
	if red := (qui.Color{R: 1, A: 1}); sw.OnTrackColor != red {
		t.Errorf("OnTrackColor = %+v, want accent red", sw.OnTrackColor)
	}
	// Plain checkbox stays a CheckBox — the switch attribute is opt-in.
	cb := res.ByID["cb"].(*El)
	cb.ensureBacking()
	if _, ok := cb.backing.(*widgets.CheckBox); !ok {
		t.Errorf("plain checkbox backing = %T, want *widgets.CheckBox", cb.backing)
	}

	// Programmatic SetChecked syncs the widget + the `checked` attribute.
	el.SetChecked(false)
	if sw.On {
		t.Error("SetChecked(false) should turn the switch off")
	}
	if _, has := el.attrs["checked"]; has {
		t.Error("checked attribute should clear with the switch")
	}
	if name, _, ok := el.controlNameValue(); ok {
		t.Errorf("off switch serialized as %q, want omitted", name)
	}
	el.SetChecked(true)
	if name, val, ok := el.controlNameValue(); !ok || name != "wifi" || val != "on" {
		t.Errorf("on switch serialized as (%q,%q,%v), want (wifi,on,true)", name, val, ok)
	}

	// A user toggle (the widget flips On, then fires OnChange) relinks
	// state + fires onToggle.
	fired := false
	el.SetOnToggle(func(on bool) { fired = true })
	sw.On = false
	sw.OnChange(false)
	if fired != true || el.checkedNow() {
		t.Errorf("user toggle: fired=%v checkedNow=%v, want true/false", fired, el.checkedNow())
	}
}

// The HTML `title` attribute becomes the widget tooltip (block elements and
// atomic inline elements; folded spans have no widget to carry it).
func TestTitleAttrBecomesTooltip(t *testing.T) {
	res := RenderDoc(`<body>
		<button id="b" title="Save the file">Save</button>
		<div id="d" title="A panel">content</div>
	</body>`, ``, Options{})
	if tip := res.ByID["b"].(*El).TooltipText(); tip != "Save the file" {
		t.Errorf("button tooltip = %q", tip)
	}
	if tip := res.ByID["d"].(*El).TooltipText(); tip != "A panel" {
		t.Errorf("div tooltip = %q", tip)
	}
}

// SetCSS swaps the stylesheet at runtime and recascades the mounted tree —
// the hook a theme switcher (e.g. a new palette) uses.
func TestSetCSSRestylesLiveTree(t *testing.T) {
	res := RenderDoc(`<body><div id="d" class="card">x</div></body>`,
		`.card { background: #ff0000; }`, Options{})
	el := res.ByID["d"].(*El)
	if bg := el.Style().Background; bg != (qui.Color{R: 1, A: 1}) {
		t.Fatalf("initial background = %+v, want red", bg)
	}
	res.Engine.SetCSS(`.card { background: #0000ff; }`)
	if bg := el.Style().Background; bg != (qui.Color{B: 1, A: 1}) {
		t.Errorf("after SetCSS background = %+v, want blue", bg)
	}
}
