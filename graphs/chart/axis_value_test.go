package chart

import (
	"math"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestValueAxisTicksNiceNumbers(t *testing.T) {
	tests := []struct {
		name   string
		min    float32
		max    float32
		minLen int
	}{
		{"0 to 1", 0, 1, 2},
		{"0 to 100", 0, 100, 3},
		{"neg to pos", -1.3, 2.7, 3},
		{"small range", 0.001, 0.009, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ValueAxis{Min: tt.min, Max: tt.max}
			ticks := a.TickPositions()
			if len(ticks) < tt.minLen {
				t.Errorf("got %d ticks for [%v..%v], want >= %d", len(ticks), tt.min, tt.max, tt.minLen)
			}
			for _, tk := range ticks {
				if tk.Value < tt.min-0.01 || tk.Value > tt.max+0.01 {
					t.Errorf("tick %v outside range [%v..%v]", tk.Value, tt.min, tt.max)
				}
				if tk.Label == "" {
					t.Errorf("tick %v has empty label", tk.Value)
				}
			}
		})
	}
}

func TestValueAxisValueToPixelRoundtrip(t *testing.T) {
	a := &ValueAxis{Min: 0, Max: 100}
	for _, v := range []float32{0, 25, 50, 75, 100} {
		px := a.ValueToPixel(v, 200)
		back := a.PixelToValue(px, 200)
		if math.Abs(float64(back-v)) > 1e-3 {
			t.Errorf("roundtrip v=%v -> px=%v -> back=%v", v, px, back)
		}
	}
}

func TestValueAxisInvertedMapsTopToMax(t *testing.T) {
	a := &ValueAxis{Min: 0, Max: 100, InvertedAxis: true}
	// At the top of a 200px axis (pixel=0), the value should be MAX.
	topPx := a.ValueToPixel(100, 200)
	if topPx != 0 {
		t.Errorf("inverted axis: Max at top pixel = %v, want 0", topPx)
	}
	bottomPx := a.ValueToPixel(0, 200)
	if bottomPx != 200 {
		t.Errorf("inverted axis: Min at bottom pixel = %v, want 200", bottomPx)
	}
}

func TestValueAxisCustomFormatUsed(t *testing.T) {
	a := &ValueAxis{Min: 0, Max: 10, Format: func(v float32) string { return "X" }}
	ticks := a.TickPositions()
	if len(ticks) == 0 || ticks[0].Label != "X" {
		t.Errorf("custom format not applied, got ticks=%+v", ticks)
	}
}

func TestDefaultValueAxisSplitMatchesEChartsPositiveAndMixedSign(t *testing.T) {
	positive := New(Option{
		Animation: boolPointer(false),
		YAxis:     AxisOption{Type: AxisValue},
		Series:    []SeriesOption{{Type: SeriesBar, Data: Numbers(20, 32, 18, 44, 30, 38)}},
	})
	positive.Layout(qui.Rect{W: 500, H: 300})
	lo, hi := positive.YAxis.Range()
	if lo != 0 || hi != 50 {
		t.Fatalf("positive default range = %v..%v, want 0..50", lo, hi)
	}

	mixed := New(Option{
		Animation: boolPointer(false),
		YAxis:     AxisOption{Type: AxisValue},
		Series: []SeriesOption{
			{Type: SeriesBar, Data: Numbers(20, 32, 18, 44, 30, 38)},
			{Type: SeriesBar, Data: Numbers(-12, -18, -10, -24, -16, -20)},
		},
	})
	mixed.Layout(qui.Rect{W: 500, H: 300})
	lo, hi = mixed.YAxis.Range()
	if lo != -30 || hi != 50 {
		t.Fatalf("mixed-sign default range = %v..%v, want -30..50", lo, hi)
	}
}
