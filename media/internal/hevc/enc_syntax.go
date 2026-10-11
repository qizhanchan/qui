package hevc

// Syntax writers. Each mirrors the decoder's parsing function of the same
// element (ctu.go, residual.go, inter.go) and reads the same picture state
// for its context selection, so a decision written here parses back to
// itself. They write into any binSink: the arithmetic coder, or the bit
// estimator during mode decision.

// encCU is a coding quadtree node: either split into sub-CUs or a coded
// CU.
type encCU struct {
	x, y, log2 int
	split      bool
	sub        [4]*encCU

	skip     bool
	predMode int
	partMode int
	// Intra: luma modes per PU and the chroma syntax value (4 = DM).
	lumaModes [4]int
	chromaSyn int
	// Inter: one entry per PU.
	pu      [4]encPU
	rqtRoot bool
	tu      *encTU
}

// encPU is one inter prediction unit's syntax.
type encPU struct {
	x, y, w, h int
	merge      bool
	mergeIdx   int
	idc        int // predL0, predL1, predBi
	refIdx     [2]int
	mvd        [2][2]int
	mvpIdx     [2]int
	m          motion // the derived motion
}

// encTU is a transform tree node. Chroma levels live at the node where
// cbf_cb / cbf_cr are coded (log2 > 2); 4x4 luma children (blkIdx 3)
// write their parent's chroma.
type encTU struct {
	x, y, log2, depth int
	split             bool
	sub               [4]*encTU
	cbfY              bool
	cbfC              [2]bool
	coefY             []int32
	coefC             [2][]int32
}

// writeQuadtree writes coding_quadtree() for node cu at depth.
func (s *sliceDec) writeQuadtree(w binSink, cu *encCU, depth int) {
	sp := s.sps
	size := 1 << cu.log2
	if cu.x+size <= sp.Width && cu.y+size <= sp.Height && cu.log2 > sp.Log2MinCb {
		s.writeSplitFlag(w, cu.x, cu.y, depth, cu.split)
	}
	if cu.split {
		for _, c := range cu.sub {
			if c != nil {
				s.writeQuadtree(w, c, depth+1)
			}
		}
		return
	}
	s.writeCU(w, cu, depth)
}

func (s *sliceDec) writeSplitFlag(w binSink, x0, y0, depth int, split bool) {
	inc := 0
	if s.available(x0, y0, x0-1, y0) && int(s.blk(x0-1, y0).depth) > depth {
		inc++
	}
	if s.available(x0, y0, x0, y0-1) && int(s.blk(x0, y0-1).depth) > depth {
		inc++
	}
	w.decision(ctxSplitCU+inc, boolInt(split))
}

// writeCU writes coding_unit().
func (s *sliceDec) writeCU(w binSink, cu *encCU, depth int) {
	sp := s.sps
	x0, y0, log2 := cu.x, cu.y, cu.log2
	// The state the decoder sets while parsing this CU.
	s.cuX, s.cuY, s.cuLog2 = x0, y0, log2
	s.predMode, s.partMode = cu.predMode, cu.partMode
	if cu.predMode == modeIntra {
		s.intraChromaMode = chromaModeOf(cu.chromaSyn, cu.lumaModes[0])
	}
	if s.h.typ != sliceI {
		inc := 0
		if s.available(x0, y0, x0-1, y0) && s.blk(x0-1, y0).skip {
			inc++
		}
		if s.available(x0, y0, x0, y0-1) && s.blk(x0, y0-1).skip {
			inc++
		}
		w.decision(ctxSkip+inc, boolInt(cu.skip))
	}
	if cu.skip {
		s.writeMergeIdx(w, cu.pu[0].mergeIdx)
		return
	}
	if s.h.typ != sliceI {
		w.decision(ctxPredMode, boolInt(cu.predMode == modeIntra))
	}
	if cu.predMode != modeIntra || log2 == sp.Log2MinCb {
		s.writePartMode(w, cu.predMode, cu.partMode, log2)
	}
	if cu.predMode == modeIntra {
		s.writeIntraModes(w, cu)
	} else {
		for i := 0; i < numPUs(cu.partMode); i++ {
			s.writePU(w, &cu.pu[i])
		}
		if !(cu.partMode == part2Nx2N && cu.pu[0].merge) {
			w.decision(ctxRqtRootCbf, boolInt(cu.rqtRoot))
		}
		if !cu.rqtRoot {
			return
		}
	}
	maxDepth := sp.MaxTrDepthInter
	intraSplit := cu.predMode == modeIntra && cu.partMode == partNxN
	if cu.predMode == modeIntra {
		maxDepth = sp.MaxTrDepthIntra + boolInt(intraSplit)
	}
	s.writeTransformTree(w, cu, cu.tu, nil, 0, maxDepth, intraSplit, [2]bool{true, true})
}

