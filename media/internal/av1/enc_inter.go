package av1

import (
	"math"
	"math/bits"
)

// Inter coding: block reconstruction through the decoder's MV stack and
// motion compensation, the syntax writers mirroring inter.go, and the
// motion search.

// codeBlock codes a block with its choice.
func (e *Encoder) codeBlock(r, c, size int, ch *blkChoice) int64 {
	if ch.inter {
		return e.codeInterBlock(r, c, size, ch)
	}
	return e.codeIntraBlock(r, c, size, ch)
}

// setupInter sets the block state of a single-reference inter block and
// derives its motion vector as assignMv does.
func (e *Encoder) setupInter(r, c, size int, ch *blkChoice) {
	t := e.t
	b := &t.b
	h := t.h
	t.initBlock(r, c, size)
	t.interNeighbors()
	b.isInter = true
	b.segID = 0
	b.lossless = h.lossless[0]
	b.skip = ch.skip
	b.skipMode = ch.skipMode
	if b.skipMode {
		b.skip = true
		ch.ref, ch.ref2 = h.skipModeFrame[0], h.skipModeFrame[1]
		ch.yMode = nearestNearestMV
	}
	b.refFrame = [2]int{ch.ref, -1}
	if ch.ref2 > 0 {
		b.refFrame[1] = ch.ref2
	}
	compound := ch.ref2 > 0
	b.yMode = ch.yMode
	b.interpFilter = [2]int{h.interpFilter, h.interpFilter}
	if h.interpFilter == filterSwitchable {
		b.interpFilter = [2]int{ch.interp, ch.interp}
	}
	b.motionMode = motionSimple
	b.compoundType = compoundAverage
	b.compGroupIdx, b.compoundIdx = 0, 1
	b.txSize = maxTxRect[size]
	if b.lossless {
		b.txSize = tx4x4
	}
	t.findMvStack(compound)
	p := &t.mvp
	p.refMvIdx = ch.drlIdx
	n := 1
	if compound {
		n = 2
	}
	for i := 0; i < n; i++ {
		mode := compMode(b.yMode, i)
		if mode == globalMV {
			p.predMv[i] = p.globalMvs[i]
		} else {
			pos := p.refMvIdx
			if mode == nearestMV || mode == newMV && p.numMvFound <= 1 {
				pos = 0
			}
			p.predMv[i] = p.refStack[pos][i]
		}
		b.mv[i] = p.predMv[i]
		if mode == newMV {
			b.mv[i] = [2]mv{ch.mv, ch.mv2}[i]
		}
	}
	if h.interpFilter == filterSwitchable && !t.needsInterpFilter() {
		b.interpFilter = [2]int{filterEightTap, filterEightTap}
	}
}

// codeInterBlock codes an inter block with the given choice (see
// codeIntraBlock) and returns the distortion.
func (e *Encoder) codeInterBlock(r, c, size int, ch *blkChoice) int64 {
	t := e.t
	b := &t.b
	f := t.f
	e.setupInter(r, c, size, ch)
	for row := b.miRow; row < min(b.miRow+b.bh4, f.miRows); row++ {
		for col := b.miCol; col < min(b.miCol+b.bw4, f.miCols); col++ {
			f.interTxSize[row*f.miCols+col] = uint8(b.txSize)
		}
	}
	if e.sink.record {
		e.writeInterModeInfo()
	}
	if b.skip {
		t.resetBlockContext()
	}
	t.storeModeInfo()
	if encHook != nil && e.sink.record {
		encHook(t)
	}
	_ = t.computePrediction()
	_ = t.residual()
	return e.blockSSD(r, c, size)
}

// writeSkip writes the skip flag.
func (e *Encoder) writeSkip() {
	t := e.t
	b := &t.b
	ctx := 0
	if b.availU && t.mi(b.miRow-1, b.miCol).skip {
		ctx++
	}
	if b.availL && t.mi(b.miRow, b.miCol-1).skip {
		ctx++
	}
	e.sink.flag(t.cdf.skip[ctx][:], b.skip)
}

