package h264

// P-slice inter decoding (spec 7.3.4, 7.4.5, 8.4.1.3). Only a single reference
// list L0 is used; reference list reordering/weighted prediction are not
// implemented (they are absent from Baseline streams in the common case).

// refNotAvail / refNotInList mirror OpenH264's cache sentinels.
const (
	refNotAvail  = -2 // block not yet decoded / outside the picture
	refNotInList = -1 // block is intra-coded
)

// initMotionState resets the per-4x4 motion arrays for a new picture.
func (s *picState) initMotionState() {
	n := s.mbWidth * 4 * s.mbHeight * 4
	s.mvLX = make([]int, n)
	s.mvLY = make([]int, n)
	s.refLX = make([]int, n)
	s.mvL1X = make([]int, n)
	s.mvL1Y = make([]int, n)
	s.refL1X = make([]int, n)
	for i := range s.refLX {
		s.refLX[i] = refNotAvail
		s.refL1X[i] = refNotAvail
	}
}

// setMotion records the motion of a sub-partition (4x4 units within the MB)
// for the given list (0 or 1).
func (s *picState) setMotion(list, bx, by, w4, h4, ref, mvx, mvy int) {
	stride := s.mbWidth * 4
	mvX, mvY, refA := s.mvLX, s.mvLY, s.refLX
	if list == 1 {
		mvX, mvY, refA = s.mvL1X, s.mvL1Y, s.refL1X
	}
	for y := by; y < by+h4; y++ {
		for x := bx; x < bx+w4; x++ {
			idx := y*stride + x
			refA[idx] = ref
			mvX[idx] = mvx
			mvY[idx] = mvy
		}
	}
}

// markIntra records that the 4x4 blocks of a macroblock are intra-coded.
func (s *picState) markIntra(mbIdx int) {
	bx0 := (mbIdx % s.mbWidth) * 4
	by0 := (mbIdx / s.mbWidth) * 4
	s.setMotion(0, bx0, by0, 4, 4, refNotInList, 0, 0)
	s.setMotion(1, bx0, by0, 4, 4, refNotInList, 0, 0)
}

// motionAt returns the reference index and MV of the 4x4 block at (x,y) for the
// given list, or refNotAvail when it is outside the picture or a different slice.
func (s *picState) motionAt(list, x, y, curSlot int) (int, int, int) {
	if x < 0 || y < 0 || x >= s.mbWidth*4 || y >= s.mbHeight*4 {
		return refNotAvail, 0, 0
	}
	idx := y*s.mbWidth*4 + x
	mvX, mvY, refA := s.mvLX, s.mvLY, s.refLX
	if list == 1 {
		mvX, mvY, refA = s.mvL1X, s.mvL1Y, s.refL1X
	}
	if refA[idx] == refNotAvail {
		return refNotAvail, 0, 0
	}
	nMB := (y/4)*s.mbWidth + (x / 4)
	if s.mbs[nMB].sliceNum != curSlot {
		return refNotAvail, 0, 0
	}
	return refA[idx], mvX[idx], mvY[idx]
}

func median3(a, b, c int) int {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b, c = c, b
	}
	if a > b {
		b = a
	}
	return b
}

// predictMv is the H.264 median motion-vector predictor for a sub-partition
// whose top-left 4x4 block is (bx,by) with width w4 4x4-blocks (spec 8.4.1.3.1).
func (s *picState) predictMv(mbIdx, list, bx, by, w4, curRef int) (int, int) {
	slot := s.mbs[mbIdx].sliceNum
	refA, ax, ay := s.motionAt(list, bx-1, by, slot)
	refB, bxm, bym := s.motionAt(list, bx, by-1, slot)
	refC, cx, cy := s.motionAt(list, bx+w4, by-1, slot)
	if refC == refNotAvail {
		refC, cx, cy = s.motionAt(list, bx-1, by-1, slot) // top-left fallback
	}

	if refB == refNotAvail && refC == refNotAvail && refA >= refNotInList {
		return ax, ay
	}
	match := 0
	if curRef == refA {
		match++
	}
	if curRef == refB {
		match++
	}
	if curRef == refC {
		match++
	}
	if match == 1 {
		switch {
		case curRef == refA:
			return ax, ay
		case curRef == refB:
			return bxm, bym
		default:
			return cx, cy
		}
	}
	return median3(ax, bxm, cx), median3(ay, bym, cy)
}

