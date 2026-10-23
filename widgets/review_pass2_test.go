package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// The open list starts on the selected option: Enter keeps it.
func TestSelectOpensOnSelectedOption(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B", "C", "D"}, nil)
	w.SetRoot(cb)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})
	cb.SelectedIdx = 2
	cb.SetFocused(true)
	cb.Handle(newKeyDown(KeySpace))
	if !cb.isOpen {
		t.Fatal("Space did not open the list")
	}
	cb.Handle(newKeyDown(KeyEnter))
	if cb.SelectedIdx != 2 {
		t.Fatalf("Enter on the opened list selected %d, want 2", cb.SelectedIdx)
	}
}

// An OnCommit that normalizes the value via SetText commits once, not
// again on the following blur.
func TestInputCommitNormalizingSetTextCommitsOnce(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	in := NewInput("")
	win.SetRoot(in)
	in.Layout(Rect{W: 300, H: 40})
	var commits []string
	in.OnCommit = func(s string) {
		commits = append(commits, s)
		in.SetText(s + ".00")
	}
	win.SetFocus(in)
	win.DispatchTestEvent(NewCharEvent('1', 0))
	win.DispatchTestEvent(NewCharEvent('2', 0))
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyEnter, 0))
	win.SetFocus(nil)
	if len(commits) != 1 || commits[0] != "12" {
		t.Fatalf("commits = %v, want [12]", commits)
	}
}

// Deleting from the end of overflowing text scrolls the start back in
// instead of leaving blank space on the right.
func TestInputScrollClampsWhenTextShrinks(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	in := NewInput("")
	win.SetRoot(in)
	in.Layout(Rect{W: 100, H: 30})
	win.SetFocus(in)
	for i := 0; i < 60; i++ {
		win.DispatchTestEvent(NewCharEvent('m', 0))
	}
	if in.scrollX <= 0 {
		t.Fatalf("long text did not scroll (scrollX %v)", in.scrollX)
	}
	for i := 0; i < 55; i++ {
		win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyBackspace, 0))
	}
	if max := in.widthUpToRune(5) - in.inputBounds().W + 4; in.scrollX > max && in.scrollX > 0 {
		t.Fatalf("scrollX %v past the text's end (max %v)", in.scrollX, max)
	}
}
