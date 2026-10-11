package h264

// B-slice decoding (spec 7.3.4, 7.4.5, 8.4.1.2): bi-prediction, spatial and
// temporal direct mode, and the B macroblock/sub-macroblock types.

// Prediction list selectors for a sub-partition.
const (
	predL0     = 0
	predL1     = 1
	predBi     = 2
	predDirect = 3
)

// bListPairs maps mb_type 4..21 to the list used by each of the two partitions.
var bListPairs = [18][2]int{
	{predL0, predL0}, {predL0, predL0}, // 4,5
	{predL1, predL1}, {predL1, predL1}, // 6,7
	{predL0, predL1}, {predL0, predL1}, // 8,9
	{predL1, predL0}, {predL1, predL0}, // 10,11
	{predL0, predBi}, {predL0, predBi}, // 12,13
	{predL1, predBi}, {predL1, predBi}, // 14,15
	{predBi, predL0}, {predBi, predL0}, // 16,17
	{predBi, predL1}, {predBi, predL1}, // 18,19
	{predBi, predBi}, {predBi, predBi}, // 20,21
}

func (s *picState) decodeBSliceCAVLC(f *Frame, hdr *SliceHeader) error {
	s.curSliceNum++
	s.slices = append(s.slices, sliceDeblock{
		disableIDC:  hdr.DisableDeblockingFilterIDC,
		alphaOffset: 52 + int(hdr.SliceAlphaC0OffsetDiv2)*2,
		betaOffset:  52 + int(hdr.SliceBetaOffsetDiv2)*2,
	})
	if len(s.refFrames) == 0 {
		return errf("h264: B slice without a reference picture")
	}
	if !s.motionInit {
		s.initMotionState()
		s.motionInit = true
	}
	mbIdx := int(hdr.FirstMbInSlice)
	if traceB {
		println("=== B SLICE", s.curSliceNum, "bits", s.br.bitsLeft())
	}
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
			if err := s.decodeBSkip(f, mbIdx); err != nil {
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
		if traceB {
			println("B MB", mbIdx, "skip", skipRun, "mt", mt, "pos", s.br.pos)
		}
		s.mbs[mbIdx].sliceNum = s.curSliceNum
		if err := s.decodeBBody(f, mbIdx, mt); err != nil {
			return err
		}
		mbIdx++
	}
	return nil
}

// decodeBSkip decodes a B_Skip macroblock (B_Direct_16x16).
func (s *picState) decodeBSkip(f *Frame, mbIdx int) error {
	mb := &s.mbs[mbIdx]
	mb.isInter = true
	mb.numSubParts = 1
	mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 4
	mb.subPred[0] = predDirect
	if err := s.deriveDirect(mbIdx, 0, 0, 4, 0); err != nil {
		return err
	}
	mb.cbpLuma, mb.cbpChroma = 0, 0
	mb.qpY = s.qpY
	return s.reconstructInterB(f, mbIdx)
}

func (s *picState) decodeBBody(f *Frame, mbIdx, mt int) error {
	mb := &s.mbs[mbIdx]
	if mt >= 23 { // intra-coded macroblock inside a B slice
		mb.mbType = mt - 23
		s.markIntra(mbIdx)
		return s.decodeIntraBody(f, mbIdx)
	}
	mb.isInter = true
	mb.mbType = mt

	switch {
	case mt == 0: // B_Direct_16x16: no ref_idx/mvd syntax; derived below
		mb.numSubParts = 1
		mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 4
		mb.subPred[0] = predDirect
	case mt >= 1 && mt <= 3:
		mb.numSubParts = 1
		mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 4
		mb.subPred[0] = mt - 1
	case mt >= 4 && mt <= 21:
		pair := bListPairs[mt-4]
		mb.numSubParts = 2
		if (mt-4)%2 == 0 { // 16x8
			mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 4, 2
			mb.subX[1], mb.subY[1], mb.subW[1], mb.subH[1] = 0, 2, 4, 2
		} else { // 8x16
			mb.subX[0], mb.subY[0], mb.subW[0], mb.subH[0] = 0, 0, 2, 4
			mb.subX[1], mb.subY[1], mb.subW[1], mb.subH[1] = 2, 0, 2, 4
		}
		mb.subPred[0], mb.subPred[1] = pair[0], pair[1]
		mb.subGroup[0], mb.subGroup[1] = 0, 1
	case mt == 22: // B_8x8 (transform_size_8x8_flag comes after cbp)
		if err := s.parseBSubMBTypes(mbIdx, mb); err != nil {
			return err
		}
	default:
		return errf("h264: unsupported B mb_type")
	}

	if err := s.parseBRefIdxAndMvd(mbIdx, mb); err != nil {
		return err
	}
	if err := s.decodeResidualInter(mbIdx); err != nil {
		return err
	}
	return s.reconstructInterB(f, mbIdx)
}

