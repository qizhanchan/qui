package h264

// Slice-data decoding (macroblock layer) and picture reconstruction.

// picState is the per-picture decoding state shared by all slices.
type picState struct {
	sps                  *SPS
	pps                  *PPS
	mbWidth, mbHeight    int
	chromaArrayType      int
	chromaQpIndexOffset  int
	transform8x8ModeFlag bool

	qpY         int // running QP for the current slice
	curSliceNum int

	mbs []mbData
	br  *bitReader

	slices []sliceDeblock

	// Reference pictures (list 0 and list 1), most recent first.
	refFrames   []*Frame
	refFramesL1 []*Frame
	refPOCL0    []int
	refPOCL1    []int
	curPOC      int
	// Per-4x4-block motion state for the current picture (for MV prediction):
	// refLX >= 0 is a reference index, -1 is intra, -2 is not-yet-available.
	mvLX, mvLY    []int
	refLX         []int
	mvL1X, mvL1Y  []int
	refL1X        []int
	motionInit    bool
	directSpatial bool
	coloc         *refPicture
	numRefL0      int
	numRefL1      int

	weightedPred   bool
	lumaLog2WD     int
	chromaLog2WD   int
	lumaWeightL0   []int
	lumaOffsetL0   []int
	chromaWeightL0 [][2]int
	chromaOffsetL0 [][2]int
	lumaWeightL1   []int
	lumaOffsetL1   []int
	chromaWeightL1 [][2]int
	chromaOffsetL1 [][2]int
}

type availability struct{ left, top, topLeft, topRight bool }

// mbNeighbors returns which neighbouring macroblocks are available.
func (s *picState) mbNeighbors(mbIdx int) availability {
	return availability{
		left:     s.mbAvailA(mbIdx) != nil,
		top:      s.mbAvailB(mbIdx) != nil,
		topLeft:  s.mbAvailD(mbIdx) != nil,
		topRight: s.mbAvailC(mbIdx) != nil,
	}
}

// decodeSliceCAVLC dispatches one CAVLC slice by slice type. Deblocking is
// applied separately, after the whole picture is reconstructed.
func (s *picState) decodeSliceCAVLC(f *Frame, hdr *SliceHeader) error {
	switch {
	case sliceTypeIsI(hdr.SliceType) || sliceTypeIsSI(hdr.SliceType):
		return s.decodeISliceCAVLC(f, hdr)
	case sliceTypeIsP(hdr.SliceType) || sliceTypeIsSP(hdr.SliceType):
		return s.decodePSliceCAVLC(f, hdr)
	case sliceTypeIsB(hdr.SliceType):
		return s.decodeBSliceCAVLC(f, hdr)
	default:
		return errf("h264: unsupported slice type")
	}
}

// decodeISliceCAVLC decodes all macroblocks of one I-slice into f.
func (s *picState) decodeISliceCAVLC(f *Frame, hdr *SliceHeader) error {
	s.curSliceNum++
	s.slices = append(s.slices, sliceDeblock{
		disableIDC:  hdr.DisableDeblockingFilterIDC,
		alphaOffset: 52 + int(hdr.SliceAlphaC0OffsetDiv2)*2,
		betaOffset:  52 + int(hdr.SliceBetaOffsetDiv2)*2,
	})
	for mbIdx := int(hdr.FirstMbInSlice); ; mbIdx++ {
		if mbIdx >= len(s.mbs) {
			return errf("h264: macroblock index out of range")
		}
		mb := &s.mbs[mbIdx]
		mb.sliceNum = s.curSliceNum
		mt := int(s.br.ue())
		if s.br.err() != nil {
			return s.br.err()
		}
		if mt > 25 {
			return errf("h264: mb_type out of range")
		}
		mb.mbType = mt
		if err := s.decodeIntraBody(f, mbIdx); err != nil {
			return err
		}
		if !s.br.moreRBSPData() {
			return nil
		}
	}
}

