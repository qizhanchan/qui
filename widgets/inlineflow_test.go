package widgets

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestInlineBoxPositionsAtomicChild(t *testing.T) {
	ib := NewInlineBox()
	f := qui.Font{Size: 16}
	ib.AddText("before ", f, qui.ColorBlack, 0, "")

	badge := NewBox(qui.FlowLayout{})
	badge.Style().Width = 24
	badge.Style().Height = 16
	ib.AddBox(badge, qui.InlineBaseline)

	ib.AddText(" after", f, qui.ColorBlack, 0, "")

	if len(ib.ChildList()) != 1 {
		t.Fatalf("expected 1 atomic child, got %d", len(ib.ChildList()))
	}

	sz := ib.Measure(qui.Size{W: 10000})
	if sz.W <= 24 || sz.H <= 0 {
		t.Fatalf("measure returned %v, expected width past the badge and positive height", sz)
	}

	ib.Layout(qui.Rect{X: 100, Y: 50, W: 10000, H: sz.H})
	bb := badge.Bounds()
	if bb.W != 24 || bb.H != 16 {
		t.Errorf("badge sized %vx%v, want 24x16", bb.W, bb.H)
	}
	// The badge must land inside the InlineBox bounds and to the right of
	// the leading text (x advanced past "before ").
	if bb.X <= 100 {
		t.Errorf("badge x=%v should be past the leading text at 100", bb.X)
	}
	if !ib.Bounds().Contains(qui.Point{X: bb.X + 1, Y: bb.Y + 1}) {
		t.Errorf("badge at %v not within inline box bounds %v", bb, ib.Bounds())
	}
}

func TestInlineBoxDrawsTextAndChild(t *testing.T) {
	ib := NewInlineBox()
	f := qui.Font{Size: 16}
	ib.AddText("hello ", f, qui.ColorBlack, 0, "")
	badge := NewBox(qui.FlowLayout{})
	badge.Style().Width = 20
	badge.Style().Height = 20
	badge.Style().Background = qui.Color{R: 1, A: 1}
	ib.AddBox(badge, qui.InlineMiddle)

	sz := ib.Measure(qui.Size{W: 10000})
	ib.Layout(qui.Rect{X: 0, Y: 0, W: 10000, H: sz.H})

	var rec qui.RecordingCanvas
	ib.Draw(&rec)
	if len(rec.Texts) == 0 {
		t.Error("expected the inline box to draw its text run")
	}
	if len(rec.Fills) == 0 {
		t.Error("expected the atomic child (red badge) to paint a fill")
	}
}

// InlineBox participates in window-level cross-widget text selection:
// point→offset→text mappings are mutually consistent, the highlight
// paints, and rebuilding content (Clear) drops the stale selection.
func TestInlineBoxTextSelectable(t *testing.T) {
	var _ qui.TextSelectable = (*InlineBox)(nil)

	ib := NewInlineBox()
	f := qui.Font{Size: 16}
	ib.AddText("hello ", f, qui.ColorBlack, 0, "")
	ib.AddText("world", f, qui.ColorBlack, qui.DecorationUnderline, "")
	sz := ib.Measure(qui.Size{W: 10000})
	ib.Layout(qui.Rect{X: 0, Y: 0, W: 10000, H: sz.H})

	n := ib.SelectableLength()
	if n != len([]rune("hello world")) {
		t.Fatalf("SelectableLength = %d, want %d", n, len("hello world"))
	}

	// Select everything and read it back.
	ib.SetSelectionRange(0, n)
	if got := ib.SelectedText(); got != "hello world" {
		t.Fatalf("SelectedText = %q, want %q", got, "hello world")
	}

	// A point at the far left maps to offset 0; far right maps to the end.
	if got := ib.SelectionOffsetAt(qui.Point{X: -5, Y: 2}); got != 0 {
		t.Fatalf("left-edge offset = %d, want 0", got)
	}
	if got := ib.SelectionOffsetAt(qui.Point{X: sz.W + 50, Y: 2}); got != n {
		t.Fatalf("right-edge offset = %d, want %d", got, n)
	}
	// Round-trip: the offset under a mid-text point selects up to there.
	mid := ib.SelectionOffsetAt(qui.Point{X: sz.W / 2, Y: 2})
	if mid <= 0 || mid >= n {
		t.Fatalf("mid offset = %d, want interior of (0,%d)", mid, n)
	}

	// The highlight paints a fill.
	var rec qui.RecordingCanvas
	ib.Draw(&rec)
	if len(rec.Fills) == 0 {
		t.Fatal("selection highlight did not paint")
	}

	// Clear-and-rebuild drops the selection (offsets are stale).
	ib.Clear()
	if got := ib.SelectedText(); got != "" {
		t.Fatalf("after Clear, SelectedText = %q, want empty", got)
	}
}

func TestInlineBoxWrapsWithChildren(t *testing.T) {
	ib := NewInlineBox()
	f := qui.Font{Size: 16}
	ib.AddText("alpha beta gamma delta epsilon zeta", f, qui.ColorBlack, 0, "")
	badge := NewBox(qui.FlowLayout{})
	badge.Style().Width = 30
	badge.Style().Height = 14
	ib.AddBox(badge, qui.InlineBaseline)

	sz := ib.Measure(qui.Size{W: 80})
	if sz.H <= 20 {
		t.Errorf("narrow width should wrap to several lines (H=%v)", sz.H)
	}
}
