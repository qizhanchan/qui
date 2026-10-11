package h264enc

import "github.com/qizhanchan/qui/media/internal/h264"

// Inter prediction: motion-vector prediction (spec 8.4.1), motion search
// and P-macroblock decisions.

// partGeom lists the partitions (x, y, w, h in 4x4 units) of each
// partitioning: 0 16x16, 1 16x8, 2 8x16, 3 8x8.
var partGeom = [4][][4]int{
	{{0, 0, 4, 4}},
	{{0, 0, 4, 2}, {0, 2, 4, 2}},
	{{0, 0, 2, 4}, {2, 0, 2, 4}},
	{{0, 0, 2, 2}, {2, 0, 2, 2}, {0, 2, 2, 2}, {2, 2, 2, 2}},
}

const (
	refNA   = -2 // neighbour not available
	refNone = -1 // intra / list unused
)

func (s *sliceEnc) idx4(bx, by int) int {
	return (s.mbY*4+by)*s.e.mbW*4 + s.mbX*4 + bx
}

// setMotion sets list-l motion of a w x h (4x4 units) region of the
// current macroblock and marks it final for prediction.
func (s *sliceEnc) setMotion(l, bx, by, w, h, ref, mx, my int) {
	e := s.e
	for y := by; y < by+h; y++ {
		i := s.idx4(bx, y)
		for x := 0; x < w; x++ {
			e.ref[l][i+x] = int8(ref)
			e.mv[l][2*(i+x)] = int16(mx)
			e.mv[l][2*(i+x)+1] = int16(my)
			s.done |= 1 << uint(y*4+bx+x)
		}
	}
}

// nbMotion returns the list-l reference index and vector of the 4x4 block
// at (xr, yr) relative to the current macroblock.
func (s *sliceEnc) nbMotion(l, xr, yr int) (ref, mx, my int) {
	switch {
	case yr >= 0 && xr >= 4:
		return refNA, 0, 0
	case yr >= 0 && xr >= 0:
		if s.done&(1<<uint(yr*4+xr)) == 0 {
			return refNA, 0, 0
		}
	case yr >= 0:
		if !s.availA {
			return refNA, 0, 0
		}
	case xr < 0:
		if !s.availD {
			return refNA, 0, 0
		}
	case xr < 4:
		if !s.availB {
			return refNA, 0, 0
		}
	default:
		if !s.availC {
			return refNA, 0, 0
		}
	}
	e := s.e
	i := (s.mbY*4+yr)*e.mbW*4 + s.mbX*4 + xr
	return int(e.ref[l][i]), int(e.mv[l][2*i]), int(e.mv[l][2*i+1])
}

// mvPred returns the motion-vector predictor of partition (px, py, pw, ph)
// for reference ref of list l; shape is the partitioning (1 16x8, 2 8x16).
func (s *sliceEnc) mvPred(l, px, py, pw, ph, ref, shape int) (int, int) {
	rA, ax, ay := s.nbMotion(l, px-1, py)
	rB, bx, by := s.nbMotion(l, px, py-1)
	rC, cx, cy := s.nbMotion(l, px+pw, py-1)
	if rC == refNA {
		rC, cx, cy = s.nbMotion(l, px-1, py-1)
	}
	switch shape {
	case 1:
		if py == 0 && rB == ref {
			return bx, by
		}
		if py != 0 && rA == ref {
			return ax, ay
		}
	case 2:
		if px == 0 && rA == ref {
			return ax, ay
		}
		if px != 0 && rC == ref {
			return cx, cy
		}
	}
	if rB == refNA && rC == refNA && rA != refNA {
		return ax, ay
	}
	n := 0
	var mx, my int
	if rA == ref {
		n, mx, my = n+1, ax, ay
	}
	if rB == ref {
		n, mx, my = n+1, bx, by
	}
	if rC == ref {
		n, mx, my = n+1, cx, cy
	}
	if n == 1 {
		return mx, my
	}
	return median(ax, bx, cx), median(ay, by, cy)
}

