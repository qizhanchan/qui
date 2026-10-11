package av1

// Motion vector prediction (spec 7.10.2) and warp samples (7.10.4).

func round2Signed(x int64, n uint) int64 {
	if n == 0 {
		return x
	}
	if x >= 0 {
		return (x + 1<<(n-1)) >> n
	}
	return -((-x + 1<<(n-1)) >> n)
}

// lowerMvPrecision is lower_mv_precision.
func (t *tile) lowerMvPrecision(m *mv) {
	h := t.h
	if h.allowHighPrecisionMV {
		return
	}
	for _, c := range []*int16{&m.row, &m.col} {
		if h.forceIntegerMV {
			a := int(*c)
			if a < 0 {
				a = -a
			}
			ai := (a + 3) >> 3
			if *c > 0 {
				*c = int16(ai << 3)
			} else {
				*c = int16(-(ai << 3))
			}
		} else if *c&1 != 0 {
			if *c > 0 {
				*c--
			} else {
				*c++
			}
		}
	}
}

// globalMv is setup_global_mv.
func (t *tile) globalMv(refList int) mv {
	b := &t.b
	h := t.h
	ref := b.refFrame[refList]
	var m mv
	if ref != intraFrame && ref > 0 {
		typ := h.gmType[ref]
		gm := &h.gmParams[ref]
		switch {
		case typ == gmIdentity:
		case typ == gmTranslation:
			m = mv{int16(gm[0] >> (warpedModelPrecBits - 3)), int16(gm[1] >> (warpedModelPrecBits - 3))}
		default:
			x := int64(b.miCol*4 + blockW(b.size)/2 - 1)
			y := int64(b.miRow*4 + blockH(b.size)/2 - 1)
			xc := (int64(gm[2])-(1<<warpedModelPrecBits))*x + int64(gm[3])*y + int64(gm[0])
			yc := int64(gm[4])*x + (int64(gm[5])-(1<<warpedModelPrecBits))*y + int64(gm[1])
			if h.allowHighPrecisionMV {
				m = mv{int16(round2Signed(yc, warpedModelPrecBits-3)), int16(round2Signed(xc, warpedModelPrecBits-3))}
			} else {
				m = mv{int16(round2Signed(yc, warpedModelPrecBits-2) * 2), int16(round2Signed(xc, warpedModelPrecBits-2) * 2)}
			}
		}
	}
	t.lowerMvPrecision(&m)
	return m
}

