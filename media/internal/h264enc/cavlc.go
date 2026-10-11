package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// CAVLC residual coding (spec 9.2, inverted).

// nCTable maps nC to the coeff_token table (-1 = chroma DC).
func nCTable(nC int) int {
	switch {
	case nC < 0:
		return 4
	case nC < 2:
		return 0
	case nC < 4:
		return 1
	case nC < 8:
		return 2
	}
	return 3
}

// writeResidual writes one residual_block_cavlc for coeff (scan order,
// length maxNumCoeff) and returns TotalCoeff.
func (w *bitWriter) writeResidual(coeff []int32, nC int) int {
	maxNum := len(coeff)
	var levels [16]int32
	var runs [16]int // zeros preceding each level, in reverse scan order
	tc := 0
	last := -1
	for i := maxNum - 1; i >= 0; i-- {
		if coeff[i] != 0 {
			if last < 0 {
				last = i
			}
			levels[tc] = coeff[i]
			tc++
		}
	}
	t := nCTable(nC)
	if tc == 0 {
		code, l := h264.CoeffTokenCode(t, 0, 0)
		w.u(l, uint32(code))
		return 0
	}
	// Runs: for level k (reverse order), the zeros between it and the next
	// lower-frequency non-zero coefficient.
	k := 0
	run := 0
	for i := last; i >= 0; i-- {
		if coeff[i] != 0 {
			if k > 0 {
				runs[k-1] = run
			}
			k++
			run = 0
		} else {
			run++
		}
	}
	runs[tc-1] = run
	t1 := 0
	for t1 < tc && t1 < 3 && (levels[t1] == 1 || levels[t1] == -1) {
		t1++
	}
	code, l := h264.CoeffTokenCode(t, tc, t1)
	w.u(l, uint32(code))
	for i := 0; i < t1; i++ {
		if levels[i] < 0 {
			w.u(1, 1)
		} else {
			w.u(1, 0)
		}
	}
	sl := 0
	if tc > 10 && t1 < 3 {
		sl = 1
	}
	for i := t1; i < tc; i++ {
		lv := levels[i]
		var lc int
		if lv > 0 {
			lc = int(2*lv - 2)
		} else {
			lc = int(-2*lv - 1)
		}
		if i == t1 && t1 < 3 {
			lc -= 2
		}
		w.writeLevel(lc, sl)
		if sl == 0 {
			sl = 1
		}
		if (lv > int32(3<<uint(sl-1)) || lv < -int32(3<<uint(sl-1))) && sl < 6 {
			sl++
		}
	}
	totalZeros := last + 1 - tc
	if tc < maxNum {
		code, l := h264.TotalZerosCode(tc, totalZeros, nC < 0)
		w.u(l, uint32(code))
	}
	zl := totalZeros
	for i := 0; i < tc-1 && zl > 0; i++ {
		code, l := h264.RunBeforeCode(zl, runs[i])
		w.u(l, uint32(code))
		zl -= runs[i]
	}
	return tc
}

// writeLevel writes level_prefix / level_suffix for levelCode lc.
func (w *bitWriter) writeLevel(lc, sl int) {
	if sl == 0 {
		switch {
		case lc < 14:
			w.u(lc+1, 1)
			return
		case lc < 30:
			w.u(15, 1)
			w.u(4, uint32(lc-14))
			return
		}
	} else if lc < 15<<uint(sl) {
		w.u((lc>>uint(sl))+1, 1)
		w.u(sl, uint32(lc&(1<<uint(sl)-1)))
		return
	}
	v := lc - 15<<uint(sl)
	if sl == 0 {
		v -= 15
	}
	if v < 4096 {
		w.u(16, 1)
		w.u(12, uint32(v))
		return
	}
	for p := 16; ; p++ {
		off := 1<<uint(p-3) - 4096
		if v-off < 1<<uint(p-3) {
			w.u(p+1, 1)
			w.u(p-3, uint32(v-off))
			return
		}
	}
}

// residualBits estimates the CAVLC cost of a block without writing it.
func residualBits(coeff []int32, nC int) int {
	var buf [80]byte
	w := bitWriter{buf: buf[:0]}
	w.writeResidual(coeff, nC)
	return w.bitLen()
}
