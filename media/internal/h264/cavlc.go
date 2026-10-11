package h264

// CAVLC residual decoding (spec 9.2). The VLC tables are the values from
// the H.264 specification (Tables 9-5, 9-7, 9-9, 9-10) and match the
// reference implementation's tables.

// coeff_token VLC tables (Table 9-5).
// Index: [table][4*totalCoeff + trailingOnes].
// Tables: 0=nC∈[0,1], 1=nC∈[2,3], 2=nC∈[4,7], 3=nC>=8.
var coeffTokenLen = [4][4 * 17]uint8{
	{
		1, 0, 0, 0,
		6, 2, 0, 0, 8, 6, 3, 0, 9, 8, 7, 5, 10, 9, 8, 6,
		11, 10, 9, 7, 13, 11, 10, 8, 13, 13, 11, 9, 13, 13, 13, 10,
		14, 14, 13, 11, 14, 14, 14, 13, 15, 15, 14, 14, 15, 15, 15, 14,
		16, 15, 15, 15, 16, 16, 16, 15, 16, 16, 16, 16, 16, 16, 16, 16,
	},
	{
		2, 0, 0, 0,
		6, 2, 0, 0, 6, 5, 3, 0, 7, 6, 6, 4, 8, 6, 6, 4,
		8, 7, 7, 5, 9, 8, 8, 6, 11, 9, 9, 6, 11, 11, 11, 7,
		12, 11, 11, 9, 12, 12, 12, 11, 12, 12, 12, 11, 13, 13, 13, 12,
		13, 13, 13, 13, 13, 14, 13, 13, 14, 14, 14, 13, 14, 14, 14, 14,
	},
	{
		4, 0, 0, 0,
		6, 4, 0, 0, 6, 5, 4, 0, 6, 5, 5, 4, 7, 5, 5, 4,
		7, 5, 5, 4, 7, 6, 6, 4, 7, 6, 6, 4, 8, 7, 7, 5,
		8, 8, 7, 6, 9, 8, 8, 7, 9, 9, 8, 8, 9, 9, 9, 8,
		10, 9, 9, 9, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10,
	},
	{
		6, 0, 0, 0,
		6, 6, 0, 0, 6, 6, 6, 0, 6, 6, 6, 6, 6, 6, 6, 6,
		6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
		6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
		6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	},
}

var coeffTokenBits = [4][4 * 17]uint8{
	{
		1, 0, 0, 0,
		5, 1, 0, 0, 7, 4, 1, 0, 7, 6, 5, 3, 7, 6, 5, 3,
		7, 6, 5, 4, 15, 6, 5, 4, 11, 14, 5, 4, 8, 10, 13, 4,
		15, 14, 9, 4, 11, 10, 13, 12, 15, 14, 9, 12, 11, 10, 13, 8,
		15, 1, 9, 12, 11, 14, 13, 8, 7, 10, 9, 12, 4, 6, 5, 8,
	},
	{
		3, 0, 0, 0,
		11, 2, 0, 0, 7, 7, 3, 0, 7, 10, 9, 5, 7, 6, 5, 4,
		4, 6, 5, 6, 7, 6, 5, 8, 15, 6, 5, 4, 11, 14, 13, 4,
		15, 10, 9, 4, 11, 14, 13, 12, 8, 10, 9, 8, 15, 14, 13, 12,
		11, 10, 9, 12, 7, 11, 6, 8, 9, 8, 10, 1, 7, 6, 5, 4,
	},
	{
		15, 0, 0, 0,
		15, 14, 0, 0, 11, 15, 13, 0, 8, 12, 14, 12, 15, 10, 11, 11,
		11, 8, 9, 10, 9, 14, 13, 9, 8, 10, 9, 8, 15, 14, 13, 13,
		11, 14, 10, 12, 15, 10, 13, 12, 11, 14, 9, 12, 8, 10, 13, 8,
		13, 7, 9, 12, 9, 12, 11, 10, 5, 8, 7, 6, 1, 4, 3, 2,
	},
	{
		3, 0, 0, 0,
		0, 1, 0, 0, 4, 5, 6, 0, 8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
		32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47,
		48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63,
	},
}

var coeffTokenMaxLen = [4]int{16, 14, 10, 6}