func median(a, b, c int) int {
	return max(min(a, b), min(max(a, b), c))
}

// skipMV returns the P_Skip motion vector (spec 8.4.1.1).
func (s *sliceEnc) skipMV() (int, int) {
	if !s.availA || !s.availB {
		return 0, 0
	}
	rA, ax, ay := s.nbMotion(0, -1, 0)
	rB, bx, by := s.nbMotion(0, 0, -1)
	if (rA == 0 && ax == 0 && ay == 0) || (rB == 0 && bx == 0 && by == 0) {
		return 0, 0
	}
	return s.mvPred(0, 0, 0, 4, 4, 0, 0)
}

func mvBits(d int) int { return seBits(int32(d)) }

// mvLimits returns the allowed quarter-sample vector range for a block at
// pixel (x, y) of size w x h.
func (s *sliceEnc) mvLimits(x, y, w, h int) (minX, maxX, minY, maxY int) {
	e := s.e
	W, H := e.mbW*16, e.mbH*16
	m := lumaPad - 8
	minX, maxX = (-m-x)*4, (W+m-w-x)*4
	minY, maxY = (-m-y)*4, (H+m-h-y)*4
	vr := 512
	switch {
	case e.level <= 10:
		vr = 64
	case e.level <= 20:
		vr = 128
	case e.level <= 30:
		vr = 256
	}
	minY, maxY = max(minY, -(vr-8)*4), min(maxY, (vr-8)*4)
	minX, maxX = max(minX, -2000*4), min(maxX, 2000*4)
	return
}

// search finds a quarter-sample vector for the w x h block at (px, py)
// (pixels inside the macroblock) in reference rp. It returns the vector
// and its SATD + rate cost.
func (s *sliceEnc) search(rp *refPic, px, py, w, h, pmx, pmy, refBits int, cands [][2]int) (int, int, int) {
	e := s.e
	x0, y0 := s.mbX*16+px, s.mbY*16+py
	src, ss := s.srcY()
	src = src[py*ss+px:]
	minX, maxX, minY, maxY := s.mvLimits(x0, y0, w, h)
	lam := s.lambda
	rs := rp.ys
	base := rp.yo + y0*rs + x0
	sadAt := func(ix, iy int) int {
		return sad(src, ss, rp.y[base+iy*rs+ix:], rs, w, h)
	}
	inRange := func(qx, qy int) bool { return qx >= minX && qx <= maxX && qy >= minY && qy <= maxY }
	bestX, bestY, best := 0, 0, 1<<30
	try := func(ix, iy int) bool {
		if !inRange(ix*4, iy*4) {
			return false
		}
		c := sadAt(ix, iy) + lam*(mvBits(ix*4-pmx)+mvBits(iy*4-pmy))
		if c < best {
			bestX, bestY, best = ix, iy, c
			return true
		}
		return false
	}
	try((pmx+2)>>2, (pmy+2)>>2)
	try(0, 0)
	for _, c := range cands {
		try((c[0]+2)>>2, (c[1]+2)>>2)
	}
	if e.cfg.Preset == 0 {
		for it := 0; it < 16; it++ {
			cx, cy := bestX, bestY
			try(cx-1, cy)
			try(cx+1, cy)
			try(cx, cy-1)
			try(cx, cy+1)
			if bestX == cx && bestY == cy {
				break
			}
		}
	} else {
		for it := 0; it < 24; it++ {
			cx, cy := bestX, bestY
			try(cx-2, cy)
			try(cx-1, cy-2)
			try(cx+1, cy-2)
			try(cx+2, cy)
			try(cx+1, cy+2)
			try(cx-1, cy+2)
			if bestX == cx && bestY == cy {
				break
			}
		}
		cx, cy := bestX, bestY
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx != 0 || dy != 0 {
					try(cx+dx, cy+dy)
				}
			}
		}
	}
	// Sub-sample refinement on SATD.
	qx, qy := bestX*4, bestY*4
	cost := func(mx, my int) int {
		p, ps := rp.qpel(s.tmpY[:], x0, y0, mx, my, w, h)
		return satd(src, ss, p, ps, w, h) + lam*(mvBits(mx-pmx)+mvBits(my-pmy)+refBits)
	}
	bc := cost(qx, qy)
	for _, step := range [2]int{2, 1} {
		cx, cy := qx, qy
		for dy := -step; dy <= step; dy += step {
			for dx := -step; dx <= step; dx += step {
				if (dx == 0 && dy == 0) || !inRange(cx+dx, cy+dy) {
					continue
				}
				if c := cost(cx+dx, cy+dy); c < bc {
					qx, qy, bc = cx+dx, cy+dy, c
				}
			}
		}
	}
	return qx, qy, bc
}

