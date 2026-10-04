package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// Regression: the selection highlight must break lines the same way the
// renderer does. A previous char-level wrap in visualLines disagreed with
// the word-wrapped render, leaving the last wrapped line's highlight
// short (the reported "…proper margins." with "margins." unhighlighted).
func TestLabelSelectionHighlightCoversWrappedLastLine(t *testing.T) {
	l := NewLabel("alpha beta gamma delta epsilon zeta eta theta iota kappa")
	l.Paragraph.Wrap = true

	w := float32(140)
	sz := l.Measure(Size{W: w, H: 0})
	l.Layout(Rect{X: 0, Y: 0, W: w, H: sz.H})

	runes := []rune(l.Text())
	l.SetSelectionRange(0, len(runes))

	var canvas RecordingCanvas
	l.Draw(&canvas)
	if len(canvas.Fills) == 0 {
		t.Fatal("no selection fills recorded")
	}

	// The render's own line layout (source of truth).
	content := l.Bounds().Inset(l.scaledPadding())
	layout := BuildTextLayout(l.Text(), l.Style().Font, l.Paragraph.LayoutOptions(content.W))
	if len(layout.Lines) < 2 {
		t.Fatalf("expected the text to wrap to >= 2 lines, got %d", len(layout.Lines))
	}
	last := layout.Lines[len(layout.Lines)-1]
	wantRight := content.X + last.Offset + last.Width

	// Bottom-most selection rect = the last visual line's highlight.
	var bottom Rect
	for _, f := range canvas.Fills {
		if f.Y >= bottom.Y {
			bottom = f
		}
	}
	gotRight := bottom.X + bottom.W

	const tol = 12 // font visual overhang + rounding
	if diff := gotRight - wantRight; diff < -tol || diff > tol {
		t.Errorf("last-line selection right edge = %.1f, want ~%.1f (last line %q); "+
			"highlight does not reach the end of the rendered line", gotRight, wantRight, last.Text)
	}
}
