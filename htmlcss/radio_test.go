package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// collectRadios returns every El backed by a RadioButton, in tree order.
func collectRadios(w qui.Widget) []*El {
	var out []*El
	var walk func(qui.Widget)
	walk = func(w qui.Widget) {
		if e, ok := w.(*El); ok {
			if _, isRadio := e.backing.(*widgets.RadioButton); isRadio {
				out = append(out, e)
			}
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(w)
	return out
}

// P0.2: <input type=radio> must build a real RadioButton (not fall through
// to a text Input), and radios sharing a name must form one exclusive group.
func TestRadioInputGroupsByName(t *testing.T) {
	res := RenderDoc(`<body>
		<input type="radio" name="plan" value="Free" checked>
		<input type="radio" name="plan" value="Pro">
		<input type="radio" name="plan" value="Team">
	</body>`, ``, Options{})

	radios := collectRadios(res.Root)
	if len(radios) != 3 {
		t.Fatalf("got %d radio-backed elements, want 3 (radios fell back to text inputs?)", len(radios))
	}

	rb0 := radios[0].backing.(*widgets.RadioButton)
	rb1 := radios[1].backing.(*widgets.RadioButton)
	rb2 := radios[2].backing.(*widgets.RadioButton)

	// One shared group.
	if radios[0].radioGrp == nil || radios[0].radioGrp != radios[1].radioGrp || radios[1].radioGrp != radios[2].radioGrp {
		t.Fatal("radios with the same name did not share one RadioGroup")
	}
	// Label comes from the value attribute.
	if rb0.Label != "Free" {
		t.Errorf("radio label = %q, want %q", rb0.Label, "Free")
	}
	// `checked` selected the first.
	if !rb0.Checked || rb1.Checked || rb2.Checked {
		t.Fatalf("initial selection wrong: %v %v %v, want true false false", rb0.Checked, rb1.Checked, rb2.Checked)
	}

	// Selecting a peer clears the previously-selected one, and pushes state
	// back onto the elements (e.checked mirrors the group).
	radios[2].radioGrp.Select(rb2)
	if rb0.Checked || rb1.Checked || !rb2.Checked {
		t.Errorf("after Select(rb2): %v %v %v, want false false true", rb0.Checked, rb1.Checked, rb2.Checked)
	}
	if radios[0].checked || !radios[2].checked {
		t.Errorf("element checked state not synced by group: el0=%v el2=%v", radios[0].checked, radios[2].checked)
	}
}

// A different name means a different, independent group.
func TestRadioSeparateGroups(t *testing.T) {
	res := RenderDoc(`<body>
		<input type="radio" name="a" value="1">
		<input type="radio" name="b" value="2">
	</body>`, ``, Options{})
	radios := collectRadios(res.Root)
	if len(radios) != 2 {
		t.Fatalf("got %d radios, want 2", len(radios))
	}
	if radios[0].radioGrp == radios[1].radioGrp {
		t.Error("radios with different names share a group; they must not")
	}
}