// chroma DC coeff_token (Table 9-5, nC = -1, 4:2:0).
var chromaDCCoeffTokenLen = [4 * 5]uint8{
	2, 0, 0, 0,
	6, 1, 0, 0,
	6, 6, 3, 0,
	6, 7, 7, 6,
	6, 8, 8, 7,
}

var chromaDCCoeffTokenBits = [4 * 5]uint8{
	1, 0, 0, 0,
	7, 1, 0, 0,
	4, 6, 1, 0,
	3, 3, 2, 5,
	2, 3, 2, 0,
}

// total_zeros (Table 9-7), indexed [totalCoeff-1][totalZeros].
var totalZerosLen = [16][16]uint8{
	{1, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 9},
	{3, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 6, 6, 6, 6},
	{4, 3, 3, 3, 4, 4, 3, 3, 4, 5, 5, 6, 5, 6},
	{5, 3, 4, 4, 3, 3, 3, 4, 3, 4, 5, 5, 5},
	{4, 4, 4, 3, 3, 3, 3, 3, 4, 5, 4, 5},
	{6, 5, 3, 3, 3, 3, 3, 3, 4, 3, 6},
	{6, 5, 3, 3, 3, 2, 3, 4, 3, 6},
	{6, 4, 5, 3, 2, 2, 3, 3, 6},
	{6, 6, 4, 2, 2, 3, 2, 5},
	{5, 5, 3, 2, 2, 2, 4},
	{4, 4, 3, 3, 1, 3},
	{4, 4, 2, 1, 3},
	{3, 3, 1, 2},
	{2, 2, 1},
	{1, 1},
}

var totalZerosBits = [16][16]uint8{
	{1, 3, 2, 3, 2, 3, 2, 3, 2, 3, 2, 3, 2, 3, 2, 1},
	{7, 6, 5, 4, 3, 5, 4, 3, 2, 3, 2, 3, 2, 1, 0},
	{5, 7, 6, 5, 4, 3, 4, 3, 2, 3, 2, 1, 1, 0},
	{3, 7, 5, 4, 6, 5, 4, 3, 3, 2, 2, 1, 0},
	{5, 4, 3, 7, 6, 5, 4, 3, 2, 1, 1, 0},
	{1, 1, 7, 6, 5, 4, 3, 2, 1, 1, 0},
	{1, 1, 5, 4, 3, 3, 2, 1, 1, 0},
	{1, 1, 1, 3, 3, 2, 2, 1, 0},
	{1, 0, 1, 3, 2, 1, 1, 1},
	{1, 0, 1, 3, 2, 1, 1},
	{0, 1, 1, 2, 1, 3},
	{0, 1, 1, 1, 1},
	{0, 1, 1, 1},
	{0, 1, 1},
	{0, 1},
}

// chroma DC total_zeros (Table 9-9), indexed [totalCoeff-1][totalZeros].
var chromaDCTotalZerosLen = [3][4]uint8{
	{1, 2, 3, 3},
	{1, 2, 2, 0},
	{1, 1, 0, 0},
}

var chromaDCTotalZerosBits = [3][4]uint8{
	{1, 1, 1, 0},
	{1, 1, 0, 0},
	{1, 0, 0, 0},
}

// run_before (Table 9-10), indexed [min(zerosLeft-1,6)][runBefore].
var runBeforeLen = [7][16]uint8{
	{1, 1},
	{1, 2, 2},
	{2, 2, 2, 2},
	{2, 2, 2, 3, 3},
	{2, 2, 3, 3, 3, 3},
	{2, 3, 3, 3, 3, 3, 3},
	{3, 3, 3, 3, 3, 3, 3, 4, 5, 6, 7, 8, 9, 10, 11},
}

var runBeforeBits = [7][16]uint8{
	{1, 0},
	{1, 1, 0},
	{3, 2, 1, 0},
	{3, 2, 1, 1, 0},
	{3, 2, 3, 2, 1, 0},
	{3, 0, 1, 3, 2, 5, 4},
	{7, 6, 5, 4, 3, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1},
}

func coeffTokenTableIdx(nC int) int {
	switch {
	case nC <= 1:
		return 0
	case nC <= 3:
		return 1
	case nC <= 7:
		return 2
	default:
		return 3
	}
}

