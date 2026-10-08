package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func selectWidget(t *testing.T, w qui.Widget) (*El, *widgets.Select) {
	t.Helper()
	el, ok := w.(*El)
	if !ok {
		t.Fatalf("widget %T is not an *El", w)
	}
	el.ensureBacking()
	sel, ok := el.backing.(*widgets.Select)
	if !ok {
		t.Fatalf("element backing is %T, not *widgets.Select", el.backing)
	}
	return el, sel
}

// The form must submit the selected option's `value`, not its visible text —
// the semantic the flat option list used to drop entirely.
func TestSelectSubmitsOptionValue(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><form id="f">
		   <select id="s" name="country">
		     <option value="us">United States</option>
		     <option value="de">Germany</option>
		   </select>
		 </form></body>`,
		``,
		Options{Window: win},
	)
	el, sel := selectWidget(t, res.ByID["s"])
	if got := sel.Items; len(got) != 2 || got[0] != "United States" {
		t.Fatalf("dropdown labels = %v, want the option TEXT", got)
	}

	var data map[string]string
	res.ByID["f"].(*El).SetOnFormSubmit(func(d map[string]string) { data = d })

	sel.SelectedIdx = 1
	res.ByID["f"].(*El).submitForm()
	if data["country"] != "de" {
		t.Errorf("submitted %q, want the option's value %q", data["country"], "de")
	}
	// An option with no `value` submits its text (the HTML fallback).
	if got := el.selectValueAt(0); got != "us" {
		t.Errorf("selectValueAt(0) = %q, want %q", got, "us")
	}
}

func TestSelectValueFallsBackToLabel(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><form id="f"><select id="s" name="pick">
		   <option>Plain</option>
		 </select></form></body>`,
		``,
		Options{Window: win},
	)
	_, sel := selectWidget(t, res.ByID["s"])
	sel.SelectedIdx = 0
	var data map[string]string
	res.ByID["f"].(*El).SetOnFormSubmit(func(d map[string]string) { data = d })
	res.ByID["f"].(*El).submitForm()
	if data["pick"] != "Plain" {
		t.Errorf("submitted %q, want the label as fallback value", data["pick"])
	}
}

// `<option selected>` sets the initial selection, and a form reset restores it.
func TestSelectSelectedAttributeAndReset(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><form id="f"><select id="s" name="size">
		   <option value="s">Small</option>
		   <option value="m" selected>Medium</option>
		   <option value="l">Large</option>
		 </select></form></body>`,
		``,
		Options{Window: win},
	)
	el, sel := selectWidget(t, res.ByID["s"])
	if sel.SelectedIdx != 1 {
		t.Fatalf("initial SelectedIdx = %d, want 1 (the `selected` option)", sel.SelectedIdx)
	}
	sel.SelectedIdx = 2
	el.resetControl()
	if sel.SelectedIdx != 1 {
		t.Errorf("after reset SelectedIdx = %d, want 1", sel.SelectedIdx)
	}
}

// `<option disabled>` renders but cannot be selected, and an `<optgroup>`
// contributes an inert heading row so group names stay visible.
func TestSelectDisabledOptionsAndOptgroupHeadings(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><select id="s" name="x">
		   <option value="a">Alpha</option>
		   <option value="b" disabled>Beta (soon)</option>
		   <optgroup label="Europe">
		     <option value="de">Germany</option>
		   </optgroup>
		   <optgroup label="Nowhere" disabled>
		     <option value="zz">Atlantis</option>
		   </optgroup>
		 </select></body>`,
		``,
		Options{Window: win},
	)
	_, sel := selectWidget(t, res.ByID["s"])

	want := []string{"Alpha", "Beta (soon)", "Europe", "Germany", "Nowhere", "Atlantis"}
	if len(sel.Items) != len(want) {
		t.Fatalf("items = %v, want %v", sel.Items, want)
	}
	for i := range want {
		if sel.Items[i] != want[i] {
			t.Fatalf("items = %v, want %v", sel.Items, want)
		}
	}
	// index: 0 Alpha ok, 1 Beta disabled, 2 "Europe" heading, 3 Germany ok,
	// 4 "Nowhere" heading, 5 Atlantis (inside a disabled group).
	for _, tc := range []struct {
		idx      int
		disabled bool
	}{{0, false}, {1, true}, {2, true}, {3, false}, {4, true}, {5, true}} {
		got := tc.idx < len(sel.ItemDisabled) && sel.ItemDisabled[tc.idx]
		if got != tc.disabled {
			t.Errorf("option %d (%q) disabled = %v, want %v", tc.idx, sel.Items[tc.idx], got, tc.disabled)
		}
	}
	// A disabled option cannot become the selection through the keyboard.
	sel.SetFocused(true)
	sel.SelectedIdx = 0
	sel.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyDown, 0))
	if sel.SelectedIdx != 3 {
		t.Errorf("ArrowDown from Alpha landed on %d (%q), want 3 (Germany — skipping disabled rows)",
			sel.SelectedIdx, sel.Items[sel.SelectedIdx])
	}
}

// SetSelectItemColors forwards per-option text colors to the backing Select so
// an HTML <select> can tint its dropdown rows individually (e.g. HTTP methods).
func TestSelectItemColorsReachBacking(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(
		`<body><select id="s"><option>GET</option><option>DELETE</option></select></body>`,
		``,
		Options{Window: win},
	)
	el, sel := selectWidget(t, res.ByID["s"])
	colors := []qui.Color{{G: 1, A: 1}, {R: 1, A: 1}}
	el.SetSelectItemColors(colors)
	if len(sel.ItemColors) != 2 || sel.ItemColors[0] != colors[0] || sel.ItemColors[1] != colors[1] {
		t.Errorf("backing ItemColors = %+v, want %+v", sel.ItemColors, colors)
	}
}

// The color swatch must submit what the user PICKED, not its initial value.
func TestColorInputSubmitsPickedValue(t *testing.T) {
	res := RenderDoc(
		`<body><form id="f"><input id="c" type="color" name="tint" value="#000000"></form></body>`,
		``,
		Options{},
	)
	c := res.ByID["c"].(*El)
	var data map[string]string
	res.ByID["f"].(*El).SetOnFormSubmit(func(d map[string]string) { data = d })

	res.ByID["f"].(*El).submitForm()
	if data["tint"] != "#000000" {
		t.Errorf("initial submit = %q, want #000000", data["tint"])
	}
	c.setColorValue("#3366ff")
	res.ByID["f"].(*El).submitForm()
	if data["tint"] != "#3366ff" {
		t.Errorf("after picking, submit = %q, want #3366ff", data["tint"])
	}
	// A reset returns the swatch to its `value` attribute.
	c.resetControl()
	res.ByID["f"].(*El).submitForm()
	if data["tint"] != "#000000" {
		t.Errorf("after reset, submit = %q, want #000000", data["tint"])
	}
}