// predictPart writes the list-l motion-compensated prediction of a
// partition into dst luma (stride 16) and chroma (stride 8) buffers.
func (s *sliceEnc) predictPart(rp *refPic, bx, by, bw, bh, mx, my int, y []byte, cb, cr []byte) {
	e := s.e
	pw, ph := e.mbW*16+2*lumaPad, e.mbH*16+2*lumaPad
	x0, y0 := s.mbX*16+bx*4, s.mbY*16+by*4
	if rp.hp[0] != nil {
		// The half-sample planes give the exact prediction (TestQpelMatchesMC).
		d := y[by*4*16+bx*4:]
		p, ps := rp.qpel(d, x0, y0, mx, my, bw*4, bh*4)
		if &p[0] != &d[0] {
			for j := 0; j < bh*4; j++ {
				copy(d[j*16:j*16+bw*4], p[j*ps:j*ps+bw*4])
			}
		}
	} else {
		s.mc.Luma(y[by*4*16+bx*4:], 16, rp.y, rp.ys, pw, ph, x0+lumaPad, y0+lumaPad, mx, my, bw*4, bh*4)
	}
	cpw, cph := e.mbW*8+2*chromaPad, e.mbH*8+2*chromaPad
	cx, cy := s.mbX*8+bx*2, s.mbY*8+by*2
	s.mc.Chroma(cb[by*2*8+bx*2:], 8, rp.cb, rp.cs, cpw, cph, cx+chromaPad, cy+chromaPad, mx, my, bw*2, bh*2)
	s.mc.Chroma(cr[by*2*8+bx*2:], 8, rp.cr, rp.cs, cpw, cph, cx+chromaPad, cy+chromaPad, mx, my, bw*2, bh*2)
}

