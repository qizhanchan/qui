package hevc

// Inter prediction (spec 8.5.3): PU syntax, merge / AMVP / TMVP motion
// derivation and motion-compensated sample prediction.

const (
	predL0 = 0
	predL1 = 1
	predBi = 2
)

// motion is the motion of one prediction block.
type motion struct {
	pred [2]bool
	ref  [2]int
	mv   [2][2]int
}

func (a *motion) same(b *motion) bool {
	for l := 0; l < 2; l++ {
		if a.pred[l] != b.pred[l] {
			return false
		}
		if a.pred[l] && (a.ref[l] != b.ref[l] || a.mv[l] != b.mv[l]) {
			return false
		}
	}
	return true
}

func packMV(x, y int) int32 { return int32(x)<<16 | int32(uint16(int16(y))) }

func unpackMV(v int32) [2]int { return [2]int{int(v >> 16), int(int16(v))} }

// motionAt returns the stored motion of the 4x4 block covering (x, y).
func (s *sliceDec) motionAt(x, y int) motion {
	pic := s.pic
	i := (y>>2)*pic.w4 + x>>2
	var m motion
	for l := 0; l < 2; l++ {
		if r := pic.refIdx[l][i]; r >= 0 {
			m.pred[l] = true
			m.ref[l] = int(r)
			m.mv[l] = unpackMV(pic.mv[l][i])
		}
	}
	return m
}

// storeMotion writes m into the w x h block at (x0, y0).
func (s *sliceDec) storeMotion(x0, y0, w, h int, m *motion) {
	pic := s.pic
	sp := s.sps
	xe, ye := min(x0+w, sp.Width), min(y0+h, sp.Height)
	n := (xe - x0 + 3) >> 2
	for l := 0; l < 2; l++ {
		idx, mv, poc, lt := int8(-1), int32(0), int32(0), false
		if m.pred[l] {
			e := &s.h.refList[l][m.ref[l]]
			idx, mv, poc, lt = int8(m.ref[l]), packMV(m.mv[l][0], m.mv[l][1]), int32(e.poc), e.lt
		}
		for y := y0; y < ye; y += 4 {
			i := (y>>2)*pic.w4 + x0>>2
			ri := pic.refIdx[l][i : i+n]
			for k := range ri {
				ri[k] = idx
			}
			mvs := pic.mv[l][i : i+n]
			for k := range mvs {
				mvs[k] = mv
			}
			if idx >= 0 {
				rp, rl := pic.refPOC[l][i:i+n], pic.refLT[l][i:i+n]
				for k := range rp {
					rp[k], rl[k] = poc, lt
				}
			}
		}
	}
	for y := y0; y < ye; y += 4 {
		i := (y>>2)*pic.w4 + x0>>2
		clear(pic.intra[i : i+n])
	}
}

// storeIntra marks a CU as intra for TMVP and deblocking.
func (s *sliceDec) storeIntra(x0, y0, n int) {
	pic := s.pic
	sp := s.sps
	for y := y0; y < min(y0+n, sp.Height); y += 4 {
		i := (y>>2)*pic.w4 + x0>>2
		for x := x0; x < min(x0+n, sp.Width); x += 4 {
			pic.intra[i] = true
			pic.refIdx[0][i], pic.refIdx[1][i] = -1, -1
			i++
		}
	}
}

// parsePUs parses and predicts the prediction units of an inter CU.
func (s *sliceDec) parsePUs(x0, y0, n int) error {
	type pu struct{ x, y, w, h int }
	var parts []pu
	q, h := n/4, n/2
	switch s.partMode {
	case part2Nx2N:
		parts = []pu{{x0, y0, n, n}}
	case part2NxN:
		parts = []pu{{x0, y0, n, h}, {x0, y0 + h, n, h}}
	case partNx2N:
		parts = []pu{{x0, y0, h, n}, {x0 + h, y0, h, n}}
	case part2NxnU:
		parts = []pu{{x0, y0, n, q}, {x0, y0 + q, n, n - q}}
	case part2NxnD:
		parts = []pu{{x0, y0, n, n - q}, {x0, y0 + n - q, n, q}}
	case partnLx2N:
		parts = []pu{{x0, y0, q, n}, {x0 + q, y0, n - q, n}}
	case partnRx2N:
		parts = []pu{{x0, y0, n - q, n}, {x0 + n - q, y0, q, n}}
	default:
		parts = []pu{{x0, y0, h, h}, {x0 + h, y0, h, h}, {x0, y0 + h, h, h}, {x0 + h, y0 + h, h, h}}
	}
	for i, p := range parts {
		if err := s.predictionUnit(p.x, p.y, p.w, p.h, i, false); err != nil {
			return err
		}
		s.markPUEdges(p.x, p.y, p.w, p.h)
	}
	return nil
}

