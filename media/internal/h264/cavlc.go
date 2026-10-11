package h264

import "math/bits"

// CAVLC residual decoding (spec 9.2) with direct lookup tables built from the
// code tables at package init.

// vlcLUT maps the next `bits` bits of the stream to an entry
// len<<8 | value; len 0 marks an invalid code.
type vlcLUT struct {
	bits int
	tab  []uint16
}

func buildLUT(maxLen int, add func(set func(length, code, value int))) vlcLUT {
	l := vlcLUT{bits: maxLen, tab: make([]uint16, 1<<uint(maxLen))}
	add(func(length, code, value int) {
		if length == 0 {
			return
		}
		shift := uint(maxLen - length)
		base := code << shift
		for i := 0; i < 1<<shift; i++ {
			l.tab[base+i] = uint16(length<<8 | value)
		}
	})
	return l
}

var (
	coeffTokenLUT   [5]vlcLUT // nC classes 0..3, 4 = chroma DC
	totalZerosLUT   [15]vlcLUT
	totalZerosDCLUT [3]vlcLUT
	runBeforeLUT    [7]vlcLUT
)

func init() {
	maxLens := [4]int{16, 14, 10, 6}
	for t := 0; t < 4; t++ {
		coeffTokenLUT[t] = buildLUT(maxLens[t], func(set func(int, int, int)) {
			for tc := 0; tc <= 16; tc++ {
				for t1 := 0; t1 <= min(tc, 3); t1++ {
					i := 4*tc + t1
					set(int(coeffTokenLen[t][i]), int(coeffTokenBits[t][i]), tc<<2|t1)
				}
			}
		})
	}
	coeffTokenLUT[4] = buildLUT(8, func(set func(int, int, int)) {
		for tc := 0; tc <= 4; tc++ {
			for t1 := 0; t1 <= min(tc, 3); t1++ {
				i := 4*tc + t1
				set(int(chromaDCCoeffTokenLen[i]), int(chromaDCCoeffTokenBits[i]), tc<<2|t1)
			}
		}
	})
	for tc := 1; tc <= 15; tc++ {
		row := tc - 1
		totalZerosLUT[row] = buildLUT(9, func(set func(int, int, int)) {
			for tz := 0; tz <= 16-tc; tz++ {
				set(int(totalZerosLen[row][tz]), int(totalZerosBits[row][tz]), tz)
			}
		})
	}
	for tc := 1; tc <= 3; tc++ {
		row := tc - 1
		totalZerosDCLUT[row] = buildLUT(3, func(set func(int, int, int)) {
			for tz := 0; tz <= 4-tc; tz++ {
				set(int(chromaDCTotalZerosLen[row][tz]), int(chromaDCTotalZerosBits[row][tz]), tz)
			}
		})
	}
	for row := 0; row < 7; row++ {
		n := row + 1
		if row == 6 {
			n = 14
		}
		runBeforeLUT[row] = buildLUT(11, func(set func(int, int, int)) {
			for rb := 0; rb <= n; rb++ {
				set(int(runBeforeLen[row][rb]), int(runBeforeBits[row][rb]), rb)
			}
		})
	}
}

func (r *bitReader) readLUT(l *vlcLUT) (int, bool) {
	e := l.tab[r.peekBits(l.bits)]
	n := int(e >> 8)
	if n == 0 || r.pos+n > r.nbits {
		r.e = errf("h264: invalid CAVLC code")
		return 0, false
	}
	r.pos += n
	return int(e & 0xff), true
}

var errCAVLC = errf("h264: invalid CAVLC residual")

// residualCAVLC decodes one residual_block_cavlc into coeff (scan order,
// length maxNumCoeff, which the caller zeroed). nC < 0 selects the chroma DC
// table. It returns TotalCoeff.
func (r *bitReader) residualCAVLC(nC, maxNumCoeff int, coeff []int32) (int, error) {
	var t int
	switch {
	case nC < 0:
		t = 4
	case nC < 2:
		t = 0
	case nC < 4:
		t = 1
	case nC < 8:
		t = 2
	default:
		t = 3
	}
	v, ok := r.readLUT(&coeffTokenLUT[t])
	if !ok {
		return 0, r.e
	}
	totalCoeff, trailingOnes := v>>2, v&3
	if totalCoeff == 0 {
		return 0, nil
	}
	if totalCoeff > maxNumCoeff {
		return 0, errCAVLC
	}
	var levels [16]int32
	for i := 0; i < trailingOnes; i++ {
		if r.u(1) == 1 {
			levels[i] = -1
		} else {
			levels[i] = 1
		}
	}
	suffixLength := 0
	if totalCoeff > 10 && trailingOnes < 3 {
		suffixLength = 1
	}
	for i := trailingOnes; i < totalCoeff; i++ {
		p := r.peek32()
		if p == 0 {
			return 0, errCAVLC
		}
		levelPrefix := bits.LeadingZeros32(p)
		r.skip(levelPrefix + 1)
		levelCode := min(15, levelPrefix) << uint(suffixLength)
		if suffixLength > 0 || levelPrefix >= 14 {
			size := suffixLength
			if levelPrefix == 14 && suffixLength == 0 {
				size = 4
			}
			if levelPrefix >= 15 {
				size = levelPrefix - 3
			}
			if size > 0 {
				levelCode += int(r.u(size))
			}
		}
		if levelPrefix >= 15 && suffixLength == 0 {
			levelCode += 15
		}
		if levelPrefix >= 16 {
			levelCode += (1 << uint(levelPrefix-3)) - 4096
		}
		if i == trailingOnes && trailingOnes < 3 {
			levelCode += 2
		}
		var lv int32
		if levelCode&1 == 0 {
			lv = int32((levelCode + 2) >> 1)
		} else {
			lv = int32((-levelCode - 1) >> 1)
		}
		levels[i] = lv
		if suffixLength == 0 {
			suffixLength = 1
		}
		if (lv > int32(3<<uint(suffixLength-1)) || lv < -int32(3<<uint(suffixLength-1))) && suffixLength < 6 {
			suffixLength++
		}
	}
	zerosLeft := 0
	if totalCoeff < maxNumCoeff {
		var lut *vlcLUT
		if nC < 0 {
			lut = &totalZerosDCLUT[totalCoeff-1]
		} else {
			lut = &totalZerosLUT[totalCoeff-1]
		}
		zerosLeft, ok = r.readLUT(lut)
		if !ok {
			return 0, r.e
		}
	}
	if totalCoeff+zerosLeft > maxNumCoeff {
		return 0, errCAVLC
	}
	idx := totalCoeff + zerosLeft - 1
	for i := 0; i < totalCoeff-1; i++ {
		coeff[idx] = levels[i]
		if zerosLeft > 0 {
			rb, ok := r.readLUT(&runBeforeLUT[min(zerosLeft-1, 6)])
			if !ok {
				return 0, r.e
			}
			if rb > zerosLeft {
				return 0, errCAVLC
			}
			zerosLeft -= rb
			idx -= rb
		}
		idx--
	}
	coeff[idx] = levels[totalCoeff-1]
	if r.e != nil {
		return 0, r.e
	}
	return totalCoeff, nil
}