// encodeP analyses, codes and writes a P-slice macroblock.
func (s *sliceEnc) encodeP() {
	e := s.e
	refs := e.lists[0]
	numRefs := s.hdr.numRefs
	s.intra = false

	// Early P_Skip: the skip prediction leaves no residual to code.
	sx, sy := s.skipMV()
	s.predictPart(refs[0], 0, 0, 4, 4, sx, sy, s.predY[:], s.predCb[:], s.predCr[:])
	if s.probeSkip() {
		s.writeSkip(sx, sy)
		return
	}

	// 16x16 motion search over every reference.
	refBits := func(r int) int {
		if numRefs > 1 {
			return ueBits(uint32(r))
		}
		return 0
	}
	cands := s.candidates()
	best16, bestRef, bmx, bmy := 1<<30, 0, 0, 0
	for r := 0; r < numRefs; r++ {
		pmx, pmy := s.mvPred(0, 0, 0, 4, 4, r, 0)
		mx, my, c := s.search(refs[r], 0, 0, 16, 16, pmx, pmy, refBits(r), cands)
		c += s.lambda * 1
		if c < best16 {
			best16, bestRef, bmx, bmy = c, r, mx, my
		}
	}
	s.part = 0
	s.pref[0][0], s.pmv[0][0] = bestRef, [2]int{bmx, bmy}
	bestCost := best16

	if e.cfg.Preset >= 1 && best16 > 16*s.lambda {
		for part := 1; part <= 3; part++ {
			c := s.searchPartition(part, bestRef, bmx, bmy, refBits(bestRef))
			if c < bestCost {
				bestCost, s.part = c, part
				for i := range partGeom[part] {
					s.pref[i][0], s.pmv[i][0] = s.tryRef[i], s.tryMV[i]
				}
			}
		}
	}

	s.applyMotion()

	// Intra in P.
	if _, c16 := s.analyseI16(); c16 < bestCost {
		interCost := bestCost + s.chromaInterCost()
		if ci := s.analyseIntra(true); ci < interCost {
			s.finishIntra()
			return
		}
		s.intra = false
		s.applyMotion() // the intra trial reused the prediction buffers
	}
	s.codeInter()
	if s.part == 0 && s.pref[0][0] == 0 && s.pmv[0][0] == [2]int{sx, sy} && s.cbpL == 0 && s.cbpC == 0 {
		s.writeSkip(sx, sy)
		return
	}
	s.mb.typ = [4]uint32{h264.MB16x16, h264.MB16x8, h264.MB8x16, h264.MB8x8}[s.part]
	s.writeMB()
}

// probeSkip reports whether coding the prediction in predY/predCb/predCr
// would leave no residual after quantisation and decimation, exiting at the
// first evidence otherwise. (P_Skip / B_Skip are valid for any prediction;
// this only decides when they are worth taking.)
func (s *sliceEnc) probeSkip() bool {
	src, ss := s.srcY()
	var blk [16]int32
	var lv [64]int32
	kept := 0
	for b8 := 0; b8 < 4; b8++ {
		score := 0
		x0, y0 := (b8&1)*8, (b8>>1)*8
		mask := dctQuant8(&lv, src[y0*ss+x0:], ss, s.predY[y0*16+x0:], 16, s.qp, false)
		for k := 0; k < 4 && mask != 0; k++ {
			if mask&(1<<uint(k)) != 0 {
				score += decimateScore((*[16]int32)(lv[16*k:16*k+16]), 0)
				if score >= 9 {
					return false
				}
			}
		}
		if score >= 4 {
			if kept += score; kept >= 6 {
				return false
			}
		}
	}
	scb, scr, scs := s.srcC()
	qpc := h264.ChromaQP(s.qp, s.e.cfg.ChromaQPOffset)
	for c, pred := range [2]*[64]byte{&s.predCb, &s.predCr} {
		sp := scb
		if c == 1 {
			sp = scr
		}
		var dc [4]int32
		score := 0
		for b := 0; b < 4; b++ {
			bx, by := (b&1)*4, (b>>1)*4
			fdct4(&blk, sp[by*scs+bx:], scs, pred[by*8+bx:], 8)
			dc[b] = blk[0]
			blk[0] = 0
			if quant4(&blk, qpc, false, 1) > 0 {
				if score += decimateScore(&blk, 1); score >= 7 {
					return false
				}
			}
		}
		var lv [4]int32
		if chromaDC(&dc, &lv, qpc, false, 4+c) > 0 {
			return false
		}
	}
	return true
}

// chromaInterCost estimates the chroma SATD of the current inter choice so
// it can be compared with the intra luma+chroma cost.
func (s *sliceEnc) chromaInterCost() int {
	scb, scr, scs := s.srcC()
	return satd(scb, scs, s.predCb[:], 8, 8, 8) + satd(scr, scs, s.predCr[:], 8, 8, 8)
}