func numPUs(part int) int {
	switch part {
	case part2Nx2N:
		return 1
	case partNxN:
		return 4
	}
	return 2
}

func (s *sliceDec) writePartMode(w binSink, predMode, part, log2 int) {
	sp := s.sps
	if part == part2Nx2N {
		w.decision(ctxPartMode, 1)
		return
	}
	w.decision(ctxPartMode, 0)
	if predMode == modeIntra {
		return // NxN
	}
	if log2 == sp.Log2MinCb {
		if part == part2NxN {
			w.decision(ctxPartMode+1, 1)
			return
		}
		w.decision(ctxPartMode+1, 0)
		if log2 == 3 {
			return // Nx2N
		}
		w.decision(ctxPartMode+2, boolInt(part == partNx2N))
		return
	}
	horizontal := part == part2NxN || part == part2NxnU || part == part2NxnD
	w.decision(ctxPartMode+1, boolInt(horizontal))
	if !sp.AMP {
		return
	}
	switch part {
	case part2NxN, partNx2N:
		w.decision(ctxPartMode+3, 1)
	case part2NxnU, partnLx2N:
		w.decision(ctxPartMode+3, 0)
		w.bypass(0)
	default:
		w.decision(ctxPartMode+3, 0)
		w.bypass(1)
	}
}

// writeIntraModes writes the luma (MPM / remaining) and chroma modes. The
// MPM lists come from the final state of the neighbouring blocks, so the
// PU modes of the CU must already be stored when an NxN CU is written.
func (s *sliceDec) writeIntraModes(w binSink, cu *encCU) {
	n := 1 << cu.log2
	pb, nParts := n, 1
	if cu.partMode == partNxN {
		pb, nParts = n/2, 4
	}
	var mpm [4]int
	var lists [4][3]int
	for i := 0; i < nParts; i++ {
		xp, yp := cu.x+(i&1)*pb, cu.y+(i>>1)*pb
		lists[i] = s.mpmCandidates(xp, yp)
		mpm[i] = -1
		for j, c := range lists[i] {
			if c == cu.lumaModes[i] {
				mpm[i] = j
			}
		}
		w.decision(ctxPrevIntra, boolInt(mpm[i] >= 0))
	}
	for i := 0; i < nParts; i++ {
		if mpm[i] >= 0 {
			switch mpm[i] {
			case 0:
				w.bypass(0)
			case 1:
				w.bypass(1)
				w.bypass(0)
			default:
				w.bypass(1)
				w.bypass(1)
			}
			continue
		}
		l := lists[i]
		if l[0] > l[1] {
			l[0], l[1] = l[1], l[0]
		}
		if l[0] > l[2] {
			l[0], l[2] = l[2], l[0]
		}
		if l[1] > l[2] {
			l[1], l[2] = l[2], l[1]
		}
		rem := cu.lumaModes[i]
		for j := 2; j >= 0; j-- {
			if rem > l[j] {
				rem--
			}
		}
		w.bypassBits(rem, 5)
	}
	if cu.chromaSyn == 4 {
		w.decision(ctxChromaMode, 0)
	} else {
		w.decision(ctxChromaMode, 1)
		w.bypassBits(cu.chromaSyn, 2)
	}
}