// decodeIntraBody decodes and reconstructs an intra macroblock whose mb_type
// (0..25) has already been stored.
func (s *picState) decodeIntraBody(f *Frame, mbIdx int) error {
	mb := &s.mbs[mbIdx]

	if mb.mbType == 25 { // I_PCM
		return s.decodeIPCM(f, mbIdx)
	}

	if mb.mbType == 0 { // I_NxN
		if s.transform8x8ModeFlag {
			mb.transform8x8 = s.br.u1()
		}
		if mb.transform8x8 {
			for i := 0; i < 4; i++ {
				mb.intra8x8PredMode[i] = s.decodeIntra8x8PredMode(mbIdx, i)
			}
		} else {
			for i := 0; i < 16; i++ {
				mb.intra4x4PredMode[i] = s.decodeIntra4x4PredMode(mbIdx, i)
			}
		}
		if s.chromaArrayType != 0 {
			mb.intraChromaMode = int(s.br.ue())
		}
		cbp := int(s.br.ue())
		if cbp > 47 {
			return errf("h264: cbp out of range")
		}
		v := int(golombToIntra4x4CBP[cbp])
		mb.cbpLuma = v & 0x0f
		mb.cbpChroma = (v >> 4) & 0x03
	} else { // I_16x16
		mb.intraPredMode16 = i16x16PredMode(mb.mbType)
		mb.cbpLuma = i16x16CBPLuma(mb.mbType)
		mb.cbpChroma = i16x16CBPChroma(mb.mbType)
		if s.chromaArrayType != 0 {
			mb.intraChromaMode = int(s.br.ue())
		}
	}

	if mb.cbpLuma > 0 || mb.cbpChroma > 0 || (mb.mbType >= 1 && mb.mbType <= 24) {
		delta := int(s.br.se())
		if s.br.err() != nil {
			return s.br.err()
		}
		mb.qpDelta = delta
		mb.qpY = clampInt(s.qpY+delta, 0, 51)
	} else {
		mb.qpY = s.qpY
	}
	s.qpY = mb.qpY

	if err := s.decodeResidualMBCAVLC(mbIdx); err != nil {
		return err
	}
	return s.reconstructMB(f, mbIdx)
}

func (s *picState) decodeResidualMBCAVLC(mbIdx int) error {
	mb := &s.mbs[mbIdx]

	if mb.mbType >= 1 && mb.mbType <= 24 {
		nC := s.deriveNC(mbIdx, 0)
		dc, _, err := decodeResidualBlock(s.br, nC, 16)
		if err != nil {
			return err
		}
		copy(mb.intra16x16DC[:], dc)

		if mb.cbpLuma > 0 {
			for i := 0; i < 16; i++ {
				if mb.cbpLuma&(1<<uint(i/4)) == 0 {
					continue
				}
				nC := s.deriveNC(mbIdx, i)
				ac, tc, err := decodeResidualBlock(s.br, nC, 15)
				if err != nil {
					return err
				}
				copy(mb.intra16x16AC[i][:], ac)
				mb.nzLuma[i] = tc
			}
		}
	} else if mb.mbType == 0 {
		if mb.transform8x8 {
			for i8x8 := 0; i8x8 < 4; i8x8++ {
				if mb.cbpLuma&(1<<uint(i8x8)) == 0 {
					continue
				}
				for i4x4 := 0; i4x4 < 4; i4x4++ {
					blkIdx := i8x8*4 + i4x4
					nC := s.deriveNC(mbIdx, blkIdx)
					coeffs, tc, err := decodeResidualBlock(s.br, nC, 16)
					if err != nil {
						return err
					}
					for j := 0; j < 16; j++ {
						mb.lumaLevel8x8[i8x8][zigzagScan8x8CAVLC[i4x4*16+j]] = coeffs[j]
					}
					mb.nzLuma[blkIdx] = tc
				}
			}
		} else {
			for i := 0; i < 16; i++ {
				if mb.cbpLuma&(1<<uint(i/4)) == 0 {
					continue
				}
				nC := s.deriveNC(mbIdx, i)
				coeffs, tc, err := decodeResidualBlock(s.br, nC, 16)
				if err != nil {
					return err
				}
				copy(mb.lumaLevel4x4[i][:], coeffs)
				mb.nzLuma[i] = tc
			}
		}
	}

	if s.chromaArrayType != 0 && mb.cbpChroma > 0 {
		for c := 0; c < 2; c++ {
			dc, _, err := decodeResidualBlock(s.br, -1, 4)
			if err != nil {
				return err
			}
			copy(mb.chromaDC[c][:], dc)
		}
		if mb.cbpChroma > 1 {
			for c := 0; c < 2; c++ {
				for i := 0; i < 4; i++ {
					blk := c*4 + i
					nC := s.deriveChromaNC(mbIdx, blk)
					ac, tc, err := decodeResidualBlock(s.br, nC, 15)
					if err != nil {
						return err
					}
					copy(mb.chromaAC[c][i][:], ac)
					mb.nzChroma[blk] = tc
				}
			}
		}
	}
	return nil
}

