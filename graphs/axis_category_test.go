package graphs

import "testing"

func TestCategoryAxisBucketCenters(t *testing.T) {
	a := &CategoryAxis{Categories: []string{"A", "B", "C", "D"}}
	// 4 buckets across 100px → slot=25, centers at 12.5, 37.5, 62.5, 87.5.
	wants := []float32{12.5, 37.5, 62.5, 87.5}
	for i, want := range wants {
		got := a.ValueToPixel(float32(i), 100)
		if got != want {
			t.Errorf("bucket[%d] center = %v, want %v", i, got, want)
		}
	}
}

func TestCategoryAxisPixelToValueInverts(t *testing.T) {
	a := &CategoryAxis{Categories: []string{"A", "B", "C", "D"}}
	for i := 0; i < 4; i++ {
		px := a.ValueToPixel(float32(i), 200)
		v := a.PixelToValue(px, 200)
		if int(v+0.5) != i {
			t.Errorf("roundtrip idx=%d -> px=%v -> v=%v", i, px, v)
		}
	}
}

func TestCategoryAxisTickPositionsOnePerCategory(t *testing.T) {
	a := &CategoryAxis{Categories: []string{"Mon", "Tue", "Wed"}}
	ticks := a.TickPositions()
	if len(ticks) != 3 {
		t.Fatalf("expected 3 ticks, got %d", len(ticks))
	}
	for i, tk := range ticks {
		if tk.Label != a.Categories[i] {
			t.Errorf("tick[%d].Label = %q, want %q", i, tk.Label, a.Categories[i])
		}
	}
}

func TestCategoryAxisBucketWidth(t *testing.T) {
	a := &CategoryAxis{Categories: []string{"A", "B"}}
	if got := a.BucketWidth(100); got != 50 {
		t.Errorf("bucket width = %v, want 50", got)
	}
}
