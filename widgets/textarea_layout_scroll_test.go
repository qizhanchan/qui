package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// Text set before the field has a size (an htmlcss textarea's value is
// applied when its backing is created) must still show once the field is
// laid out: the scroll that kept the caret visible in the zero-sized box
// is clamped to the real size.
func TestTextAreaTextSetBeforeLayoutIsVisible(t *testing.T) {
	ta := NewTextArea("placeholder")
	ta.SetText("hello world")
	ta.SetCursorPos(0)
	ta.Layout(Rect{W: 300, H: 60})
	rc := &RecordingCanvas{}
	ta.Draw(rc)
	if len(rc.Texts) == 0 {
		t.Fatal("no text drawn")
	}
	content, _, _ := ta.visibleContentRect(ta.Bounds())
	if got := rc.Texts[0]; got.X != content.X || got.Y != content.Y {
		t.Errorf("text drawn at %v,%v, want the content origin %v,%v (scrolled out)", got.X, got.Y, content.X, content.Y)
	}
}