// decodeCoeffToken reads a coeff_token VLC. nC is the predicted non-zero
// neighbour count (-1 selects the chroma-DC table).
func decodeCoeffToken(r *bitReader, nC int) (totalCoeff, trailingOnes int, err error) {
	if nC == -1 {
		const maxLen = 8
		peeked := r.peekBits(maxLen)
		if r.err() != nil {
			return 0, 0, r.err()
		}
		for tc := 0; tc <= 4; tc++ {
			maxT1 := min(tc, 3)
			for t1 := 0; t1 <= maxT1; t1++ {
				idx := 4*tc + t1
				codeLen := int(chromaDCCoeffTokenLen[idx])
				if codeLen == 0 {
					continue
				}
				if peeked>>uint(maxLen-codeLen) == uint32(chromaDCCoeffTokenBits[idx]) {
					r.skip(codeLen)
					return tc, t1, nil
				}
			}
		}
		return 0, 0, errf("h264: no chroma DC coeff_token match")
	}

	tableIdx := coeffTokenTableIdx(nC)
	maxLen := coeffTokenMaxLen[tableIdx]
	peeked := r.peekBits(maxLen)
	if r.err() != nil {
		return 0, 0, r.err()
	}
	for tc := 0; tc <= 16; tc++ {
		maxT1 := min(tc, 3)
		for t1 := 0; t1 <= maxT1; t1++ {
			idx := 4*tc + t1
			codeLen := int(coeffTokenLen[tableIdx][idx])
			if codeLen == 0 {
				continue
			}
			if peeked>>uint(maxLen-codeLen) == uint32(coeffTokenBits[tableIdx][idx]) {
				r.skip(codeLen)
				return tc, t1, nil
			}
		}
	}
	return 0, 0, errf("h264: no coeff_token match")
}

// decodeLevelPrefix counts leading zeros up to and including the first 1.
func decodeLevelPrefix(r *bitReader) (int, error) {
	zeros := 0
	for {
		b := r.readBit()
		if r.err() != nil {
			return 0, r.err()
		}
		if b == 1 {
			return zeros, nil
		}
		zeros++
		if zeros > 25 {
			return 0, errf("h264: level_prefix too long")
		}
	}
}

func decodeTotalZeros(r *bitReader, totalCoeff, maxNumCoeff int) (int, error) {
	if maxNumCoeff == 4 {
		return decodeChromaDCTotalZeros(r, totalCoeff)
	}
	row := totalCoeff - 1
	if row < 0 || row >= 15 {
		return 0, errf("h264: invalid totalCoeff for total_zeros")
	}
	maxZeros := maxNumCoeff - totalCoeff
	maxLen := 0
	for tz := 0; tz <= maxZeros && tz < 16; tz++ {
		if int(totalZerosLen[row][tz]) > maxLen {
			maxLen = int(totalZerosLen[row][tz])
		}
	}
	peeked := r.peekBits(maxLen)
	if r.err() != nil {
		return 0, r.err()
	}
	for tz := 0; tz <= maxZeros && tz < 16; tz++ {
		l := int(totalZerosLen[row][tz])
		if l == 0 {
			continue
		}
		if peeked>>uint(maxLen-l) == uint32(totalZerosBits[row][tz]) {
			r.skip(l)
			return tz, nil
		}
	}
	return 0, errf("h264: no total_zeros match")
}

func decodeChromaDCTotalZeros(r *bitReader, totalCoeff int) (int, error) {
	row := totalCoeff - 1
	if row < 0 || row >= 3 {
		return 0, errf("h264: invalid chroma DC totalCoeff")
	}
	maxZeros := 4 - totalCoeff
	maxLen := 0
	for tz := 0; tz <= maxZeros && tz < 4; tz++ {
		if int(chromaDCTotalZerosLen[row][tz]) > maxLen {
			maxLen = int(chromaDCTotalZerosLen[row][tz])
		}
	}
	peeked := r.peekBits(maxLen)
	if r.err() != nil {
		return 0, r.err()
	}
	for tz := 0; tz <= maxZeros && tz < 4; tz++ {
		l := int(chromaDCTotalZerosLen[row][tz])
		if l == 0 {
			continue
		}
		if peeked>>uint(maxLen-l) == uint32(chromaDCTotalZerosBits[row][tz]) {
			r.skip(l)
			return tz, nil
		}
	}
	return 0, errf("h264: no chroma DC total_zeros match")
}

