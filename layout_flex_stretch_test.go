package qui

import "testing"

// CSS `align-items: stretch` — qui's effective default — fills the cross axis
// only when the item's cross size is `auto`. An explicit cross size (Style
// width in a column container / height in a row one, a percentage of the
// container, or SetPreferredSize) keeps the item at that size, flush to the
// line's start edge. Before this, stretch overrode every declared cross size,
// so e.g. `input { width: 100px }` inside a `flex-direction: column` filled
// the whole column.
func TestFlexStretchRespectsExplicitCrossSize(t *testing.T) {
	bounds := Rect{X: 0, Y: 0, W: 300, H: 200}

	tests := []struct {
		name       string
		dir        Direction
		setup      func(*sizedWidget)
		wantCross  float32
		wantOffset float32
	}{{
		name:      "column/auto width stretches",
		dir:       Vertical,
		setup:     func(w *sizedWidget) {},
		wantCross: 300,
	}, {
		name:      "column/explicit width wins",
		dir:       Vertical,
		setup:     func(w *sizedWidget) { w.Style().Width = 100 },
		wantCross: 100,
	}, {
		name:      "column/percentage width wins",
		dir:       Vertical,
		setup:     func(w *sizedWidget) { w.Style().WidthPct = 0.5 },
		wantCross: 150,
	}, {
		name:      "column/PreferredSize width wins",
		dir:       Vertical,
		setup:     func(w *sizedWidget) { w.SetPreferredSize(120, 0) },
		wantCross: 120,
	}, {
		name: "column/explicit width sits inside its margins",
		dir:  Vertical,
		setup: func(w *sizedWidget) {
			w.Style().Width = 100
			w.Style().Margin = Insets{Left: 10, Right: 4}
		},
		wantCross:  100,
		wantOffset: 10,
	}, {
		name:      "column/explicit width still clamped by MaxSize",
		dir:       Vertical,
		setup:     func(w *sizedWidget) { w.Style().Width = 250; w.SetMaxSize(160, 0) },
		wantCross: 160,
	}, {
		name:      "column/explicit width still clamped by MinSize",
		dir:       Vertical,
		setup:     func(w *sizedWidget) { w.Style().Width = 30; w.SetMinSize(80, 0) },
		wantCross: 80,
	}, {
		name:      "row/auto height stretches",
		dir:       Horizontal,
		setup:     func(w *sizedWidget) {},
		wantCross: 200,
	}, {
		name:      "row/explicit height wins",
		dir:       Horizontal,
		setup:     func(w *sizedWidget) { w.Style().Height = 26 },
		wantCross: 26,
	}, {
		name:      "row/percentage height wins",
		dir:       Horizontal,
		setup:     func(w *sizedWidget) { w.Style().HeightPct = 0.25 },
		wantCross: 50,
	}, {
		// A per-item align-self: stretch must behave like the container-level
		// default, not resurrect the old fill-regardless behavior.
		name: "row/explicit height wins under align-self stretch",
		dir:  Horizontal,
		setup: func(w *sizedWidget) {
			w.Style().Height = 26
			w.UpdateFlexItem(func(item *FlexItem) { item.Align = AlignStretch })
		},
		wantCross: 26,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newSized(40, 20)
			tc.setup(w)
			FlexLayout{Direction: tc.dir, AlignItems: AlignStretch}.Apply([]Widget{w}, bounds)

			gotCross, gotOffset := w.Bounds().H, w.Bounds().Y
			axis := "H/Y"
			if tc.dir == Vertical {
				gotCross, gotOffset = w.Bounds().W, w.Bounds().X
				axis = "W/X"
			}
			if gotCross != tc.wantCross {
				t.Errorf("cross size (%s) = %v, want %v", axis, gotCross, tc.wantCross)
			}
			if gotOffset != tc.wantOffset {
				t.Errorf("cross offset (%s) = %v, want %v", axis, gotOffset, tc.wantOffset)
			}
		})
	}
}

// The wrap path resolves the cross axis per line through the same helper, so
// an explicit cross size must survive there too: two 80-wide items wrap into
// two lines of a 100-wide column-less row, and the one declaring height:30
// keeps 30 instead of filling its line.
func TestFlexWrapStretchRespectsExplicitCrossSize(t *testing.T) {
	a := newSized(80, 20)
	b := newSized(80, 20)
	b.Style().Height = 30
	layout := FlexLayout{Direction: Horizontal, Wrap: true, AlignItems: AlignStretch,
		AlignContent: AlignContentStretch}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 100, H: 200})

	if a.Bounds().Y != 0 || b.Bounds().Y == 0 {
		t.Fatalf("expected two lines: a.Y=%v b.Y=%v", a.Bounds().Y, b.Bounds().Y)
	}
	if b.Bounds().H != 30 {
		t.Errorf("wrapped item with height:30 = %v, want 30", b.Bounds().H)
	}
	if a.Bounds().H <= 30 {
		t.Errorf("auto-height sibling should still stretch its line, got %v", a.Bounds().H)
	}
}