// findMvStack is find_mv_stack (spec 7.10.2.1).
func (t *tile) findMvStack(compound bool) {
	b := &t.b
	p := &t.mvp
	bw4, bh4 := b.bw4, b.bh4
	p.numMvFound, p.newMvCount = 0, 0
	p.globalMvs[0] = t.globalMv(0)
	if compound {
		p.globalMvs[1] = t.globalMv(1)
	}
	p.foundMatch = false
	t.scanRow(-1, compound)
	foundAbove := p.foundMatch
	p.foundMatch = false
	t.scanCol(-1, compound)
	foundLeft := p.foundMatch
	p.foundMatch = false
	if max(bw4, bh4) <= 16 {
		t.scanPoint(-1, bw4, compound)
	}
	if p.foundMatch {
		foundAbove = true
	}
	p.closeMatches = btoi(foundAbove) + btoi(foundLeft)
	numNearest, numNew := p.numMvFound, p.newMvCount
	for i := 0; i < numNearest; i++ {
		p.weight[i] += refCatLevel
	}
	p.zeroMvCtx = 0
	if t.h.useRefFrameMVs {
		t.temporalScan(compound)
	}
	p.foundMatch = false
	t.scanPoint(-1, -1, compound)
	if p.foundMatch {
		foundAbove = true
	}
	p.foundMatch = false
	t.scanRow(-3, compound)
	if p.foundMatch {
		foundAbove = true
	}
	p.foundMatch = false
	t.scanCol(-3, compound)
	if p.foundMatch {
		foundLeft = true
	}
	p.foundMatch = false
	if bh4 > 1 {
		t.scanRow(-5, compound)
	}
	if p.foundMatch {
		foundAbove = true
	}
	p.foundMatch = false
	if bw4 > 1 {
		t.scanCol(-5, compound)
	}
	if p.foundMatch {
		foundLeft = true
	}
	p.totalMatches = btoi(foundAbove) + btoi(foundLeft)
	p.sort(0, numNearest)
	p.sort(numNearest, p.numMvFound)
	if p.numMvFound < 2 {
		t.extraSearch(compound)
	}
	t.contextAndClamping(compound, numNew)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (p *mvPred) sort(start, end int) {
	for end > start {
		newEnd := start
		for i := start + 1; i < end; i++ {
			if p.weight[i-1] < p.weight[i] {
				p.weight[i-1], p.weight[i] = p.weight[i], p.weight[i-1]
				p.refStack[i-1], p.refStack[i] = p.refStack[i], p.refStack[i-1]
				newEnd = i
			}
		}
		end = newEnd
	}
}

func (t *tile) scanRow(deltaRow int, compound bool) {
	b := &t.b
	f := t.f
	bw4 := b.bw4
	end4 := min(bw4, f.miCols-b.miCol, 16)
	deltaCol := 0
	useStep16 := bw4 >= 16
	if iabs(deltaRow) > 1 {
		deltaRow += b.miRow & 1
		deltaCol = 1 - (b.miCol & 1)
	}
	for i := 0; i < end4; {
		r, c := b.miRow+deltaRow, b.miCol+deltaCol+i
		if !t.inside(r, c) {
			break
		}
		l := min(bw4, num4x4W[t.mi(r, c).size])
		if iabs(deltaRow) > 1 {
			l = max(2, l)
		}
		if useStep16 {
			l = max(4, l)
		}
		t.addRefMvCandidate(r, c, compound, 2*l)
		i += l
	}
}

func (t *tile) scanCol(deltaCol int, compound bool) {
	b := &t.b
	f := t.f
	bh4 := b.bh4
	end4 := min(bh4, f.miRows-b.miRow, 16)
	deltaRow := 0
	useStep16 := bh4 >= 16
	if iabs(deltaCol) > 1 {
		deltaRow = 1 - (b.miRow & 1)
		deltaCol += b.miCol & 1
	}
	for i := 0; i < end4; {
		r, c := b.miRow+deltaRow+i, b.miCol+deltaCol
		if !t.inside(r, c) {
			break
		}
		l := min(bh4, num4x4H[t.mi(r, c).size])
		if iabs(deltaCol) > 1 {
			l = max(2, l)
		}
		if useStep16 {
			l = max(4, l)
		}
		t.addRefMvCandidate(r, c, compound, 2*l)
		i += l
	}
}

func (t *tile) scanPoint(deltaRow, deltaCol int, compound bool) {
	b := &t.b
	r, c := b.miRow+deltaRow, b.miCol+deltaCol
	if t.inside(r, c) && t.mi(r, c).written {
		t.addRefMvCandidate(r, c, compound, 4)
	}
}

func (t *tile) addRefMvCandidate(r, c int, compound bool, weight int) {
	m := t.mi(r, c)
	if !m.isInter {
		return
	}
	b := &t.b
	if !compound {
		for l := 0; l < 2; l++ {
			if int(m.refFrame[l]) == b.refFrame[0] {
				t.searchStack(m, l, weight)
			}
		}
	} else if int(m.refFrame[0]) == b.refFrame[0] && int(m.refFrame[1]) == b.refFrame[1] {
		t.compoundSearchStack(m, weight)
	}
}

func (t *tile) searchStack(m *miInfo, l, weight int) {
	b := &t.b
	p := &t.mvp
	mode := int(m.yMode)
	large := min(blockW(int(m.size)), blockH(int(m.size))) >= 8
	var cand mv
	if (mode == globalMV || mode == globalGlobalMV) && t.h.gmType[b.refFrame[0]] > gmTranslation && large {
		cand = p.globalMvs[0]
	} else {
		cand = m.mv[l]
	}
	t.lowerMvPrecision(&cand)
	if hasNewmv(mode) {
		p.newMvCount++
	}
	p.foundMatch = true
	i := 0
	for ; i < p.numMvFound; i++ {
		if p.refStack[i][0] == cand {
			break
		}
	}
	if i < p.numMvFound {
		p.weight[i] += weight
	} else if p.numMvFound < maxRefMvStack {
		p.refStack[p.numMvFound][0] = cand
		p.weight[p.numMvFound] = weight
		p.numMvFound++
	}
}

func (t *tile) compoundSearchStack(m *miInfo, weight int) {
	b := &t.b
	p := &t.mvp
	cand := m.mv
	mode := int(m.yMode)
	large := min(blockW(int(m.size)), blockH(int(m.size))) >= 8
	if mode == globalGlobalMV && large {
		for l := 0; l < 2; l++ {
			if t.h.gmType[b.refFrame[l]] > gmTranslation {
				cand[l] = p.globalMvs[l]
			}
		}
	}
	t.lowerMvPrecision(&cand[0])
	t.lowerMvPrecision(&cand[1])
	p.foundMatch = true
	i := 0
	for ; i < p.numMvFound; i++ {
		if p.refStack[i][0] == cand[0] && p.refStack[i][1] == cand[1] {
			break
		}
	}
	if i < p.numMvFound {
		p.weight[i] += weight
	} else if p.numMvFound < maxRefMvStack {
		p.refStack[p.numMvFound] = cand
		p.weight[p.numMvFound] = weight
		p.numMvFound++
	}
	if hasNewmv(mode) {
		p.newMvCount++
	}
}

func (t *tile) temporalScan(compound bool) {
	b := &t.b
	bw4, bh4 := b.bw4, b.bh4
	stepW, stepH := 2, 2
	if bw4 >= 16 {
		stepW = 4
	}
	if bh4 >= 16 {
		stepH = 4
	}
	for dr := 0; dr < min(bh4, 16); dr += stepH {
		for dc := 0; dc < min(bw4, 16); dc += stepW {
			t.addTplRefMv(dr, dc, compound)
		}
	}
	if bh4 >= 2 && bh4 < 16 && bw4 >= 2 && bw4 < 16 {
		for _, pos := range [3][2]int{{bh4, -2}, {bh4, bw4}, {bh4 - 2, bw4}} {
			r := (b.miRow & 15) + pos[0]
			c := (b.miCol & 15) + pos[1]
			if r >= 0 && r < 16 && c >= 0 && c < 16 {
				t.addTplRefMv(pos[0], pos[1], compound)
			}
		}
	}
}

func (t *tile) addTplRefMv(dr, dc int, compound bool) {
	b := &t.b
	p := &t.mvp
	f := t.f
	r := (b.miRow + dr) | 1
	c := (b.miCol + dc) | 1
	if !t.inside(r, c) {
		return
	}
	if dr == 0 && dc == 0 {
		p.zeroMvCtx = 1
	}
	mf := &f.motionField[(r>>1)*(f.miCols>>1)+c>>1]
	if mf.mv.row == invalidMv {
		return
	}
	n := 1
	if compound {
		n = 2
	}
	var cand [2]mv
	for l := 0; l < n; l++ {
		cand[l] = f.projectTpl(mf, b.refFrame[l])
		t.lowerMvPrecision(&cand[l])
	}
	if dr == 0 && dc == 0 {
		far := false
		for l := 0; l < n; l++ {
			if iabs(int(cand[l].row)-int(p.globalMvs[l].row)) >= 16 || iabs(int(cand[l].col)-int(p.globalMvs[l].col)) >= 16 {
				far = true
			}
		}
		p.zeroMvCtx = btoi(far)
	}
	i := 0
	for ; i < p.numMvFound; i++ {
		if p.refStack[i][0] == cand[0] && (!compound || p.refStack[i][1] == cand[1]) {
			break
		}
	}
	if i < p.numMvFound {
		p.weight[i] += 2
	} else if p.numMvFound < maxRefMvStack {
		p.refStack[p.numMvFound][0] = cand[0]
		if compound {
			p.refStack[p.numMvFound][1] = cand[1]
		}
		p.weight[p.numMvFound] = 2
		p.numMvFound++
	}
}

func (t *tile) extraSearch(compound bool) {
	b := &t.b
	p := &t.mvp
	f := t.f
	var refIDCount, refDiffCount [2]int
	var refIDMvs, refDiffMvs [2][2]mv
	w4 := min(16, b.bw4, f.miCols-b.miCol)
	h4 := min(16, b.bh4, f.miRows-b.miRow)
	num4x4 := min(w4, h4)
	h := t.h
	for pass := 0; pass < 2 && p.numMvFound < 2; pass++ {
		for idx := 0; idx < num4x4 && p.numMvFound < 2; {
			var r, c int
			if pass == 0 {
				r, c = b.miRow-1, b.miCol+idx
			} else {
				r, c = b.miRow+idx, b.miCol-1
			}
			if !t.inside(r, c) {
				break
			}
			m := t.mi(r, c)
			// add_extra_mv_candidate
			for cl := 0; cl < 2; cl++ {
				candRef := int(m.refFrame[cl])
				if candRef <= intraFrame {
					continue
				}
				if compound {
					for l := 0; l < 2; l++ {
						cand := m.mv[cl]
						if candRef == b.refFrame[l] && refIDCount[l] < 2 {
							refIDMvs[l][refIDCount[l]] = cand
							refIDCount[l]++
						} else if refDiffCount[l] < 2 {
							if h.refFrameSignBias[candRef] != h.refFrameSignBias[b.refFrame[l]] {
								cand = mv{-cand.row, -cand.col}
							}
							refDiffMvs[l][refDiffCount[l]] = cand
							refDiffCount[l]++
						}
					}
				} else {
					cand := m.mv[cl]
					if h.refFrameSignBias[candRef] != h.refFrameSignBias[b.refFrame[0]] {
						cand = mv{-cand.row, -cand.col}
					}
					i := 0
					for ; i < p.numMvFound; i++ {
						if p.refStack[i][0] == cand {
							break
						}
					}
					if i == p.numMvFound {
						p.refStack[i][0] = cand
						p.weight[i] = 2
						p.numMvFound++
					}
				}
			}
			if pass == 0 {
				idx += num4x4W[m.size]
			} else {
				idx += num4x4H[m.size]
			}
		}
	}
	if compound {
		var combined [2][2]mv
		for l := 0; l < 2; l++ {
			n := 0
			for i := 0; i < refIDCount[l]; i++ {
				combined[n][l] = refIDMvs[l][i]
				n++
			}
			for i := 0; i < refDiffCount[l] && n < 2; i++ {
				combined[n][l] = refDiffMvs[l][i]
				n++
			}
			for n < 2 {
				combined[n][l] = p.globalMvs[l]
				n++
			}
		}
		if p.numMvFound == 1 {
			if combined[0][0] == p.refStack[0][0] && combined[0][1] == p.refStack[0][1] {
				p.refStack[1] = combined[1]
			} else {
				p.refStack[1] = combined[0]
			}
			p.weight[1] = 2
			p.numMvFound++
		} else {
			for i := 0; i < 2; i++ {
				p.refStack[p.numMvFound] = combined[i]
				p.weight[p.numMvFound] = 2
				p.numMvFound++
			}
		}
	} else {
		for i := p.numMvFound; i < 2; i++ {
			p.refStack[i][0] = p.globalMvs[0]
		}
	}
}

func (t *tile) contextAndClamping(compound bool, numNew int) {
	b := &t.b
	p := &t.mvp
	f := t.f
	for i := 0; i < p.numMvFound; i++ {
		z := 0
		if i+1 < p.numMvFound {
			w0, w1 := p.weight[i], p.weight[i+1]
			if w0 >= refCatLevel {
				if w1 < refCatLevel {
					z = 1
				}
			} else {
				z = 2
			}
		}
		p.drlCtx[i] = z
	}
	n := 1
	if compound {
		n = 2
	}
	top := -(b.miRow * 4 * 8)
	bottom := (f.miRows - b.bh4 - b.miRow) * 4 * 8
	left := -(b.miCol * 4 * 8)
	right := (f.miCols - b.bw4 - b.miCol) * 4 * 8
	bh := 128 + b.bh4*4*8
	bw := 128 + b.bw4*4*8
	for l := 0; l < n; l++ {
		for i := 0; i < p.numMvFound; i++ {
			m := &p.refStack[i][l]
			m.row = int16(clip3(top-bh, bottom+bh, int(m.row)))
			m.col = int16(clip3(left-bw, right+bw, int(m.col)))
		}
	}
	switch p.closeMatches {
	case 0:
		p.newMvCtx = min(p.totalMatches, 1)
		p.refMvCtx = p.totalMatches
	case 1:
		p.newMvCtx = 3 - min(numNew, 1)
		p.refMvCtx = 2 + p.totalMatches
	default:
		p.newMvCtx = 5 - min(numNew, 1)
		p.refMvCtx = 5
	}
}

// findWarpSamples is find_warp_samples (spec 7.10.4).
func (t *tile) findWarpSamples() {
	b := &t.b
	p := &t.mvp
	f := t.f
	p.numSamples, p.numSamplesScanned = 0, 0
	w4, h4 := b.bw4, b.bh4
	doTopLeft, doTopRight := true, true
	if b.availU {
		srcW := num4x4W[t.mi(b.miRow-1, b.miCol).size]
		if w4 <= srcW {
			colOff := -(b.miCol & (srcW - 1))
			if colOff < 0 {
				doTopLeft = false
			}
			if colOff+srcW > w4 {
				doTopRight = false
			}
			t.addSample(-1, 0)
		} else {
			for i := 0; i < min(w4, f.miCols-b.miCol); {
				srcW = num4x4W[t.mi(b.miRow-1, b.miCol+i).size]
				t.addSample(-1, i)
				i += min(srcW, w4)
			}
		}
	}
	if b.availL {
		srcH := num4x4H[t.mi(b.miRow, b.miCol-1).size]
		if h4 <= srcH {
			rowOff := -(b.miRow & (srcH - 1))
			if rowOff < 0 {
				doTopLeft = false
			}
			t.addSample(0, -1)
		} else {
			for i := 0; i < min(h4, f.miRows-b.miRow); {
				srcH = num4x4H[t.mi(b.miRow+i, b.miCol-1).size]
				t.addSample(i, -1)
				i += min(srcH, h4)
			}
		}
	}
	if doTopLeft {
		t.addSample(-1, -1)
	}
	if doTopRight && max(w4, h4) <= 16 {
		t.addSample(-1, w4)
	}
	if p.numSamples == 0 && p.numSamplesScanned > 0 {
		p.numSamples = 1
	}
}

func (t *tile) addSample(dr, dc int) {
	b := &t.b
	p := &t.mvp
	if p.numSamplesScanned >= 8 {
		return
	}
	r, c := b.miRow+dr, b.miCol+dc
	if !t.inside(r, c) {
		return
	}
	m := t.mi(r, c)
	if !m.written || int(m.refFrame[0]) != b.refFrame[0] || int(m.refFrame[1]) != -1 {
		return
	}
	cw4, ch4 := num4x4W[m.size], num4x4H[m.size]
	candRow := r &^ (ch4 - 1)
	candCol := c &^ (cw4 - 1)
	midY := candRow*4 + ch4*4/2 - 1
	midX := candCol*4 + cw4*4/2 - 1
	threshold := clip3(16, 112, max(blockW(b.size), blockH(b.size)))
	dRow := iabs(int(m.mv[0].row) - int(b.mv[0].row))
	dCol := iabs(int(m.mv[0].col) - int(b.mv[0].col))
	valid := dRow+dCol <= threshold
	cand := [4]int{midY * 8, midX * 8, midY*8 + int(m.mv[0].row), midX*8 + int(m.mv[0].col)}
	p.numSamplesScanned++
	if !valid && p.numSamplesScanned > 1 {
		return
	}
	p.candList[p.numSamples] = cand
	if valid {
		p.numSamples++
	}
}