// writeSkipMode mirrors readSkipMode.
func (e *Encoder) writeSkipMode() {
	t := e.t
	b := &t.b
	if !t.h.skipModePresent || blockW(b.size) < 8 || blockH(b.size) < 8 {
		return
	}
	ctx := 0
	if b.availU && t.mi(b.miRow-1, b.miCol).skipMode {
		ctx++
	}
	if b.availL && t.mi(b.miRow, b.miCol-1).skipMode {
		ctx++
	}
	e.sink.flag(t.cdf.skipMode[ctx][:], b.skipMode)
}

// writeIsInter mirrors readIsInter.
func (e *Encoder) writeIsInter() {
	t := e.t
	b := &t.b
	p := &t.mvp
	ctx := 0
	switch {
	case b.availU && b.availL:
		if p.leftIntra && p.aboveIntra {
			ctx = 3
		} else if p.leftIntra || p.aboveIntra {
			ctx = 1
		}
	case b.availU:
		if p.aboveIntra {
			ctx = 2
		}
	case b.availL:
		if p.leftIntra {
			ctx = 2
		}
	}
	e.sink.flag(t.cdf.isInter[ctx][:], b.isInter)
}

// writeInterModeInfo mirrors interFrameModeInfo for a single-reference
// inter block, then its transform size.
func (e *Encoder) writeInterModeInfo() {
	t := e.t
	b := &t.b
	p := &t.mvp
	c := &t.cdf
	k := &e.sink
	h := t.h
	e.writeSkipMode()
	if b.skipMode {
		return
	}
	e.writeSkip()
	e.writeCdef()
	e.writeIsInter()
	compound := b.refFrame[1] > intraFrame
	if h.referenceSelect && min(b.bw4, b.bh4) >= 2 {
		k.flag(c.compInter[t.compModeCtx()][:], compound)
	}
	ref := b.refFrame[0]
	if compound {
		// Bidirectional pairs only: LAST..GOLDEN with BWDREF..ALTREF.
		k.flag(c.compRefType[t.compRefTypeCtx()][:], true)
		l3g := ref == last3Frame || ref == goldenFrame
		k.flag(c.compRef[t.refCtx(refsLL2, refsL3G)][0][:], l3g)
		if l3g {
			k.flag(c.compRef[t.refCtx(refsL3, refsGold)][2][:], ref == goldenFrame)
		} else {
			k.flag(c.compRef[t.refCtx(refsLast, refsL2)][1][:], ref == last2Frame)
		}
		ref2 := b.refFrame[1]
		k.flag(c.compBwdRef[t.refCtx(refsBA2, refsAlt)][0][:], ref2 == altrefFrame)
		if ref2 != altrefFrame {
			k.flag(c.compBwdRef[t.refCtx(refsBwd1, refsAlt2)][1][:], ref2 == altref2Frame)
		}
	} else {
		bwd := ref >= bwdrefFrame
		k.flag(c.singleRef[t.refCtx(refsFwd, refsBwd)][0][:], bwd)
		if bwd {
			k.flag(c.singleRef[t.refCtx(refsBA2, refsAlt)][1][:], ref == altrefFrame)
			if ref != altrefFrame {
				k.flag(c.singleRef[t.refCtx(refsBwd1, refsAlt2)][5][:], ref == altref2Frame)
			}
		} else {
			l3g := ref == last3Frame || ref == goldenFrame
			k.flag(c.singleRef[t.refCtx(refsLL2, refsL3G)][2][:], l3g)
			if l3g {
				k.flag(c.singleRef[t.refCtx(refsL3, refsGold)][4][:], ref == goldenFrame)
			} else {
				k.flag(c.singleRef[t.refCtx(refsLast, refsL2)][3][:], ref == last2Frame)
			}
		}
	}
	mode := b.yMode
	if compound {
		ctx := compoundModeCtxMap[p.refMvCtx>>1][min(p.newMvCtx, 4)]
		k.sym(c.compoundMode[ctx][:], mode-nearestNearestMV, 8)
	} else {
		k.flag(c.newMV[p.newMvCtx][:], mode != newMV)
		if mode != newMV {
			k.flag(c.zeroMV[p.zeroMvCtx][:], mode != globalMV)
			if mode != globalMV {
				k.flag(c.refMV[p.refMvCtx][:], mode == nearMV)
			}
		}
	}
	switch mode {
	case newMV, newNewMV:
		for idx := 0; idx < 2; idx++ {
			if p.numMvFound > idx+1 {
				k.flag(c.drl[p.drlCtx[idx]][:], p.refMvIdx > idx)
				if p.refMvIdx == idx {
					break
				}
			}
		}
	case nearMV, nearNearMV, nearNewMV, newNearMV:
		for idx := 1; idx < 3; idx++ {
			if p.numMvFound > idx+1 {
				k.flag(c.drl[p.drlCtx[idx]][:], p.refMvIdx > idx)
				if p.refMvIdx == idx {
					break
				}
			}
		}
	}
	n := 1
	if compound {
		n = 2
	}
	for i := 0; i < n; i++ {
		if compMode(mode, i) == newMV {
			e.writeMv(mv{b.mv[i].row - p.predMv[i].row, b.mv[i].col - p.predMv[i].col})
		}
	}
	if t.h.interpFilter == filterSwitchable && t.needsInterpFilter() {
		k.sym(c.interpFilter[t.interpCtx(0)][:], b.interpFilter[0], 3)
	}
	e.writeInterTxSize()
}