// parseBSubMBTypes fills sub-partitions for a B_8x8 macroblock.
func (s *picState) parseBSubMBTypes(mbIdx int, mb *mbData) error {
	mb.numSubParts = 0
	add := func(q, x, y, w, h, pred int) {
		mb.addSub(x, y, w, h)
		mb.subPred[mb.numSubParts-1] = pred
		mb.subGroup[mb.numSubParts-1] = q
	}
	for q := 0; q < 4; q++ {
		smt := int(s.br.ue())
		if s.br.err() != nil {
			return s.br.err()
		}
		qx := (q % 2) * 2
		qy := (q / 2) * 2
		switch smt {
		case 0: // B_Direct_8x8
			add(q, qx, qy, 2, 2, predDirect)
		case 1, 2, 3: // B_L0/L1/Bi_8x8
			add(q, qx, qy, 2, 2, smt-1)
		case 4, 6, 8: // *_8x4
			list := (smt - 4) / 2
			add(q, qx, qy, 2, 1, list)
			add(q, qx, qy+1, 2, 1, list)
		case 5, 7, 9: // *_4x8
			list := (smt - 5) / 2
			add(q, qx, qy, 1, 2, list)
			add(q, qx+1, qy, 1, 2, list)
		case 10, 11, 12: // *_4x4
			list := smt - 10
			add(q, qx, qy, 1, 1, list)
			add(q, qx+1, qy, 1, 1, list)
			add(q, qx, qy+1, 1, 1, list)
			add(q, qx+1, qy+1, 1, 1, list)
		default:
			return errf("h264: bad B sub_mb_type")
		}
	}
	return nil
}

// parseBRefIdxAndMvd reads the B macroblock's ref_idx and mvd and stores the
// resulting motion vectors for both lists. The syntax order matters (spec
// 7.3.5.1/7.3.5.2): all ref_idx_l0, then all ref_idx_l1, then all mvd_l0, then
// all mvd_l1. For B_8x8 the reference index is per 8x8 quadrant while the mvd is
// per sub-partition.
func (s *picState) parseBRefIdxAndMvd(mbIdx int, mb *mbData) error {
	bx0 := (mbIdx % s.mbWidth) * 4
	by0 := (mbIdx / s.mbWidth) * 4

	uses := func(p, list int) bool {
		if list == 0 {
			return p == predL0 || p == predBi
		}
		return p == predL1 || p == predBi
	}

	// Direct sub-partitions first (their neighbours are the already-decoded
	// macroblocks), matching FFmpeg's ff_h264_pred_direct_motion.
	for i := 0; i < mb.numSubParts; i++ {
		if mb.subPred[i] == predDirect {
			if err := s.deriveDirect(mbIdx, mb.subX[i], mb.subY[i], mb.subW[i], i); err != nil {
				return err
			}
		}
	}

	readRefs := func(list int) error {
		n := s.numRefL0
		if list == 1 {
			n = s.numRefL1
		}
		for i := 0; i < mb.numSubParts; i++ {
			if i > 0 && mb.subGroup[i] == mb.subGroup[i-1] {
				continue // one ref_idx per group (B_8x8 quadrant)
			}
			if !uses(mb.subPred[i], list) {
				continue
			}
			ref := 0
			if n > 1 {
				ref = int(s.br.ue())
				if s.br.err() != nil {
					return s.br.err()
				}
			}
			for j := i; j < mb.numSubParts && mb.subGroup[j] == mb.subGroup[i]; j++ {
				if !uses(mb.subPred[j], list) {
					continue
				}
				if list == 0 {
					mb.subRef[j] = ref
				} else {
					mb.subRef1[j] = ref
				}
			}
		}
		return nil
	}
	if err := readRefs(0); err != nil {
		return err
	}
	if err := readRefs(1); err != nil {
		return err
	}

	readMvd := func(list int) error {
		for i := 0; i < mb.numSubParts; i++ {
			if !uses(mb.subPred[i], list) {
				continue
			}
			ref := mb.subRef[i]
			if list == 1 {
				ref = mb.subRef1[i]
			}
			pmx, pmy := s.predictMv(mbIdx, list, bx0+mb.subX[i], by0+mb.subY[i], mb.subW[i], ref)
			mdx := int(s.br.se())
			mdy := int(s.br.se())
			if s.br.err() != nil {
				return s.br.err()
			}
			mvx, mvy := pmx+mdx, pmy+mdy
			if list == 0 {
				mb.subMv[i] = [2]int{mvx, mvy}
				s.setMotion(0, bx0+mb.subX[i], by0+mb.subY[i], mb.subW[i], mb.subH[i], ref, mvx, mvy)
			} else {
				mb.subMv1[i] = [2]int{mvx, mvy}
				s.setMotion(1, bx0+mb.subX[i], by0+mb.subY[i], mb.subW[i], mb.subH[i], ref, mvx, mvy)
			}
		}
		return nil
	}
	if err := readMvd(0); err != nil {
		return err
	}
	return readMvd(1)
}

