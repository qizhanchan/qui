package h264

// mbData holds the decoded syntax and residual coefficients of one
// macroblock. Only the subset needed by the intra CAVLC path is populated.
type mbData struct {
	sliceNum int

	mbType           int
	transform8x8     bool
	intraPredMode16  int
	intra4x4PredMode [16]int
	intra8x8PredMode [4]int
	transformSize8x8 [4]bool
	intraChromaMode  int

	cbpLuma   int
	cbpChroma int
	qpY       int
	qpDelta   int

	intra16x16DC [16]int32
	intra16x16AC [16][15]int32
	lumaLevel4x4 [16][16]int32
	lumaLevel8x8 [4][64]int32
	chromaDC     [2][4]int32
	chromaAC     [2][4][15]int32

	nzLuma   [16]int
	nzChroma [8]int

	// Inter (P-slice) fields. A macroblock is decomposed into up to 16
	// sub-partitions, each with its own reference and motion vector (in
	// quarter-luma-samples); positions/sizes are in 4x4 luma units.
	isInter     bool
	numSubParts int
	subX, subY  [16]int
	subW, subH  [16]int
	subRef      [16]int
	subMv       [16][2]int
	subPred     [16]int // 0 = L0, 1 = L1, 2 = bi-prediction
	subRef1     [16]int
	subMv1      [16][2]int
	// subGroup is the reference-index group of each sub-partition. For
	// 16x16/16x8/8x16 it equals the partition index; for B_8x8 the four
	// sub-partitions of a quadrant share one reference index (spec 7.3.5.2).
	subGroup [16]int
}

// luma neighbour block-index maps for nC derivation (spec 9.2.1).
var (
	lumaLeftNeighbor = [16]int{-1, 0, -1, 2, 1, 4, 3, 6, -1, 8, -1, 10, 9, 12, 11, 14}
	lumaTopNeighbor  = [16]int{-1, -1, 0, 1, -1, -1, 4, 5, 2, 3, 8, 9, 6, 7, 12, 13}
	lumaLeftFromMBA  = [16]int{5, -1, 7, -1, -1, -1, -1, -1, 13, -1, 15, -1, -1, -1, -1, -1}
	lumaTopFromMBB   = [16]int{10, 11, -1, -1, 14, 15, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}

	// 8x8 mode indices within an I_8x8 neighbour, indexed by the 4x4 block
	// (z-scan) index (spec 8.3.1.1).
	lumaLeftFromMBA8x8 = [16]int{1, 1, 1, 1, 1, 1, 1, 1, 3, 3, 3, 3, 3, 3, 3, 3}
	lumaTopFromMBB8x8  = [16]int{2, 2, 2, 2, 3, 3, 3, 3, 2, 2, 2, 2, 3, 3, 3, 3}
)

// deriveNC computes nC for a luma block from neighbour non-zero counts
// (spec 9.2.1).
func (s *picState) deriveNC(mbIdx, blkIdx int) int {
	mb := &s.mbs[mbIdx]
	nA, nB := -1, -1

	if li := lumaLeftNeighbor[blkIdx]; li >= 0 {
		nA = mb.nzLuma[li]
	} else if a := s.mbAvailA(mbIdx); a != nil {
		if li := lumaLeftFromMBA[blkIdx]; li >= 0 {
			nA = a.nzLuma[li]
		}
	}
	if ti := lumaTopNeighbor[blkIdx]; ti >= 0 {
		nB = mb.nzLuma[ti]
	} else if b := s.mbAvailB(mbIdx); b != nil {
		if ti := lumaTopFromMBB[blkIdx]; ti >= 0 {
			nB = b.nzLuma[ti]
		}
	}
	return combineNC(nA, nB)
}

// deriveChromaNC computes nC for a chroma AC block (blkIdx = comp*4 + i).
func (s *picState) deriveChromaNC(mbIdx, blkIdx int) int {
	mb := &s.mbs[mbIdx]
	nA, nB := -1, -1
	compBase := (blkIdx / 4) * 4
	local := blkIdx % 4
	x, y := local%2, local/2

	if x > 0 {
		nA = mb.nzChroma[compBase+local-1]
	} else if a := s.mbAvailA(mbIdx); a != nil {
		nA = a.nzChroma[compBase+y*2+1]
	}
	if y > 0 {
		nB = mb.nzChroma[compBase+local-2]
	} else if b := s.mbAvailB(mbIdx); b != nil {
		nB = b.nzChroma[compBase+2+x]
	}
	return combineNC(nA, nB)
}

func combineNC(nA, nB int) int {
	switch {
	case nA >= 0 && nB >= 0:
		return (nA + nB + 1) >> 1
	case nA >= 0:
		return nA
	case nB >= 0:
		return nB
	default:
		return 0
	}
}

// mbAvailA / B return the left / top neighbour if it is inside the picture
// and in the same slice (clause 6.4.9).
func (s *picState) mbAvailA(mbIdx int) *mbData {
	if mbIdx%s.mbWidth == 0 {
		return nil
	}
	return s.sameSlice(mbIdx, mbIdx-1)
}

func (s *picState) mbAvailB(mbIdx int) *mbData {
	if mbIdx < s.mbWidth {
		return nil
	}
	return s.sameSlice(mbIdx, mbIdx-s.mbWidth)
}

func (s *picState) mbAvailC(mbIdx int) *mbData {
	if mbIdx < s.mbWidth || (mbIdx+1)%s.mbWidth == 0 {
		return nil
	}
	return s.sameSlice(mbIdx, mbIdx-s.mbWidth+1)
}

func (s *picState) mbAvailD(mbIdx int) *mbData {
	if mbIdx < s.mbWidth || mbIdx%s.mbWidth == 0 {
		return nil
	}
	return s.sameSlice(mbIdx, mbIdx-s.mbWidth-1)
}

func (s *picState) sameSlice(mbIdx, nIdx int) *mbData {
	n := &s.mbs[nIdx]
	if n.sliceNum != s.mbs[mbIdx].sliceNum {
		return nil
	}
	return n
}