// chromaModeOf derives IntraPredModeC from the chroma syntax value.
func chromaModeOf(syn, luma int) int {
	if syn == 4 {
		return luma
	}
	m := [4]int{0, 26, 10, 1}[syn]
	if m == luma {
		m = 34
	}
	return m
}

func (s *sliceDec) writeMergeIdx(w binSink, idx int) {
	if s.h.maxMergeCand <= 1 {
		return
	}
	if idx == 0 {
		w.decision(ctxMergeIdx, 0)
		return
	}
	w.decision(ctxMergeIdx, 1)
	for i := 1; i < s.h.maxMergeCand-1; i++ {
		if i < idx {
			w.bypass(1)
		} else {
			w.bypass(0)
			return
		}
	}
}

// writePU writes prediction_unit() (not for skipped CUs).
func (s *sliceDec) writePU(w binSink, pu *encPU) {
	hd := s.h
	w.decision(ctxMergeFlag, boolInt(pu.merge))
	if pu.merge {
		s.writeMergeIdx(w, pu.mergeIdx)
		return
	}
	if hd.typ == sliceB {
		if pu.w+pu.h != 12 {
			depth := int(s.blk(s.cuX, s.cuY).depth)
			if pu.idc == predBi {
				w.decision(ctxInterPredIdc+depth, 1)
			} else {
				w.decision(ctxInterPredIdc+depth, 0)
				w.decision(ctxInterPredIdc+4, pu.idc)
			}
		} else {
			w.decision(ctxInterPredIdc+4, pu.idc)
		}
	}
	for l := 0; l < 2; l++ {
		if !(pu.idc == predBi || pu.idc == l) {
			continue
		}
		if n := hd.numRefIdx[l]; n > 1 {
			r := pu.refIdx[l]
			for i := 0; i < n-1; i++ {
				b := boolInt(i < r)
				if i < 2 {
					w.decision(ctxRefIdx+i, b)
				} else {
					w.bypass(b)
				}
				if b == 0 {
					break
				}
			}
		}
		if !(l == 1 && hd.mvdL1Zero && pu.idc == predBi) {
			writeMvd(w, pu.mvd[l])
		}
		w.decision(ctxMvpFlag, pu.mvpIdx[l])
	}
}

func writeMvd(w binSink, v [2]int) {
	a := [2]int{iabs(v[0]), iabs(v[1])}
	w.decision(ctxMvdGt0, boolInt(a[0] > 0))
	w.decision(ctxMvdGt0, boolInt(a[1] > 0))
	for k := 0; k < 2; k++ {
		if a[k] > 0 {
			w.decision(ctxMvdGt1, boolInt(a[k] > 1))
		}
	}
	for k := 0; k < 2; k++ {
		if a[k] == 0 {
			continue
		}
		if a[k] > 1 {
			writeEGk(w, a[k]-2, 1)
		}
		w.bypass(boolInt(v[k] < 0))
	}
}

// writeEGk writes a k-th order Exp-Golomb bypass code.
func writeEGk(w binSink, v, k int) {
	for v >= 1<<uint(k) {
		w.bypass(1)
		v -= 1 << uint(k)
		k++
	}
	w.bypass(0)
	w.bypassBits(v, k)
}