// decodeIntra4x4PredMode reads prev_intra4x4_pred_mode_flag / rem and derives
// the mode (spec 7.4.5).
func (s *picState) decodeIntra4x4PredMode(mbIdx, blkIdx int) int {
	prev := s.br.u1()
	var rem int
	if !prev {
		rem = int(s.br.u(3))
	}
	predicted := s.derivePredIntra4x4PredMode(mbIdx, blkIdx)
	if prev {
		return predicted
	}
	if rem < predicted {
		return rem
	}
	return rem + 1
}

func (s *picState) derivePredIntra4x4PredMode(mbIdx, blkIdx int) int {
	modeA, modeB := -1, -1
	if li := lumaLeftNeighbor[blkIdx]; li >= 0 {
		modeA = s.mbs[mbIdx].intra4x4PredMode[li]
	} else if a := s.mbAvailA(mbIdx); a != nil {
		switch {
		case a.mbType == 0 && a.transform8x8:
			modeA = a.intra8x8PredMode[lumaLeftFromMBA8x8[blkIdx]]
		case a.mbType == 0:
			if li := lumaLeftFromMBA[blkIdx]; li >= 0 {
				modeA = a.intra4x4PredMode[li]
			}
		default:
			modeA = 2
		}
	}
	if ti := lumaTopNeighbor[blkIdx]; ti >= 0 {
		modeB = s.mbs[mbIdx].intra4x4PredMode[ti]
	} else if b := s.mbAvailB(mbIdx); b != nil {
		switch {
		case b.mbType == 0 && b.transform8x8:
			modeB = b.intra8x8PredMode[lumaTopFromMBB8x8[blkIdx]]
		case b.mbType == 0:
			if ti := lumaTopFromMBB[blkIdx]; ti >= 0 {
				modeB = b.intra4x4PredMode[ti]
			}
		default:
			modeB = 2
		}
	}
	if modeA < 0 || modeB < 0 {
		return 2 // a neighbour is unavailable
	}
	if modeA < modeB {
		return modeA
	}
	return modeB
}

// decodeIntra8x8PredMode reads prev_intra8x8_pred_mode_flag / rem and derives
// the mode (spec 7.4.5).
func (s *picState) decodeIntra8x8PredMode(mbIdx, blkIdx int) int {
	prev := s.br.u1()
	var rem int
	if !prev {
		rem = int(s.br.u(3))
	}
	predicted := s.derivePredIntra8x8PredMode(mbIdx, blkIdx)
	if prev {
		return predicted
	}
	if rem < predicted {
		return rem
	}
	return rem + 1
}

func (s *picState) derivePredIntra8x8PredMode(mbIdx, blkIdx int) int {
	x, y := blkIdx%2, blkIdx/2
	modeA, modeB := -1, -1
	if x > 0 {
		modeA = s.mbs[mbIdx].intra8x8PredMode[blkIdx-1]
	} else if a := s.mbAvailA(mbIdx); a != nil {
		switch {
		case a.mbType == 0 && a.transform8x8:
			modeA = a.intra8x8PredMode[y*2+1]
		case a.mbType == 0:
			modeA = a.intra4x4PredMode[zScanToRaster[2*y*4+3]]
		default:
			modeA = 2
		}
	}
	if y > 0 {
		modeB = s.mbs[mbIdx].intra8x8PredMode[blkIdx-2]
	} else if b := s.mbAvailB(mbIdx); b != nil {
		switch {
		case b.mbType == 0 && b.transform8x8:
			modeB = b.intra8x8PredMode[2+x]
		case b.mbType == 0:
			modeB = b.intra4x4PredMode[zScanToRaster[3*4+2*x]]
		default:
			modeB = 2
		}
	}
	if modeA < 0 || modeB < 0 {
		return 2
	}
	if modeA < modeB {
		return modeA
	}
	return modeB
}

func (s *picState) decodeIPCM(f *Frame, mbIdx int) error {
	s.br.align()
	if s.br.err() != nil {
		return s.br.err()
	}
	mbX := (mbIdx % s.mbWidth) * 16
	mbY := (mbIdx / s.mbWidth) * 16
	y := s.br.readBytes(256)
	if y == nil {
		return s.br.err()
	}
	for r := 0; r < 16; r++ {
		for c := 0; c < 16; c++ {
			f.Y.Set(mbX+c, mbY+r, y[r*16+c])
		}
	}
	for comp := 0; comp < 2; comp++ {
		buf := s.br.readBytes(64)
		if buf == nil {
			return s.br.err()
		}
		plane := f.Cb
		if comp == 1 {
			plane = f.Cr
		}
		for r := 0; r < 8; r++ {
			for c := 0; c < 8; c++ {
				plane.Set(mbX/2+c, mbY/2+r, buf[r*8+c])
			}
		}
	}
	return nil
}