// predictionUnit parses prediction_unit() (7.3.8.6), derives its motion
// and predicts its samples.
func (s *sliceDec) predictionUnit(x0, y0, w, h, partIdx int, skip bool) error {
	hd := s.h
	c := &s.cb
	var m motion
	merge := skip
	mergeIdx := 0
	if !skip {
		merge = c.decision(&s.ctx[ctxMergeFlag]) == 1
	}
	if partIdx == 0 {
		s.mergeFlag = merge
	}
	if merge {
		if hd.maxMergeCand > 1 {
			if c.decision(&s.ctx[ctxMergeIdx]) == 1 {
				mergeIdx = 1
				for mergeIdx < hd.maxMergeCand-1 && c.bypass() == 1 {
					mergeIdx++
				}
			}
		}
		m = s.mergeMotion(x0, y0, w, h, partIdx, mergeIdx)
	} else {
		idc := predL0
		if hd.typ == sliceB {
			if w+h != 12 {
				depth := int(s.blk(s.cuX, s.cuY).depth)
				if c.decision(&s.ctx[ctxInterPredIdc+depth]) == 1 {
					idc = predBi
				} else {
					idc = c.decision(&s.ctx[ctxInterPredIdc+4])
				}
			} else {
				idc = c.decision(&s.ctx[ctxInterPredIdc+4])
			}
		}
		var mvd [2][2]int
		var mvpFlag [2]int
		for l := 0; l < 2; l++ {
			if !(idc == predBi || idc == l) {
				continue
			}
			m.pred[l] = true
			if n := hd.numRefIdx[l]; n > 1 {
				r := 0
				for r < n-1 {
					var b int
					if r < 2 {
						b = c.decision(&s.ctx[ctxRefIdx+r])
					} else {
						b = c.bypass()
					}
					if b == 0 {
						break
					}
					r++
				}
				m.ref[l] = r
			}
			if !(l == 1 && hd.mvdL1Zero && idc == predBi) {
				mvd[l] = s.parseMvd()
			}
			mvpFlag[l] = c.decision(&s.ctx[ctxMvpFlag])
		}
		for l := 0; l < 2; l++ {
			if !m.pred[l] {
				continue
			}
			if m.ref[l] >= len(hd.refList[l]) {
				return errCorrupt
			}
			cand := s.amvp(x0, y0, w, h, partIdx, l, m.ref[l])
			p := cand[mvpFlag[l]]
			for k := 0; k < 2; k++ {
				m.mv[l][k] = int(int16(p[k] + mvd[l][k]))
			}
		}
	}
	for l := 0; l < 2; l++ {
		if m.pred[l] && m.ref[l] >= len(hd.refList[l]) {
			return errCorrupt
		}
	}
	if !m.pred[0] && !m.pred[1] {
		return errCorrupt
	}
	s.storeMotion(x0, y0, w, h, &m)
	s.motionCompensate(x0, y0, w, h, &m)
	return nil
}

func (s *sliceDec) parseMvd() [2]int {
	c := &s.cb
	g0 := [2]bool{c.decision(&s.ctx[ctxMvdGt0]) == 1, c.decision(&s.ctx[ctxMvdGt0]) == 1}
	var g1 [2]bool
	for k := 0; k < 2; k++ {
		if g0[k] {
			g1[k] = c.decision(&s.ctx[ctxMvdGt1]) == 1
		}
	}
	var v [2]int
	for k := 0; k < 2; k++ {
		if !g0[k] {
			continue
		}
		a := 1
		if g1[k] {
			// abs_mvd_minus2: EG1.
			e, kk := 0, 1
			for c.bypass() == 1 {
				e += 1 << uint(kk)
				kk++
				if kk > 31 {
					break
				}
			}
			e += c.bypassBits(kk)
			a = e + 2
		}
		if c.bypass() == 1 {
			a = -a
		}
		v[k] = a
	}
	return v
}

