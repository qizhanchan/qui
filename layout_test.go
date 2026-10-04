package qui

import "testing"

// sizedWidget is a test fixture whose Measure returns a fixed size.
// Useful for asserting FlexLayout's final rects against known naturals.
type sizedWidget struct {
	BaseWidget
	size        Size
	measureMode measureMode
}

type measureMode int

const (
	measureFixed measureMode = iota
	measureFill              // returns the available size verbatim
)

func newSized(w, h float32) *sizedWidget {
	s := &sizedWidget{size: Size{W: w, H: h}}
	s.BaseWidget = NewBaseWidget()
	return s
}

func (s *sizedWidget) Measure(available Size) Size {
	if s.measureMode == measureFill {
		return available
	}
	return s.size
}

// ---- Natural sizing, no grow ----

func TestFlexNaturalSizingNoGrow(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(30, 20)
	c := newSized(40, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 10}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 500, H: 40})

	if a.Bounds().W != 50 {
		t.Errorf("a.W = %v, want 50", a.Bounds().W)
	}
	if b.Bounds().W != 30 {
		t.Errorf("b.W = %v, want 30", b.Bounds().W)
	}
	// First item at x=0. Second at x=50+10. Third at x=50+10+30+10.
	if a.Bounds().X != 0 {
		t.Errorf("a.X = %v, want 0", a.Bounds().X)
	}
	if b.Bounds().X != 60 {
		t.Errorf("b.X = %v, want 60", b.Bounds().X)
	}
	if c.Bounds().X != 100 {
		t.Errorf("c.X = %v, want 100", c.Bounds().X)
	}
}

// ---- Grow distribution ----

func TestFlexSingleGrowTakesRemaining(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(30, 20)
	c := newSized(40, 20)
	b.SetFlex(1)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 200, H: 40})
	// Natural = 50+30+40 = 120. Free = 80. All to b.
	if b.Bounds().W != 30+80 {
		t.Errorf("b.W with Grow=1: got %v, want 110", b.Bounds().W)
	}
	if a.Bounds().W != 50 || c.Bounds().W != 40 {
		t.Errorf("siblings should keep natural: a.W=%v c.W=%v", a.Bounds().W, c.Bounds().W)
	}
}

func TestFlexGrowProportional(t *testing.T) {
	a := newSized(0, 0)
	b := newSized(0, 0)
	a.SetFlex(1)
	b.SetFlex(2)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 300, H: 40})
	// Both basis 0, free = 300. a gets 1/3 = 100, b gets 2/3 = 200.
	if a.Bounds().W != 100 {
		t.Errorf("a.W = %v, want 100", a.Bounds().W)
	}
	if b.Bounds().W != 200 {
		t.Errorf("b.W = %v, want 200", b.Bounds().W)
	}
}

// ---- Shrink ----

func TestFlexShrinkProportional(t *testing.T) {
	a := newSized(100, 20)
	b := newSized(100, 20)
	a.UpdateFlexItem(func(item *FlexItem) { item.Shrink = 1 })
	b.UpdateFlexItem(func(item *FlexItem) { item.Shrink = 1 })
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 150, H: 40})
	// Total = 200, avail = 150, overflow = 50. Weighted shrink sum =
	// 1*100 + 1*100 = 200. Each reduced by 50*(1*100)/200 = 25.
	// Each ends up 75.
	if a.Bounds().W != 75 {
		t.Errorf("a.W = %v, want 75", a.Bounds().W)
	}
	if b.Bounds().W != 75 {
		t.Errorf("b.W = %v, want 75", b.Bounds().W)
	}
}

func TestFlexShrinkDefaultIsOne(t *testing.T) {
	a := newSized(100, 20)
	b := newSized(100, 20)
	// CSS-aligned default: Shrink unset → treated as 1. Overflow
	// distributes proportionally so both ends up at 60.
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 120, H: 40})
	if a.Bounds().W != 60 || b.Bounds().W != 60 {
		t.Errorf("default shrink should split overflow; got a.W=%v b.W=%v want 60/60", a.Bounds().W, b.Bounds().W)
	}
}

