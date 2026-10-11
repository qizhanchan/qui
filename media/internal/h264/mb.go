package h264

// The macroblock layer (spec 7.3.4, 7.3.5): one code path for CAVLC and
// CABAC, with the entropy-specific syntax element readers in syntax.go.

var errMB = errf("h264: invalid macroblock data")

// decode decodes one slice into the current picture.
func (s *sliceCtx) decode(h *sliceHeader, lists [2][]*picture) error {
	d := s.d
	pc := d.pc
	s.pc, s.pic, s.h, s.sps, s.pps = pc, d.cur, h, h.sps, h.pps
	s.refs = lists
	if pc.dq == nil || !pc.dq.same(h.pps) {
		pc.dq = newDequantTables(h.pps)
	}
	s.sliceNum = int32(len(pc.slices))
	pc.slices = append(pc.slices, sliceInfo{
		disable:     h.disableDeblock,
		alphaOffset: h.alphaOffset,
		betaOffset:  h.betaOffset,
		chromaOff:   [2]int{h.pps.ChromaQPIndexOffset, h.pps.SecondChromaQPOffset},
	})
	s.qp = h.pps.PicInitQP + h.qpDelta
	s.lastDQuant = 0
	s.cabacMode = h.pps.EntropyCodingCABAC
	if h.isB() {
		s.setupSliceDirect()
	}
	n := pc.mbW * pc.mbH
	addr := h.firstMB
	if s.cabacMode {
		s.br.align()
		s.cb.initContexts(h.sliceType, h.cabacInitIDC, s.qp)
		if err := s.cb.start(&s.br); err != nil {
			return err
		}
		for {
			if addr >= n || pc.mbs[addr].slice >= 0 {
				return errMB
			}
			s.setupMB(addr)
			var err error
			if !h.isI() && s.readSkipFlag() {
				err = s.decodeSkip()
			} else {
				err = s.decodeMB()
			}
			if err != nil {
				s.mb.slice = -1
				if debugErr != nil {
					debugErr(s, err)
				}
				return err
			}
			if s.cb.overrun() {
				return errMB
			}
			if s.cb.terminate() == 1 {
				return nil
			}
			addr++
		}
	}
	br := &s.br
	for {
		if !h.isI() {
			run := int(br.ue())
			if br.err() != nil {
				return br.err()
			}
			for ; run > 0; run-- {
				if addr >= n || pc.mbs[addr].slice >= 0 {
					return errMB
				}
				s.setupMB(addr)
				if err := s.decodeSkip(); err != nil {
					return err
				}
				addr++
				if run == 1 && !br.moreRBSPData() {
					return nil
				}
			}
		}
		if addr >= n || pc.mbs[addr].slice >= 0 {
			return errMB
		}
		s.setupMB(addr)
		if err := s.decodeMB(); err != nil {
			s.mb.slice = -1
			return err
		}
		if br.err() != nil {
			s.mb.slice = -1
			return br.err()
		}
		if !br.moreRBSPData() {
			return nil
		}
		addr++
	}
}

// resetMB clears the per-macroblock state before parsing.
func (s *sliceCtx) resetMB() {
	mb := s.mb
	mb.cbp, mb.t8x8, mb.chromaMode, mb.direct = 0, false, 0, 0
	mb.nz = [16]uint8{}
	mb.nzC = [2][4]uint8{}
	mb.cbf, mb.nzMask = 0, 0
	for i := range mb.intraMode {
		mb.intraMode[i] = -1
	}
	s.lumaNZ, s.chromaNZ = 0, 0
	s.t8x8 = false
	s.cbp = 0
}

func (s *sliceCtx) setQP(mb *mbState) {
	mb.qp = int8(s.qp)
	mb.qpc[0] = int8(chromaQP(s.qp, s.pps.ChromaQPIndexOffset))
	mb.qpc[1] = int8(chromaQP(s.qp, s.pps.SecondChromaQPOffset))
}

