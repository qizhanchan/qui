package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// An atomic box in an inline run (an inline-flex button) paints its
// box-shadow past the run's bounds; the run's PaintBounds must cover it,
// or a damage repaint clipped to the run cuts the shadow off (go.dev's
// Share / Tour buttons lost theirs in the real window).
func TestInlineBoxPaintBoundsCoverAtomicShadow(t *testing.T) {
	btn := NewBox(FlowLayout{})
	btn.SetPreferredSize(60, 28)
	UpdateStyle(btn, func(s *Style) {
		s.Shadow = ShadowStyle{Y: 2, Blur: 5, Color: Color{A: 0.2}}
	})
	run := NewInlineBox()
	run.AddBox(btn, InlineBaseline)
	sz := run.Measure(Size{W: 200})
	run.Layout(Rect{W: 200, H: sz.H})
	pb := run.PaintBounds()
	if b := btn.Bounds(); pb.Y+pb.H <= b.Y+b.H {
		t.Fatalf("run PaintBounds %+v ends at the button's bottom %v; its shadow paints below", pb, b.Y+b.H)
	}
}