// availablePB implements 6.4.2 for a neighbouring prediction block.
func (s *sliceDec) availablePB(xPb, yPb, w, h, partIdx, xN, yN int) bool {
	n := 1 << s.cuLog2
	xCb, yCb := s.cuX, s.cuY
	var ok bool
	if xN >= xCb && yN >= yCb && xN < xCb+n && yN < yCb+n {
		ok = !(w<<1 == n && h<<1 == n && partIdx == 1 && yCb+h <= yN && xCb+w > xN)
	} else {
		ok = s.available(xPb, yPb, xN, yN)
	}
	return ok && s.blk(xN, yN).predMode == modeInter
}

// mergeMotion derives the merge candidate mergeIdx (8.5.3.2.2).
func (s *sliceDec) mergeMotion(xPb, yPb, w, h, partIdx, mergeIdx int) motion {
	hd := s.h
	pm := s.partMode
	nCb := 1 << s.cuLog2
	origW, origH := w, h
	if s.pps.Log2ParMrgLevel > 2 && nCb == 8 {
		xPb, yPb, w, h, partIdx = s.cuX, s.cuY, nCb, nCb, 0
		pm = part2Nx2N
	}
	lvl := uint(s.pps.Log2ParMrgLevel)
	parOK := func(xN, yN int) bool { return !(xPb>>lvl == xN>>lvl && yPb>>lvl == yN>>lvl) }
	var cands [5]motion
	nc := 0
	get := func(xN, yN int) (motion, bool) {
		if !parOK(xN, yN) || !s.availablePB(xPb, yPb, w, h, partIdx, xN, yN) {
			return motion{}, false
		}
		return s.motionAt(xN, yN), true
	}
	a1, okA1 := get(xPb-1, yPb+h-1)
	if partIdx == 1 && (pm == partNx2N || pm == partnLx2N || pm == partnRx2N) {
		okA1 = false
	}
	if okA1 {
		cands[nc] = a1
		nc++
	}
	b1, okB1 := get(xPb+w-1, yPb-1)
	if partIdx == 1 && (pm == part2NxN || pm == part2NxnU || pm == part2NxnD) {
		okB1 = false
	}
	// Pruning compares against availability, not against whether the
	// neighbour itself survived pruning.
	if okB1 && !(okA1 && a1.same(&b1)) {
		cands[nc] = b1
		nc++
	}
	b0, okB0 := get(xPb+w, yPb-1)
	if okB0 && !(okB1 && b1.same(&b0)) {
		cands[nc] = b0
		nc++
	}
	a0, okA0 := get(xPb-1, yPb+h)
	if okA0 && !(okA1 && a1.same(&a0)) {
		cands[nc] = a0
		nc++
	}
	if nc < 4 {
		b2, okB2 := get(xPb-1, yPb-1)
		if okB2 && !(okA1 && a1.same(&b2)) && !(okB1 && b1.same(&b2)) {
			cands[nc] = b2
			nc++
		}
	}
	max := hd.maxMergeCand
	if nc > mergeIdx {
		return s.finishMerge(cands[mergeIdx], origW, origH)
	}
	// Temporal candidate.
	if hd.temporalMVP && nc < max {
		var col motion
		if mv, ok := s.temporalMV(xPb, yPb, w, h, 0, 0); ok {
			col.pred[0], col.mv[0] = true, mv
		}
		if hd.typ == sliceB {
			if mv, ok := s.temporalMV(xPb, yPb, w, h, 0, 1); ok {
				col.pred[1], col.mv[1] = true, mv
			}
		}
		if col.pred[0] || col.pred[1] {
			cands[nc] = col
			nc++
		}
	}
	if nc > mergeIdx {
		return s.finishMerge(cands[mergeIdx], origW, origH)
	}
	all := make([]motion, nc, max)
	copy(all, cands[:nc])
	// Combined bi-predictive candidates.
	if hd.typ == sliceB && nc > 1 && nc < max {
		l0 := [12]int{0, 1, 0, 2, 1, 2, 0, 3, 1, 3, 2, 3}
		l1 := [12]int{1, 0, 2, 0, 2, 1, 3, 0, 3, 1, 3, 2}
		orig := nc
		for ci := 0; ci < orig*(orig-1) && len(all) < max; ci++ {
			a, b := all[l0[ci]], all[l1[ci]]
			if a.pred[0] && b.pred[1] &&
				(hd.refList[0][a.ref[0]].poc != hd.refList[1][b.ref[1]].poc || a.mv[0] != b.mv[1]) {
				var m motion
				m.pred = [2]bool{true, true}
				m.ref = [2]int{a.ref[0], b.ref[1]}
				m.mv = [2][2]int{a.mv[0], b.mv[1]}
				all = append(all, m)
			}
		}
	}
	// Zero candidates.
	numRef := hd.numRefIdx[0]
	if hd.typ == sliceB {
		numRef = min(hd.numRefIdx[0], hd.numRefIdx[1])
	}
	for z := 0; len(all) < max; z++ {
		r := 0
		if z < numRef {
			r = z
		}
		var m motion
		m.pred[0], m.ref[0] = true, r
		if hd.typ == sliceB {
			m.pred[1], m.ref[1] = true, r
		}
		all = append(all, m)
	}
	return s.finishMerge(all[mergeIdx], origW, origH)
}