func TestFlexNoShrinkOptOutKeepsBasis(t *testing.T) {
	a := newSized(100, 20)
	b := newSized(100, 20)
	a.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	b.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 120, H: 40})
	if a.Bounds().W != 100 || b.Bounds().W != 100 {
		t.Errorf("NoShrink should overflow; got a.W=%v b.W=%v want 100/100", a.Bounds().W, b.Bounds().W)
	}
}

func TestFlexGrowAloneImpliesBasisZero(t *testing.T) {
	// Regression for the "ScrollView ate all available space" bug:
	// Grow=1 alone (no Basis, no Shrink set) used to fall back to
	// basis=measured, so a Measure-returns-available widget would
	// claim everything and push fixed-size siblings off-screen. New
	// behavior matches CSS `flex: 1`: basis=0, item grows from zero.
	header := newSized(0, 28)
	scroll := newMeasureFillsAvailable()
	scroll.SetFlex(1)
	footer := newSized(0, 38)
	layout := FlexLayout{Direction: Vertical, Gap: 0}
	layout.Apply([]Widget{header, scroll, footer}, Rect{X: 0, Y: 0, W: 200, H: 300})

	if header.Bounds().H != 28 {
		t.Errorf("header.H = %v, want 28", header.Bounds().H)
	}
	if scroll.Bounds().H != 300-28-38 {
		t.Errorf("scroll.H = %v, want %v (avail - header - footer)", scroll.Bounds().H, 300-28-38)
	}
	if footer.Bounds().H != 38 {
		t.Errorf("footer.H = %v, want 38", footer.Bounds().H)
	}
	if footer.Bounds().Y != 300-38 {
		t.Errorf("footer.Y = %v, want %v (within container bottom)", footer.Bounds().Y, 300-38)
	}
}

// newMeasureFillsAvailable returns a fixture whose Measure pretends to
// be the available area — the antipattern that ScrollView / TabView /
// editor follow. Combined with Grow=1 it was the trigger for the
// off-screen-footer bug.
func newMeasureFillsAvailable() *sizedWidget {
	w := &sizedWidget{measureMode: measureFill}
	w.BaseWidget = NewBaseWidget()
	return w
}

// ---- Basis override ----

func TestFlexBasisOverridesMeasure(t *testing.T) {
	a := newSized(100, 20)
	a.UpdateFlexItem(func(item *FlexItem) { item.Basis = 40 })
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 40})
	if a.Bounds().W != 40 {
		t.Errorf("Basis override: got %v, want 40", a.Bounds().W)
	}
}

// ---- Justify ----

func TestFlexJustifyStartPutsExtraAtEnd(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 0, Justify: JustifyStart}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 40})
	if a.Bounds().X != 0 {
		t.Errorf("Start: a.X=%v, want 0", a.Bounds().X)
	}
}

func TestFlexJustifyCenter(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 0, Justify: JustifyCenter}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 40})
	// Total used = 100, free = 100, center start = 50.
	if a.Bounds().X != 50 {
		t.Errorf("Center: a.X=%v, want 50", a.Bounds().X)
	}
}

func TestFlexJustifyEnd(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 0, Justify: JustifyEnd}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 40})
	if a.Bounds().X != 100 {
		t.Errorf("End: a.X=%v, want 100", a.Bounds().X)
	}
	if b.Bounds().X != 150 {
		t.Errorf("End: b.X=%v, want 150", b.Bounds().X)
	}
}

func TestFlexJustifySpaceBetween(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	c := newSized(50, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 0, Justify: JustifySpaceBetween}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 250, H: 40})
	// Free = 100, 2 gaps → 50 each.
	if a.Bounds().X != 0 {
		t.Errorf("SpaceBetween first: a.X=%v, want 0", a.Bounds().X)
	}
	if b.Bounds().X != 100 {
		t.Errorf("SpaceBetween middle: b.X=%v, want 100 (50 + 50 gap)", b.Bounds().X)
	}
	if c.Bounds().X != 200 {
		t.Errorf("SpaceBetween last: c.X=%v, want 200", c.Bounds().X)
	}
}

