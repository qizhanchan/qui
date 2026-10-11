package hevc

import (
	"math/rand"
	"testing"
)

// TestForwardTransformNEONMatchesGo pins the NEON forward transform to the
// Go reference on random residuals of every size (and the 4x4 DST).
func TestForwardTransformNEONMatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON")
	}
	rng := rand.New(rand.NewSource(1))
	for _, n := range []int{4, 8, 16, 32} {
		for _, dst4 := range []bool{false, true} {
			if dst4 && n != 4 {
				continue
			}
			for it := 0; it < 50; it++ {
				r := make([]int32, n*n)
				for i := range r {
					r[i] = int32(rng.Intn(511) - 255)
				}
				var a, b [32 * 32]int32
				var tmp [2 * 32 * 32]int32
				forwardTransform(a[:n*n], r, n, dst4, tmp[:])
				forwardTransformGo(b[:n*n], r, n, dst4)
				if a != b {
					t.Fatalf("n=%d dst=%v: NEON and Go differ", n, dst4)
				}
			}
		}
	}
}

// forwardTransformGo is the scalar path of forwardTransform.
func forwardTransformGo(c, r []int32, n int, dst4 bool) {
	m := basis[log2(n)-2]
	if dst4 {
		m = dstBasis
	}
	l := log2(n)
	shift1, shift2 := l-1, l+6
	tmp := make([]int32, n*n)
	for y := 0; y < n; y++ {
		for k := 0; k < n; k++ {
			var s int32
			for x := 0; x < n; x++ {
				s += m[k*n+x] * r[y*n+x]
			}
			tmp[y*n+k] = (s + 1<<uint(shift1-1)) >> uint(shift1)
		}
	}
	for j := 0; j < n; j++ {
		for k := 0; k < n; k++ {
			var s int32
			for y := 0; y < n; y++ {
				s += m[j*n+y] * tmp[y*n+k]
			}
			c[j*n+k] = (s + 1<<uint(shift2-1)) >> uint(shift2)
		}
	}
}