// decodeSkip decodes a P_Skip or B_Skip macroblock.
func (s *sliceCtx) decodeSkip() error {
	s.resetMB()
	mb := s.mb
	s.fillMotionCaches()
	if s.h.isB() {
		s.typ = mtDirect | mtSkip | mt16x16
		for q := 0; q < 4; q++ {
			s.predDirect8x8(q)
		}
		mb.direct = 0xf
	} else {
		s.typ = mtSkip | mt16x16 | mtP0L0
		mx, my := s.predPSkip()
		s.setMotion(0, 0, 0, 4, 4, 0, mx, my)
	}
	mb.typ = s.typ
	s.lastDQuant = 0
	s.setQP(mb)
	s.interPred()
	s.writeBackMotion()
	s.pic.mbType[s.mbAddr] = s.typ
	return nil
}

// decodeMB decodes a non-skipped macroblock.
func (s *sliceCtx) decodeMB() error {
	s.resetMB()
	mb := s.mb
	h := s.h
	mbType, err := s.readMbType()
	if err != nil {
		return err
	}
	intraType := -1
	switch {
	case h.isI():
		intraType = mbType
	case h.isP():
		if mbType >= 5 {
			intraType = mbType - 5
		} else {
			s.typ = pMbType[mbType]
		}
	default:
		if mbType >= 23 {
			intraType = mbType - 23
		} else {
			s.typ = bMbType[mbType]
		}
	}
	if intraType >= 0 {
		if intraType > 25 {
			return errMB
		}
		return s.decodeIntraMB(intraType)
	}

	// Inter macroblock.
	s.fillMotionCaches()
	noSmall := true
	if s.typ&mt8x8 != 0 {
		if err := s.parseSubMB(&noSmall); err != nil {
			return err
		}
	} else if s.typ&mtDirect != 0 {
		for q := 0; q < 4; q++ {
			s.predDirect8x8(q)
		}
		mb.direct = 0xf
		noSmall = s.sps.Direct8x8Inference
	} else if err := s.parsePartitions(); err != nil {
		return err
	}
	mb.typ = s.typ

	cbp, err := s.readCBP(false)
	if err != nil {
		return err
	}
	s.cbp = cbp
	mb.cbp = uint8(cbp)
	if cbp&15 != 0 && s.pps.Transform8x8Mode && noSmall {
		s.t8x8 = s.readT8x8()
		mb.t8x8 = s.t8x8
	}
	if cbp != 0 {
		if err := s.readQPDelta(); err != nil {
			return err
		}
	} else {
		s.lastDQuant = 0
	}
	s.setQP(mb)
	if cbp != 0 {
		if err := s.residual(false, false); err != nil {
			return err
		}
	}
	s.interPred()
	s.addResidualInter()
	s.writeBackMotion()
	s.pic.mbType[s.mbAddr] = s.typ
	return nil
}

// parsePartitions parses ref_idx and mvd for 16x16 / 16x8 / 8x16
// macroblocks.
func (s *sliceCtx) parsePartitions() error {
	typ := s.typ
	nparts, pw, ph := 1, 4, 4
	if typ&mt16x8 != 0 {
		nparts, ph = 2, 2
	} else if typ&mt8x16 != 0 {
		nparts, pw = 2, 2
	}
	var refs [2][2]int8
	for l := 0; l < 2; l++ {
		for p := 0; p < nparts; p++ {
			x, y := 0, 0
			if typ&mt16x8 != 0 {
				y = p * 2
			} else if typ&mt8x16 != 0 {
				x = p * 2
			}
			if typ&(mtP0L0<<uint(2*l+p)) == 0 {
				s.setMotion(l, x, y, pw, ph, refNone, 0, 0)
				refs[l][p] = refNone
				continue
			}
			ref := 0
			if s.h.numRefIdx[l] > 1 {
				r, err := s.readRefIdx(l, x, y)
				if err != nil {
					return err
				}
				if r >= s.h.numRefIdx[l] {
					return errMB
				}
				ref = r
			}
			refs[l][p] = int8(ref)
			for j := y; j < y+ph; j++ {
				for i := x; i < x+pw; i++ {
					s.refCache[l][cidx(i, j)] = int8(ref)
				}
			}
		}
	}
	for l := 0; l < 2; l++ {
		for p := 0; p < nparts; p++ {
			if refs[l][p] < 0 {
				continue
			}
			x, y := 0, 0
			if typ&mt16x8 != 0 {
				y = p * 2
			} else if typ&mt8x16 != 0 {
				x = p * 2
			}
			px, py := s.mvPredPart(l, typ, p, x, y, pw, refs[l][p])
			dx, dy, err := s.readMvd(l, x, y)
			if err != nil {
				return err
			}
			s.setMotion(l, x, y, pw, ph, refs[l][p], px+int16(dx), py+int16(dy))
			if s.cabacMode {
				s.setMvd(l, x, y, pw, ph, dx, dy)
			}
		}
	}
	return nil
}