// candidates returns extra search starts: neighbour and co-located vectors.
func (s *sliceEnc) candidates() [][2]int {
	c := s.cands[:0]
	s.done = 0
	for _, p := range [3][2]int{{-1, 0}, {0, -1}, {4, -1}} {
		if r, x, y := s.nbMotion(0, p[0], p[1]); r >= 0 {
			c = append(c, [2]int{x, y})
		}
	}
	if refs := s.e.lists[0]; len(refs) > 0 {
		i := s.idx4(0, 0)
		if refs[0].ref[0][i] >= 0 {
			c = append(c, [2]int{int(refs[0].mv[0][2*i]), int(refs[0].mv[0][2*i+1])})
		}
	}
	return c
}

// searchPartition searches every partition of a partitioning in coding
// order (later partitions predict from earlier ones) and returns the total
// cost; the per-partition results are left in tryRef/tryMV.
func (s *sliceEnc) searchPartition(part, ref, mx, my, refBits int) int {
	s.done = 0
	refs := s.e.lists[0]
	total := s.lambda * [4]int{1, 3, 3, 7}[part]
	for i, g := range partGeom[part] {
		pmx, pmy := s.mvPred(0, g[0], g[1], g[2], g[3], ref, part)
		x, y, c := s.search(refs[ref], g[0]*4, g[1]*4, g[2]*4, g[3]*4, pmx, pmy, refBits, s.oneCand(mx, my))
		s.tryRef[i], s.tryMV[i] = ref, [2]int{x, y}
		s.setMotion(0, g[0], g[1], g[2], g[3], ref, x, y)
		total += c
	}
	return total
}

func (s *sliceEnc) oneCand(mx, my int) [][2]int {
	s.cands[0] = [2]int{mx, my}
	return s.cands[:1]
}

// applyMotion stores the chosen partition motion, computes the predictors
// (mvd) in coding order and builds the prediction.
func (s *sliceEnc) applyMotion() {
	s.done = 0
	refs := s.e.lists[0]
	for l := 1; l < 2; l++ {
		s.setMotionAll(l, refNone)
	}
	s.done = 0
	for i, g := range partGeom[s.part] {
		r := s.pref[i][0]
		pmx, pmy := s.mvPred(0, g[0], g[1], g[2], g[3], r, s.part)
		mv := s.pmv[i][0]
		s.mvp[i][0] = [2]int{pmx, pmy}
		s.setMotion(0, g[0], g[1], g[2], g[3], r, mv[0], mv[1])
		s.predictPart(refs[r], g[0], g[1], g[2], g[3], mv[0], mv[1], s.predY[:], s.predCb[:], s.predCr[:])
	}
}

// setMotionAll sets every 4x4 of list l to ref with zero motion, without
// touching the availability mask.
func (s *sliceEnc) setMotionAll(l, ref int) {
	e := s.e
	for by := 0; by < 4; by++ {
		i := s.idx4(0, by)
		for bx := 0; bx < 4; bx++ {
			e.ref[l][i+bx] = int8(ref)
			e.mv[l][2*(i+bx)] = 0
			e.mv[l][2*(i+bx)+1] = 0
		}
	}
}

// writeSkip codes the macroblock as P_Skip with vector (mx, my); the
// prediction is already reconstructed.
func (s *sliceEnc) writeSkip(mx, my int) {
	s.setMotionAll(1, refNone)
	s.setMotion(0, 0, 0, 4, 4, 0, mx, my)
	s.copyPredToRecon()
	s.cbpL, s.cbpC = 0, 0
	mb := s.mb
	mb.typ = h264.MB16x16 | h264.MBSkip
	mb.skip = true
	if s.e.cfg.CABAC {
		s.cabacSkip(true)
	} else {
		s.skipRun++
	}
	s.recordDeblock(s.lastQP)
}

func (s *sliceEnc) copyPredToRecon() {
	Y, ys := s.plane()
	for y := 0; y < 16; y++ {
		copy(Y[y*ys:y*ys+16], s.predY[y*16:y*16+16])
	}
	cb, cr, cs := s.chromaPlanes()
	for y := 0; y < 8; y++ {
		copy(cb[y*cs:y*cs+8], s.predCb[y*8:y*8+8])
		copy(cr[y*cs:y*cs+8], s.predCr[y*8:y*8+8])
	}
}

