package qui

import "testing"

// flowChild is a minimal Widget for exercising FlowLayout geometry.
type flowChild struct {
	BaseWidget
	w, h  float32
	level FlowLevel
}

func newFlowChild(w, h float32, level FlowLevel) *flowChild {
	c := &flowChild{w: w, h: h, level: level}
	c.BaseWidget = NewBaseWidget()
	c.SetSelf(c)
	return c
}

func (c *flowChild) Measure(available Size) Size { return Size{W: c.w, H: c.h} }
func (c *flowChild) FlowLevel() FlowLevel        { return c.level }

func TestFlowLayoutStacksBlocksVertically(t *testing.T) {
	a := newFlowChild(50, 20, FlowBlock)
	b := newFlowChild(50, 30, FlowBlock)
	FlowLayout{}.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 500})

	if a.Bounds().Y != 0 {
		t.Errorf("block a.Y = %v, want 0", a.Bounds().Y)
	}
	if b.Bounds().Y != 20 {
		t.Errorf("block b.Y = %v, want 20 (stacked below a)", b.Bounds().Y)
	}
	// Block boxes fill the content width.
	if a.Bounds().W != 200 {
		t.Errorf("block a.W = %v, want 200 (full width)", a.Bounds().W)
	}
}

func TestFlowLayoutWrapsInlineBoxes(t *testing.T) {
	// Three 80px boxes in a 200px line: first two share line 0, third wraps.
	a := newFlowChild(80, 20, FlowInline)
	b := newFlowChild(80, 20, FlowInline)
	c := newFlowChild(80, 20, FlowInline)
	FlowLayout{}.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 200, H: 500})

	if a.Bounds().Y != 0 || b.Bounds().Y != 0 {
		t.Errorf("a.Y=%v b.Y=%v, want both 0 (same line)", a.Bounds().Y, b.Bounds().Y)
	}
	if b.Bounds().X != 80 {
		t.Errorf("b.X = %v, want 80 (right of a)", b.Bounds().X)
	}
	if c.Bounds().Y != 20 {
		t.Errorf("c.Y = %v, want 20 (wrapped to line 1)", c.Bounds().Y)
	}
	if c.Bounds().X != 0 {
		t.Errorf("c.X = %v, want 0 (line start after wrap)", c.Bounds().X)
	}
}

// Style.WidthPct/HeightPct resolve against the band's available content
// area at layout time (CSS `width: 50%`).
func TestFlowLayoutPercentSize(t *testing.T) {
	a := newFlowChild(50, 20, FlowBlock)
	a.Style().WidthPct = 0.5
	b := newFlowChild(50, 20, FlowBlock)
	b.Style().HeightPct = 0.25
	FlowLayout{}.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 400, H: 200})

	if a.Bounds().W != 200 {
		t.Errorf("WidthPct 0.5 of 400: got %v, want 200", a.Bounds().W)
	}
	if a.Bounds().X != 0 {
		t.Errorf("pct-width block X = %v, want 0 (left-aligned like explicit width)", a.Bounds().X)
	}
	if b.Bounds().H != 50 {
		t.Errorf("HeightPct 0.25 of 200: got %v, want 50", b.Bounds().H)
	}
}

// Auto horizontal margins on an explicit-width block absorb the leftover
// band space: both auto centers, left-only pushes right.
func TestFlowLayoutAutoMargins(t *testing.T) {
	center := newFlowChild(100, 20, FlowBlock)
	center.Style().Width = 100
	center.Style().MarginLeftAuto = true
	center.Style().MarginRightAuto = true
	right := newFlowChild(100, 20, FlowBlock)
	right.Style().Width = 100
	right.Style().MarginLeftAuto = true
	FlowLayout{}.Apply([]Widget{center, right}, Rect{X: 0, Y: 0, W: 400, H: 200})

	if center.Bounds().X != 150 {
		t.Errorf("margin:0 auto X = %v, want 150 ((400-100)/2)", center.Bounds().X)
	}
	if right.Bounds().X != 300 {
		t.Errorf("margin-left:auto X = %v, want 300 (pushed to right edge)", right.Bounds().X)
	}
}

func TestFlowLayoutMeasureHeight(t *testing.T) {
	a := newFlowChild(50, 20, FlowBlock)
	b := newFlowChild(50, 30, FlowBlock)
	got := FlowLayout{}.Measure([]Widget{a, b}, Size{W: 200, H: 0})
	if got.H != 50 {
		t.Errorf("flow measure H = %v, want 50 (20+30)", got.H)
	}
	// Width is the intrinsic content width (widest child = 50), NOT the
	// full available width — a flow box used as a flex item must report
	// its content size so flex-basis sizes it to its content.
	if got.W != 50 {
		t.Errorf("flow measure W = %v, want 50 (intrinsic content width)", got.W)
	}
}

// Percentage min/max sizes resolve against the band, clamping the block.
// max-width:50% caps a full-width block; min-height:50% floors its height.
func TestFlowLayoutPercentMinMax(t *testing.T) {
	capped := newFlowChild(50, 20, FlowBlock)
	capped.Style().MaxWidthPct = 0.5 // 50% of 400 = 200
	tall := newFlowChild(50, 20, FlowBlock)
	tall.Style().MinHeightPct = 0.5 // 50% of 200 = 100
	FlowLayout{}.Apply([]Widget{capped, tall}, Rect{X: 0, Y: 0, W: 400, H: 200})

	if capped.Bounds().W != 200 {
		t.Errorf("max-width:50%% of 400: got %v, want 200 (full-width block capped)", capped.Bounds().W)
	}
	if tall.Bounds().H != 100 {
		t.Errorf("min-height:50%% of 200: got %v, want 100", tall.Bounds().H)
	}
}