// parseSubMB parses sub_mb_pred() for P_8x8 / B_8x8.
func (s *sliceCtx) parseSubMB(noSmall *bool) error {
	isB := s.h.isB()
	mb := s.mb
	for q := 0; q < 4; q++ {
		t, err := s.readSubMbType()
		if err != nil {
			return err
		}
		if isB {
			if t > 12 {
				return errMB
			}
			s.subType[q] = bSubType[t]
		} else {
			if t > 3 {
				return errMB
			}
			s.subType[q] = pSubType[t]
		}
		st := s.subType[q]
		if st.direct {
			if !s.sps.Direct8x8Inference {
				*noSmall = false
			}
		} else if st.shape != 0 {
			*noSmall = false
		}
	}
	// Direct sub-macroblocks are derived first so that later partitions
	// can use them as neighbours.
	var saveRef [2][4][4]int8
	var saveMv [2][4][4][2]int16
	anyDirect := false
	for q := 0; q < 4; q++ {
		if !s.subType[q].direct {
			continue
		}
		anyDirect = true
		s.predDirect8x8(q)
		mb.direct |= 1 << uint(q)
		x0, y0 := (q&1)*2, (q>>1)*2
		for l := 0; l < 2; l++ {
			for k := 0; k < 4; k++ {
				c := cidx(x0+k&1, y0+k>>1)
				saveRef[l][q][k] = s.refCache[l][c]
				saveMv[l][q][k] = s.mvCache[l][c]
			}
		}
		if s.cabacMode {
			s.directCache[cidx(x0, y0)] = true
			s.directCache[cidx(x0+1, y0)] = true
			s.directCache[cidx(x0, y0+1)] = true
			s.directCache[cidx(x0+1, y0+1)] = true
		}
	}
	if anyDirect {
		// Blocks of later sub-macroblocks are not available as neighbour C.
		for l := 0; l < 2; l++ {
			s.refCache[l][cidx(2, 0)] = refNA
			s.refCache[l][cidx(2, 2)] = refNA
		}
	}
	var refs [2][4]int8
	ref0 := s.typ&mtRef0 != 0
	for l := 0; l < 2; l++ {
		for q := 0; q < 4; q++ {
			st := s.subType[q]
			if st.direct {
				continue
			}
			x0, y0 := (q&1)*2, (q>>1)*2
			if st.lists&(1<<uint(l)) == 0 {
				refs[l][q] = refNone
				s.setMotion(l, x0, y0, 2, 2, refNone, 0, 0)
				continue
			}
			ref := 0
			if s.h.numRefIdx[l] > 1 && !ref0 {
				r, err := s.readRefIdx(l, x0, y0)
				if err != nil {
					return err
				}
				if r >= s.h.numRefIdx[l] {
					return errMB
				}
				ref = r
			}
			refs[l][q] = int8(ref)
			for k := 0; k < 4; k++ {
				s.refCache[l][cidx(x0+k&1, y0+k>>1)] = int8(ref)
			}
		}
	}
	// The top-left blocks of quadrants 1 and 3 are neighbour C of earlier
	// quadrants' sub-partitions but are decoded later: keep them
	// unavailable until their own quadrant is reached.
	for l := 0; l < 2; l++ {
		if !s.subType[1].direct {
			s.refCache[l][cidx(2, 0)] = refNA
		}
		if !s.subType[3].direct {
			s.refCache[l][cidx(2, 2)] = refNA
		}
	}
	for l := 0; l < 2; l++ {
		for q := 0; q < 4; q++ {
			st := s.subType[q]
			if st.direct {
				continue
			}
			x0, y0 := (q&1)*2, (q>>1)*2
			s.refCache[l][cidx(x0, y0)] = refs[l][q]
			if refs[l][q] < 0 {
				continue
			}
			for _, sp := range subParts[st.shape] {
				x, y, w, hh := x0+sp[0], y0+sp[1], sp[2], sp[3]
				px, py := s.mvPred(l, x, y, w, refs[l][q])
				dx, dy, err := s.readMvd(l, x, y)
				if err != nil {
					return err
				}
				s.setMotion(l, x, y, w, hh, refs[l][q], px+int16(dx), py+int16(dy))
				if s.cabacMode {
					s.setMvd(l, x, y, w, hh, dx, dy)
				}
			}
		}
	}
	for q := 0; q < 4; q++ {
		if !s.subType[q].direct {
			continue
		}
		x0, y0 := (q&1)*2, (q>>1)*2
		for l := 0; l < 2; l++ {
			for k := 0; k < 4; k++ {
				c := cidx(x0+k&1, y0+k>>1)
				s.refCache[l][c] = saveRef[l][q][k]
				s.mvCache[l][c] = saveMv[l][q][k]
			}
		}
	}
	return nil
}