// writeInterTxSize mirrors readBlockTxSize for inter blocks: no
// transform split.
func (e *Encoder) writeInterTxSize() {
	t := e.t
	b := &t.b
	f := t.f
	if t.h.txMode != txModeSelect || b.size == block4x4 || b.skip || b.lossless {
		return
	}
	maxTx := maxTxRect[b.size]
	w4, h4 := txW[maxTx]/4, txH[maxTx]/4
	for row := b.miRow; row < b.miRow+b.bh4; row += h4 {
		for col := b.miCol; col < b.miCol+b.bw4; col += w4 {
			if row >= f.miRows || col >= f.miCols || maxTx == tx4x4 {
				continue
			}
			e.sink.flag(t.cdf.txfmSplit[t.txfmSplitCtx(row, col, maxTx)][:], false)
		}
	}
}

// writeMv mirrors readMv.
func (e *Encoder) writeMv(d mv) {
	t := e.t
	mc := &t.cdf.mv[0]
	joint := 0
	if d.row != 0 {
		joint |= 2
	}
	if d.col != 0 {
		joint |= 1
	}
	e.sink.sym(mc.joints[:], joint, 4)
	if d.row != 0 {
		e.writeMvComponent(&mc.comp[0], int(d.row))
	}
	if d.col != 0 {
		e.writeMvComponent(&mc.comp[1], int(d.col))
	}
}

// writeMvComponent mirrors readMvComponent for a non-zero difference.
func (e *Encoder) writeMvComponent(c *mvCompCDF, v int) {
	h := e.t.h
	k := &e.sink
	k.flag(c.sign[:], v < 0)
	a := abs(v) - 1
	if a < 16 {
		k.sym(c.classes[:], 0, 11)
		c0 := a >> 3
		k.sym(c.class0[:], c0, 2)
		if !h.forceIntegerMV {
			k.sym(c.class0FP[c0][:], a>>1&3, 4)
		}
		if h.allowHighPrecisionMV {
			k.sym(c.class0HP[:], a&1, 2)
		}
		return
	}
	cls := bits.Len(uint(a>>3)) - 1
	off := a - 1<<uint(cls+3)
	k.sym(c.classes[:], cls, 11)
	d := off >> 3
	for i := 0; i < cls; i++ {
		k.sym(c.bits[i][:], d>>uint(i)&1, 2)
	}
	if !h.forceIntegerMV {
		k.sym(c.fp[:], off>>1&3, 4)
	}
	if h.allowHighPrecisionMV {
		k.sym(c.hp[:], off&1, 2)
	}
}

// mvBits is the estimated cost of coding the difference d (1/256 bits).
func (e *Encoder) mvBits(d mv) int64 {
	k := &e.sink
	rec, before := k.record, k.cost
	k.record = false
	e.writeMv(d)
	bits := k.cost - before
	k.cost, k.record = before, rec
	return bits
}