// deriveDirect fills the L0/L1 motion of sub-partition i using spatial or
// temporal direct mode.
func (s *picState) deriveDirect(mbIdx, sx, sy, w4, i int) error {
	mb := &s.mbs[mbIdx]
	bx0 := (mbIdx % s.mbWidth) * 4
	by0 := (mbIdx / s.mbWidth) * 4
	bx, by := bx0+sx, by0+sy
	if s.directSpatial {
		r0, mx0, my0 := s.directSpatialRefMv(mbIdx, bx, by, w4, 0)
		r1, mx1, my1 := s.directSpatialRefMv(mbIdx, bx, by, w4, 1)
		if r0 < 0 && r1 < 0 {
			r0, r1 = 0, 0
		} else if r1 < 0 {
			r1 = 0
		} else if r0 < 0 {
			r0 = 0
		}
		mb.subRef[i], mb.subMv[i] = r0, [2]int{mx0, my0}
		mb.subRef1[i], mb.subMv1[i] = r1, [2]int{mx1, my1}
		s.setMotion(0, bx, by, w4, w4, r0, mx0, my0)
		s.setMotion(1, bx, by, w4, w4, r1, mx1, my1)
		return nil
	}
	return s.directTemporal(mbIdx, bx, by, w4, i)
}

// directSpatialRefMv returns the direct-mode reference index and MV for one list
// from the A/B/C neighbours of the sub-partition (spec 8.4.1.2.2).
func (s *picState) directSpatialRefMv(mbIdx, bx, by, w4, list int) (int, int, int) {
	slot := s.mbs[mbIdx].sliceNum
	refA, ax, ay := s.motionAt(list, bx-1, by, slot)
	refB, bxm, bym := s.motionAt(list, bx, by-1, slot)
	refC, cx, cy := s.motionAt(list, bx+w4, by-1, slot)
	if refC == refNotAvail {
		refC, cx, cy = s.motionAt(list, bx-1, by-1, slot)
	}
	ref := minPositive(refA, minPositive(refB, refC))
	if ref < 0 {
		return refNotInList, 0, 0
	}
	match := 0
	if refA == ref {
		match++
	}
	if refB == ref {
		match++
	}
	if refC == ref {
		match++
	}
	if match == 1 {
		switch {
		case refA == ref:
			return ref, ax, ay
		case refB == ref:
			return ref, bxm, bym
		default:
			return ref, cx, cy
		}
	}
	return ref, median3(ax, bxm, cx), median3(ay, bym, cy)
}

// directTemporal derives the L0/L1 motion from the co-located picture
// (spec 8.4.1.2.3).
func (s *picState) directTemporal(mbIdx, bx, by, w4, i int) error {
	mb := &s.mbs[mbIdx]
	coloc := s.coloc
	if coloc == nil {
		return errf("h264: temporal direct without a co-located picture")
	}
	// Co-located 4x4 block in the reference picture.
	cIdx := by*s.mbWidth*4 + bx
	if cIdx >= len(coloc.refLX) {
		return errf("h264: co-located block out of range")
	}
	mvx, mvy := 0, 0
	colocRefL0 := coloc.refLX[cIdx]
	if colocRefL0 >= 0 {
		mvx, mvy = coloc.mvLX[cIdx], coloc.mvLY[cIdx]
	} else if len(coloc.refL1X) > cIdx && coloc.refL1X[cIdx] >= 0 {
		mvx, mvy = coloc.mvL1X[cIdx], coloc.mvL1Y[cIdx]
	}
	// MV scaling from the POC distances (spec 8.4.1.2.3).
	scale := s.mvScale(0)
	mvL0x := (scale*mvx + 128) >> 8
	mvL0y := (scale*mvy + 128) >> 8
	mvL1x := mvL0x - mvx
	mvL1y := mvL0y - mvy
	mb.subRef[i], mb.subMv[i] = 0, [2]int{mvL0x, mvL0y}
	mb.subRef1[i], mb.subMv1[i] = 0, [2]int{mvL1x, mvL1y}
	s.setMotion(0, bx, by, w4, w4, 0, mvL0x, mvL0y)
	s.setMotion(1, bx, by, w4, w4, 0, mvL1x, mvL1y)
	return nil
}