// ---- Align ----

func TestFlexAlignStretchDefault(t *testing.T) {
	a := newSized(50, 10)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 40})
	// Stretch fills cross axis.
	if a.Bounds().H != 40 {
		t.Errorf("default stretch H=%v, want 40", a.Bounds().H)
	}
}

func TestFlexAlignCenterUsesNatural(t *testing.T) {
	a := newSized(50, 10)
	layout := FlexLayout{Direction: Horizontal, Gap: 0, AlignItems: AlignCenter}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 40})
	if a.Bounds().H != 10 {
		t.Errorf("Center H: got %v, want natural 10", a.Bounds().H)
	}
	// Centered vertically: (40-10)/2 = 15.
	if a.Bounds().Y != 15 {
		t.Errorf("Center Y: got %v, want 15", a.Bounds().Y)
	}
}

func TestFlexAlignItemEndOverrides(t *testing.T) {
	a := newSized(50, 10)
	b := newSized(50, 10)
	b.UpdateFlexItem(func(item *FlexItem) { item.Align = AlignEnd })
	layout := FlexLayout{Direction: Horizontal, Gap: 0, AlignItems: AlignStart}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 40})
	// a: AlignStart → Y=0. b: per-item AlignEnd → Y = 40-10 = 30.
	if a.Bounds().Y != 0 {
		t.Errorf("a (container Start): Y=%v, want 0", a.Bounds().Y)
	}
	if b.Bounds().Y != 30 {
		t.Errorf("b (per-item End): Y=%v, want 30", b.Bounds().Y)
	}
}

// ---- Vertical ----

func TestFlexVerticalDirectionSwapsAxes(t *testing.T) {
	a := newSized(50, 30)
	b := newSized(50, 30)
	b.SetFlex(1)
	layout := FlexLayout{Direction: Vertical, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 100, H: 200})
	// a: natural H=30. b: Grow=1, takes 200-30 = 170.
	if a.Bounds().H != 30 {
		t.Errorf("vertical a.H=%v, want 30", a.Bounds().H)
	}
	if b.Bounds().H != 170 {
		t.Errorf("vertical b.H=%v, want 170", b.Bounds().H)
	}
	// Stacked: a at Y=0, b at Y=30.
	if a.Bounds().Y != 0 {
		t.Errorf("vertical a.Y=%v, want 0", a.Bounds().Y)
	}
	if b.Bounds().Y != 30 {
		t.Errorf("vertical b.Y=%v, want 30", b.Bounds().Y)
	}
}

// ---- Empty / single / gap arithmetic ----

func TestFlexEmptyChildrenNoop(t *testing.T) {
	// Should not crash.
	layout := FlexLayout{Direction: Horizontal}
	layout.Apply([]Widget{}, Rect{X: 0, Y: 0, W: 100, H: 40})
}

func TestFlexGapCountedNMinusOne(t *testing.T) {
	a := newSized(10, 20)
	b := newSized(10, 20)
	c := newSized(10, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 5}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 200, H: 40})
	// gaps = 2 * 5 = 10. First at 0, second at 15, third at 30.
	if b.Bounds().X != 15 {
		t.Errorf("b.X = %v, want 15 (10 + gap 5)", b.Bounds().X)
	}
	if c.Bounds().X != 30 {
		t.Errorf("c.X = %v, want 30 (10 + 5 + 10 + 5)", c.Bounds().X)
	}
}

func TestFlexShrinkHonorsMinSize(t *testing.T) {
	a := newSized(100, 20)
	b := newSized(100, 20)
	a.UpdateFlexItem(func(item *FlexItem) { item.Shrink = 1 })
	b.UpdateFlexItem(func(item *FlexItem) { item.Shrink = 1 })
	a.SetMinSize(90, 0)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 150, H: 40})

	if a.Bounds().W != 90 {
		t.Errorf("a.W should honor min width 90, got %v", a.Bounds().W)
	}
	if b.Bounds().W != 60 {
		t.Errorf("b.W should absorb remaining shrink, got %v want 60", b.Bounds().W)
	}
}

