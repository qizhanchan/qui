package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func clearableInput(text string) *Input {
	in := NewInput("Search")
	in.TrailingButton = InputTrailingClear
	in.SetText(text)
	in.Layout(Rect{X: 0, Y: 0, W: 200, H: 30})
	return in
}

// The clear button appears only when there is something to clear (browser
// behavior) and reserves text space only while visible.
func TestInputClearButtonVisibility(t *testing.T) {
	empty := clearableInput("")
	if empty.showTrailingButton() {
		t.Error("clear button should be hidden on an empty field")
	}
	if w := empty.trailingButtonWidth(); w != 0 {
		t.Errorf("hidden affordance reserved %v px of text space", w)
	}

	full := clearableInput("hello")
	if !full.showTrailingButton() {
		t.Fatal("clear button should show once the field has text")
	}
	if full.trailingButtonWidth() <= 0 {
		t.Error("visible affordance must reserve trailing space")
	}
	// The text area shrinks by exactly the reserved width.
	if got, want := full.inputBounds().W, empty.inputBounds().W-full.trailingButtonWidth(); got != want {
		t.Errorf("text width = %v, want %v (reserved for the affordance)", got, want)
	}

	// A disabled field shows no clear button.
	full.SetEnabled(false)
	if full.showTrailingButton() {
		t.Error("clear button should be hidden while disabled")
	}
}

// Clicking the ✕ empties the field, fires OnChange, and does NOT also move
// the caret into the text (the press is consumed).
func TestInputClearButtonClick(t *testing.T) {
	in := clearableInput("hello")
	var changes []string
	in.OnChange = func(s string) { changes = append(changes, s) }

	r := in.trailingButtonRect()
	consumed := in.Handle(NewMouseEvent(EventMouseDown, r.X+r.W/2, r.Y+r.H/2, MouseButtonLeft, 0))
	if !consumed {
		t.Error("the affordance must consume its own press")
	}
	if in.Text != "" {
		t.Errorf("field text = %q, want empty", in.Text)
	}
	if len(changes) != 1 || changes[0] != "" {
		t.Errorf("OnChange calls = %v, want one empty-string call", changes)
	}
	if in.cursorPos != 0 {
		t.Errorf("caret = %d, want 0", in.cursorPos)
	}
}

// A press in the text area is unaffected by the affordance.
func TestInputClearButtonIgnoresTextClicks(t *testing.T) {
	in := clearableInput("hello world")
	in.Handle(NewMouseEvent(EventMouseDown, in.inputBounds().X+2, in.Bounds().Y+10, MouseButtonLeft, 0))
	if in.Text != "hello world" {
		t.Errorf("clicking the text cleared the field: %q", in.Text)
	}
}

// OnTrailingClick overrides the built-in action.
func TestInputTrailingClickOverride(t *testing.T) {
	in := clearableInput("keep me")
	called := false
	in.OnTrailingClick = func() { called = true }
	r := in.trailingButtonRect()
	in.Handle(NewMouseEvent(EventMouseDown, r.X+r.W/2, r.Y+r.H/2, MouseButtonLeft, 0))
	if !called {
		t.Error("OnTrailingClick was not invoked")
	}
	if in.Text != "keep me" {
		t.Errorf("override should suppress the default clear, text = %q", in.Text)
	}
}

// A plain field has no affordance and loses no text width.
func TestInputNoTrailingButtonByDefault(t *testing.T) {
	in := NewInput("x")
	in.SetText("abc")
	in.Layout(Rect{W: 200, H: 30})
	if in.showTrailingButton() || in.trailingButtonWidth() != 0 {
		t.Error("a default input must not reserve trailing affordance space")
	}
}
