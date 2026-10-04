package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// A press on the scrollbar thumb must stay a scrollbar drag: the
// provisional blank-press selection arm is disarmed when the ScrollView
// consumes the MouseDown, so dragging the thumb scrolls instead of
// sweeping a text selection over the content.
func TestScrollbarDragDoesNotStartTextSelection(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 100})
	sv := NewScrollView()
	label := NewLabel("first block of copyable text that is fairly long and wraps across the tall content area")
	label.Paragraph.Wrap = true
	sv.SetContent(label, Size{W: 180, H: 600})
	win.SetRoot(sv)
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 100})

	// Thumb sits at the top-right at scroll offset 0.
	tx := float32(200) - 3
	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, tx, 5, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseMove, tx, 45, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, tx, 45, MouseButtonLeft, 0))

	if sv.ScrollOffset() <= 0 {
		t.Fatal("thumb drag did not scroll — test point missed the scrollbar")
	}
	if got := label.SelectedText(); got != "" {
		t.Errorf("thumb drag selected %q, want no text selection", got)
	}
}
