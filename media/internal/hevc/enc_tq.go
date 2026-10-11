package hevc

// Forward transform and quantisation (the inverse of transform.go's
// dequant + inverseTransform, HM's scaling).

var quantScale = [6]int32{26214, 23302, 20560, 18396, 16384, 14564}

// forwardTransform turns the n x n residual r (raster) into coefficients
// c: rows first, then columns, with HM's intermediate shifts for 8-bit
// video. dst4 selects the 4x4 DST. tmp needs 2*n*n entries.
func forwardTransform(c, r []int32, n int, dst4 bool, tmp []int32) {
	m := basis[log2(n)-2]
	if dst4 {
		m = dstBasis
	}
	l := log2(n)
	shift1, shift2 := l-1, l+6
	if haveNEON {
		// Rows as a column transform of the transpose, then columns.
		t, x := tmp[:n*n], tmp[n*n:2*n*n]
		for y := 0; y < n; y++ {
			for k := 0; k < n; k++ {
				t[k*n+y] = r[y*n+k]
			}
		}
		neonColTransform(&x[0], &t[0], &m[0], n, shift1) // x = X^T
		for y := 0; y < n; y++ {
			for k := 0; k < n; k++ {
				t[k*n+y] = x[y*n+k]
			}
		}
		neonColTransform(&c[0], &t[0], &m[0], n, shift2)
		return
	}
	rnd1 := int32(1) << uint(shift1-1)
	rnd2 := int32(1) << uint(shift2-1)
	// Rows: tmp[y][k] = sum_x M[k][x] * r[y][x].
	for y := 0; y < n; y++ {
		row := r[y*n : y*n+n]
		for k := 0; k < n; k++ {
			mk := m[k*n : k*n+n]
			var s int32
			for x, v := range row {
				s += mk[x] * v
			}
			tmp[y*n+k] = (s + rnd1) >> uint(shift1)
		}
	}
	// Columns: c[j][k] = sum_y M[j][y] * tmp[y][k].
	for j := 0; j < n; j++ {
		mj := m[j*n : j*n+n]
		for k := 0; k < n; k++ {
			var s int32
			for y, v := range mj {
				s += v * tmp[y*n+k]
			}
			c[j*n+k] = (s + rnd2) >> uint(shift2)
		}
	}
}

// quantParams returns the multiplier, shift and dead-zone rounding of
// uniform quantisation for an n x n block at qp.
func quantParams(n, qp int, intra bool) (scale int64, shift uint, add int64) {
	shift = uint(14 + qp/6 + 7 - log2(n))
	scale = int64(quantScale[qp%6])
	if intra {
		add = 171 << (shift - 9) // 1/3
	} else {
		add = 85 << (shift - 9) // 1/6
	}
	return
}

// quantize sets levels from coefficients and reports the number of
// non-zero levels. frac (if not nil) receives each level's rounding
// remainder relative to 1 << shift, for sign-data-hiding adjustments.
func quantize(levels, c []int32, n, qp int, intra bool, frac []int32) int {
	scale, shift, add := quantParams(n, qp, intra)
	nz := 0
	for i, v := range c {
		a := int64(v)
		neg := a < 0
		if neg {
			a = -a
		}
		x := a * scale
		l := (x + add) >> shift
		if frac != nil {
			frac[i] = int32((x - l<<shift) >> (shift - 8)) // in 1/256 levels
		}
		if l != 0 {
			nz++
			if neg {
				l = -l
			}
		}
		levels[i] = int32(l)
	}
	return nz
}

// hideSigns makes every 4x4 coefficient group of levels (scanned with
// scanIdx) satisfy the sign-data-hiding parity rule: where the group's
// first and last non-zero scan positions are more than 3 apart, the sign
// of the first non-zero coefficient (lowest scan position) must equal the
// parity of the group's absolute sum (odd = negative). A mismatch is fixed
// by moving one level by 1 where its rounding remainder (frac, in 1/256
// levels) makes that cheapest. c gives the signs of new levels. Returns
// the new non-zero count.
func hideSigns(levels, c, frac []int32, log2, scanIdx int) int {
	n := 1 << log2
	log2SB := log2 - 2
	nSB := 1 << log2SB
	scanSB := scans[log2SB][scanIdx]
	scan4 := scans[2][scanIdx]
	for i := nSB*nSB - 1; i >= 0; i-- {
		xS, yS := int(scanSB[i][0]), int(scanSB[i][1])
		var idx [16]int
		for p := range idx {
			idx[p] = (yS<<2+int(scan4[p][1]))*n + xS<<2 + int(scan4[p][0])
		}
		first, last := -1, -1
		sum := 0
		for p := 0; p < 16; p++ {
			if v := levels[idx[p]]; v != 0 {
				if first < 0 {
					first = p
				}
				last = p
				sum += int(iabs32(v))
			}
		}
		if first < 0 || last-first <= 3 {
			continue
		}
		if (sum&1 == 1) == (levels[idx[first]] < 0) {
			continue
		}
		// Raising |l| costs about 256 - frac, lowering costs frac. Zeros
		// before the first non-zero may not be raised and the first may
		// not drop to zero: either would move the hidden sign.
		best, bestCost, up := -1, int32(1<<30), false
		for p := 0; p < 16; p++ {
			k := idx[p]
			v, f := levels[k], frac[k]
			if v == 0 && p < first {
				continue
			}
			if cost := 256 - f; cost < bestCost {
				best, bestCost, up = k, cost, true
			}
			if v != 0 && !(p == first && iabs32(v) == 1) && f < bestCost {
				best, bestCost, up = k, f, false
			}
		}
		if best < 0 {
			continue
		}
		v := levels[best]
		switch {
		case v == 0:
			levels[best] = 1
			if c[best] < 0 {
				levels[best] = -1
			}
		case up == (v > 0):
			levels[best] = v + 1
		default:
			levels[best] = v - 1
		}
	}
	nz := 0
	for _, v := range levels[:n*n] {
		if v != 0 {
			nz++
		}
	}
	return nz
}
