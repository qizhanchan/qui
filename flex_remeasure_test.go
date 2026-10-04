package qui

import (
	"math"
	"testing"
)

// wrapWidget simulates a wrapping-text widget: its height depends on the
// available width (narrower → more lines → taller), exactly the case that
// broke flex cards before the resolved-width cross re-measure.
type wrapWidget struct {
	BaseWidget
	contentW float32 // total text width on one line
	lineH    float32
}

func newWrapWidget(contentW, lineH, grow float32) *wrapWidget {
	w := &wrapWidget{contentW: contentW, lineH: lineH}
	w.BaseWidget = NewBaseWidget()
	w.SetSelf(w)
	w.Style().Grow = grow
	w.Style().Basis = 0
	return w
}

func (w *wrapWidget) Measure(available Size) Size {
	lines := float32(1)
	if available.W > 0 {
		lines = float32(math.Ceil(float64(w.contentW / available.W)))
		if lines < 1 {
			lines = 1
		}
	}
	return Size{W: w.contentW, H: lines * w.lineH}
}

func TestFlexRemeasuresCrossAtResolvedWidth(t *testing.T) {
	// Two growing items, each wanting 180px of text on one line, in a
	// 200px row. Each resolves to ~100px → the text wraps to 2 lines.
	a := newWrapWidget(180, 10, 1)
	b := newWrapWidget(180, 10, 1)
	c := NewContainer(FlexLayout{Direction: Horizontal}, a, b)

	got := c.Measure(Size{W: 200, H: 0})
	// At the resolved ~100px width, contentW 180 wraps to 2 lines = 20px.
	// The pre-fix behavior measured at the full 200px width → 1 line = 10.
	if got.H < 20 {
		t.Fatalf("flex row measured H = %v, want >= 20 (2 wrapped lines); "+
			"cross was not re-measured at resolved width", got.H)
	}

	// And after layout at that height, stretch fills each item to the row
	// height so its box contains the wrapped text.
	c.Layout(Rect{X: 0, Y: 0, W: 200, H: got.H})
	if a.Bounds().H < 20 {
		t.Errorf("item laid out H = %v, want >= 20", a.Bounds().H)
	}
}
