package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// stepper returns the element plus its backing field, laid out so the
// affordance has real geometry.
func stepper(t *testing.T, html string) (*El, *widgets.Input) {
	t.Helper()
	res := RenderDoc(`<body>`+html+`</body>`, ``, Options{})
	el := res.ByID["n"].(*El)
	in := backingInput(t, el)
	in.Layout(qui.Rect{W: 160, H: 30})
	return el, in
}

// clickStepper presses the up or down half of the spinner.
func clickStepper(in *widgets.Input, up bool) {
	r, ok := in.TrailingButtonBounds()
	if !ok {
		panic("no stepper affordance on this field")
	}
	y := r.Y + r.H*0.25
	if !up {
		y = r.Y + r.H*0.75
	}
	in.Handle(qui.NewMouseEvent(qui.EventMouseDown, r.X+r.W/2, y, qui.MouseButtonLeft, 0))
}

func TestNumberStepperStepsByStepAttribute(t *testing.T) {
	_, in := stepper(t, `<input id="n" type="number" value="3" step="0.5">`)
	clickStepper(in, true)
	if in.Text != "3.5" {
		t.Errorf("after up: %q, want 3.5", in.Text)
	}
	clickStepper(in, false)
	clickStepper(in, false)
	if in.Text != "2.5" {
		t.Errorf("after two downs: %q, want 2.5", in.Text)
	}
}

func TestNumberStepperDefaultsToOne(t *testing.T) {
	_, in := stepper(t, `<input id="n" type="number" value="7">`)
	clickStepper(in, true)
	if in.Text != "8" {
		t.Errorf("default step: %q, want 8", in.Text)
	}
}

func TestNumberStepperClampsToMinMax(t *testing.T) {
	_, in := stepper(t, `<input id="n" type="number" value="9" min="0" max="10">`)
	clickStepper(in, true)
	clickStepper(in, true) // would be 11
	if in.Text != "10" {
		t.Errorf("clamped at max: %q, want 10", in.Text)
	}
	in.SetText("0")
	clickStepper(in, false) // would be -1
	if in.Text != "0" {
		t.Errorf("clamped at min: %q, want 0", in.Text)
	}
}

// An empty (or garbage) field starts at the range floor rather than jumping
// to a surprising value.
func TestNumberStepperFromEmpty(t *testing.T) {
	_, in := stepper(t, `<input id="n" type="number" min="5" max="9">`)
	clickStepper(in, true)
	if in.Text != "5" {
		t.Errorf("first up from empty: %q, want the min (5)", in.Text)
	}

	_, noMin := stepper(t, `<input id="n" type="number">`)
	clickStepper(noMin, true)
	if noMin.Text != "0" {
		t.Errorf("first up with no min: %q, want 0", noMin.Text)
	}
}

// Stepping reports through onInput, the same channel a keystroke uses.
func TestNumberStepperFiresOnInput(t *testing.T) {
	el, in := stepper(t, `<input id="n" type="number" value="1">`)
	var seen []string
	el.SetOnInput(func(s string) { seen = append(seen, s) })
	clickStepper(in, true)
	if len(seen) != 1 || seen[0] != "2" {
		t.Errorf("onInput calls = %v, want [2]", seen)
	}
}

// A plain text field gets no spinner.
func TestNoStepperOnTextInput(t *testing.T) {
	_, in := stepper(t, `<input id="n" type="text" value="3">`)
	if in.TrailingButton != widgets.InputTrailingNone {
		t.Errorf("text input got affordance %v, want none", in.TrailingButton)
	}
}
