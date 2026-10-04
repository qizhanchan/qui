package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// CSS `color` and `accent-color` must reach the backing control chrome so
// a dark stylesheet can fully retint form controls (whose native default
// is light). See applyControlColors.
func TestControlColorAndAccentApplied(t *testing.T) {
	css := `
		input, textarea, select { color: #ffffff; accent-color: #ff0000; }
	`
	html := `<body>
		<input id="txt" type="text" value="hi">
		<textarea id="ta"></textarea>
		<select id="sel"><option>a</option></select>
		<input id="cb" type="checkbox">
		<input id="rd" type="radio" name="g">
		<input id="rng" type="range" min="0" max="10">
	</body>`
	res := RenderDoc(html, css, Options{})

	red := qui.Color{R: 1, A: 1}
	white := qui.Color{R: 1, G: 1, B: 1, A: 1}

	txt := res.ByID["txt"].(*El)
	txt.ensureBacking()
	in := txt.backing.(*widgets.Input)
	if fg := in.Style().Foreground; fg != white {
		t.Errorf("input base foreground = %+v, want white", fg)
	}
	// The focused-state copy must track the CSS color too — otherwise text
	// goes black the moment the field is focused (the frozen-copy bug).
	if fg := in.States.Focused.Foreground; fg != white {
		t.Errorf("input focused foreground = %+v, want white (state copy stale)", fg)
	}

	ta := res.ByID["ta"].(*El)
	ta.ensureBacking()
	if fg := ta.backing.(*widgets.TextArea).Style().Foreground; fg != white {
		t.Errorf("textarea foreground = %+v, want white", fg)
	}

	cb := res.ByID["cb"].(*El)
	cb.ensureBacking()
	if c := cb.backing.(*widgets.CheckBox).CheckedFillColor; c != red {
		t.Errorf("checkbox CheckedFillColor = %+v, want red", c)
	}

	rd := res.ByID["rd"].(*El)
	rd.ensureBacking()
	if c := rd.backing.(*widgets.RadioButton).SelectedColor; c != red {
		t.Errorf("radio SelectedColor = %+v, want red", c)
	}

	rng := res.ByID["rng"].(*El)
	rng.ensureBacking()
	if c := rng.backing.(*widgets.Slider).ActiveTrackColor; c != red {
		t.Errorf("slider ActiveTrackColor = %+v, want red", c)
	}
}

// Without authored color/accent-color, controls keep their native (light)
// chrome — the override must not fire, or the raw-HTML alignment breaks.
func TestControlColorsDefaultUnchanged(t *testing.T) {
	res := RenderDoc(`<body><input id="cb" type="checkbox"><input id="txt" type="text"></body>`, ``, Options{})

	cb := res.ByID["cb"].(*El)
	cb.ensureBacking()
	def := widgets.NewCheckBox("", nil)
	if got := cb.backing.(*widgets.CheckBox).CheckedFillColor; got != def.CheckedFillColor {
		t.Errorf("unstyled checkbox fill = %+v, want native default %+v", got, def.CheckedFillColor)
	}

	txt := res.ByID["txt"].(*El)
	txt.ensureBacking()
	defIn := widgets.NewInput("")
	if got := txt.backing.(*widgets.Input).Style().Foreground; got != defIn.Style().Foreground {
		t.Errorf("unstyled input foreground = %+v, want native default %+v", got, defIn.Style().Foreground)
	}
}