// writeTransformTree writes transform_tree(). parent is nil at the root.
func (s *sliceDec) writeTransformTree(w binSink, cu *encCU, tu, parent *encTU, blkIdx, maxDepth int, intraSplit bool, parentCbf [2]bool) {
	sp := s.sps
	log2, depth := tu.log2, tu.depth
	if log2 <= sp.Log2MaxTb && log2 > sp.Log2MinTb && depth < maxDepth && !(intraSplit && depth == 0) {
		w.decision(ctxSplitTransform+5-log2, boolInt(tu.split))
	}
	if log2 > 2 {
		for c := 0; c < 2; c++ {
			if depth == 0 || parentCbf[c] {
				w.decision(ctxCbfChroma+depth, boolInt(tu.cbfC[c]))
			}
		}
	}
	cbfC := tu.cbfC
	if log2 == 2 {
		cbfC = parentCbf
	}
	if tu.split {
		for i, c := range tu.sub {
			s.writeTransformTree(w, cu, c, tu, i, maxDepth, intraSplit, cbfC)
		}
		return
	}
	if cu.predMode == modeIntra || depth != 0 || cbfC[0] || cbfC[1] {
		inc := 0
		if depth == 0 {
			inc = 1
		}
		w.decision(ctxCbfLuma+inc, boolInt(tu.cbfY))
	}
	// transform_unit(): no cu_qp_delta (constant QP within a slice).
	if tu.cbfY {
		s.writeResidual(w, tu.coefY, log2, 0, s.scanIdx(cu, tu.x, tu.y, log2, 0))
	}
	if log2 > 2 {
		for c := 0; c < 2; c++ {
			if tu.cbfC[c] {
				s.writeResidual(w, tu.coefC[c], log2-1, c+1, s.scanIdx(cu, tu.x, tu.y, log2-1, c+1))
			}
		}
	} else if blkIdx == 3 {
		for c := 0; c < 2; c++ {
			if parent.cbfC[c] {
				s.writeResidual(w, parent.coefC[c], 2, c+1, s.scanIdx(cu, parent.x, parent.y, 2, c+1))
			}
		}
	}
}

// scanIdx derives the coefficient scan of a block (7.4.9.11).
func (s *sliceDec) scanIdx(cu *encCU, x0, y0, log2, cIdx int) int {
	if cu.predMode != modeIntra || !(log2 == 2 || (log2 == 3 && cIdx == 0)) {
		return 0
	}
	m := s.intraChromaMode
	if cIdx == 0 {
		m = int(s.blk(x0, y0).ipm)
	}
	switch {
	case m >= 6 && m <= 14:
		return 2
	case m >= 22 && m <= 30:
		return 1
	}
	return 0
}

// lastPosBins returns the prefix and suffix (with its length) of a last
// significant coefficient coordinate.
func lastPosBins(v int) (prefix, suffix, nb int) {
	if v < 4 {
		return v, 0, 0
	}
	for p := 4; ; p++ {
		nb = p>>1 - 1
		lo := (1 << uint(nb)) * (2 + p&1)
		if v < lo+1<<uint(nb) {
			return p, v - lo, nb
		}
	}
}

func (s *sliceDec) writeLastPrefix(w binSink, base, log2, cIdx, p int) {
	var off, shift int
	if cIdx == 0 {
		off, shift = 3*(log2-2)+((log2-1)>>2), (log2+1)>>2
	} else {
		off, shift = 15, log2-2
	}
	max := log2<<1 - 1
	for i := 0; i < p; i++ {
		w.decision(base+off+i>>shift, 1)
	}
	if p < max {
		w.decision(base+off+p>>shift, 0)
	}
}

