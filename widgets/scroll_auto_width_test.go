package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// reflowBox is "wrapping text": its height grows as its width shrinks.
type reflowBox struct{ BaseWidget }

func (b *reflowBox) Measure(c Size) Size {
	if c.W <= 0 {
		return Size{W: 300, H: 100}
	}
	return Size{W: c.W, H: 30000 / c.W}
}

// An auto-sized vertical ScrollView measures its content at the width the
// content is laid out at (viewport minus the classic bar), so the last
// wrapped line is still reachable.
func TestScrollViewAutoMeasuresAtLaidOutWidth(t *testing.T) {
	content := &reflowBox{BaseWidget: NewBaseWidget()}
	content.SetSelf(content)
	sv := NewScrollView()
	sv.SetContentAuto(content)
	sv.Layout(Rect{W: 300, H: 50})
	laidW := content.Bounds().W
	want := 30000/laidW - 50
	if got := sv.MaxScroll(); got < want-0.01 {
		t.Fatalf("MaxScroll = %v; content laid out %vpx wide needs %v", got, laidW, want)
	}
}
