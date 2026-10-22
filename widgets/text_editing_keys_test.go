package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestInputForwardDelete(t *testing.T) {
	in := NewInput("")
	in.SetText("abc")
	in.SetCursorPos(1)
	in.SetFocused(true)
	in.Handle(NewKeyEvent(EventKeyDown, KeyDelete, 0))
	if in.Text != "ac" || in.CursorPos() != 1 {
		t.Fatalf("after Delete: %q cursor %d, want \"ac\" cursor 1", in.Text, in.CursorPos())
	}
}

func TestTextAreaForwardDelete(t *testing.T) {
	ta := NewTextArea("")
	ta.SetText("abc")
	ta.SetCursorPos(1)
	ta.SetFocused(true)
	ta.Layout(Rect{W: 200, H: 100})
	ta.Handle(NewKeyEvent(EventKeyDown, KeyDelete, 0))
	if ta.Text != "ac" {
		t.Fatalf("after Delete: %q, want \"ac\"", ta.Text)
	}
}

func TestTextAreaShiftDownExtendsSelection(t *testing.T) {
	ta := NewTextArea("")
	ta.SetText("one\ntwo\nthree")
	ta.SetCursorPos(0)
	ta.SetFocused(true)
	ta.Layout(Rect{W: 300, H: 200})
	ta.Handle(NewKeyEvent(EventKeyDown, KeyDown, ModShift))
	if ta.selStart != 0 || ta.selEnd <= 0 {
		t.Fatalf("Shift+Down selection = [%d,%d), want it anchored at 0 and extended", ta.selStart, ta.selEnd)
	}
}
