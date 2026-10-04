package qui

import "testing"

// ---- Fixed / Fraction / Auto track sizing ----

func TestGridFixedColumns(t *testing.T) {
	a := newSized(0, 0)
	b := newSized(0, 0)
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(100), GridFixedTrack(50)},
		Rows:    []GridTrack{GridFixedTrack(30)},
	}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 500, H: 30})
	if a.Bounds().W != 100 {
		t.Errorf("a col 0 width = %v, want 100", a.Bounds().W)
	}
	if b.Bounds().W != 50 {
		t.Errorf("b col 1 width = %v, want 50", b.Bounds().W)
	}
	if b.Bounds().X != 100 {
		t.Errorf("b.X = %v, want 100", b.Bounds().X)
	}
}

func TestGridFractionSplitsRemaining(t *testing.T) {
	a := newSized(0, 0)
	b := newSized(0, 0)
	c := newSized(0, 0)
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(100), GridFractionTrack(1), GridFractionTrack(2)},
		Rows:    []GridTrack{GridFixedTrack(30)},
	}
	// Total W=400, fixed=100, remaining=300, 1fr+2fr → 100 + 200.
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 400, H: 30})
	if b.Bounds().W != 100 {
		t.Errorf("1fr width = %v, want 100", b.Bounds().W)
	}
	if c.Bounds().W != 200 {
		t.Errorf("2fr width = %v, want 200", c.Bounds().W)
	}
}

func TestGridAutoTakesMaxChildWidth(t *testing.T) {
	a := newSized(30, 20)
	b := newSized(70, 20) // wider sibling in same auto col
	a.SetGridCell(0, 0)
	b.SetGridCell(0, 1)
	layout := GridLayout{
		Columns: []GridTrack{GridAutoTrack()},
		Rows:    []GridTrack{GridFixedTrack(20), GridFixedTrack(20)},
	}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 40})
	// Auto column = max Measure.W = 70.
	if a.Bounds().W != 70 {
		t.Errorf("auto col width for a = %v, want 70", a.Bounds().W)
	}
	if b.Bounds().W != 70 {
		t.Errorf("auto col width for b = %v, want 70", b.Bounds().W)
	}
}

// ---- ColSpan / RowSpan ----

func TestGridColSpanJoinsTracks(t *testing.T) {
	header := newSized(0, 0)
	left := newSized(0, 0)
	right := newSized(0, 0)
	header.SetGridItem(GridItem{Col: 0, Row: 0, ColSpan: 2, Explicit: true})
	left.SetGridCell(0, 1)
	right.SetGridCell(1, 1)

	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(100), GridFixedTrack(100)},
		Rows:    []GridTrack{GridFixedTrack(30), GridFixedTrack(30)},
		ColGap:  10,
	}
	layout.Apply([]Widget{header, left, right}, Rect{X: 0, Y: 0, W: 300, H: 60})

	// Header spans 2 cols → 100 + 10 (gap) + 100 = 210.
	if header.Bounds().W != 210 {
		t.Errorf("header spanning 2 cols: W=%v, want 210", header.Bounds().W)
	}
}

func TestGridRowSpanVerticalExtend(t *testing.T) {
	a := newSized(0, 0)
	a.SetGridItem(GridItem{Col: 0, Row: 0, RowSpan: 2, Explicit: true})
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(50)},
		Rows:    []GridTrack{GridFixedTrack(30), GridFixedTrack(30)},
		RowGap:  4,
	}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 50, H: 64})
	// 2 rows × 30 + 1 gap × 4 = 64.
	if a.Bounds().H != 64 {
		t.Errorf("RowSpan=2: H=%v, want 64", a.Bounds().H)
	}
}

// A rowspan cell taller than its spanned Auto rows grows those rows so it fits
// (span distribution), instead of overflowing them.
func TestGridRowSpanGrowsAutoRows(t *testing.T) {
	tall := newSized(50, 100) // spans 2 auto rows, natural height 100
	tall.SetGridItem(GridItem{Col: 0, Row: 0, RowSpan: 2, Explicit: true})
	short := newSized(50, 10) // occupies row 1, col 1
	short.SetGridCell(1, 1)
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(50), GridFixedTrack(50)},
		Rows:    []GridTrack{GridAutoTrack(), GridAutoTrack()},
		RowGap:  4,
	}
	layout.Apply([]Widget{tall, short}, Rect{X: 0, Y: 0, W: 104, H: 200})
	// Without distribution the two auto rows would be ~10+0, so the rowspan
	// cell would be far shorter than 100. It must now reach ~100 (2 rows +
	// 1 gap).
	if h := tall.Bounds().H; h < 99 {
		t.Errorf("rowspan cell H=%v, want >=100 (auto rows grew to fit)", h)
	}
	// Measure must report the same grown intrinsic height.
	got := layout.Measure([]Widget{tall, short}, Size{W: 104, H: 0})
	if got.H < 99 {
		t.Errorf("Measure H=%v, want >=100 (rowspan distribution)", got.H)
	}
}

