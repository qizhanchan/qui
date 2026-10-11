package h264

import (
	"math/rand"
	"testing"
)

func TestAvg2MatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, w := range []int{2, 4, 8, 16} {
		for _, h := range []int{1, 2, 4, 8, 16} {
			ds, as, bs := 40, 24, 33
			dst := make([]byte, (h-1)*ds+w)
			a := make([]byte, (h-1)*as+w)
			b := make([]byte, (h-1)*bs+w)
			rng.Read(a)
			rng.Read(b)
			avg2(dst, ds, a, as, b, bs, w, h)
			for j := 0; j < h; j++ {
				for i := 0; i < w; i++ {
					want := byte((int(a[j*as+i]) + int(b[j*bs+i]) + 1) >> 1)
					if got := dst[j*ds+i]; got != want {
						t.Fatalf("w=%d h=%d (%d,%d): got %d want %d", w, h, i, j, got, want)
					}
				}
			}
		}
	}
}