func TestFlexCrossAxisHonorsMinSize(t *testing.T) {
	a := newSized(50, 10)
	a.SetMinSize(0, 60)
	layout := FlexLayout{Direction: Horizontal, AlignItems: AlignStretch}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 40})

	if a.Bounds().H != 60 {
		t.Errorf("cross axis min height should win over stretch avail, got %v", a.Bounds().H)
	}
}

// SetFixedWidth pins both min and max width so a flex item with Grow > 0
// stops growing at the cap, leaving the rest as trailing slack instead
// of consuming all available main-axis space.
func TestFlexFixedWidthCapsGrow(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	a.SetFixedWidth(80)
	a.SetFlex(1)
	b.SetFlex(1)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 300, H: 40})

	if a.Bounds().W != 80 {
		t.Errorf("a.W should be capped at fixed width 80, got %v", a.Bounds().W)
	}
	if b.Bounds().W <= 80 {
		t.Errorf("b.W should absorb the rest, got %v", b.Bounds().W)
	}
}

// SetMaxSize caps the natural width without raising the min — a Measure
// of 200 lands at the 120 cap so the second child gets the rest at its
// natural width.
func TestFlexMaxSizeCapsNaturalWidth(t *testing.T) {
	a := newSized(200, 20)
	b := newSized(50, 20)
	a.SetMaxSize(120, 0)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 400, H: 40})

	if a.Bounds().W != 120 {
		t.Errorf("a.W should be capped at max width 120, got %v", a.Bounds().W)
	}
	if b.Bounds().X != 120 {
		t.Errorf("b should sit flush after a's capped width, X=%v want 120", b.Bounds().X)
	}
}

// SetPreferredSize overrides Measure on the chosen axis. A 50-wide
// Measure with PreferredSize.W=150 advertises 150 as its natural basis
// so the flex pass treats it like a 150-wide widget.
func TestFlexPreferredSizeOverridesMeasure(t *testing.T) {
	a := newSized(50, 20)
	a.SetPreferredSize(150, 0)
	layout := FlexLayout{Direction: Horizontal, Gap: 0}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 300, H: 40})

	if a.Bounds().W != 150 {
		t.Errorf("a.W should pick up preferred width 150 (overriding Measure 50), got %v", a.Bounds().W)
	}
}

// AlignStretch must not exceed maxCross — a 20-tall widget with
// MaxSize.H=40 in a 200-tall row should be stretched to 40, not 200.
func TestFlexCrossAxisHonorsMaxSize(t *testing.T) {
	a := newSized(50, 20)
	a.SetMaxSize(0, 40)
	layout := FlexLayout{Direction: Horizontal, AlignItems: AlignStretch}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 200})

	if a.Bounds().H != 40 {
		t.Errorf("cross axis stretch should be capped at max height 40, got %v", a.Bounds().H)
	}
}

// ---- Wrap ----

// Three 80-wide items in a 200-wide row should pack 2+1: items A, B on
// the first row, C on the second. Cross-axis advances by the line's
// natural cross size (20) so C lands at Y=20 with no overlap.
func TestFlexWrapBreaksLineOnOverflow(t *testing.T) {
	a := newSized(80, 20)
	b := newSized(80, 20)
	c := newSized(80, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 10, Wrap: true}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 200, H: 200})

	// Line 1: A at 0, B at 90 (80+10). Sum 80+10+80=170 fits in 200.
	// C wouldn't fit (170+10+80=260 > 200) → starts a new line.
	if a.Bounds().X != 0 || a.Bounds().Y != 0 {
		t.Errorf("A: got (%v,%v) want (0,0)", a.Bounds().X, a.Bounds().Y)
	}
	if b.Bounds().X != 90 || b.Bounds().Y != 0 {
		t.Errorf("B: got (%v,%v) want (90,0)", b.Bounds().X, b.Bounds().Y)
	}
	if c.Bounds().X != 0 || c.Bounds().Y != 20 {
		t.Errorf("C: got (%v,%v) want (0,20)", c.Bounds().X, c.Bounds().Y)
	}
}

