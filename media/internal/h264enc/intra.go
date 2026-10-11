package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// Intra analysis and coding.

// analyseIntra chooses between Intra16x16 and Intra4x4 (and Intra8x8 with
// the 8x8 transform) and the chroma mode. It leaves the macroblock
// reconstructed only for the I_NxN choices; finishIntra codes the rest.
// It returns the luma+chroma cost.
func (s *sliceEnc) analyseIntra(inP bool) int {
	s.intra = true
	mode16, c16 := s.analyseI16()
	c16 += s.lambda * 4
	if inP {
		c16 += s.lambda * 4
	}
	s.i16, s.i16Mode, s.t8x8 = true, mode16, false
	best := c16
	if c4 := s.analyseI4(best) + s.lambda*16; c4 < best {
		s.i16, best = false, c4
	}
	if s.e.cfg.Transform8x8 && s.e.cfg.Preset >= 1 {
		saveModes, saveLv, saveCbp, saveI16 := s.modes, s.lumaLv, s.cbpL, s.i16
		if c8 := s.analyseI8() + s.lambda*12; c8 < best {
			s.i16, s.t8x8, best = false, true, c8
		} else {
			s.modes, s.lumaLv, s.cbpL, s.i16 = saveModes, saveLv, saveCbp, saveI16
			s.t8x8 = false
			if !s.i16 {
				// Restore the I4x4 reconstruction overwritten by the 8x8 trial.
				s.reconI4()
			}
		}
	}
	best += s.analyseChroma()
	return best
}

// intraNeighbours16 gathers the 16x16 prediction neighbours from the
// reconstruction.
func (s *sliceEnc) intraNeighbours16() (top, left []byte, tl byte) {
	c := s.cur
	Y, ys := c.y, c.ys
	o := s.yOff(0, 0)
	if s.availB {
		top = Y[o-ys : o-ys+16]
	}
	if s.availA {
		l := &s.leftY
		for i := range l {
			l[i] = Y[o+i*ys-1]
		}
		left = l[:]
	}
	if s.availD {
		tl = Y[o-ys-1]
	}
	return
}

// yOff is the offset in cur.y of luma sample (x, y) of the current
// macroblock (x, y may be -1 for neighbours).
func (s *sliceEnc) yOff(x, y int) int {
	c := s.cur
	return c.yo + (s.mbY*16+y)*c.ys + s.mbX*16 + x
}

// cOff is the chroma counterpart of yOff.
func (s *sliceEnc) cOff(x, y int) int {
	c := s.cur
	return c.co + (s.mbY*8+y)*c.cs + s.mbX*8 + x
}

func (s *sliceEnc) analyseI16() (int, int) {
	top, left, tl := s.intraNeighbours16()
	src, ss := s.srcY()
	best, bestCost := -1, 1<<30
	for mode := 0; mode < 4; mode++ {
		switch mode {
		case h264.I16x16Vertical:
			if top == nil {
				continue
			}
		case h264.I16x16Horizontal:
			if left == nil {
				continue
			}
		case h264.I16x16Plane:
			if top == nil || left == nil || !s.availD {
				continue
			}
		}
		p := h264.Predict16x16(mode, top, left, tl)
		for y := 0; y < 16; y++ {
			copy(s.tmpY[y*16:], p[y][:])
		}
		c := satd(src, ss, s.tmpY[:], 16, 16, 16)
		if c < bestCost {
			best, bestCost = mode, c
			s.predI16 = s.tmpY
		}
	}
	return best, bestCost
}