// ---- Auto-placement ----

func TestGridAutoPlaceRowMajor(t *testing.T) {
	a := newSized(0, 0)
	b := newSized(0, 0)
	c := newSized(0, 0)
	d := newSized(0, 0)
	// All GridItem zero → auto.
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(50), GridFixedTrack(50)},
		Rows:    []GridTrack{GridFixedTrack(20), GridFixedTrack(20)},
	}
	layout.Apply([]Widget{a, b, c, d}, Rect{X: 0, Y: 0, W: 100, H: 40})
	// a=(0,0), b=(1,0), c=(0,1), d=(1,1).
	if a.Bounds().X != 0 || a.Bounds().Y != 0 {
		t.Errorf("a should be top-left; got X=%v Y=%v", a.Bounds().X, a.Bounds().Y)
	}
	if b.Bounds().X != 50 || b.Bounds().Y != 0 {
		t.Errorf("b (0,1): got X=%v Y=%v", b.Bounds().X, b.Bounds().Y)
	}
	if c.Bounds().X != 0 || c.Bounds().Y != 20 {
		t.Errorf("c (1,0): got X=%v Y=%v", c.Bounds().X, c.Bounds().Y)
	}
	if d.Bounds().X != 50 || d.Bounds().Y != 20 {
		t.Errorf("d (1,1): got X=%v Y=%v", d.Bounds().X, d.Bounds().Y)
	}
}

func TestGridAutoPlaceSkipsExplicitlyOccupied(t *testing.T) {
	// a explicit at (1,0). b auto. c auto. Expected: b at (0,0), c at (0,1).
	a := newSized(0, 0)
	b := newSized(0, 0)
	c := newSized(0, 0)
	a.SetGridCell(1, 0)

	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(50), GridFixedTrack(50)},
		Rows:    []GridTrack{GridFixedTrack(20), GridFixedTrack(20)},
	}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 100, H: 40})
	if b.Bounds().X != 0 || b.Bounds().Y != 0 {
		t.Errorf("b should land at (0,0); got X=%v Y=%v", b.Bounds().X, b.Bounds().Y)
	}
	// c auto: skips (1,0) (a's) and should land at (0,1).
	if c.Bounds().X != 0 || c.Bounds().Y != 20 {
		t.Errorf("c should land at (0,1); got X=%v Y=%v", c.Bounds().X, c.Bounds().Y)
	}
}

// ---- Gaps ----

func TestGridColGapBetweenTracks(t *testing.T) {
	a := newSized(0, 0)
	b := newSized(0, 0)
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(50), GridFixedTrack(50)},
		Rows:    []GridTrack{GridFixedTrack(20)},
		ColGap:  8,
	}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 108, H: 20})
	if b.Bounds().X != 58 {
		t.Errorf("b.X with 8px gap: got %v, want 58", b.Bounds().X)
	}
}

// ---- Track Min ----

func TestGridFractionMinFloor(t *testing.T) {
	a := newSized(0, 0)
	layout := GridLayout{
		Columns: []GridTrack{GridTrack{Kind: GridFraction, Size: 1, Min: 100}},
		Rows:    []GridTrack{GridFixedTrack(20)},
	}
	// Avail 50, one fraction track would be 50, but Min=100 forces 100.
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 50, H: 20})
	if a.Bounds().W != 100 {
		t.Errorf("fraction Min floor: got %v, want 100", a.Bounds().W)
	}
}

func TestGridChildMinSizeFloor(t *testing.T) {
	a := newSized(0, 0)
	a.SetMinSize(80, 30)
	layout := GridLayout{
		Columns: []GridTrack{GridFixedTrack(50)},
		Rows:    []GridTrack{GridFixedTrack(20)},
	}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 50, H: 20})
	if a.Bounds().W != 80 {
		t.Errorf("child min width should floor cell width: got %v, want 80", a.Bounds().W)
	}
	if a.Bounds().H != 30 {
		t.Errorf("child min height should floor cell height: got %v, want 30", a.Bounds().H)
	}
}
