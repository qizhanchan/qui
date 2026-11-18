package qui

import "testing"

func TestEscapedFloatsRestoreMeasuredWidth(t *testing.T) {
	floated := NewContainer(FlowLayout{})
	floated.Style().Float = FloatRight
	floated.Style().Width = 120
	floated.Style().Height = 96
	block := NewContainer(FlowLayout{Floats: &FloatState{}}, floated)
	block.Style().Padding = Insets{Left: 8, Right: 8}
	offer := Size{W: 400}
	measured := MeasureChild(block, offer)
	// Margin collapsing can probe an empty float host at another width.
	// The subsequent real-width size query is a cache hit.
	block.Measure(Size{W: 1})
	if got := MeasureChild(block, offer); got != measured {
		t.Fatalf("cached size changed: %v, want %v", got, measured)
	}
	ex := block.FlowFloatExclusions(offer)
	want := Rect{X: 272, W: 120, H: 96}
	if len(ex) != 1 || ex[0].Rect != want || ex[0].Side != FloatRight {
		t.Fatalf("escaped floats use the probe width: %v, want %v", ex, want)
	}
}