// predIntraMode returns predIntra4x4PredMode for raster block (bx, by)
// (also used for 8x8 blocks with their top-left 4x4 position). Intra8x8
// modes are stored in all four raster positions of their 8x8, which makes
// the spec's mixed 4x4/8x8 neighbour rules (8.3.1.1, 8.3.2.1) fall out.
func (s *sliceEnc) predIntraMode(bx, by int) int {
	var a, b int
	if bx > 0 {
		a = int(s.modes[by*4+bx-1])
	} else if !s.availA {
		return 2
	} else if l := s.left(); l.typ&h264.MBIntra4x4 != 0 {
		a = int(l.modes[by*4+3])
	} else {
		a = 2
	}
	if by > 0 {
		b = int(s.modes[(by-1)*4+bx])
	} else if !s.availB {
		return 2
	} else if t := s.top(); t.typ&h264.MBIntra4x4 != 0 {
		b = int(t.modes[12+bx])
	} else {
		b = 2
	}
	return min(a, b)
}

// i4Avail reports the neighbour availability of raster 4x4 block r.
func (s *sliceEnc) i4Avail(bx, by int) (left, top, tr, tl bool) {
	left = bx > 0 || s.availA
	top = by > 0 || s.availB
	switch {
	case bx > 0 && by > 0:
		tl = true
	case by > 0:
		tl = s.availA
	case bx > 0:
		tl = s.availB
	default:
		tl = s.availD
	}
	if by == 0 {
		if bx < 3 {
			tr = s.availB
		} else {
			tr = s.availC
		}
	} else if bx < 3 {
		tr = rasterToBlk[(by-1)*4+bx+1] < rasterToBlk[by*4+bx]
	}
	return
}

func (s *sliceEnc) i4Ref(bx, by int) (ref [13]byte, left, top, tl bool) {
	Y, ys := s.cur.y, s.cur.ys
	var tr bool
	left, top, tr, tl = s.i4Avail(bx, by)
	o := s.yOff(bx*4, by*4)
	for i := range ref {
		ref[i] = 128
	}
	if left {
		for i := 0; i < 4; i++ {
			ref[3-i] = Y[o+i*ys-1]
		}
	}
	if tl {
		ref[4] = Y[o-ys-1]
	}
	if top {
		copy(ref[5:9], Y[o-ys:])
		if tr {
			copy(ref[9:13], Y[o-ys+4:])
		} else {
			ref[9], ref[10], ref[11], ref[12] = ref[8], ref[8], ref[8], ref[8]
		}
	}
	return
}

func i4ModeOK(mode int, left, top, tl bool) bool {
	switch mode {
	case h264.I4x4Vertical, h264.I4x4DiagDownLeft, h264.I4x4VerticalLeft:
		return top
	case h264.I4x4Horizontal, h264.I4x4HorizontalUp:
		return left
	case h264.I4x4DiagDownRight, h264.I4x4VerticalRight, h264.I4x4HorizontalDown:
		return top && left && tl
	}
	return true
}

// analyseI4 chooses Intra4x4 modes block by block, coding and
// reconstructing each block as it goes. It gives up (returning a large
// cost) once the running cost exceeds limit.
func (s *sliceEnc) analyseI4(limit int) int {
	Y, ys := s.plane()
	src, ss := s.srcY()
	s.t8x8 = false
	cost := 0
	s.cbpL = 0
	var pbuf [16]byte
	for _, r := range blkOrder {
		bx, by := r&3, r>>2
		ref, left, top, tl := s.i4Ref(bx, by)
		pm := s.predIntraMode(bx, by)
		sb := src[by*4*ss+bx*4:]
		best, bestCost := 2, 1<<30
		var bestPred [4][4]byte
		for mode := 0; mode < 9; mode++ {
			if !i4ModeOK(mode, left, top, tl) {
				continue
			}
			p := h264.Predict4x4(mode, ref, left, top)
			for y := 0; y < 4; y++ {
				copy(pbuf[y*4:], p[y][:])
			}
			c := satd4(sb, ss, pbuf[:], 4)
			if mode == pm {
				c += s.lambda
			} else {
				c += 4 * s.lambda
			}
			if c < bestCost {
				best, bestCost, bestPred = mode, c, p
			}
		}
		s.modes[r] = int8(best)
		cost += bestCost
		db := Y[by*4*ys+bx*4:]
		for y := 0; y < 4; y++ {
			copy(db[y*ys:y*ys+4], bestPred[y][:])
		}
		blk := &s.lumaLv[r]
		fdct4(blk, sb, ss, db, ys)
		if quant4(blk, s.qp, true, 0) > 0 {
			s.cbpL |= 1 << uint((by>>1)*2+bx>>1)
			c := *blk
			dequant4(&c, 0, s.qp, 0)
			h264.IDCT4Add(db, ys, &c)
		}
		if cost > limit {
			return 1 << 30
		}
	}
	return cost
}