// Wrap with NoShrink children must NOT shrink them — that's the whole
// reason to wrap. Three 80-wide NoShrink items in a 200-wide row pack
// 2+1, each keeping width 80.
func TestFlexWrapDoesNotShrinkChildren(t *testing.T) {
	a := newSized(80, 20)
	b := newSized(80, 20)
	c := newSized(80, 20)
	a.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	b.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	c.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	layout := FlexLayout{Direction: Horizontal, Gap: 10, Wrap: true}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 200, H: 200})

	for name, w := range map[string]*sizedWidget{"A": a, "B": b, "C": c} {
		if w.Bounds().W != 80 {
			t.Errorf("%s.W shrank under wrap: got %v want 80", name, w.Bounds().W)
		}
	}
}

// Wrap with CrossGap inserts vertical spacing between lines so the
// second row sits at Y = lineHeight + CrossGap.
func TestFlexWrapCrossGap(t *testing.T) {
	a := newSized(80, 20)
	b := newSized(80, 20)
	c := newSized(80, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 10, Wrap: true, CrossGap: 8}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 200, H: 200})

	if c.Bounds().Y != 28 {
		t.Errorf("C.Y with CrossGap=8: got %v want 28 (20 line + 8 gap)", c.Bounds().Y)
	}
}

// align-content distributes wrap lines in leftover cross space. Two lines
// of 20 in a 100-tall container leave 60 free; each mode places the lines
// per CSS. Zero value (AlignContentStart) must keep today's start-packing.
func TestFlexWrapAlignContent(t *testing.T) {
	// Every case wraps three 80×20 items into lines AB + C in a 200×100 box.
	cases := []struct {
		name   string
		mode   AlignContent
		line1Y float32 // A/B row
		line2Y float32 // C row
		wantH  float32 // laid-out child height (stretch grows lines)
	}{
		{"start (zero value)", AlignContentStart, 0, 20, 20},
		{"center", AlignContentCenter, 30, 50, 20},
		{"end", AlignContentEnd, 60, 80, 20},
		{"space-between", AlignContentSpaceBetween, 0, 80, 20},
		{"space-around", AlignContentSpaceAround, 15, 65, 20},
		{"space-evenly", AlignContentSpaceEvenly, 20, 60, 20},
		{"stretch", AlignContentStretch, 0, 50, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newSized(80, 20)
			b := newSized(80, 20)
			cc := newSized(80, 20)
			layout := FlexLayout{Direction: Horizontal, Gap: 10, Wrap: true, AlignContent: c.mode}
			layout.Apply([]Widget{a, b, cc}, Rect{X: 0, Y: 0, W: 200, H: 100})

			if a.Bounds().Y != c.line1Y {
				t.Errorf("line1 Y: got %v want %v", a.Bounds().Y, c.line1Y)
			}
			if cc.Bounds().Y != c.line2Y {
				t.Errorf("line2 Y: got %v want %v", cc.Bounds().Y, c.line2Y)
			}
			if a.Bounds().H != c.wantH {
				t.Errorf("child H: got %v want %v", a.Bounds().H, c.wantH)
			}
		})
	}
}

// Container.Measure must report multi-line cross size when its layout
// is a wrap FlexLayout — otherwise the parent allocates only a single
// row's worth of height and the wrapped content clips.
func TestContainerMeasureReportsMultiLineHeightForWrap(t *testing.T) {
	a := newSized(80, 20)
	b := newSized(80, 20)
	c := newSized(80, 20)
	c2 := NewContainer(FlexLayout{Direction: Horizontal, Gap: 10, Wrap: true}, a, b, c)
	c2.Style().Padding = Insets{} // strip default padding so the math is direct

	got := c2.Measure(Size{W: 200, H: 200})
	// Lines: AB on row 1, C on row 2. Each line cross = 20. No CrossGap.
	// Total H = 20 + 20 = 40.
	if got.H != 40 {
		t.Errorf("wrap Container.Measure H: got %v want 40", got.H)
	}
	// Widest line = AB = 80+10+80 = 170.
	if got.W != 170 {
		t.Errorf("wrap Container.Measure W: got %v want 170", got.W)
	}
}