// trialInter codes the block with the sink estimating and returns its RD
// cost (see trialIntra).
func (e *Encoder) trialInter(r, c, size int, ch *blkChoice) float64 {
	k := &e.sink
	ch.skip = ch.skipMode
	before := k.cost
	e.nonZero = false
	dist := e.codeInterBlock(r, c, size, ch)
	coefBits := k.cost - before
	if !e.nonZero {
		ch.skip = true
		coefBits = 0
		e.t.b.skip = true
		e.t.storeModeInfo()
	}
	k.cost = before
	e.writeInterModeInfo()
	modeBits := k.cost - before
	k.cost = before
	return float64(dist) + e.lambda*float64(coefBits+modeBits)/256
}

// interCandidates lists the inter choices tried for a block: NEAREST,
// NEAR (each DRL index), GLOBAL and NEWMV (searched from each candidate
// predictor).
func (e *Encoder) interCandidates(r, c, size, ref int) []blkChoice {
	t := e.t
	ch := blkChoice{inter: true, ref: ref, yMode: nearestMV}
	e.setupInter(r, c, size, &ch)
	p := &t.mvp
	out := e.interBuf[:0]
	out = append(out, ch)
	for idx := 1; idx < min(max(p.numMvFound, 2), 4); idx++ {
		out = append(out, blkChoice{inter: true, ref: ref, yMode: nearMV, drlIdx: idx})
	}
	out = append(out, blkChoice{inter: true, ref: ref, yMode: globalMV})
	// NEWMV: search around the best predictor.
	best, bestDrl := e.motionSearch(r, c, size, ref)
	e.searched[ref] = best
	out = append(out, blkChoice{inter: true, ref: ref, yMode: newMV, drlIdx: bestDrl, mv: best})
	e.interBuf = out
	return out
}

// compoundCandidates lists the compound choices of a past reference with
// a future one (after both were searched as single references): the
// predictor pairs at each DRL index, GLOBAL_GLOBAL and NEW_NEW with the
// searched vectors.
func (e *Encoder) compoundCandidates(r, c, size, ref, ref2 int) []blkChoice {
	t := e.t
	if min(num4x4W[size], num4x4H[size]) < 2 {
		return nil
	}
	ch := blkChoice{inter: true, ref: ref, ref2: ref2, yMode: nearestNearestMV}
	e.setupInter(r, c, size, &ch)
	p := &t.mvp
	out := e.interBuf[:0]
	out = append(out, ch)
	for idx := 1; idx < min(max(p.numMvFound, 2), 3); idx++ {
		out = append(out, blkChoice{inter: true, ref: ref, ref2: ref2, yMode: nearNearMV, drlIdx: idx})
	}
	out = append(out, blkChoice{inter: true, ref: ref, ref2: ref2, yMode: globalGlobalMV})
	nn := blkChoice{inter: true, ref: ref, ref2: ref2, yMode: newNewMV, mv: e.searched[ref], mv2: e.searched[ref2]}
	if p.numMvFound > 1 {
		// The DRL index whose predictors are closest to the vectors.
		best := math.MaxInt
		for i := 0; i < min(p.numMvFound, 3); i++ {
			s := p.refStack[i]
			d := abs(int(s[0].row-nn.mv.row)) + abs(int(s[0].col-nn.mv.col)) + abs(int(s[1].row-nn.mv2.row)) + abs(int(s[1].col-nn.mv2.col))
			if d < best {
				best, nn.drlIdx = d, i
			}
		}
	}
	out = append(out, nn)
	e.interBuf = out
	return out
}

