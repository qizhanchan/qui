package hevc

import "math"

// Inter decisions: skip / merge, AMVP with motion search, and the inter
// residual.

// decideCU picks the coding of a leaf CU in a P or B slice: skip, merge,
// AMVP 2Nx2N or intra. The winner's reconstruction and state are left in
// place and the estimator advanced past its syntax.
func (e *Encoder) decideCU(x0, y0, log2, depth int) (*encCU, float64) {
	s := &e.d.sd
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
	// AMVP with motion search.
	if pu, ok := e.searchPU(x0, y0, n, n, log2, depth); ok {
		cu, d := e.codeInter(x0, y0, log2, depth, pu, false)
		try(cu, d)
	}
	// Intra.
	if log2 <= 5 {
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
	s := &e.d.sd
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
	sp := e.d.sd.sps
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
	pic := e.d.sd.pic
	sp := e.d.sd.sps
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
// (x0, y0) over the L0 references (P slices). The CU state must be set
// up for a 2Nx2N inter CU.
func (e *Encoder) searchPU(x0, y0, w, h, log2, depth int) (encPU, bool) {
	s := &e.d.sd
	hd := s.h
	e.setupCU(x0, y0, log2, depth, modeInter, part2Nx2N, false)
	best := encPU{idc: predL0}
	bestCost := math.Inf(1)
	for r := 0; r < hd.numRefIdx[0] && r < len(hd.refList[0]); r++ {
		ref := hd.refList[0][r].pic
		mvp := s.amvp(x0, y0, w, h, 0, 0, r)
		mv, cost := e.motionSearch(ref, x0, y0, w, h, mvp)
		refBits := 0.0
		if hd.numRefIdx[0] > 1 {
			refBits = float64(min(r+1, hd.numRefIdx[0]-1))
		}
		cost += e.sqrtL * refBits
		if cost < bestCost {
			bestCost = cost
			idx := 0
			if mvdBits(sub2(mv, mvp[1])) < mvdBits(sub2(mv, mvp[0])) {
				idx = 1
			}
			best = encPU{idc: predL0, refIdx: [2]int{r, 0}, mvpIdx: [2]int{idx, 0}}
			best.mvd[0] = sub2(mv, mvp[idx])
			best.m = motion{pred: [2]bool{true, false}, ref: [2]int{r, 0}, mv: [2][2]int{mv, {}}}
		}
	}
	return best, bestCost < math.Inf(1)
}

func sub2(a, b [2]int) [2]int { return [2]int{a[0] - b[0], a[1] - b[1]} }

// motionSearch searches the reference luma for the w x h block at
// (x0, y0): an integer diamond search from the predictors and zero, then
// half- and quarter-sample refinement by SATD. It returns the best vector
// (quarter samples) and its SATD + lambda * bits cost.
func (e *Encoder) motionSearch(ref *picture, x0, y0, w, h int, mvp [2][2]int) ([2]int, float64) {
	sp := e.d.sd.sps
	src := e.srcY[y0*e.ys+x0:]
	mvCost := func(mv [2]int) float64 {
		return e.sqrtL * math.Min(mvdBits(sub2(mv, mvp[0])), mvdBits(sub2(mv, mvp[1])))
	}
	sad := func(ix, iy int) int {
		b, bs := fetch(e.d.sd.mc.edge[:], ref.y, ref.ys, sp.Width, sp.Height, x0+ix, y0+iy, w, h, 0)
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
	for _, step := range []int{2, 1} {
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
	s := &e.d.sd
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
