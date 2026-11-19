package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// findEl returns the first El with the given tag in tree order.
func findEl(w qui.Widget, tag string) *El {
	var found *El
	var walk func(qui.Widget)
	walk = func(w qui.Widget) {
		if found != nil {
			return
		}
		if e, ok := w.(*El); ok && e.tag == tag {
			found = e
			return
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(w)
	return found
}

// TestCheckedLiveRelink: toggling a checkbox at runtime must relink
// :checked-gated CSS (attribute-driven state now updated + scoped restyle).
func TestCheckedLiveRelink(t *testing.T) {
	res := RenderDoc(
		`<body><input id="c" type="checkbox"></body>`,
		`#c:checked { border: 3px solid #00ff00 }`,
		Options{},
	)
	el := res.ByID["c"].(*El)
	if el.lastCS != nil && el.lastCS.BorderWidth == 3 {
		t.Fatal(":checked matched before the box was checked")
	}
	cb := el.backing.(*widgets.CheckBox)

	// Simulate a user toggle: the widget flips Checked then fires OnChange.
	cb.Checked = true
	cb.OnChange(cb.Checked)
	if el.lastCS == nil || el.lastCS.BorderWidth != 3 {
		t.Fatalf(":checked did not relink after toggle-on (BorderWidth=%v)", borderWidthOf(el))
	}

	// Toggling back off must drop the :checked style.
	cb.Checked = false
	cb.OnChange(cb.Checked)
	if el.lastCS.BorderWidth == 3 {
		t.Errorf(":checked style survived toggle-off (BorderWidth=%v)", borderWidthOf(el))
	}
}

func borderWidthOf(e *El) float32 {
	if e.lastCS == nil {
		return -1
	}
	return e.lastCS.BorderWidth
}

// TestDisabledLiveRelink: SetDisabled must relink :disabled CSS and disable
// the backing control.
func TestDisabledLiveRelink(t *testing.T) {
	res := RenderDoc(
		`<body><input id="i" type="text"></body>`,
		`#i:disabled { border: 2px solid #999 }`,
		Options{},
	)
	el := res.ByID["i"].(*El)
	if bw := borderWidthOf(el); bw == 2 {
		t.Fatal(":disabled matched while enabled")
	}
	el.SetDisabled(true)
	if borderWidthOf(el) != 2 {
		t.Fatalf(":disabled did not relink after SetDisabled(true) (BorderWidth=%v)", borderWidthOf(el))
	}
	if in, ok := el.backing.(*widgets.Input); ok && in.Enabled() {
		t.Error("backing input still enabled after SetDisabled(true)")
	}
	el.SetDisabled(false)
	if borderWidthOf(el) == 2 {
		t.Error(":disabled style survived SetDisabled(false)")
	}
}

// TestFormSubmitGathersNamedControls: a submit <button> click serializes the
// form's named controls (text value, checked checkbox, selected radio) and
// omits unchecked / unnamed controls.
func TestFormSubmitGathersNamedControls(t *testing.T) {
	res := RenderDoc(`<body>
		<form id="f">
			<input name="user" type="text" value="alice">
			<input type="text" value="ignored">
			<input name="subscribe" type="checkbox" value="yes" checked>
			<input name="plan" type="radio" value="free">
			<input name="plan" type="radio" value="pro" checked>
			<button type="submit">Go</button>
		</form>
	</body>`, ``, Options{})

	form := res.ByID["f"].(*El)
	var got map[string]string
	form.SetOnFormSubmit(func(m map[string]string) { got = m })

	btn := findEl(res.Root, "button")
	if btn == nil {
		t.Fatal("submit button not found")
	}
	if !btn.runBuiltinClick() {
		t.Fatal("submit button did not perform its built-in click action")
	}

	if got == nil {
		t.Fatal("form submit handler never fired")
	}
	if got["user"] != "alice" {
		t.Errorf("user = %q, want alice", got["user"])
	}
	if got["subscribe"] != "yes" {
		t.Errorf("subscribe = %q, want yes (checked checkbox)", got["subscribe"])
	}
	if got["plan"] != "pro" {
		t.Errorf("plan = %q, want pro (checked radio)", got["plan"])
	}
	if _, unnamed := got[""]; unnamed {
		t.Error("unnamed control leaked into form data")
	}
	if len(got) != 3 {
		t.Errorf("form data = %v, want exactly 3 entries", got)
	}
}

// TestFormSubmitOnEnter: pressing Enter in a text input inside a form submits
// the form.
func TestFormSubmitOnEnter(t *testing.T) {
	res := RenderDoc(`<body>
		<form id="f">
			<input id="q" name="q" type="text" value="hello">
		</form>
	</body>`, ``, Options{})
	form := res.ByID["f"].(*El)
	fired := false
	form.SetOnFormSubmit(func(m map[string]string) {
		fired = true
		if m["q"] != "hello" {
			t.Errorf("q = %q, want hello", m["q"])
		}
	})
	in := res.ByID["q"].(*El).backing.(*widgets.Input)
	if in.OnSubmit == nil {
		t.Fatal("input OnSubmit not wired")
	}
	in.OnSubmit(in.Text) // simulate Enter
	if !fired {
		t.Error("Enter in a form field did not submit the form")
	}
}

// A <button type=reset> resets its form like <input type=reset>, after
// asking the engine's reset handler (the page's cancelable reset event);
// DocSearch clears its query from that event and cancels the default.
func TestButtonResetAsksHandlerFirst(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><body>
		<form id="f"><input id="q" type="search" value="start"><button id="r" type="reset">x</button></form>
	</body>`)
	q, r := res.ByID["q"].(*El), res.ByID["r"].(*El)
	var asked *El
	cancel := true
	res.Engine.SetFormResetHandler(func(form *El) bool { asked = form; return !cancel })

	q.SetInputValue("typed")
	if !r.runBuiltinClick() {
		t.Fatal("reset button did not act")
	}
	if asked != res.ByID["f"].(*El) {
		t.Fatalf("reset handler asked with %v, want the form", asked)
	}
	if got := q.InputValue(); got != "typed" {
		t.Fatalf("canceled reset changed the value to %q", got)
	}
	cancel = false
	r.runBuiltinClick()
	if got := q.InputValue(); got != "start" {
		t.Fatalf("value after reset = %q, want start", got)
	}
}

// ::-webkit-search-cancel-button / ::-webkit-inner-spin-button
// { display: none } remove the UA clear button and spinner.
func TestHiddenSearchCancelAndSpinButtons(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><head><style>
.bare::-webkit-search-cancel-button, .bare::-webkit-inner-spin-button { display: none }
</style></head><body>
<input id="s1" type="search"><input id="s2" class="bare" type="search">
<input id="n1" type="number"><input id="n2" class="bare" type="number">
</body>`)
	want := map[string]widgets.InputTrailingButton{
		"s1": widgets.InputTrailingClear, "s2": widgets.InputTrailingNone,
		"n1": widgets.InputTrailingStepper, "n2": widgets.InputTrailingNone,
	}
	for id, w := range want {
		in, ok := res.ByID[id].(*El).backing.(*widgets.Input)
		if !ok {
			t.Fatalf("%s has no Input backing", id)
		}
		if in.TrailingButton != w {
			t.Errorf("%s trailing button = %v, want %v", id, in.TrailingButton, w)
		}
	}
	// The class goes away: the clear button comes back.
	res.ByID["s2"].(*El).SetClass("")
	res.Engine.FlushPendingStyles()
	if in := res.ByID["s2"].(*El).backing.(*widgets.Input); in.TrailingButton != widgets.InputTrailingClear {
		t.Errorf("s2 after class removal = %v, want clear", in.TrailingButton)
	}
}
