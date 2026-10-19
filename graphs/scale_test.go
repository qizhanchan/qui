package graphs

import (
	"math"
	"testing"
)

func TestExtentOfIgnoresNonFiniteValues(t *testing.T) {
	e := ExtentOf([]float64{math.NaN(), 3, math.Inf(1), -2, 7})
	if !e.Valid || e.Min != -2 || e.Max != 7 {
		t.Fatalf("ExtentOf = %+v, want [-2, 7]", e)
	}
	if e := ExtentOf(nil); e.Valid {
		t.Fatalf("empty extent should be invalid, got %+v", e)
	}
}

func TestLinearScaleNormalizeRoundTrip(t *testing.T) {
	s := NewLinearScale(-10, 30)
	for _, v := range []float64{-10, -2.5, 0, 17, 30} {
		p := s.Normalize(v)
		if p < 0 || p > 1 {
			t.Fatalf("Normalize(%v) = %v, outside [0,1]", v, p)
		}
		if back := s.Invert(p); math.Abs(back-v) > 1e-9 {
			t.Fatalf("roundtrip %v -> %v -> %v", v, p, back)
		}
	}
}

func TestLinearScaleDegenerateDomain(t *testing.T) {
	s := NewLinearScale(5, 5)
	if got := s.Normalize(5); got != 0 {
		t.Fatalf("degenerate Normalize = %v, want 0", got)
	}
}

func TestNiceLinearTicksStayVisibleAndHitTarget(t *testing.T) {
	ticks := NiceLinearTicks(-1.3, 2.7, 5)
	if len(ticks) < 3 {
		t.Fatalf("got %d ticks, want at least 3", len(ticks))
	}
	for _, tick := range ticks {
		if tick.Value < -1.300001 || tick.Value > 2.700001 {
			t.Fatalf("tick %v outside visible extent", tick.Value)
		}
		if tick.Label == "" {
			t.Fatal("tick label should not be empty")
		}
	}
}

func TestCategoryScaleCentersAndBands(t *testing.T) {
	s := &CategoryScale{Categories: []string{"A", "B", "C", "D"}}
	want := []float64{0.125, 0.375, 0.625, 0.875}
	for i, value := range want {
		if got := s.Normalize(float64(i)); math.Abs(got-value) > 1e-9 {
			t.Fatalf("Normalize(%d) = %v, want %v", i, got, value)
		}
		start, end := s.Band(i)
		if math.Abs(start-value+0.125) > 1e-9 || math.Abs(end-value-0.125) > 1e-9 {
			t.Fatalf("Band(%d) = [%v,%v], want center %v", i, start, end, value)
		}
	}
	if got := s.BandWidth(); got != 0.25 {
		t.Fatalf("BandWidth = %v, want 0.25", got)
	}
}