func decodeRunBefore(r *bitReader, zerosLeft int) (int, error) {
	if zerosLeft <= 0 {
		return 0, nil
	}
	row := min(zerosLeft-1, 6)
	maxLen := 0
	for rb := 0; rb <= zerosLeft && rb < 16; rb++ {
		if int(runBeforeLen[row][rb]) > maxLen {
			maxLen = int(runBeforeLen[row][rb])
		}
	}
	peeked := r.peekBits(maxLen)
	if r.err() != nil {
		return 0, r.err()
	}
	maxRB := zerosLeft
	if row == 6 && maxRB > 14 {
		maxRB = 14
	} else if row < 6 && maxRB > row+1 {
		maxRB = row + 1
	}
	for rb := 0; rb <= maxRB && rb < 16; rb++ {
		l := int(runBeforeLen[row][rb])
		if l == 0 {
			continue
		}
		if peeked>>uint(maxLen-l) == uint32(runBeforeBits[row][rb]) {
			r.skip(l)
			return rb, nil
		}
	}
	return 0, errf("h264: no run_before match")
}

// decodeResidualBlock decodes one CAVLC residual block and returns the
// coefficients in scan order (length maxNumCoeff) plus totalCoeff.
// nC is the predicted non-zero coefficient count (-1 for chroma DC).
// maxNumCoeff is 4 (chroma DC), 15 (AC-only) or 16 (4x4).
func decodeResidualBlock(r *bitReader, nC, maxNumCoeff int) ([]int32, int, error) {
	coeffs := make([]int32, maxNumCoeff)

	totalCoeff, trailingOnes, err := decodeCoeffToken(r, nC)
	if err != nil {
		return coeffs, 0, err
	}
	if totalCoeff == 0 {
		return coeffs, 0, nil
	}

	levels := make([]int32, totalCoeff)
	for i := 0; i < trailingOnes; i++ {
		sign := r.readBit()
		if r.err() != nil {
			return coeffs, 0, r.err()
		}
		if sign == 0 {
			levels[i] = 1
		} else {
			levels[i] = -1
		}
	}

	suffixLength := 0
	if totalCoeff > 10 && trailingOnes < 3 {
		suffixLength = 1
	}
	for i := trailingOnes; i < totalCoeff; i++ {
		levelPrefix, err := decodeLevelPrefix(r)
		if err != nil {
			return coeffs, 0, err
		}
		levelSuffixSize := suffixLength
		if levelPrefix == 14 && suffixLength == 0 {
			levelSuffixSize = 4
		} else if levelPrefix >= 15 {
			levelSuffixSize = levelPrefix - 3
		}
		var levelCode int
		if levelSuffixSize > 0 {
			suffix := int(r.u(levelSuffixSize))
			if r.err() != nil {
				return coeffs, 0, r.err()
			}
			levelCode = (min(levelPrefix, 15) << uint(suffixLength)) + suffix
		} else {
			levelCode = levelPrefix
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
		if levelCode%2 == 0 {
			levels[i] = int32(levelCode/2 + 1)
		} else {
			levels[i] = int32(-(levelCode + 1) / 2)
		}
		if suffixLength == 0 {
			suffixLength = 1
		}
		abs := levels[i]
		if abs < 0 {
			abs = -abs
		}
		if int(abs) > (3<<uint(suffixLength-1)) && suffixLength < 6 {
			suffixLength++
		}
	}

	zerosLeft := 0
	if totalCoeff < maxNumCoeff {
		zerosLeft, err = decodeTotalZeros(r, totalCoeff, maxNumCoeff)
		if err != nil {
			return coeffs, 0, err
		}
	}

	coeffIdx := totalCoeff + zerosLeft - 1
	for i := 0; i < totalCoeff-1; i++ {
		runBefore := 0
		if zerosLeft > 0 {
			runBefore, err = decodeRunBefore(r, zerosLeft)
			if err != nil {
				return coeffs, 0, err
			}
		}
		if coeffIdx >= 0 && coeffIdx < maxNumCoeff {
			coeffs[coeffIdx] = levels[i]
		}
		coeffIdx -= 1 + runBefore
		zerosLeft -= runBefore
	}
	if coeffIdx >= 0 && coeffIdx < maxNumCoeff {
		coeffs[coeffIdx] = levels[totalCoeff-1]
	}
	return coeffs, totalCoeff, nil
}