// finishMerge restricts 8x4 / 4x8 bi-prediction to L0 (8.5.3.2.2).
func (s *sliceDec) finishMerge(m motion, w, h int) motion {
	if m.pred[0] && m.pred[1] && w+h == 12 {
		m.pred[1] = false
		m.ref[1] = 0
		m.mv[1] = [2]int{}
	}
	return m
}

func scaleMV(mv [2]int, td, tb int) [2]int {
	td = clip3(-128, 127, td)
	tb = clip3(-128, 127, tb)
	if td == 0 {
		return mv
	}
	tx := (16384 + iabs(td)/2) / td
	f := clip3(-4096, 4095, (tb*tx+32)>>6)
	var out [2]int
	for k := 0; k < 2; k++ {
		p := f * mv[k]
		v := (iabs(p) + 127) >> 8
		if p < 0 {
			v = -v
		}
		out[k] = clip3(-32768, 32767, v)
	}
	return out
}

// noBackwardPred reports whether no reference picture follows the current
// picture in output order.
func (s *sliceDec) noBackwardPred() bool {
	cur := s.pic.poc
	for l := 0; l < 2; l++ {
		for _, e := range s.h.refList[l] {
			if e.poc > cur {
				return false
			}
		}
	}
	return true
}

// temporalMV derives the temporal luma motion vector predictor for list X
// and reference refIdx (8.5.3.2.8).
func (s *sliceDec) temporalMV(xPb, yPb, w, h, refIdx, X int) ([2]int, bool) {
	hd := s.h
	if !hd.temporalMVP || refIdx >= len(hd.refList[X]) {
		return [2]int{}, false
	}
	cl := 1
	if hd.colFromL0 || hd.typ == sliceP {
		cl = 0
	}
	if hd.colRefIdx >= len(hd.refList[cl]) {
		return [2]int{}, false
	}
	col := hd.refList[cl][hd.colRefIdx].pic
	sp := s.sps
	xBr, yBr := xPb+w, yPb+h
	if yPb>>sp.Log2Ctb == yBr>>sp.Log2Ctb && yBr < sp.Height && xBr < sp.Width {
		if mv, ok := s.colMV(col, (xBr>>4)<<4, (yBr>>4)<<4, refIdx, X); ok {
			return mv, true
		}
	}
	xC, yC := xPb+w>>1, yPb+h>>1
	return s.colMV(col, (xC>>4)<<4, (yC>>4)<<4, refIdx, X)
}