// codeInter transforms, quantises and reconstructs the residual of an
// inter macroblock predicted by predY/predCb/predCr.
func (s *sliceEnc) codeInter() {
	Y, ys := s.plane()
	src, ss := s.srcY()
	s.t8x8 = false
	if s.e.cfg.Transform8x8 && s.allow8x8() && s.e.cfg.Preset >= 1 {
		s.t8x8 = s.choose8x8Inter()
	}
	s.cbpL = 0
	if s.t8x8 {
		s.codeInter8()
	} else {
		total := 0
		var score [4]int
		var nz [16]bool
		var lv [64]int32
		for b8 := 0; b8 < 4; b8++ {
			x0, y0 := (b8&1)*8, (b8>>1)*8
			mask := dctQuant8(&lv, src[y0*ss+x0:], ss, s.predY[y0*16+x0:], 16, s.qp, false)
			for k := 0; k < 4; k++ {
				r := (y0/4+k>>1)*4 + x0/4 + k&1
				blk := &s.lumaLv[r]
				copy(blk[:], lv[16*k:16*k+16])
				if mask&(1<<uint(k)) != 0 {
					nz[r] = true
					score[b8] += decimateScore(blk, 0)
				}
			}
		}
		for b8 := 0; b8 < 4; b8++ {
			if score[b8] < 4 {
				s.zero8(b8, &nz)
				score[b8] = 0
			}
			total += score[b8]
		}
		if total < 6 {
			for b8 := 0; b8 < 4; b8++ {
				s.zero8(b8, &nz)
			}
		}
		for y := 0; y < 16; y++ {
			copy(Y[y*ys:y*ys+16], s.predY[y*16:y*16+16])
		}
		for r := 0; r < 16; r++ {
			if !nz[r] {
				continue
			}
			bx, by := r&3, r>>2
			s.cbpL |= 1 << uint((by>>1)*2+bx>>1)
			c := s.lumaLv[r]
			dequant4(&c, 3, s.qp, 0)
			h264.IDCT4Add(Y[by*4*ys+bx*4:], ys, &c)
		}
	}
	if s.cbpL == 0 {
		s.t8x8 = false // transform_size_8x8_flag is only sent with luma coefficients
	}
	s.mb.t8x8 = s.t8x8
	s.codeChroma(false)
}

func (s *sliceEnc) zero8(b8 int, nz *[16]bool) {
	x0, y0 := (b8&1)*2, (b8>>1)*2
	for k := 0; k < 4; k++ {
		r := (y0+k>>1)*4 + x0 + k&1
		s.lumaLv[r] = [16]int32{}
		nz[r] = false
	}
}

// writeInterPred writes mb_type and the motion of an inter macroblock
// (CAVLC).
func (s *sliceEnc) writeInterPred() {
	w := &s.w
	numRefs := s.hdr.numRefs
	if s.typ == sliceB {
		s.writeBPredCAVLC()
		return
	}
	w.ue(uint32(s.part))
	n := len(partGeom[s.part])
	if s.part == 3 {
		for i := 0; i < 4; i++ {
			w.ue(0)
		}
	}
	if numRefs > 1 {
		for i := 0; i < n; i++ {
			w.te(numRefs-1, uint32(s.pref[i][0]))
		}
	}
	for i := 0; i < n; i++ {
		w.se(int32(s.pmv[i][0][0] - s.mvp[i][0][0]))
		w.se(int32(s.pmv[i][0][1] - s.mvp[i][0][1]))
	}
}

// allow8x8 reports whether transform_size_8x8_flag may be sent for the
// current inter macroblock: no partition smaller than 8x8 (B_8x8 direct
// sub-blocks qualify because direct_8x8_inference_flag is set).
func (s *sliceEnc) allow8x8() bool { return true }