// ---- Flex auto margins (CSS `margin: auto` free-space absorption) ----

// margin-left:auto on the last item pushes it (and only it) to the far
// end, absorbing all main-axis slack; justify-content is ignored.
func TestFlexAutoMarginPushesToEnd(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	b.Style().MarginLeftAuto = true
	layout := FlexLayout{Direction: Horizontal}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 300, H: 40})

	if a.Bounds().X != 0 {
		t.Errorf("a.X = %v, want 0", a.Bounds().X)
	}
	// b starts after all free space (300-50-50 = 200) + a's width.
	if b.Bounds().X != 250 {
		t.Errorf("b.X = %v, want 250 (pushed right)", b.Bounds().X)
	}
}

// margin: 0 auto (both horizontal margins auto) on a single item centers
// it: the slack splits equally left and right.
func TestFlexAutoMarginBothCenters(t *testing.T) {
	a := newSized(100, 20)
	a.Style().MarginLeftAuto = true
	a.Style().MarginRightAuto = true
	layout := FlexLayout{Direction: Horizontal}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 300, H: 40})

	// free = 200, split → 100 leading. a centered at x=100.
	if a.Bounds().X != 100 {
		t.Errorf("a.X = %v, want 100 (centered)", a.Bounds().X)
	}
}

// Auto margins only absorb POSITIVE free space. With zero free space
// (container exactly fits both items) the auto margin gets nothing and b
// packs directly after a.
func TestFlexAutoMarginIgnoredWhenNoSlack(t *testing.T) {
	a := newSized(200, 20)
	b := newSized(200, 20)
	b.Style().MarginLeftAuto = true
	layout := FlexLayout{Direction: Horizontal}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 400, H: 40})

	if b.Bounds().X != 200 {
		t.Errorf("b.X = %v, want 200 (no slack, packed after a)", b.Bounds().X)
	}
}

// Auto margins on the cross axis center the item, overriding align-items.
func TestFlexAutoMarginCrossCenters(t *testing.T) {
	a := newSized(50, 40)
	a.Style().MarginTopAuto = true
	a.Style().MarginBottomAuto = true
	// AlignItems stretch would normally fill 100 tall; auto cross margins
	// keep the natural 40 and center it → offset (100-40)/2 = 30.
	layout := FlexLayout{Direction: Horizontal, AlignItems: AlignStretch}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 300, H: 100})

	if a.Bounds().H != 40 {
		t.Errorf("a.H = %v, want 40 (auto cross margin defeats stretch)", a.Bounds().H)
	}
	if a.Bounds().Y != 30 {
		t.Errorf("a.Y = %v, want 30 (centered on cross axis)", a.Bounds().Y)
	}
}

// Vertical flex: margin-top:auto pushes the item down the main (vertical)
// axis, confirming the axis mapping.
func TestFlexAutoMarginVerticalMainAxis(t *testing.T) {
	a := newSized(50, 40)
	a.Style().MarginTopAuto = true
	layout := FlexLayout{Direction: Vertical}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 100, H: 300})

	// free = 300-40 = 260 pushed above a → a.Y = 260.
	if a.Bounds().Y != 260 {
		t.Errorf("a.Y = %v, want 260 (pushed down)", a.Bounds().Y)
	}
}

// Percentage max-width on a flex item resolves against the flex
// container's content box and caps the item's basis.
func TestFlexPercentMaxWidth(t *testing.T) {
	a := newSized(300, 20)
	a.Style().MaxWidthPct = 0.5 // 50% of 400 = 200
	layout := FlexLayout{Direction: Horizontal}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 400, H: 40})

	if a.Bounds().W != 200 {
		t.Errorf("flex item max-width:50%% of 400: got %v, want 200", a.Bounds().W)
	}
}