// buildList0 sets L0 from the reference pictures (most recent first). Only the
// default ordering with no reordering is implemented.
func (s *picState) buildList0() {
	// refFrames is already ordered most-recent-first by the decoder.
}

// decodePSliceCAVLC decodes a full P slice.
func (s *picState) decodePSliceCAVLC(f *Frame, hdr *SliceHeader) error {
	s.curSliceNum++
	s.slices = append(s.slices, sliceDeblock{
		disableIDC:  hdr.DisableDeblockingFilterIDC,
		alphaOffset: 52 + int(hdr.SliceAlphaC0OffsetDiv2)*2,
		betaOffset:  52 + int(hdr.SliceBetaOffsetDiv2)*2,
	})
	if len(s.refFrames) == 0 {
		return errf("h264: P slice without a reference picture")
	}
	if !s.motionInit {
		s.initMotionState()
		s.motionInit = true
	}
	mbIdx := int(hdr.FirstMbInSlice)
	for s.br.moreRBSPData() {
		skipRun := int(s.br.ue())
		if s.br.err() != nil {
			return s.br.err()
		}
		for k := 0; k < skipRun; k++ {
			if mbIdx >= len(s.mbs) {
				return errf("h264: too many skipped macroblocks")
			}
			s.mbs[mbIdx].sliceNum = s.curSliceNum
			if err := s.decodePSkip(f, mbIdx); err != nil {
				return err
			}
			mbIdx++
		}
		if !s.br.moreRBSPData() {
			break
		}
		if mbIdx >= len(s.mbs) {
			return errf("h264: macroblock index out of range")
		}
		mt := int(s.br.ue())
		if s.br.err() != nil {
			return s.br.err()
		}
		s.mbs[mbIdx].sliceNum = s.curSliceNum
		if err := s.decodePBody(f, mbIdx, mt); err != nil {
			return err
		}
		mbIdx++
	}
	return nil
}

// decodePSkip decodes a P_Skip macroblock: 16x16, ref 0, predicted MV.
func (s *picState) decodePSkip(f *Frame, mbIdx int) error {
	mb := &s.mbs[mbIdx]
	mb.isInter = true
	mb.numSubParts = 1
	mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 4
	mb.subRef[0] = 0
	bx0 := (mbIdx % s.mbWidth) * 4
	by0 := (mbIdx / s.mbWidth) * 4
	mvx, mvy := s.predictMv(mbIdx, 0, bx0, by0, 4, 0)
	mb.subMv[0] = [2]int{mvx, mvy}
	s.setMotion(0, bx0, by0, 4, 4, 0, mvx, mvy)
	mb.cbpLuma, mb.cbpChroma = 0, 0
	mb.qpY = s.qpY
	return s.reconstructInter(f, mbIdx)
}

// decodePBody decodes a non-skip P macroblock.
func (s *picState) decodePBody(f *Frame, mbIdx, mt int) error {
	mb := &s.mbs[mbIdx]
	if mt >= 5 { // intra-coded macroblock inside a P slice
		mb.mbType = mt - 5
		s.markIntra(mbIdx)
		return s.decodeIntraBody(f, mbIdx)
	}
	mb.isInter = true
	mb.mbType = mt

	switch mt {
	case 0: // P_L0_16x16
		mb.numSubParts = 1
		mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 4
	case 1: // P_L0_L0_16x8
		mb.numSubParts = 2
		mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 2
		mb.subX[1], mb.subY[1], mb.subW[1], mb.subH[1] = 0, 2, 4, 2
	case 2: // P_L0_L0_8x16
		mb.numSubParts = 2
		mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 2, 4
		mb.subX[1], mb.subY[1], mb.subW[1], mb.subH[1] = 2, 0, 2, 4
	case 3, 4: // P_8x8 / P_8x8ref0
		if err := s.parseSubMBTypes(mb); err != nil {
			return err
		}
	default:
		return errf("h264: unsupported P mb_type")
	}

	numRef := s.numRefL0
	if err := s.parseRefIdxAndMvd(mbIdx, mb, numRef, mt == 4); err != nil {
		return err
	}

	if err := s.decodeResidualInter(mbIdx); err != nil {
		return err
	}
	return s.reconstructInter(f, mbIdx)
}

