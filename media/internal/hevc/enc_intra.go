package hevc

import "math"

// Coding quadtree and intra decisions (rate-distortion with the CABAC bit
// estimator; mode candidates ranked by SATD).

// estCost converts the estimator's bits since start into lambda-weighted
// cost.
func (e *Encoder) estCost(start uint64) float64 {
	return e.lambda * float64(e.est.bits-start) / bitScale
}

// decideQT decides the coding quadtree node at (x0, y0), leaving the
// chosen reconstruction and state in place and the estimator advanced
// past its syntax. It returns the node and its cost.
func (e *Encoder) decideQT(x0, y0, log2, depth int) (*encCU, float64) {
	s := &e.d.sd
	sp := s.sps
	size := 1 << log2
	node := &encCU{x: x0, y: y0, log2: log2}
	inside := x0+size <= sp.Width && y0+size <= sp.Height
	canSplit := log2 > sp.Log2MinCb
	if !inside {
		// Implicit split at the picture edge.
		node.split = true
		cost := 0.0
		h := size >> 1
		for i := 0; i < 4; i++ {
			x, y := x0+(i&1)*h, y0+(i>>1)*h
			if x < sp.Width && y < sp.Height {
				c, cc := e.decideQT(x, y, log2-1, depth+1)
				node.sub[i] = c
				cost += cc
			}
		}
		return node, cost
	}
	// Intra CUs up to 32x32 (the largest TB); inter CUs up to the CTU.
	allowLeaf := log2 <= 5 || s.h.typ != sliceI
	snap0 := &e.snaps[2*depth]
	snapLeaf := &e.snaps[2*depth+1]
	e.save(snap0, x0, y0, size)
	start := e.est.bits
	leafCost := math.Inf(1)
	var leaf *encCU
	if allowLeaf {
		if canSplit {
			s.writeSplitFlag(&e.est, x0, y0, depth, false)
		}
		flagCost := e.estCost(start)
		var c float64
		leaf, c = e.decideCU(x0, y0, log2, depth)
		leafCost = c + flagCost
	}
	if !canSplit {
		return leaf, leafCost
	}
	if allowLeaf {
		e.save(snapLeaf, x0, y0, size)
		e.load(snap0)
	}
	s.writeSplitFlag(&e.est, x0, y0, depth, true)
	splitCost := e.estCost(start)
	h := size >> 1
	for i := 0; i < 4; i++ {
		c, cc := e.decideQT(x0+(i&1)*h, y0+(i>>1)*h, log2-1, depth+1)
		node.sub[i] = c
		splitCost += cc
		if splitCost >= leafCost {
			break
		}
	}
	if splitCost < leafCost {
		node.split = true
		return node, splitCost
	}
	e.load(snapLeaf)
	return leaf, leafCost
}

// setupCU records a CU's geometry in the 4x4 state, as the decoder does
// before parsing its prediction.
func (e *Encoder) setupCU(x0, y0, log2, depth, predMode, part int, skip bool) {
	s := &e.d.sd
	n := 1 << log2
	s.cuX, s.cuY, s.cuLog2 = x0, y0, log2
	s.cuTransquantBypass = false
	s.predMode, s.partMode = predMode, part
	s.intraSplit = predMode == modeIntra && part == partNxN
	s.fillCU(x0, y0, n, depth, skip)
	s.setCUMode(x0, y0, n, predMode)
	if predMode == modeIntra {
		s.storeIntra(x0, y0, n)
	}
}

// setIPM stores the luma intra mode of a prediction block.
func (e *Encoder) setIPM(x0, y0, n, mode int) {
	s := &e.d.sd
	sp := s.sps
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			s.blk(x, y).ipm = uint8(mode)
		}
	}
}

// decideIntraCU picks the intra CU coding at (x0, y0): 2Nx2N with the best
// luma mode, or (8x8 CUs) four 4x4 prediction blocks. It returns the CU
// and its distortion plus the rate of its syntax (the estimator advanced
// past it).
func (e *Encoder) decideIntraCU(x0, y0, log2, depth int) (*encCU, float64) {
	s := &e.d.sd
	sp := s.sps
	n := 1 << log2
	start := e.est
	k := []int{2, 3, 5}[clip3(0, 2, e.cfg.Preset)]
	cands := e.searchLumaModes(x0, y0, n, k)
	var best *encCU
	bestCost := math.Inf(1)
	var bestEst cabacEst
	var last *encCU // the candidate whose reconstruction is in place
	for _, m := range cands {
		e.est = start
		cu, d := e.codeIntra2Nx2N(x0, y0, log2, depth, m)
		b := e.est.bits
		s.writeCU(&e.est, cu, depth)
		c := float64(d) + e.lambda*float64(e.est.bits-b)/bitScale
		last = cu
		if c < bestCost {
			best, bestCost, bestEst = cu, c, e.est
		}
	}
	if log2 == sp.Log2MinCb && log2 == 3 {
		e.est = start
		cu, d := e.codeIntraNxN(x0, y0, depth)
		b := e.est.bits
		s.writeCU(&e.est, cu, depth)
		c := float64(d) + e.lambda*float64(e.est.bits-b)/bitScale
		last = cu
		if c < bestCost {
			best, bestCost, bestEst = cu, c, e.est
		}
	}
	if best != last {
		e.codeIntra2Nx2N(x0, y0, log2, depth, best.lumaModes[0])
	}
	e.est = bestEst
	return best, bestCost
}