// decodeIntraMB decodes an intra macroblock with intra mb_type t (0..25).
func (s *sliceCtx) decodeIntraMB(t int) error {
	mb := s.mb
	if s.h.isI() || true {
		s.writeBackIntra()
	}
	if t == 25 {
		s.typ = mtIPCM
		mb.typ = mtIPCM
		return s.decodePCM()
	}
	if t == 0 {
		s.typ = mtIntra4x4
		if s.pps.Transform8x8Mode {
			s.t8x8 = s.readT8x8()
		}
		mb.t8x8 = s.t8x8
		if s.t8x8 {
			for b8 := 0; b8 < 4; b8++ {
				x, y := (b8&1)*2, (b8>>1)*2
				pred := s.predIntraMode(x, y)
				m, err := s.readIntraMode(pred)
				if err != nil {
					return err
				}
				for k := 0; k < 4; k++ {
					mb.intraMode[(y+k>>1)*4+x+k&1] = int8(m)
				}
			}
		} else {
			for blk := 0; blk < 16; blk++ {
				x, y := int(blkX[blk]), int(blkY[blk])
				pred := s.predIntraMode(x, y)
				m, err := s.readIntraMode(pred)
				if err != nil {
					return err
				}
				mb.intraMode[y*4+x] = int8(m)
			}
		}
	} else {
		s.typ = mtIntra16x16
		s.i16Mode = (t - 1) % 4
		s.cbp = ((t-1)/4%3)<<4 | (15 * ((t - 1) / 12))
	}
	mb.typ = s.typ
	cm, err := s.readChromaMode()
	if err != nil {
		return err
	}
	if cm > 3 {
		return errMB
	}
	s.chromaMode = cm
	mb.chromaMode = uint8(cm)
	if s.typ == mtIntra4x4 {
		cbp, err := s.readCBP(true)
		if err != nil {
			return err
		}
		s.cbp = cbp
	}
	mb.cbp = uint8(s.cbp)
	if s.cbp != 0 || s.typ == mtIntra16x16 {
		if err := s.readQPDelta(); err != nil {
			return err
		}
	} else {
		s.lastDQuant = 0
	}
	s.setQP(mb)
	if s.cbp != 0 || s.typ == mtIntra16x16 {
		if err := s.residual(true, s.typ == mtIntra16x16); err != nil {
			return err
		}
	}
	s.pic.mbType[s.mbAddr] = s.typ
	s.intraRecon()
	return nil
}

