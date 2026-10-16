package qui

import "testing"

// Items past the explicit grid get implicit tracks sized by AutoRows /
// AutoColumns, and a far-off line is clamped instead of allocating tracks.
func TestGridImplicitTracks(t *testing.T) {
	a, b, far := NewBaseWidget(), NewBaseWidget(), NewBaseWidget()
	for _, w := range []*BaseWidget{&a, &b, &far} {
		w.SetSelf(w)
	}
	a.SetGridItem(GridItem{Col: 0, Row: 0, Explicit: true})
	b.SetGridItem(GridItem{Col: 2, Row: 3, Explicit: true})
	far.SetGridItem(GridItem{Col: 0, Row: 1 << 30, Explicit: true})
	g := GridLayout{
		Columns:     []GridTrack{GridFixedTrack(50)},
		Rows:        []GridTrack{GridFixedTrack(10)},
		AutoRows:    []GridTrack{GridFixedTrack(20)},
		AutoColumns: []GridTrack{GridFixedTrack(30)},
	}
	ig, _ := g.withImplicit([]Widget{&a, &b})
	if len(ig.Columns) != 3 || len(ig.Rows) != 4 {
		t.Fatalf("implicit grid = %d×%d, want 3 columns × 4 rows", len(ig.Columns), len(ig.Rows))
	}
	if sz := g.Measure([]Widget{&a, &b}, Size{W: 500}); sz.W != 110 || sz.H != 70 {
		t.Errorf("Measure = %+v, want 110×70 (50+30+30, 10+20+20+20)", sz)
	}
	ig, _ = g.withImplicit([]Widget{&a, &far})
	if len(ig.Rows) > gridMaxTracks {
		t.Errorf("rows = %d, want at most %d", len(ig.Rows), gridMaxTracks)
	}
}
