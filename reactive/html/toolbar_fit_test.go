package html

import "testing"

func TestOverflowFit(t *testing.T) {
	cases := []struct {
		name             string
		w                []float32
		gap, more, avail float32
		want             int
	}{
		{"empty", nil, 4, 30, 100, 0},
		{"all fit", []float32{100, 100, 100}, 0, 30, 300, 3},
		{"all fit within tolerance", []float32{100, 100, 100}, 0, 30, 299.6, 3},
		{"just over keeps room for more", []float32{100, 100, 100}, 0, 30, 299, 2},
		{"exact boundary with more", []float32{100, 100, 100}, 0, 30, 230, 2},
		{"boundary tolerance with more", []float32{100, 100, 100}, 0, 30, 229.5, 2},
		{"below boundary with more", []float32{100, 100, 100}, 0, 30, 229, 1},
		{"nothing fits", []float32{100, 100}, 0, 30, 120, 0},
		{"gaps count", []float32{50, 50, 50}, 10, 20, 150, 2}, // 50+10+50+10+20=140; 3 items need 170
		{"gaps tip it", []float32{50, 50, 50}, 10, 20, 139, 1},
		{"single wide item", []float32{500}, 0, 30, 100, 0},
	}
	for _, c := range cases {
		if got := overflowFit(c.w, c.gap, c.more, c.avail); got != c.want {
			t.Errorf("%s: overflowFit(%v, gap %v, more %v, avail %v) = %d, want %d",
				c.name, c.w, c.gap, c.more, c.avail, got, c.want)
		}
	}
}