// predIntraMode derives predIntra4x4PredMode / predIntra8x8PredMode for the
// block whose top-left 4x4 is (x, y) (spec 8.3.1.1).
func (s *sliceCtx) predIntraMode(x, y int) int {
	mb := s.mb
	ci := s.pps.ConstrainedIntraPred
	var a, b int
	if x > 0 {
		a = int(mb.intraMode[y*4+x-1])
	} else {
		n := s.mbA
		if n == nil || (ci && n.typ&mtIntra == 0) {
			return 2
		}
		a = int(n.intraMode[y*4+3])
	}
	if y > 0 {
		b = int(mb.intraMode[(y-1)*4+x])
	} else {
		n := s.mbB
		if n == nil || (ci && n.typ&mtIntra == 0) {
			return 2
		}
		b = int(n.intraMode[12+x])
	}
	if a < 0 {
		a = 2
	}
	if b < 0 {
		b = 2
	}
	return min(a, b)
}

func (s *sliceCtx) decodePCM() error {
	mb := s.mb
	var raw [384]byte
	if s.cabacMode {
		// The arithmetic decoder has consumed exactly up to the end of the
		// terminating bin; PCM samples start at the next byte boundary.
		s.cb.sync()
		s.br.align()
		for i := range raw {
			raw[i] = byte(s.br.u(8))
		}
		if err := s.cb.start(&s.br); err != nil {
			return err
		}
	} else {
		s.br.align()
		for i := range raw {
			raw[i] = byte(s.br.u(8))
		}
	}
	if s.br.err() != nil {
		return s.br.err()
	}
	pic := s.pic
	x0, y0 := s.mbX*16, s.mbY*16
	for y := 0; y < 16; y++ {
		copy(pic.y[(y0+y)*pic.stride+x0:], raw[y*16:y*16+16])
	}
	cx, cy := s.mbX*8, s.mbY*8
	for y := 0; y < 8; y++ {
		copy(pic.cb[(cy+y)*pic.cstride+cx:], raw[256+y*8:256+y*8+8])
		copy(pic.cr[(cy+y)*pic.cstride+cx:], raw[320+y*8:320+y*8+8])
	}
	mb.qp = 0
	mb.qpc[0] = int8(chromaQP(0, s.pps.ChromaQPIndexOffset))
	mb.qpc[1] = int8(chromaQP(0, s.pps.SecondChromaQPOffset))
	mb.cbp = 0x2f
	for i := range mb.nz {
		mb.nz[i] = 16
	}
	mb.nzC = [2][4]uint8{{16, 16, 16, 16}, {16, 16, 16, 16}}
	mb.cbf = 0x7ffffff
	mb.nzMask = 0xffff
	s.lastDQuant = 0
	s.pic.mbType[s.mbAddr] = mtIPCM
	return nil
}