// writeResidual writes residual_coding() for the raster levels c of an
// n x n block. With sign data hiding the levels must already satisfy the
// parity rule (quantisation ensures it).
func (s *sliceDec) writeResidual(w binSink, c []int32, log2, cIdx, scanIdx int) {
	n := 1 << log2
	log2SB := log2 - 2
	nSB := 1 << log2SB
	scanSB := scans[log2SB][scanIdx]
	scan4 := scans[2][scanIdx]
	// Last significant coefficient in scan order.
	lastSB, lastPos := -1, -1
	for i := nSB*nSB - 1; i >= 0 && lastSB < 0; i-- {
		xS, yS := int(scanSB[i][0]), int(scanSB[i][1])
		for p := 15; p >= 0; p-- {
			xC, yC := xS<<2+int(scan4[p][0]), yS<<2+int(scan4[p][1])
			if c[yC*n+xC] != 0 {
				lastSB, lastPos = i, p
				break
			}
		}
	}
	sb := scanSB[lastSB]
	lastX, lastY := int(sb[0])<<2+int(scan4[lastPos][0]), int(sb[1])<<2+int(scan4[lastPos][1])
	if scanIdx == 2 {
		lastX, lastY = lastY, lastX
	}
	px, sx, nx := lastPosBins(lastX)
	py, sy, ny := lastPosBins(lastY)
	s.writeLastPrefix(w, ctxLastX, log2, cIdx, px)
	s.writeLastPrefix(w, ctxLastY, log2, cIdx, py)
	if px > 3 {
		w.bypassBits(sx, nx)
	}
	if py > 3 {
		w.bypassBits(sy, ny)
	}
	var csbf [8][8]bool
	greater1Ctx := 1
	chroma := cIdx > 0
	sdh := s.pps.SignDataHiding && !s.cuTransquantBypass
	for i := lastSB; i >= 0; i-- {
		xS, yS := int(scanSB[i][0]), int(scanSB[i][1])
		at := func(p int) int32 {
			return c[(yS<<2+int(scan4[p][1]))*n+xS<<2+int(scan4[p][0])]
		}
		inferDC := false
		if i < lastSB && i > 0 {
			any := false
			for p := 0; p < 16; p++ {
				if at(p) != 0 {
					any = true
					break
				}
			}
			cc := 0
			if xS < nSB-1 && csbf[xS+1][yS] {
				cc++
			}
			if yS < nSB-1 && csbf[xS][yS+1] {
				cc++
			}
			inc := min(cc, 1)
			if chroma {
				inc += 2
			}
			w.decision(ctxCodedSubBlock+inc, boolInt(any))
			csbf[xS][yS] = any
			inferDC = true
		} else {
			csbf[xS][yS] = true
		}
		var sig [16]uint8
		nsig := 0
		start := 15
		if i == lastSB {
			start = lastPos - 1
			sig[0] = uint8(lastPos)
			nsig = 1
		}
		if csbf[xS][yS] {
			prev := 0
			if xS < nSB-1 && csbf[xS+1][yS] {
				prev |= 1
			}
			if yS < nSB-1 && csbf[xS][yS+1] {
				prev |= 2
			}
			for p := start; p >= 0; p-- {
				xC := xS<<2 + int(scan4[p][0])
				yC := yS<<2 + int(scan4[p][1])
				nz := at(p) != 0
				if p > 0 || !inferDC {
					inc := sigCtxInc(log2, cIdx, xC, yC, scanIdx, prev)
					w.decision(ctxSig+inc, boolInt(nz))
					if nz {
						sig[nsig] = uint8(p)
						nsig++
						inferDC = false
					}
				} else {
					sig[nsig] = 0
					nsig++
				}
			}
		}
		if nsig == 0 {
			continue
		}
		ctxSet := 0
		if i != 0 && !chroma {
			ctxSet = 2
		}
		if i != lastSB && greater1Ctx == 0 {
			ctxSet++
		}
		greater1Ctx = 1
		var abs [16]int
		for m := 0; m < nsig; m++ {
			abs[m] = int(iabs32(at(int(sig[m]))))
		}
		firstG1 := -1
		for m := 0; m < nsig && m < 8; m++ {
			inc := ctxSet*4 + greater1Ctx
			if chroma {
				inc += 16
			}
			g1 := abs[m] > 1
			w.decision(ctxGt1+inc, boolInt(g1))
			if g1 {
				greater1Ctx = 0
				if firstG1 < 0 {
					firstG1 = m
				}
			} else if greater1Ctx > 0 && greater1Ctx < 3 {
				greater1Ctx++
			}
		}
		if firstG1 >= 0 {
			inc := ctxSet
			if chroma {
				inc += 4
			}
			w.decision(ctxGt2+inc, boolInt(abs[firstG1] > 2))
		}
		hidden := sdh && int(sig[0])-int(sig[nsig-1]) > 3
		nSigns := nsig
		if hidden {
			nSigns--
		}
		for m := 0; m < nSigns; m++ {
			w.bypass(boolInt(at(int(sig[m])) < 0))
		}
		rice := 0
		for m := 0; m < nsig; m++ {
			base, threshold := 1, 1
			if m < 8 {
				threshold = 2
				if abs[m] > 1 {
					base++
					if m == firstG1 {
						threshold = 3
						if abs[m] > 2 {
							base++
						}
					}
				}
			}
			if base == threshold {
				writeRemaining(w, abs[m]-base, rice)
				if abs[m] > 3*(1<<uint(rice)) {
					rice = min(rice+1, 4)
				}
			}
		}
	}
}

