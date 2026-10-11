package aac

import (
	"math"
	"math/rand"
	"testing"
)

func TestIMDCTMatchesDirect(t *testing.T) {
	for _, m := range []int{1024, 128} {
		f := newDCT4(m)
		rng := rand.New(rand.NewSource(1))
		spec := make([]float64, m)
		for i := range spec {
			spec[i] = rng.Float64()*2 - 1
		}
		got := make([]float64, 2*m)
		f.imdct(spec, got)
		n := 2 * m
		n0 := (float64(n)/2 + 1) / 2
		for i := 0; i < n; i++ {
			var s float64
			for k := 0; k < m; k++ {
				s += spec[k] * math.Cos(2*math.Pi/float64(n)*(float64(i)+n0)*(float64(k)+0.5))
			}
			s *= 2 / float64(n)
			if math.Abs(s-got[i]) > 1e-9 {
				t.Fatalf("m=%d i=%d: got %v want %v", m, i, got[i], s)
			}
		}
	}
}

func TestSWBTables(t *testing.T) {
	long := map[int]int{0: 41, 2: 47, 3: 49, 5: 51, 6: 47, 8: 43, 11: 40}
	short := map[int]int{0: 12, 3: 14, 6: 15, 8: 15, 11: 15}
	for i, n := range long {
		if got := len(swbLong[i]) - 1; got != n || swbLong[i][n] != 1024 {
			t.Errorf("long table %d: %d bands", i, got)
		}
	}
	for i, n := range short {
		if got := len(swbShort[i]) - 1; got != n || swbShort[i][n] != 128 {
			t.Errorf("short table %d: %d bands", i, got)
		}
	}
}