// reconI4 rebuilds the Intra4x4 reconstruction from s.modes and s.lumaLv.
func (s *sliceEnc) reconI4() {
	Y, ys := s.plane()
	for _, r := range blkOrder {
		bx, by := r&3, r>>2
		ref, left, top, _ := s.i4Ref(bx, by)
		p := h264.Predict4x4(int(s.modes[r]), ref, left, top)
		db := Y[by*4*ys+bx*4:]
		for y := 0; y < 4; y++ {
			copy(db[y*ys:y*ys+4], p[y][:])
		}
		c := s.lumaLv[r]
		dequant4(&c, 0, s.qp, 0)
		h264.IDCT4Add(db, ys, &c)
	}
}

// codeI16 transforms, quantises and reconstructs an Intra16x16 macroblock
// predicted by s.predY.
func (s *sliceEnc) codeI16() {
	Y, ys := s.plane()
	src, ss := s.srcY()
	var dc [16]int32
	nAC := 0
	for r := 0; r < 16; r++ {
		bx, by := r&3, r>>2
		blk := &s.lumaLv[r]
		fdct4(blk, src[by*4*ss+bx*4:], ss, s.predI16[by*64+bx*4:], 16)
		dc[r] = blk[0]
		blk[0] = 0
		nAC += quant4(blk, s.qp, true, 1)
	}
	lumaDC(&dc, &s.dcLv, s.qp)
	s.cbpL = 0
	if nAC > 0 {
		s.cbpL = 15
	}
	for y := 0; y < 16; y++ {
		copy(Y[y*ys:y*ys+16], s.predI16[y*16:y*16+16])
	}
	for r := 0; r < 16; r++ {
		bx, by := r&3, r>>2
		c := s.lumaLv[r]
		dequant4(&c, 0, s.qp, 1)
		c[0] = dc[r]
		h264.IDCT4Add(Y[by*4*ys+bx*4:], ys, &c)
	}
}

func (s *sliceEnc) chromaNeighbours(P []byte, c int) (top, left []byte, tl byte) {
	cs := s.cur.cs
	o := s.cOff(0, 0)
	if s.availB {
		top = P[o-cs : o-cs+8]
	}
	if s.availA {
		l := &s.leftC[c]
		for i := range l {
			l[i] = P[o+i*cs-1]
		}
		left = l[:]
	}
	if s.availD {
		tl = P[o-cs-1]
	}
	return
}

// analyseChroma picks the intra chroma mode (leaving its prediction in
// predCb/predCr) and returns its cost.
func (s *sliceEnc) analyseChroma() int {
	scb, scr, scs := s.srcC()
	t0, l0, tl0 := s.chromaNeighbours(s.cur.cb, 0)
	t1, l1, tl1 := s.chromaNeighbours(s.cur.cr, 1)
	best, bestCost := 0, 1<<30
	for mode := 0; mode < 4; mode++ {
		switch mode {
		case h264.ChromaHorizontal:
			if l0 == nil {
				continue
			}
		case h264.ChromaVertical:
			if t0 == nil {
				continue
			}
		case h264.ChromaPlane:
			if l0 == nil || t0 == nil || !s.availD {
				continue
			}
		}
		p0 := h264.PredictChroma(mode, t0, l0, tl0)
		p1 := h264.PredictChroma(mode, t1, l1, tl1)
		for y := 0; y < 8; y++ {
			copy(s.tmpC[0][y*8:], p0[y][:])
			copy(s.tmpC[1][y*8:], p1[y][:])
		}
		c := satd(scb, scs, s.tmpC[0][:], 8, 8, 8) + satd(scr, scs, s.tmpC[1][:], 8, 8, 8)
		c += s.lambda * ueBits(uint32(mode))
		if c < bestCost {
			best, bestCost = mode, c
			s.predCb, s.predCr = s.tmpC[0], s.tmpC[1]
		}
	}
	s.chromaMode = best
	return bestCost
}