func iabs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// writeRemaining writes coeff_abs_level_remaining with Rice parameter k.
func writeRemaining(w binSink, v, k int) {
	if v < 4<<uint(k) {
		p := v >> uint(k)
		for i := 0; i < p; i++ {
			w.bypass(1)
		}
		w.bypass(0)
		w.bypassBits(v&(1<<uint(k)-1), k)
		return
	}
	p := 4
	for v >= ((1<<uint(p-2))+2)<<uint(k) {
		p++
	}
	for i := 0; i < p; i++ {
		w.bypass(1)
	}
	w.bypass(0)
	w.bypassBits(v-((1<<uint(p-3))+2)<<uint(k), p-3+k)
}

// writeSAO writes sao() for the current CTB from prm; merge selects
// sao_merge_left (1) or sao_merge_up (2), 0 for explicit parameters.
func (s *sliceDec) writeSAO(w binSink, prm *saoParams, merge int) {
	sp, p, h := s.sps, s.pps, s.h
	fc := s.d.fc
	rx, ry := s.ctbX, s.ctbY
	if rx > 0 {
		left := s.ctbAddrRs - 1
		if fc.ctbSliceAddr[left] == fc.ctbSliceAddr[s.ctbAddrRs] && p.tileID[p.ctbRsToTs[left]] == p.tileID[s.ctbAddrTs] {
			w.decision(ctxSaoMerge, boolInt(merge == 1))
			if merge == 1 {
				return
			}
		}
	}
	if ry > 0 {
		up := s.ctbAddrRs - sp.PicWCtb
		if fc.ctbSliceAddr[up] == fc.ctbSliceAddr[s.ctbAddrRs] && p.tileID[p.ctbRsToTs[up]] == p.tileID[s.ctbAddrTs] {
			w.decision(ctxSaoMerge, boolInt(merge == 2))
			if merge == 2 {
				return
			}
		}
	}
	for c := 0; c < 3; c++ {
		if (c == 0 && !h.saoLuma) || (c > 0 && !h.saoChroma) {
			continue
		}
		if c != 2 {
			t := int(prm.typ[c])
			if t == 0 {
				w.decision(ctxSaoType, 0)
			} else {
				w.decision(ctxSaoType, 1)
				w.bypass(t - 1)
			}
		}
		if prm.typ[c] == 0 {
			continue
		}
		for i := 0; i < 4; i++ {
			a := iabs(int(prm.offset[c][i]))
			for k := 0; k < a; k++ {
				w.bypass(1)
			}
			if a < 7 {
				w.bypass(0)
			}
		}
		if prm.typ[c] == 1 {
			for i := 0; i < 4; i++ {
				if prm.offset[c][i] != 0 {
					w.bypass(boolInt(prm.offset[c][i] < 0))
				}
			}
			w.bypassBits(int(prm.bandPos[c]), 5)
		} else if c != 2 {
			w.bypassBits(int(prm.eoClass[c]), 2)
		}
	}
}