// parseSubMBTypes fills the sub-partition layout for a P_8x8 macroblock.
func (s *picState) parseSubMBTypes(mb *mbData) error {
	mb.numSubParts = 0
	for q := 0; q < 4; q++ {
		smt := int(s.br.ue())
		if s.br.err() != nil {
			return s.br.err()
		}
		qx := (q % 2) * 2 // quadrant origin in 4x4 units
		qy := (q / 2) * 2
		switch smt {
		case 0: // P_L0_8x8
			mb.addSub(qx, qy, 2, 2)
		case 1: // P_L0_8x4
			mb.addSub(qx, qy, 2, 1)
			mb.addSub(qx, qy+1, 2, 1)
		case 2: // P_L0_4x8
			mb.addSub(qx, qy, 1, 2)
			mb.addSub(qx+1, qy, 1, 2)
		case 3: // P_L0_4x4
			mb.addSub(qx, qy, 1, 1)
			mb.addSub(qx+1, qy, 1, 1)
			mb.addSub(qx, qy+1, 1, 1)
			mb.addSub(qx+1, qy+1, 1, 1)
		default:
			return errf("h264: bad sub_mb_type")
		}
	}
	return nil
}

// dct8x8Allowed reports whether the 8x8 transform is allowed for this inter
// macroblock (all sub-partitions are at least 8x8; spec 7.3.5.1 / FFmpeg's
// get_dct8x8_allowed).
func (mb *mbData) dct8x8Allowed() bool {
	for i := 0; i < mb.numSubParts; i++ {
		if mb.subW[i] < 2 || mb.subH[i] < 2 {
			return false
		}
	}
	return true
}

func (mb *mbData) addSub(x, y, w, h int) {
	i := mb.numSubParts
	mb.subX[i], mb.subY[i], mb.subW[i], mb.subH[i] = x, y, w, h
	mb.numSubParts++
}

// predictMvPartition is the motion-vector predictor for one partition of a
// 16x8/8x16 inter macroblock. OpenH264's PredInter16x8Mv/PredInter8x16Mv use
// the matching neighbour directly for these geometries (which the spec's
// median rule reduces to when the reference index matches).
func (s *picState) predictMvPartition(mbIdx, mt, partIdx, bx, by, w4, curRef int) (int, int) {
	slot := s.mbs[mbIdx].sliceNum
	useIfMatch := func(x, y int) (int, int, bool) {
		r, mx, my := s.motionAt(0, x, y, slot)
		if r == curRef {
			return mx, my, true
		}
		return 0, 0, false
	}
	switch {
	case mt == 1 && partIdx == 0: // 16x8 top: top neighbour
		if mx, my, ok := useIfMatch(bx, by-1); ok {
			return mx, my
		}
	case mt == 1 && partIdx == 1: // 16x8 bottom: left neighbour
		if mx, my, ok := useIfMatch(bx-1, by); ok {
			return mx, my
		}
	case mt == 2 && partIdx == 0: // 8x16 left: left neighbour
		if mx, my, ok := useIfMatch(bx-1, by); ok {
			return mx, my
		}
	case mt == 2 && partIdx == 1: // 8x16 right: top-right, else top-left
		if mx, my, ok := useIfMatch(bx+w4, by-1); ok {
			return mx, my
		}
		if mx, my, ok := useIfMatch(bx-1, by-1); ok {
			return mx, my
		}
	}
	return s.predictMv(mbIdx, 0, bx, by, w4, curRef)
}