// mvScale returns the temporal-direct MV scaling factor for list 0
// (spec 8.4.1.2.3).
func (s *picState) mvScale(ref int) int {
	if ref >= len(s.refPOCL0) || len(s.refPOCL1) == 0 {
		return 256
	}
	td := s.refPOCL1[0] - s.refPOCL0[ref]
	tb := s.curPOC - s.refPOCL0[ref]
	if td == 0 {
		return 256
	}
	tx := (16384 + absInt(td/2)) / td
	scale := clipInt((tb*tx+32)>>6, -1024, 1023)
	return scale
}

func minPositive(a, b int) int {
	switch {
	case a < 0:
		return b
	case b < 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

// reconstructInterB performs motion compensation for a B macroblock and adds
// the decoded residual.
func (s *picState) reconstructInterB(f *Frame, mbIdx int) error {
	mb := &s.mbs[mbIdx]
	mbX, mbY := mbIdx%s.mbWidth, mbIdx/s.mbWidth
	var p0, p1 [256]byte
	var c0, c1 [64]byte
	for i := 0; i < mb.numSubParts; i++ {
		w, h := mb.subW[i]*4, mb.subH[i]*4
		cw, ch := mb.subW[i]*2, mb.subH[i]*2
		x0 := mbX*16 + mb.subX[i]*4
		y0 := mbY*16 + mb.subY[i]*4
		cx0 := mbX*8 + mb.subX[i]*2
		cy0 := mbY*8 + mb.subY[i]*2

		// Direct mode uses both lists (bi-prediction) when both have a valid
		// reference, like explicit bi-prediction.
		useL0 := (mb.subPred[i] == predL0 || mb.subPred[i] == predBi || mb.subPred[i] == predDirect) && len(s.refFrames) > 0
		useL1 := (mb.subPred[i] == predL1 || mb.subPred[i] == predBi || mb.subPred[i] == predDirect) && len(s.refFramesL1) > 0
		if useL0 {
			mb.subRef[i] = clampInt(mb.subRef[i], 0, len(s.refFrames)-1)
		}
		if useL1 {
			mb.subRef1[i] = clampInt(mb.subRef1[i], 0, len(s.refFramesL1)-1)
		}

		if useL0 {
			predLuma(p0[:], w, s.refFrames[mb.subRef[i]].Y, x0, y0, w, h, mb.subMv[i][0], mb.subMv[i][1])
			predChroma(c0[:], cw, s.refFrames[mb.subRef[i]].Cb, cx0, cy0, cw, ch, mb.subMv[i][0], mb.subMv[i][1])
		}
		if useL1 {
			predLuma(p1[:], w, s.refFramesL1[mb.subRef1[i]].Y, x0, y0, w, h, mb.subMv1[i][0], mb.subMv1[i][1])
			predChroma(c1[:], cw, s.refFramesL1[mb.subRef1[i]].Cb, cx0, cy0, cw, ch, mb.subMv1[i][0], mb.subMv1[i][1])
		}
		for j := 0; j < h; j++ {
			for k := 0; k < w; k++ {
				v := combinePred(p0[j*w+k], useL0, p1[j*w+k], useL1)
				f.Y.Set(x0+k, y0+j, v)
			}
		}
		for j := 0; j < ch; j++ {
			for k := 0; k < cw; k++ {
				v := combinePred(c0[j*cw+k], useL0, c1[j*cw+k], useL1)
				f.Cb.Set(cx0+k, cy0+j, v)
			}
		}
		// Chroma Cr.
		if useL0 {
			predChroma(c0[:], cw, s.refFrames[mb.subRef[i]].Cr, cx0, cy0, cw, ch, mb.subMv[i][0], mb.subMv[i][1])
		}
		if useL1 {
			predChroma(c1[:], cw, s.refFramesL1[mb.subRef1[i]].Cr, cx0, cy0, cw, ch, mb.subMv1[i][0], mb.subMv1[i][1])
		}
		for j := 0; j < ch; j++ {
			for k := 0; k < cw; k++ {
				v := combinePred(c0[j*cw+k], useL0, c1[j*cw+k], useL1)
				f.Cr.Set(cx0+k, cy0+j, v)
			}
		}
	}
	s.addInterResidual(f, mbIdx, mb)
	return nil
}

func combinePred(v0 byte, use0 bool, v1 byte, use1 bool) byte {
	switch {
	case use0 && use1:
		return byte((int(v0) + int(v1) + 1) >> 1)
	case use0:
		return v0
	default:
		return v1
	}
}

var traceB = false
