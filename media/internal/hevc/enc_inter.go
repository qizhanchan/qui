package hevc

import "math"

// Inter decisions: skip / merge, AMVP with motion search, and the inter
// residual.

// decideCU picks the coding of a leaf CU in a P or B slice: skip, merge,
// AMVP 2Nx2N or intra. The winner's reconstruction and state are left in
// place and the estimator advanced past its syntax.
func (e *Encoder) decideCU(x0, y0, log2, depth int) (*encCU, float64) {
	s := e.s
	if s.h.typ == sliceI {
		return e.decideIntraCU(x0, y0, log2, depth)
	}
	n := 1 << log2
	start := e.est
	bestSnap := &e.snaps[8+depth]
	var best *encCU
	bestCost := math.Inf(1)
	try := func(cu *encCU, d int64) {
		b := e.est.bits
		s.writeCU(&e.est, cu, depth)
		c := float64(d) + e.lambda*float64(e.est.bits-b)/bitScale
		if c < bestCost {
			best, bestCost = cu, c
			e.save(bestSnap, x0, y0, n)
		}
		e.est = start
	}
	// Merge candidates ranked by luma SATD of their prediction.
	e.setupCU(x0, y0, log2, depth, modeInter, part2Nx2N, false)
	nm := s.h.maxMergeCand
	type cand struct {
		idx int
		m   motion
		c   int
	}
	var merges []cand
	for i := 0; i < nm; i++ {
		m := s.mergeMotion(x0, y0, n, n, 0, i)
		dup := false
		for _, c := range merges {
			if c.m.same(&m) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		s.motionCompensate(x0, y0, n, n, &m)
		merges = append(merges, cand{i, m, satd(e.srcY[y0*e.ys+x0:], e.ys, s.pic.y[y0*s.pic.ys+x0:], s.pic.ys, n)})
	}
	for i := 1; i < len(merges); i++ {
		for j := i; j > 0 && merges[j].c < merges[j-1].c; j-- {
			merges[j], merges[j-1] = merges[j-1], merges[j]
		}
	}
	nTry := min(len(merges), []int{1, 2, 3}[clip3(0, 2, e.cfg.Preset)])
	for _, c := range merges[:nTry] {
		// Skip.
		cu, d := e.codeInter(x0, y0, log2, depth, encPU{merge: true, mergeIdx: c.idx, m: c.m}, true)
		try(cu, d)
		// Merge with a residual.
		cu, d = e.codeInter(x0, y0, log2, depth, encPU{merge: true, mergeIdx: c.idx, m: c.m}, false)
		if !cu.skip {
			try(cu, d)
		}
	}
	fast := e.cfg.Preset < 2
	if fast && best != nil && best.skip {
		// Early skip: a skip that wins the merge stage ends the search.
		e.load(bestSnap)
		return best, bestCost
	}
	// AMVP with motion search.
	interSATD := math.Inf(1)
	if len(merges) > 0 {
		interSATD = float64(merges[0].c) + e.sqrtL
	}
	if pu, c, ok := e.searchPU(x0, y0, n, n, log2, depth); ok {
		interSATD = math.Min(interSATD, c)
		cu, d := e.codeInter(x0, y0, log2, depth, pu, false)
		try(cu, d)
	}
	// Intra, in the fast presets only when its SATD estimate beats inter.
	if log2 <= 5 && (!fast || e.intraSATD(x0, y0, n) < interSATD) {
		cu, c := e.decideIntraCU(x0, y0, log2, depth)
		if c < bestCost {
			return cu, c
		}
	}
	e.load(bestSnap) // also restores the estimator past the winner
	return best, bestCost
}

// codeInter codes a 2Nx2N inter CU with prediction unit pu (merge or
// AMVP); skip codes no residual. It returns the CU (a merge CU whose
// residual quantises to zero becomes a skip) and its distortion.
func (e *Encoder) codeInter(x0, y0, log2, depth int, pu encPU, skip bool) (*encCU, int64) {
	s := e.s
	n := 1 << log2
	pu.x, pu.y, pu.w, pu.h = x0, y0, n, n
	cu := &encCU{x: x0, y: y0, log2: log2, predMode: modeInter, partMode: part2Nx2N, skip: skip}
	cu.pu[0] = pu
	s.mergeFlag = pu.merge
	e.setupCU(x0, y0, log2, depth, modeInter, part2Nx2N, skip)
	s.storeMotion(x0, y0, n, n, &pu.m)
	s.motionCompensate(x0, y0, n, n, &pu.m)
	if skip {
		s.markTUEdges(x0, y0, n)
		s.finishCUQP(x0, y0, n)
		return cu, e.cuSSD(x0, y0, n)
	}
	s.markPUEdges(x0, y0, n, n)
	tu, any := e.codeInterTree(x0, y0, log2, 0)
	if !any {
		// Nothing coded: a merge CU becomes a skip; an AMVP CU signals
		// rqt_root_cbf = 0. Rebuild the state the decoder would have
		// (no transform edges inside the CU).
		if pu.merge {
			return e.codeInter(x0, y0, log2, depth, pu, true)
		}
		e.setupCU(x0, y0, log2, depth, modeInter, part2Nx2N, false)
		s.storeMotion(x0, y0, n, n, &pu.m)
		s.markPUEdges(x0, y0, n, n)
		s.markTUEdges(x0, y0, n)
		s.finishCUQP(x0, y0, n)
		return cu, e.cuSSD(x0, y0, n)
	}
	cu.rqtRoot = true
	cu.tu = tu
	s.finishCUQP(x0, y0, n)
	return cu, e.cuSSD(x0, y0, n)
}

// codeInterTree codes the residual transform tree of an inter CU: one
// transform unit per CU, split only where the CU exceeds the largest
// transform. It reports whether any block has coefficients.
func (e *Encoder) codeInterTree(x0, y0, log2, depth int) (*encTU, bool) {
	sp := e.s.sps
	tu := &encTU{x: x0, y: y0, log2: log2, depth: depth}
	if log2 > sp.Log2MaxTb {
		tu.split = true
		h := 1 << (log2 - 1)
		any := false
		for i := 0; i < 4; i++ {
			c, a := e.codeInterTree(x0+(i&1)*h, y0+(i>>1)*h, log2-1, depth+1)
			tu.sub[i] = c
			any = any || a
			tu.cbfC[0] = tu.cbfC[0] || c.cbfC[0]
			tu.cbfC[1] = tu.cbfC[1] || c.cbfC[1]
		}
		return tu, any
	}
	tu.coefY, _ = e.codeTU(0, x0, y0, log2, 0, false)
	tu.cbfY = tu.coefY != nil
	for c := 1; c <= 2; c++ {
		lv, _ := e.codeTU(c, x0, y0, log2-1, 0, false)
		tu.coefC[c-1], tu.cbfC[c-1] = lv, lv != nil
	}
	// At depth 0 with no chroma, cbf_luma is inferred to be 1: a block
	// with no coefficients at all must not be coded as a tree.
	return tu, tu.cbfY || tu.cbfC[0] || tu.cbfC[1]
}

// cuSSD is the luma plus chroma distortion of the n x n CU at (x0, y0).
func (e *Encoder) cuSSD(x0, y0, n int) int64 {
	pic := e.s.pic
	sp := e.s.sps
	w, h := min(n, sp.Width-x0), min(n, sp.Height-y0)
	d := ssd(e.srcY[y0*e.ys+x0:], e.ys, pic.y[y0*pic.ys+x0:], pic.ys, w, h)
	o, so := (y0/2)*pic.cs+x0/2, (y0/2)*e.cs+x0/2
	d += ssd(e.srcCb[so:], e.cs, pic.cb[o:], pic.cs, w/2, h/2)
	d += ssd(e.srcCr[so:], e.cs, pic.cr[o:], pic.cs, w/2, h/2)
	return d
}

// mvdBits approximates the bits of coding one motion vector difference.
func mvdBits(d [2]int) float64 {
	b := 0.0
	for _, v := range d {
		a := iabs(v)
		switch {
		case a == 0:
			b += 1
		case a == 1:
			b += 3
		default:
			// gt0, gt1, sign plus EG1 of a-2.
			k, v := 1, a-2
			n := 1
			for v >= 1<<uint(k) {
				v -= 1 << uint(k)
				k++
				n++
			}
			b += 3 + float64(n+k)
		}
	}
	return b
}

// searchPU finds the best AMVP motion for the w x h prediction block at
// (x0, y0): per list the best reference and vector, and for B slices the
// bi-prediction of the two. The CU state must be set up for a 2Nx2N
// inter CU.
func (e *Encoder) searchPU(x0, y0, w, h, log2, depth int) (encPU, float64, bool) {
	s := e.s
	hd := s.h
	e.setupCU(x0, y0, log2, depth, modeInter, part2Nx2N, false)
	type uni struct {
		ok     bool
		ref    int
		mv     [2]int
		mvpIdx int
		mvp    [2]int
		cost   float64
	}
	var best [2]uni
	lists := 1
	if hd.typ == sliceB {
		lists = 2
	}
	for l := 0; l < lists; l++ {
		best[l].cost = math.Inf(1)
		for r := 0; r < hd.numRefIdx[l] && r < len(hd.refList[l]); r++ {
			ref := hd.refList[l][r].pic
			mvp := s.amvp(x0, y0, w, h, 0, l, r)
			mv, cost := e.motionSearch(ref, x0, y0, w, h, mvp)
			if hd.numRefIdx[l] > 1 {
				cost += e.sqrtL * float64(min(r+1, hd.numRefIdx[l]-1))
			}
			if cost < best[l].cost {
				idx := 0
				if mvdBits(sub2(mv, mvp[1])) < mvdBits(sub2(mv, mvp[0])) {
					idx = 1
				}
				best[l] = uni{true, r, mv, idx, mvp[idx], cost}
			}
		}
	}
	if !best[0].ok && !best[1].ok {
		return encPU{}, 0, false
	}
	idc := predL0
	cost := best[0].cost
	if hd.typ == sliceB {
		// inter_pred_idc bits: about one for bi, two for a single list.
		c0, c1 := best[0].cost+2*e.sqrtL, best[1].cost+2*e.sqrtL
		cBi := math.Inf(1)
		if best[0].ok && best[1].ok {
			r0, r1 := hd.refList[0][best[0].ref].pic, hd.refList[1][best[1].ref].pic
			cBi = float64(e.predBiSATD(r0, best[0].mv, r1, best[1].mv, x0, y0, w, h)) + e.sqrtL*(1+mvdBits(sub2(best[0].mv, best[0].mvp))+mvdBits(sub2(best[1].mv, best[1].mvp)))
		}
		cost = c0
		switch {
		case cBi < c0 && cBi < c1:
			idc, cost = predBi, cBi
		case c1 < c0:
			idc, cost = predL1, c1
		}
	}
	pu := encPU{idc: idc}
	for l := 0; l < 2; l++ {
		if idc == predBi || idc == l {
			b := best[l]
			pu.refIdx[l], pu.mvpIdx[l], pu.mvd[l] = b.ref, b.mvpIdx, sub2(b.mv, b.mvp)
			pu.m.pred[l], pu.m.ref[l], pu.m.mv[l] = true, b.ref, b.mv
		}
	}
	return pu, cost, true
}

// intraSATD estimates the intra cost of the n x n block at (x0, y0): the
// best luma mode's SATD plus its approximate bits.
func (e *Encoder) intraSATD(x0, y0, n int) float64 {
	s := e.s
	tq := &e.tq
	m := e.searchLumaModes(x0, y0, n, 1)[0]
	predictIntra(tq.pred[:], n, &tq.refs, n, m, true, s.sps.StrongIntraSmoothing)
	return float64(satd(e.srcY[y0*e.ys+x0:], e.ys, tq.pred[:], n, n)) + e.sqrtL*modeBits(m, s.mpmCandidates(x0, y0))
}

// predBiSATD is the SATD of the bi-prediction from (ref0, mv0) and
// (ref1, mv1) against the source block.
func (e *Encoder) predBiSATD(ref0 *picture, mv0 [2]int, ref1 *picture, mv1 [2]int, x0, y0, w, h int) int {
	s := e.s
	sp := s.sps
	mc := &s.mc
	p0, p1 := mc.pred[0][:w*h], mc.pred[1][:w*h]
	mc.interpLuma(p0, ref0.y, ref0.ys, sp.Width, sp.Height, x0+mv0[0]>>2, y0+mv0[1]>>2, mv0[0]&3, mv0[1]&3, w, h)
	mc.interpLuma(p1, ref1.y, ref1.ys, sp.Width, sp.Height, x0+mv1[0]>>2, y0+mv1[1]>>2, mv1[0]&3, mv1[1]&3, w, h)
	buf := e.tq.pred[:w*h]
	for i := range buf {
		buf[i] = clipU8((int(p0[i]) + int(p1[i]) + 2*predOffset + 64) >> 7)
	}
	return satdWH(e.srcY[y0*e.ys+x0:], e.ys, buf, w, w, h)
}

func sub2(a, b [2]int) [2]int { return [2]int{a[0] - b[0], a[1] - b[1]} }

// motionSearch searches the reference luma for the w x h block at
// (x0, y0): an integer diamond search from the predictors and zero, then
// half- and quarter-sample refinement by SATD. It returns the best vector
// (quarter samples) and its SATD + lambda * bits cost.
func (e *Encoder) motionSearch(ref *picture, x0, y0, w, h int, mvp [2][2]int) ([2]int, float64) {
	sp := e.s.sps
	src := e.srcY[y0*e.ys+x0:]
	mvCost := func(mv [2]int) float64 {
		return e.sqrtL * math.Min(mvdBits(sub2(mv, mvp[0])), mvdBits(sub2(mv, mvp[1])))
	}
	sad := func(ix, iy int) int {
		b, bs := fetch(e.s.mc.edge[:], ref.y, ref.ys, sp.Width, sp.Height, x0+ix, y0+iy, w, h, 0)
		return sad(src, e.ys, b, bs, w, h)
	}
	rng := []int{16, 32, 64}[clip3(0, 2, e.cfg.Preset)]
	bx, by := 0, 0
	bestInt := math.Inf(1)
	visit := func(ix, iy int) bool {
		ix, iy = clip3(-rng-w, sp.Width+rng-x0, ix), clip3(-rng-h, sp.Height+rng-y0, iy)
		c := float64(sad(ix, iy)) + mvCost([2]int{ix * 4, iy * 4})
		if c < bestInt {
			bestInt, bx, by = c, ix, iy
			return true
		}
		return false
	}
	visit(0, 0)
	for _, p := range mvp {
		visit((p[0]+2)>>2, (p[1]+2)>>2)
	}
	// Small diamond until no improvement (bounded).
	for it := 0; it < rng; it++ {
		cx, cy := bx, by
		moved := false
		for _, d := range [4][2]int{{0, -1}, {-1, 0}, {1, 0}, {0, 1}} {
			if visit(cx+d[0], cy+d[1]) {
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	// Sub-sample refinement on SATD of the interpolated prediction.
	mv := [2]int{bx * 4, by * 4}
	best := float64(e.predSATD(ref, x0, y0, w, h, mv)) + mvCost(mv)
	steps := []int{2, 1}
	if e.cfg.Preset == 0 {
		steps = steps[:1]
	}
	for _, step := range steps {
		c0 := mv
		for _, d := range [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}} {
			cand := [2]int{c0[0] + d[0]*step, c0[1] + d[1]*step}
			c := float64(e.predSATD(ref, x0, y0, w, h, cand)) + mvCost(cand)
			if c < best {
				best, mv = c, cand
			}
		}
	}
	return mv, best
}

// predSATD is the SATD between the source block and its uni-directional
// luma prediction from ref with mv.
func (e *Encoder) predSATD(ref *picture, x0, y0, w, h int, mv [2]int) int {
	s := e.s
	sp := s.sps
	mc := &s.mc
	p := mc.pred[0][:w*h]
	mc.interpLuma(p, ref.y, ref.ys, sp.Width, sp.Height, x0+mv[0]>>2, y0+mv[1]>>2, mv[0]&3, mv[1]&3, w, h)
	buf := e.tq.pred[:w*h]
	for i, v := range p {
		buf[i] = clipU8((int(v) + predOffset + 32) >> 6)
	}
	return satdWH(e.srcY[y0*e.ys+x0:], e.ys, buf, w, w, h)
}
