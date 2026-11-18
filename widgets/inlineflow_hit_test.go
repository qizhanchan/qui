package widgets

import (
	. "github.com/qizhanchan/qui"
	"testing"
)

func TestInlineBoxFloatExclusionsPassThroughHitTest(t *testing.T) {
	root := NewBox(FlowLayout{})
	floating := NewBox(FlowLayout{})
	root.AddChild(floating)
	inline := NewInlineBox()
	inline.AddText("text", Font{Size: 16}, ColorBlack, 0, "")
	root.AddChild(inline)
	root.Layout(Rect{W: 300, H: 60})
	floating.Layout(Rect{X: 240, W: 60, H: 30})
	inline.SetFloatExclusions([]FloatExclusion{{Rect: Rect{X: 240, W: 60, H: 30}, Side: FloatRight}})
	inline.Measure(Size{W: 300})
	inline.Layout(Rect{W: 300, H: 60})
	if got := root.HitTest(Point{X: 260, Y: 15}); got != floating {
		t.Fatalf("float covered by anonymous inline box: %T", got)
	}
	if got := inline.HitTest(Point{X: 10, Y: 10}); got == nil {
		t.Fatal("inline text no longer hit testable")
	}
}
