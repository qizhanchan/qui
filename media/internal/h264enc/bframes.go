package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// B pictures: mini-GOPs of up to Config.BFrames non-reference B pictures
// between reference anchors, coded as anchor first (P, or IDR at a key
// interval), then the B pictures in display order. B macroblocks choose
// between B_Skip / B_Direct_16x16 (spatial direct), L0, L1 and
// bi-predicted 16x16, or intra.

// queueB buffers a source picture and codes complete mini-GOPs.
func (e *Encoder) queueB(src *srcPic, pts int64) []Packet {
	var out []Packet
	if e.coded == 0 && len(e.queue) == 0 || pts-e.lastIDR >= int64(e.cfg.KeyInt) {
		out = append(out, e.flushB()...)
		e.lastIDR = pts
		return append(out, e.codePicture(src, pts, sliceI, true, true))
	}
	e.queue = append(e.queue, queued{src, pts})
	if len(e.queue) > e.cfg.BFrames {
		out = append(out, e.flushB()...)
	}
	return out
}

// flushB codes the queued pictures: the last as the P anchor, the others
// as B pictures between the previous anchor and it.
func (e *Encoder) flushB() []Packet {
	if len(e.queue) == 0 {
		return nil
	}
	q := append([]queued(nil), e.queue...)
	e.queue = e.queue[:0]
	last := q[len(q)-1]
	out := []Packet{e.codePicture(last.img, last.pts, sliceP, true, false)}
	for _, b := range q[:len(q)-1] {
		out = append(out, e.codePicture(b.img, b.pts, sliceB, false, false))
	}
	return out
}

// setupBLists sets L0 = the previous anchor and L1 = the next anchor
// (the most recently coded reference), one active entry each.
func (e *Encoder) setupBLists() {
	e.lists[0] = e.refs[1:2]
	e.lists[1] = e.refs[0:1]
}

func minPositive(a, b int) int {
	if a >= 0 && b >= 0 {
		return min(a, b)
	}
	return max(a, b)
}

// colocated returns the motion of the co-located block of 4x4 block (x, y)
// in L1[0] (direct_8x8_inference: the quadrant's corner block).
func (s *sliceEnc) colocated(x, y int) (mx, my, ref int) {
	col := s.e.lists[1][0]
	if col.intra[s.addr] {
		return 0, 0, -1
	}
	i := s.idx4((x>>1)*3, (y>>1)*3)
	l := 0
	if col.ref[0][i] < 0 {
		l = 1
	}
	return int(col.mv[l][2*i]), int(col.mv[l][2*i+1]), int(col.ref[l][i])
}

// spatialDirect derives the B_Skip / B_Direct_16x16 motion (spec
// 8.4.1.2.2) into dRef / dMV (per 8x8 quadrant).
func (s *sliceEnc) spatialDirect() {
	s.done = 0
	var ref [2]int
	for l := 0; l < 2; l++ {
		ra, _, _ := s.nbMotion(l, -1, 0)
		rb, _, _ := s.nbMotion(l, 0, -1)
		rc, _, _ := s.nbMotion(l, 4, -1)
		if rc == refNA {
			rc, _, _ = s.nbMotion(l, -1, -1)
		}
		ref[l] = minPositive(ra, minPositive(rb, rc))
	}
	var mv [2][2]int
	zero := false
	if ref[0] < 0 && ref[1] < 0 {
		ref = [2]int{0, 0}
		zero = true
	} else {
		for l := 0; l < 2; l++ {
			if ref[l] >= 0 {
				x, y := s.mvPred(l, 0, 0, 4, 4, ref[l], 0)
				mv[l] = [2]int{x, y}
			} else {
				ref[l] = -1
			}
		}
	}
	s.dRef = ref
	for q := 0; q < 4; q++ {
		m := mv
		if !zero && (ref[0] == 0 || ref[1] == 0) {
			mx, my, r := s.colocated((q&1)*2, (q>>1)*2)
			if r == 0 && mx >= -1 && mx <= 1 && my >= -1 && my <= 1 {
				for l := 0; l < 2; l++ {
					if ref[l] == 0 {
						m[l] = [2]int{}
					}
				}
			}
		}
		s.dMV[q] = m
	}
}

// predictBi predicts a partition from the lists with ref >= 0, averaging
// when both are used.
func (s *sliceEnc) predictBi(bx, by, w, h int, ref [2]int, mv [2][2]int) {
	l := s.e.lists
	switch {
	case ref[0] >= 0 && ref[1] >= 0:
		s.predictPart(l[0][ref[0]], bx, by, w, h, mv[0][0], mv[0][1], s.predY[:], s.predCb[:], s.predCr[:])
		s.predictPart(l[1][ref[1]], bx, by, w, h, mv[1][0], mv[1][1], s.tmpY2[:], s.tmpC[0][:], s.tmpC[1][:])
		o := by*4*16 + bx*4
		h264.Avg2(s.predY[o:], 16, s.predY[o:], 16, s.tmpY2[o:], 16, w*4, h*4)
		co := by*2*8 + bx*2
		h264.Avg2(s.predCb[co:], 8, s.predCb[co:], 8, s.tmpC[0][co:], 8, w*2, h*2)
		h264.Avg2(s.predCr[co:], 8, s.predCr[co:], 8, s.tmpC[1][co:], 8, w*2, h*2)
	case ref[0] >= 0:
		s.predictPart(l[0][ref[0]], bx, by, w, h, mv[0][0], mv[0][1], s.predY[:], s.predCb[:], s.predCr[:])
	default:
		s.predictPart(l[1][ref[1]], bx, by, w, h, mv[1][0], mv[1][1], s.predY[:], s.predCb[:], s.predCr[:])
	}
}