// codeIntra2Nx2N codes a 2Nx2N intra CU with luma mode m and chroma DM
// (one transform unit), returning the CU and its distortion.
func (e *Encoder) codeIntra2Nx2N(x0, y0, log2, depth, m int) (*encCU, int64) {
	s := &e.d.sd
	n := 1 << log2
	e.setupCU(x0, y0, log2, depth, modeIntra, part2Nx2N, false)
	e.setIPM(x0, y0, n, m)
	s.intraChromaMode = chromaModeOf(4, m)
	cu := &encCU{x: x0, y: y0, log2: log2, predMode: modeIntra, partMode: part2Nx2N, chromaSyn: 4}
	cu.lumaModes[0] = m
	tu := &encTU{x: x0, y: y0, log2: log2}
	var d int64
	tu.coefY, d = e.codeTU(0, x0, y0, log2, m, true)
	tu.cbfY = tu.coefY != nil
	for c := 1; c <= 2; c++ {
		lv, dc := e.codeTU(c, x0, y0, log2-1, s.intraChromaMode, true)
		tu.coefC[c-1], tu.cbfC[c-1] = lv, lv != nil
		d += dc
	}
	cu.tu = tu
	s.finishCUQP(x0, y0, n)
	return cu, d
}

// codeIntraNxN codes an 8x8 CU as four 4x4 prediction blocks, choosing
// each block's mode in turn.
func (e *Encoder) codeIntraNxN(x0, y0, depth int) (*encCU, int64) {
	s := &e.d.sd
	e.setupCU(x0, y0, 3, depth, modeIntra, partNxN, false)
	cu := &encCU{x: x0, y: y0, log2: 3, predMode: modeIntra, partMode: partNxN, chromaSyn: 4}
	root := &encTU{x: x0, y: y0, log2: 3, split: true}
	var d int64
	k := []int{1, 2, 3}[clip3(0, 2, e.cfg.Preset)]
	for i := 0; i < 4; i++ {
		xp, yp := x0+(i&1)*4, y0+(i>>1)*4
		cands := e.searchLumaModes(xp, yp, 4, k)
		bestMode, bestCost := cands[0], math.Inf(1)
		mpm := s.mpmCandidates(xp, yp)
		for _, m := range cands {
			e.setIPM(xp, yp, 4, m)
			lv, dd := e.codeTU(0, xp, yp, 2, m, true)
			start := e.est
			if lv != nil {
				s.writeResidual(&e.est, lv, 2, 0, scanIdxFor(modeIntra, 2, 0, m))
			}
			c := float64(dd) + e.lambda*(float64(e.est.bits-start.bits)/bitScale+modeBits(m, mpm))
			e.est = start
			if c < bestCost {
				bestMode, bestCost = m, c
			}
		}
		e.setIPM(xp, yp, 4, bestMode)
		lv, dd := e.codeTU(0, xp, yp, 2, bestMode, true)
		cu.lumaModes[i] = bestMode
		root.sub[i] = &encTU{x: xp, y: yp, log2: 2, depth: 1, coefY: lv, cbfY: lv != nil}
		d += dd
	}
	s.intraChromaMode = chromaModeOf(4, cu.lumaModes[0])
	for c := 1; c <= 2; c++ {
		lv, dc := e.codeTU(c, x0, y0, 2, s.intraChromaMode, true)
		root.coefC[c-1], root.cbfC[c-1] = lv, lv != nil
		d += dc
	}
	cu.tu = root
	s.finishCUQP(x0, y0, 8)
	return cu, d
}

// modeBits approximates the bits of coding luma mode m given the MPMs.
func modeBits(m int, mpm [3]int) float64 {
	switch m {
	case mpm[0]:
		return 2
	case mpm[1], mpm[2]:
		return 3
	}
	return 6
}

