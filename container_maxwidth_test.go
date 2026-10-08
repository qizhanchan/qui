package qui

import "testing"

// A wrapping row with a max-width wraps AT the max-width when measured with
// a wider offer, and reports the height of all its lines — not the height of
// the single line it would be at the full offer.
func TestContainerMeasureWrapsAtMaxWidth(t *testing.T) {
	kids := []Widget{newSized(100, 30), newSized(100, 30), newSized(100, 30)}
	c := NewContainer(FlexLayout{Direction: Horizontal, Wrap: true}, kids...)
	UpdateStyle(c, func(s *Style) { s.Padding = Insets{} })
	c.SetMaxSize(250, 0)
	if got := c.Measure(Size{W: 900, H: 600}); got.H != 60 || got.W > 250 {
		t.Errorf("measured %v at a 900 offer, want two lines (H 60, W <= 250)", got)
	}
	// An offer below the cap still wins: one item per line.
	if got := c.Measure(Size{W: 150, H: 600}); got.H != 90 {
		t.Errorf("measured %v at a 150 offer, want three lines (H 90)", got)
	}
}