// parseRefIdxAndMvd reads ref_idx_l0 (when multiple references) and mvd_l0 for
// each sub-partition, then stores the resulting motion vectors.
func (s *picState) parseRefIdxAndMvd(mbIdx int, mb *mbData, numRef int, ref0 bool) error {
	bx0 := (mbIdx % s.mbWidth) * 4
	by0 := (mbIdx / s.mbWidth) * 4
	for i := 0; i < mb.numSubParts; i++ {
		ref := 0
		if numRef > 1 && !ref0 {
			ref = int(s.br.ue())
			if s.br.err() != nil {
				return s.br.err()
			}
			if ref >= len(s.refFrames) {
				return errf("h264: ref_idx out of range")
			}
		}
		mb.subRef[i] = ref
		pmx, pmy := s.predictMvPartition(mbIdx, mb.mbType, i, bx0+mb.subX[i], by0+mb.subY[i], mb.subW[i], ref)
		mdx := int(s.br.se())
		mdy := int(s.br.se())
		if s.br.err() != nil {
			return s.br.err()
		}
		mvx, mvy := pmx+mdx, pmy+mdy
		mb.subMv[i] = [2]int{mvx, mvy}
		s.setMotion(0, bx0+mb.subX[i], by0+mb.subY[i], mb.subW[i], mb.subH[i], ref, mvx, mvy)
	}
	return nil
}