// residual parses residual() and dequantises it (spec 7.3.5.3).
func (s *sliceCtx) residual(intra, i16 bool) error {
	mb := s.mb
	dq := s.pc.dq
	qp := s.qp
	m4, m8 := 3, 1
	if intra {
		m4, m8 = 0, 0
	}
	coeff := s.scanTmp[:]
	cbpL := s.cbp & 15
	if i16 {
		clear(coeff[:16])
		n, err := s.readBlock(catLumaDC, 0, 0, 16, coeff)
		if err != nil {
			return err
		}
		if n > 0 {
			mb.cbf |= 1 << 24
			for i := 0; i < 16; i++ {
				s.coefDC[zigzag4x4[i]] = coeff[i]
			}
		} else {
			s.coefDC = [16]int32{}
		}
		q := &dq.q4[0][qp]
		for blk := 0; blk < 16; blk++ {
			x, y := int(blkX[blk]), int(blkY[blk])
			r := y*4 + x
			if cbpL == 0 {
				continue
			}
			clear(coeff[:15])
			n, err := s.readBlock(catLumaAC, x, y, 15, coeff)
			if err != nil {
				return err
			}
			mb.nz[r] = uint8(n)
			if n > 0 {
				mb.cbf |= 1 << uint(r)
				c := &s.coef[r]
				for i := 0; i < 15; i++ {
					if v := coeff[i]; v != 0 {
						p := zigzag4x4[i+1]
						c[p] = (v*q[p] + 8) >> 4
					}
				}
			}
		}
		s.lumaNZ = 0xffff
	} else if !s.t8x8 {
		q := &dq.q4[m4][qp]
		for blk := 0; blk < 16; blk++ {
			if cbpL&(1<<uint(blk>>2)) == 0 {
				continue
			}
			x, y := int(blkX[blk]), int(blkY[blk])
			r := y*4 + x
			clear(coeff[:16])
			n, err := s.readBlock(catLuma4x4, x, y, 16, coeff)
			if err != nil {
				return err
			}
			mb.nz[r] = uint8(n)
			if n > 0 {
				mb.cbf |= 1 << uint(r)
				mb.nzMask |= 1 << uint(r)
				s.lumaNZ |= 1 << uint(r)
				c := &s.coef[r]
				for i := 0; i < 16; i++ {
					if v := coeff[i]; v != 0 {
						p := zigzag4x4[i]
						c[p] = (v*q[p] + 8) >> 4
					}
				}
			}
		}
	} else {
		q := &dq.q8[m8][qp]
		for b8 := 0; b8 < 4; b8++ {
			if cbpL&(1<<uint(b8)) == 0 {
				continue
			}
			x0, y0 := (b8&1)*2, (b8>>1)*2
			mask := uint16(0x33) << uint(y0*4+x0)
			c := &s.coef8[b8]
			total := 0
			if s.cabacMode {
				clear(coeff[:64])
				n, err := s.readBlock(catLuma8x8, x0, y0, 64, coeff)
				if err != nil {
					return err
				}
				for k := 0; k < 4; k++ {
					mb.nz[(y0+k>>1)*4+x0+k&1] = uint8(n)
				}
				mb.cbf |= uint32(mask)
				total = n
				for i := 0; i < 64; i++ {
					if v := coeff[i]; v != 0 {
						p := zigzag8x8[i]
						c[p] = (v*q[p] + 32) >> 6
					}
				}
			} else {
				for i4 := 0; i4 < 4; i4++ {
					x, y := x0+i4&1, y0+i4>>1
					clear(coeff[:16])
					n, err := s.readBlock(catLuma4x4, x, y, 16, coeff)
					if err != nil {
						return err
					}
					mb.nz[y*4+x] = uint8(n)
					total += n
					for i := 0; i < 16; i++ {
						if v := coeff[i]; v != 0 {
							p := zigzag8x8[4*i+i4]
							c[p] = (v*q[p] + 32) >> 6
						}
					}
				}
			}
			if total > 0 {
				mb.nzMask |= mask
				s.lumaNZ |= mask
			}
		}
	}
	cbpC := s.cbp >> 4
	if cbpC != 0 {
		for c := 0; c < 2; c++ {
			clear(coeff[:4])
			n, err := s.readBlock(catChromaDC, c, 0, 4, coeff)
			if err != nil {
				return err
			}
			if n > 0 {
				mb.cbf |= 1 << uint(25+c)
				copy(s.chromaDC[c][:], coeff[:4])
				s.chromaNZ |= 0xf << uint(4*c)
			} else {
				s.chromaDC[c] = [4]int32{}
			}
		}
	}
	if cbpC == 2 {
		for c := 0; c < 2; c++ {
			mc := 4 + c
			if intra {
				mc = 1 + c
			}
			q := &dq.q4[mc][mb.qpc[c]]
			for blk := 0; blk < 4; blk++ {
				clear(coeff[:15])
				n, err := s.readBlock(catChromaAC, c, blk, 15, coeff)
				if err != nil {
					return err
				}
				mb.nzC[c][blk] = uint8(n)
				if n > 0 {
					mb.cbf |= 1 << uint(16+4*c+blk)
					s.chromaNZ |= 1 << uint(4*c+blk)
					cf := &s.chromaAC[c][blk]
					for i := 0; i < 15; i++ {
						if v := coeff[i]; v != 0 {
							p := zigzag4x4[i+1]
							cf[p] = (v*q[p] + 8) >> 4
						}
					}
				}
			}
		}
	}
	return nil
}

// debugErr is a test hook called when a macroblock fails to parse.
var debugErr func(s *sliceCtx, err error)
