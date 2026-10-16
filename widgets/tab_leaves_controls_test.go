package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// Tab keydown reaches the focused control before it navigates (DOM
// order), so every built-in focusable must let an unmodified Tab through:
// focus moves on and no text field takes it as input.
func TestTabLeavesEveryControl(t *testing.T) {
	ta := NewTextArea("")
	ta.SetText("ab")
	in := NewInput("x")
	in.SetText("cd")
	cases := []Widget{ta, in, NewButton("b", nil), NewCheckBox("c", nil), NewSlider(0, 10, 5, nil), NewSwitch("s", nil)}
	for _, w := range cases {
		after := NewButton("after", nil)
		win := NewTestWindow(Size{W: 400, H: 300})
		win.SetRoot(NewContainer(FlexLayout{Direction: Vertical}, w, after))
		win.SetFocus(w)
		if win.Focused() != w {
			t.Errorf("%T: setup focus failed (focused %T)", w, win.Focused())
			continue
		}
		k, _, _ := ParseShortcut("Tab")
		win.DispatchTestEvent(NewKeyEvent(EventKeyDown, k, 0))
		win.DispatchTestEvent(NewKeyEvent(EventKeyUp, k, 0))
		if win.Focused() != Widget(after) {
			t.Errorf("%T: Tab left focus on %T", w, win.Focused())
		}
	}
	if ta.GetText() != "ab" || in.GetText() != "cd" {
		t.Errorf("Tab edited text: textarea %q input %q", ta.GetText(), in.GetText())
	}
}