// decodeResidualInter decodes the coded residual of an inter macroblock.
func (s *picState) decodeResidualInter(mbIdx int) error {
	mb := &s.mbs[mbIdx]
	pos := s.br.pos
	cbp := int(s.br.ue())
	if s.br.err() != nil {
		return s.br.err()
	}
	if traceB && cbp > 47 {
		println("cbp out of range at MB", mbIdx, "mt", mb.mbType, "cbp", cbp, "pos", pos)
	}
	if cbp > 47 {
		return errf("h264: cbp out of range")
	}
	v := int(golombToInterCBP[cbp])
	mb.cbpLuma = v & 0x0f
	mb.cbpChroma = (v >> 4) & 0x03

	if mb.cbpLuma > 0 || mb.cbpChroma > 0 {
		if s.transform8x8ModeFlag && mb.cbpLuma > 0 && mb.dct8x8Allowed() {
			mb.transform8x8 = s.br.u1()
		}
		delta := int(s.br.se())
		if s.br.err() != nil {
			return s.br.err()
		}
		qp := clampInt(s.qpY+delta, 0, 51)
		mb.qpY = qp
	} else {
		mb.qpY = s.qpY
	}
	s.qpY = mb.qpY

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

// reconstructInter performs motion compensation into the frame and then adds
// the decoded residual.
func (s *picState) reconstructInter(f *Frame, mbIdx int) error {
	mb := &s.mbs[mbIdx]
	mbX, mbY := mbIdx%s.mbWidth, mbIdx/s.mbWidth
	var p0, c0 [256]byte
	for i := 0; i < mb.numSubParts; i++ {
		refIdx := mb.subRef[i]
		ref := s.refFrames[refIdx]
		w, h := mb.subW[i]*4, mb.subH[i]*4
		cw, ch := mb.subW[i]*2, mb.subH[i]*2
		x0 := mbX*16 + mb.subX[i]*4
		y0 := mbY*16 + mb.subY[i]*4
		cx0 := mbX*8 + mb.subX[i]*2
		cy0 := mbY*8 + mb.subY[i]*2

		predLuma(p0[:], w, ref.Y, x0, y0, w, h, mb.subMv[i][0], mb.subMv[i][1])
		predChroma(c0[:], cw, ref.Cb, cx0, cy0, cw, ch, mb.subMv[i][0], mb.subMv[i][1])
		if s.weightedPred {
			weightBuf(p0[:], w*h, s.lumaWeightL0[refIdx], s.lumaOffsetL0[refIdx], s.lumaLog2WD)
			weightBuf(c0[:], cw*ch, s.chromaWeightL0[refIdx][0], s.chromaOffsetL0[refIdx][0], s.chromaLog2WD)
		}
		for j := 0; j < h; j++ {
			for k := 0; k < w; k++ {
				f.Y.Set(x0+k, y0+j, p0[j*w+k])
			}
		}
		for j := 0; j < ch; j++ {
			for k := 0; k < cw; k++ {
				f.Cb.Set(cx0+k, cy0+j, c0[j*cw+k])
			}
		}

		predChroma(c0[:], cw, ref.Cr, cx0, cy0, cw, ch, mb.subMv[i][0], mb.subMv[i][1])
		if s.weightedPred {
			weightBuf(c0[:], cw*ch, s.chromaWeightL0[refIdx][1], s.chromaOffsetL0[refIdx][1], s.chromaLog2WD)
		}
		for j := 0; j < ch; j++ {
			for k := 0; k < cw; k++ {
				f.Cr.Set(cx0+k, cy0+j, c0[j*cw+k])
			}
		}
	}
	s.addInterResidual(f, mbIdx, mb)
	return nil
}

// addInterResidual adds the decoded 4x4 residual to the (already predicted)
// luma and chroma samples of an inter macroblock.
func (s *picState) addInterResidual(f *Frame, mbIdx int, mb *mbData) {
	mbX, mbY := mbIdx%s.mbWidth, mbIdx/s.mbWidth
	if mb.transform8x8 {
		for i8x8 := 0; i8x8 < 4; i8x8++ {
			if mb.cbpLuma&(1<<uint(i8x8)) == 0 {
				continue
			}
			bx := (i8x8 % 2) * 8
			by := (i8x8 / 2) * 8
			res := idct8x8(dequant8x8(mb.lumaLevel8x8[i8x8], mb.qpY))
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					px, py := mbX*16+bx+x, mbY*16+by+y
					f.Y.Set(px, py, clip8(int32(f.Y.At(px, py))+res[y*8+x]))
				}
			}
		}
	} else {
		for i := 0; i < 16; i++ {
			if mb.cbpLuma&(1<<uint(i/4)) == 0 {
				continue
			}
			bx := inverseRasterX4x4[i]
			by := inverseRasterY4x4[i]
			var coeffs [16]int32
			for j := 0; j < 16; j++ {
				coeffs[zigzag4x4[j]] = mb.lumaLevel4x4[i][j]
			}
			res := idct4x4(dequant4x4(coeffs, mb.qpY))
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					px, py := mbX*16+bx+x, mbY*16+by+y
					f.Y.Set(px, py, clip8(int32(f.Y.At(px, py))+res[y*4+x]))
				}
			}
		}
	}
	if s.chromaArrayType == 0 || mb.cbpChroma == 0 {
		return
	}
	qpc := chromaQP(mb.qpY + s.chromaQpIndexOffset)
	for c := 0; c < 2; c++ {
		dcScaled := dequantChromaDC2x2(hadamard2x2(mb.chromaDC[c]), qpc)
		plane := f.Cb
		if c == 1 {
			plane = f.Cr
		}
		for blk := 0; blk < 4; blk++ {
			bx := (blk % 2) * 4
			by := (blk / 2) * 4
			var block [16]int32
			block[0] = dcScaled[blk]
			if mb.cbpChroma > 1 {
				for j := 0; j < 15; j++ {
					block[zigzag4x4AC[j]] = mb.chromaAC[c][blk][j]
				}
			}
			deq := dequant4x4(block, qpc)
			deq[0] = dcScaled[blk]
			res := idct4x4(deq)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					px, py := mbX*8+bx+x, mbY*8+by+y
					plane.Set(px, py, clip8(int32(plane.At(px, py))+res[y*4+x]))
				}
			}
		}
	}
}