// applyDirect stores the direct motion and builds its prediction.
func (s *sliceEnc) applyDirect() {
	for q := 0; q < 4; q++ {
		x0, y0 := (q&1)*2, (q>>1)*2
		for l := 0; l < 2; l++ {
			s.setMotion(l, x0, y0, 2, 2, s.dRef[l], s.dMV[q][l][0], s.dMV[q][l][1])
		}
		s.predictBi(x0, y0, 2, 2, s.dRef, s.dMV[q])
	}
}

// encodeB analyses, codes and writes a B-slice macroblock.
func (s *sliceEnc) encodeB() {
	s.intra, s.direct, s.part = false, false, 0
	s.spatialDirect()
	s.done = 0
	s.applyDirect()
	src, ss := s.srcY()
	if s.probeSkip() {
		s.writeBSkip()
		return
	}
	directSATD := satd(src, ss, s.predY[:], 16, 16, 16)
	best, bestType := directSATD+s.lambda, 0

	// L0 and L1 16x16 searches, then the bi-predicted combination.
	var mv [2][2]int
	var c [2]int
	var pmv [2][2]int
	s.done = 0
	for l := 0; l < 2; l++ {
		px, py := s.mvPred(l, 0, 0, 4, 4, 0, 0)
		pmv[l] = [2]int{px, py}
		x, y, cost := s.search(s.e.lists[l][0], 0, 0, 16, 16, px, py, 0, s.oneCand(px, py))
		mv[l], c[l] = [2]int{x, y}, cost+3*s.lambda
		if c[l] < best {
			best, bestType = c[l], 1+l
		}
	}
	s.predictBi(0, 0, 4, 4, [2]int{0, 0}, mv)
	cbi := satd(src, ss, s.predY[:], 16, 16, 16) +
		s.lambda*(5+mvBits(mv[0][0]-pmv[0][0])+mvBits(mv[0][1]-pmv[0][1])+mvBits(mv[1][0]-pmv[1][0])+mvBits(mv[1][1]-pmv[1][1]))
	if cbi < best {
		best, bestType = cbi, 3
	}

	s.bType = bestType
	s.applyB(mv, pmv)
	if _, c16 := s.analyseI16(); c16 < best {
		interCost := best + s.chromaInterCost()
		if ci := s.analyseIntra(true); ci < interCost {
			s.directMaskSet(0)
			s.finishIntra()
			return
		}
		s.intra = false
		s.applyB(mv, pmv)
	}
	s.codeInter()
	if s.bType == 0 && s.cbpL == 0 && s.cbpC == 0 {
		s.writeBSkip()
		return
	}
	mb := s.mb
	if s.bType == 0 {
		mb.typ = h264.MBDirect | h264.MB16x16
		s.directMaskSet(0xf)
	} else {
		mb.typ = h264.MB16x16
	}
	s.writeMB()
}

func (s *sliceEnc) directMaskSet(m uint8) {
	s.mb.directMask = m
	s.mb.direct = m == 0xf
	s.direct = m == 0xf
}

// applyB stores the motion of the chosen B macroblock type and builds the
// prediction.
func (s *sliceEnc) applyB(mv, pmv [2][2]int) {
	s.done = 0
	if s.bType == 0 {
		s.applyDirect()
		s.direct = true
		return
	}
	s.direct = false
	var ref [2]int
	for l := 0; l < 2; l++ {
		if s.bType == 3 || s.bType == 1+l {
			ref[l] = 0
			s.pmv[0][l] = mv[l]
			s.mvp[0][l] = pmv[l]
			s.setMotion(l, 0, 0, 4, 4, 0, mv[l][0], mv[l][1])
		} else {
			ref[l] = -1
			s.setMotion(l, 0, 0, 4, 4, refNone, 0, 0)
		}
	}
	s.predictBi(0, 0, 4, 4, ref, mv)
}

// writeBSkip codes B_Skip; the direct prediction is reconstructed.
func (s *sliceEnc) writeBSkip() {
	s.applyDirect()
	s.copyPredToRecon()
	s.cbpL, s.cbpC = 0, 0
	mb := s.mb
	mb.typ = h264.MBDirect | h264.MBSkip | h264.MB16x16
	mb.skip = true
	s.directMaskSet(0xf)
	if s.e.cfg.CABAC {
		s.cabacSkip(true)
	} else {
		s.skipRun++
	}
	s.recordDeblock(s.lastQP)
}

// bLists reports which lists the current B 16x16 type uses.
func (s *sliceEnc) bLists() (l0, l1 bool) {
	return s.bType == 1 || s.bType == 3, s.bType == 2 || s.bType == 3
}

func (s *sliceEnc) writeBPredCAVLC() {
	w := &s.w
	w.ue(uint32(s.bType))
	if s.bType == 0 {
		return
	}
	u0, u1 := s.bLists()
	for l, use := range [2]bool{u0, u1} {
		if use {
			w.se(int32(s.pmv[0][l][0] - s.mvp[0][l][0]))
			w.se(int32(s.pmv[0][l][1] - s.mvp[0][l][1]))
		}
	}
}

func (s *sliceEnc) writeBPredCABAC() {
	if s.bType == 0 {
		return
	}
	u0, u1 := s.bLists()
	for l, use := range [2]bool{u0, u1} {
		if use {
			s.cabacMvd(l, 0, 0, 4, 4, s.pmv[0][l][0]-s.mvp[0][l][0], s.pmv[0][l][1]-s.mvp[0][l][1])
		}
	}
}