// scanIdxFor derives the coefficient scan (7.4.9.11) of a block.
func scanIdxFor(predMode, log2, cIdx, mode int) int {
	if predMode != modeIntra || !(log2 == 2 || (log2 == 3 && cIdx == 0)) {
		return 0
	}
	switch {
	case mode >= 6 && mode <= 14:
		return 2
	case mode >= 22 && mode <= 30:
		return 1
	}
	return 0
}

// searchLumaModes ranks the luma modes of the n x n block at (x, y) by
// SATD plus approximate mode bits and returns the best k. The best preset
// tries all 35; the others try planar, DC, every fourth angle and the
// MPMs, then refine around the best angle.
func (e *Encoder) searchLumaModes(x, y, n, k int) []int {
	s := &e.d.sd
	tq := &e.tq
	s.intraRefs(&tq.refs, 0, x, y, n)
	mpm := s.mpmCandidates(x, y)
	src := e.srcY[y*e.ys+x:]
	type mc struct {
		m int
		c float64
	}
	best := make([]mc, 0, k+1)
	var tried [35]bool
	try := func(m int) {
		if m < 0 || m > 34 || tried[m] {
			return
		}
		tried[m] = true
		predictIntra(tq.pred[:], n, &tq.refs, n, m, true, s.sps.StrongIntraSmoothing)
		c := float64(satd(src, e.ys, tq.pred[:], n, n)) + e.sqrtL*modeBits(m, mpm)
		i := len(best)
		for i > 0 && best[i-1].c > c {
			i--
		}
		if i < k {
			best = append(best, mc{})
			copy(best[i+1:], best[i:])
			best[i] = mc{m, c}
			if len(best) > k {
				best = best[:k]
			}
		}
	}
	if e.cfg.Preset >= 2 {
		for m := 0; m < 35; m++ {
			try(m)
		}
	} else {
		try(0)
		try(1)
		for m := 2; m <= 34; m += 4 {
			try(m)
		}
		for _, m := range mpm {
			try(m)
		}
		// Refine around the best angular mode.
		ang := -1
		for _, b := range best {
			if b.m >= 2 {
				ang = b.m
				break
			}
		}
		if ang >= 0 {
			try(ang - 2)
			try(ang + 2)
			c := best[0].m
			if c >= 2 {
				try(c - 1)
				try(c + 1)
			}
		}
	}
	out := make([]int, len(best))
	for i, b := range best {
		out[i] = b.m
	}
	return out
}

// codeTU predicts (intra) and codes the residual of one transform block of
// component cIdx at luma location (x, y), log2 its size in that
// component, reconstructing it into the picture. It returns the levels
// (nil when all are zero) and the distortion.
func (e *Encoder) codeTU(cIdx, x, y, log2, mode int, intra bool) ([]int32, int64) {
	s := &e.d.sd
	pic := s.pic
	tq := &e.tq
	n := 1 << log2
	plane, stride, src, ss := pic.y, pic.ys, e.srcY, e.ys
	px, py := x, y
	if cIdx > 0 {
		plane, stride, ss = pic.cb, pic.cs, e.cs
		src = e.srcCb
		if cIdx == 2 {
			plane, src = pic.cr, e.srcCr
		}
		px, py = x/2, y/2
	}
	if intra {
		s.intraPredict(cIdx, px, py, n, mode)
	}
	p := plane[py*stride+px:]
	sr := src[py*ss+px:]
	r := tq.resid[:n*n]
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			r[j*n+i] = int32(sr[j*ss+i]) - int32(p[j*stride+i])
		}
	}
	dst4 := cIdx == 0 && n == 4 && intra
	forwardTransform(tq.coef[:n*n], r, n, dst4, tq.tmp[:])
	levels := e.allocLevels(n * n)
	qp := s.qpFor(cIdx)
	nz := quantize(levels, tq.coef[:n*n], n, qp, intra, tq.frac[:n*n])
	if nz > 0 && s.pps.SignDataHiding {
		predMode := modeInter
		if intra {
			predMode = modeIntra
		}
		nz = hideSigns(levels, tq.coef[:n*n], tq.frac[:n*n], log2, scanIdxFor(predMode, log2, cIdx, mode))
	}
	if cIdx == 0 {
		s.markTU(x, y, n, nz > 0)
	}
	if nz == 0 {
		e.arena = e.arena[:len(e.arena)-n*n]
		return nil, ssd(sr, ss, p, stride, n, n)
	}
	copy(s.coeff[:n*n], levels)
	s.reconstruct(x, y, log2, cIdx, false)
	return levels, ssd(sr, ss, p, stride, n, n)
}