// codeChroma codes and reconstructs both chroma planes from predCb/predCr.
func (s *sliceEnc) codeChroma(intra bool) {
	cb, cr, cs := s.chromaPlanes()
	scb, scr, scs := s.srcC()
	qpc := h264.ChromaQP(s.qp, s.e.cfg.ChromaQPOffset)
	planes := [2][]byte{cb, cr}
	srcs := [2][]byte{scb, scr}
	preds := [2]*[64]byte{&s.predCb, &s.predCr}
	var dcRec [2][4]int32
	anyAC, anyDC := false, false
	for c := 0; c < 2; c++ {
		var dc [4]int32
		nAC := 0
		for blk := 0; blk < 4; blk++ {
			bx, by := blk&1, blk>>1
			b := &s.cACLv[c][blk]
			fdct4(b, srcs[c][by*4*scs+bx*4:], scs, preds[c][by*32+bx*4:], 8)
			dc[blk] = b[0]
			b[0] = 0
			nAC += quant4(b, qpc, intra, 1)
		}
		if !intra && nAC > 0 {
			score := 0
			for blk := 0; blk < 4; blk++ {
				score += decimateScore(&s.cACLv[c][blk], 1)
			}
			if score < 7 {
				for blk := range s.cACLv[c] {
					s.cACLv[c][blk] = [16]int32{}
				}
				nAC = 0
			}
		}
		list := 4 + c
		if intra {
			list = 1 + c
		}
		if chromaDC(&dc, &s.cDCLv[c], qpc, intra, list) > 0 {
			anyDC = true
		}
		dcRec[c] = dc
		if nAC > 0 {
			anyAC = true
		}
	}
	switch {
	case anyAC:
		s.cbpC = 2
	case anyDC:
		s.cbpC = 1
	default:
		s.cbpC = 0
	}
	for c := 0; c < 2; c++ {
		P := planes[c]
		for y := 0; y < 8; y++ {
			copy(P[y*cs:y*cs+8], preds[c][y*8:y*8+8])
		}
		if s.cbpC == 0 {
			continue
		}
		list := 4 + c
		if intra {
			list = 1 + c
		}
		for blk := 0; blk < 4; blk++ {
			bx, by := blk&1, blk>>1
			b := s.cACLv[c][blk]
			if s.cbpC == 2 {
				dequant4(&b, list, qpc, 1)
			} else {
				b = [16]int32{}
			}
			b[0] = dcRec[c][blk]
			h264.IDCT4Add(P[by*4*cs+bx*4:], cs, &b)
		}
	}
}

// decimateScore is x264's sparse-block score over a raster 4x4 block
// (start = 1 skips the DC): large when worth coding.
func decimateScore(b *[16]int32, start int) int {
	var sc [16]int32
	n := 0
	for i := start; i < 16; i++ {
		sc[n] = b[zz4[i]]
		n++
	}
	i := n - 1
	for i >= 0 && sc[i] == 0 {
		i--
	}
	score := 0
	for i >= 0 {
		if sc[i] > 1 || sc[i] < -1 {
			return 9
		}
		run := 0
		i--
		for i >= 0 && sc[i] == 0 {
			i--
			run++
		}
		score += int(decimateTab[min(run, 15)])
	}
	return score
}

var decimateTab = [16]uint8{3, 2, 2, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