func (s *sliceDec) colMV(col *picture, x, y, refIdx, X int) ([2]int, bool) {
	hd := s.h
	i := (y>>2)*col.w4 + x>>2
	if col.intra[i] {
		return [2]int{}, false
	}
	var l int
	switch {
	case col.refIdx[0][i] < 0:
		l = 1
	case col.refIdx[1][i] < 0:
		l = 0
	default:
		if s.noBackwardPred() {
			l = X
		} else if hd.colFromL0 {
			l = 1
		} else {
			l = 0
		}
	}
	mvCol := unpackMV(col.mv[l][i])
	e := hd.refList[X][refIdx]
	if col.refLT[l][i] != e.lt {
		return [2]int{}, false
	}
	colDiff := col.poc - int(col.refPOC[l][i])
	curDiff := s.pic.poc - e.poc
	if e.lt || colDiff == curDiff {
		return mvCol, true
	}
	return scaleMV(mvCol, colDiff, curDiff), true
}

// amvp derives the two motion vector predictor candidates for list X and
// reference refIdx (8.5.3.2.6).
func (s *sliceDec) amvp(xPb, yPb, w, h, partIdx, X, refIdx int) [2][2]int {
	hd := s.h
	Y := 1 - X
	target := hd.refList[X][refIdx]
	cur := s.pic.poc
	refOf := func(m *motion, l int) refEntry { return hd.refList[l][m.ref[l]] }
	// Same picture (no scaling) check.
	sameMV := func(m *motion) ([2]int, bool) {
		if m.pred[X] && refOf(m, X).poc == target.poc {
			return m.mv[X], true
		}
		if m.pred[Y] && refOf(m, Y).poc == target.poc {
			return m.mv[Y], true
		}
		return [2]int{}, false
	}
	// Long-term-compatible candidate, scaled when both are short-term.
	scaledMV := func(m *motion) ([2]int, bool) {
		for _, l := range [2]int{X, Y} {
			if m.pred[l] && refOf(m, l).lt == target.lt {
				mv := m.mv[l]
				r := refOf(m, l)
				if !r.lt && !target.lt {
					mv = scaleMV(mv, cur-r.poc, cur-target.poc)
				}
				return mv, true
			}
		}
		return [2]int{}, false
	}
	nb := func(xN, yN int) (motion, bool) {
		if !s.availablePB(xPb, yPb, w, h, partIdx, xN, yN) {
			return motion{}, false
		}
		return s.motionAt(xN, yN), true
	}
	a0, okA0 := nb(xPb-1, yPb+h)
	a1, okA1 := nb(xPb-1, yPb+h-1)
	isScaled := okA0 || okA1
	var mvA, mvB [2]int
	availA, availB := false, false
	for _, c := range []struct {
		m  motion
		ok bool
	}{{a0, okA0}, {a1, okA1}} {
		if c.ok && !availA {
			mvA, availA = sameMV(&c.m)
		}
	}
	if !availA {
		for _, c := range []struct {
			m  motion
			ok bool
		}{{a0, okA0}, {a1, okA1}} {
			if c.ok && !availA {
				mvA, availA = scaledMV(&c.m)
			}
		}
	}
	b0, okB0 := nb(xPb+w, yPb-1)
	b1, okB1 := nb(xPb+w-1, yPb-1)
	b2, okB2 := nb(xPb-1, yPb-1)
	bs := []struct {
		m  motion
		ok bool
	}{{b0, okB0}, {b1, okB1}, {b2, okB2}}
	for _, c := range bs {
		if c.ok && !availB {
			mvB, availB = sameMV(&c.m)
		}
	}
	if !isScaled && availB {
		mvA, availA = mvB, true
	}
	if !isScaled {
		availB = false
		for _, c := range bs {
			if c.ok && !availB {
				mvB, availB = scaledMV(&c.m)
			}
		}
	}
	var list [2][2]int
	n := 0
	if availA {
		list[n] = mvA
		n++
	}
	if availB && !(availA && mvA == mvB) {
		list[n] = mvB
		n++
	}
	if n < 2 {
		if mv, ok := s.temporalMV(xPb, yPb, w, h, refIdx, X); ok {
			list[n] = mv
			n++
		}
	}
	return list
}
