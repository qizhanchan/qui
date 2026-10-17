package hevc

import "testing"

// TestInverseTransform pins inverseTransform (partial butterflies over
// the non-zero bounding box) to inverseTransformRef, the matrix product.
func TestInverseTransform(t *testing.T) {
	rng := uint32(3)
	rnd := func(n int) int {
		rng = rng*1664525 + 1013904223
		return int(rng>>8) % n
	}
	for iter := 0; iter < 20000; iter++ {
		n := 4 << rnd(4)
		dst4 := n == 4 && rnd(2) == 0
		c := make([]int32, n*n)
		bw, bh := 1+rnd(n), 1+rnd(n)
		if rnd(4) == 0 {
			bw, bh = 1, 1
		}
		density := 1 + rnd(4)
		for y := 0; y < bh; y++ {
			for x := 0; x < bw; x++ {
				if rnd(density) == 0 {
					c[y*n+x] = int32(rnd(65536) - 32768)
					if rnd(2) == 0 {
						c[y*n+x] >>= 8
					}
				}
			}
		}
		var cols uint32
		by := 0
		for i, v := range c {
			if v != 0 {
				cols, by = cols|1<<uint(i%n), i/n
			}
		}
		want := append([]int32(nil), c...)
		inverseTransformRef(want, n, dst4, make([]int32, n*n))
		inverseTransform(c, n, dst4, make([]int32, n*n), cols, by)
		for i := range c {
			if c[i] != want[i] {
				t.Fatalf("n %d dst %v cols %b by %d: sample %d = %d, want %d", n, dst4, cols, by, i, c[i], want[i])
			}
		}
	}
}

// inverseTransform turns d (raster n x n, in place) into residual
// samples (8.6.4.2). dst4 selects the 4x4 DST. Both stages accumulate
// basis rows scaled by the non-zero inputs only, which keeps sparse
// blocks (the common case) cheap.
func inverseTransformRef(c []int32, n int, dst4 bool, tmp []int32) {
	var m []int32
	switch {
	case dst4:
		m = dstBasis
	default:
		m = basis[log2(n)-2]
	}
	var acc [32]int32
	// Vertical (columns): e[y][x] = sum_j M[j][y] * d[j][x].
	maxCol := -1
	for x := 0; x < n; x++ {
		a := acc[:n]
		clear(a)
		any := false
		for j := 0; j < n; j++ {
			cj := c[j*n+x]
			if cj == 0 {
				continue
			}
			any = true
			row := m[j*n : j*n+n]
			for y, v := range row {
				a[y] += v * cj
			}
		}
		if !any {
			for y := 0; y < n; y++ {
				tmp[y*n+x] = 0
			}
			continue
		}
		maxCol = x
		for y, v := range a {
			tmp[y*n+x] = clip16((v + 64) >> 7)
		}
	}
	// Horizontal: r[y][x] = sum_j M[j][x] * g[y][j], j <= maxCol.
	for y := 0; y < n; y++ {
		g := tmp[y*n : y*n+maxCol+1]
		a := acc[:n]
		clear(a)
		for j, gj := range g {
			if gj == 0 {
				continue
			}
			row := m[j*n : j*n+n]
			for x, v := range row {
				a[x] += v * gj
			}
		}
		out := c[y*n : y*n+n]
		for x, v := range a {
			out[x] = (v + 2048) >> 12
		}
	}
}