// motionSearch finds a motion vector for the block: an integer diamond
// search from every predictor and zero (SAD plus estimated MV bits),
// then half- and quarter-sample refinement by SATD on the decoder's
// prediction. It returns the vector and the DRL index of the predictor
// it is coded against.
func (e *Encoder) motionSearch(r, c, size, ref int) (mv, int) {
	t := e.t
	b := &t.b
	p := &t.mvp
	f := t.f
	ch := blkChoice{inter: true, ref: ref, yMode: newMV}
	e.setupInter(r, c, size, &ch)
	nPred := 1
	if p.numMvFound > 1 {
		nPred = min(p.numMvFound, 3)
	}
	var preds [3]mv
	for i := 0; i < nPred; i++ {
		preds[i] = p.refStack[i][0]
	}
	w, h := blockW(size), blockH(size)
	x0, y0 := c*4, r*4
	refPl := &f.refs[ref].pic.planes[0]
	src, ss := e.src[0], e.srcStride[0]
	vw := min(w, f.cur.planes[0].w-x0)
	vh := min(h, f.cur.planes[0].h-y0)
	// Cost of a vector against its best predictor.
	mvCost := func(m mv) (float64, int) {
		best, bi := math.Inf(1), 0
		for i := 0; i < nPred; i++ {
			d := mv{m.row - preds[i].row, m.col - preds[i].col}
			if c := e.sqrtLambda * float64(e.mvBits(d)) / 256; c < best {
				best, bi = c, i
			}
		}
		return best, bi
	}
	sadAt := func(m mv) float64 {
		dx, dy := int(m.col)>>3, int(m.row)>>3
		return float64(sadRef(src[y0*ss+x0:], ss, refPl, x0+dx, y0+dy, vw, vh))
	}
	const rng = 64 << 3
	var bestMv mv
	bestCost := math.Inf(1)
	tryInt := func(m mv) bool {
		m = mv{int16(int(m.row) &^ 7), int16(int(m.col) &^ 7)}
		if abs(int(m.row)) > rng || abs(int(m.col)) > rng {
			return false
		}
		mc, _ := mvCost(m)
		if cost := sadAt(m) + mc; cost < bestCost {
			bestMv, bestCost = m, cost
			return true
		}
		return false
	}
	starts := append([]mv{{}}, preds[:nPred]...)
	for _, s := range starts {
		tryInt(s)
	}
	// Diamond search from the best start.
	for step := 16; step >= 1; step /= 2 {
		for moved := true; moved; {
			moved = false
			cm := bestMv
			for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				if tryInt(mv{cm.row + int16(d[0]*step*8), cm.col + int16(d[1]*step*8)}) {
					moved = true
				}
			}
		}
	}
	// Subpel refinement on the decoder's prediction (luma).
	b.mv[0] = bestMv
	t.storeModeInfo()
	m := t.mi(r, c)
	pb := &f.cur.planes[0]
	predCost := func(cand mv) float64 {
		m.mv[0] = cand
		t.predictInter(0, x0, y0, w, h, r, c)
		mc, _ := mvCost(cand)
		return float64(satd(src[y0*ss+x0:], ss, pb.pix8[y0*pb.stride+x0:], pb.stride, w, h)) + mc
	}
	bestCost = predCost(bestMv)
	steps := []int{4, 2}
	if t.h.allowHighPrecisionMV {
		steps = append(steps, 1)
	}
	for _, st := range steps {
		cm := bestMv
		for _, d := range [8][2]int{{-1, -1}, {-1, 0}, {-1, 1}, {0, -1}, {0, 1}, {1, -1}, {1, 0}, {1, 1}} {
			cand := mv{cm.row + int16(d[0]*st), cm.col + int16(d[1]*st)}
			if cost := predCost(cand); cost < bestCost {
				bestMv, bestCost = cand, cost
			}
		}
	}
	_, drl := mvCost(bestMv)
	return bestMv, drl
}

// sadRef is the SAD between a source block and the reference block at
// (x, y) (coordinates clamped to the reference like the decoder's MC).
func sadRef(src []byte, ss int, ref *planeBuf, x, y, w, h int) int {
	sum := 0
	lastX, lastY := ref.w-1, ref.h-1
	if x >= 0 && y >= 0 && x+w <= ref.w && y+h <= ref.h {
		for r := 0; r < h; r++ {
			a := src[r*ss : r*ss+w]
			b := ref.pix8[(y+r)*ref.stride+x:]
			for i, v := range a {
				sum += abs(int(v) - int(b[i]))
			}
		}
		return sum
	}
	for r := 0; r < h; r++ {
		a := src[r*ss : r*ss+w]
		row := ref.pix8[clip3(0, lastY, y+r)*ref.stride:]
		for i, v := range a {
			sum += abs(int(v) - int(row[clip3(0, lastX, x+i)]))
		}
	}
	return sum
}
