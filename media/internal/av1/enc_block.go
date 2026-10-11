package av1

import "math"

// Block coding: partition walk, intra block decisions, and the syntax
// writers that mirror the decoder's parsers (tile.go, block.go).

// blkChoice is the mode decision of one block: intra modes, or (inter)
// the reference, the inter mode, the DRL index and the motion vector.
type blkChoice struct {
	yMode, angleY   int
	uvMode, angleUV int
	cflU, cflV      int
	txDepth         int
	skip            bool

	inter  bool
	ref    int
	drlIdx int
	mv     mv
}

// codePartition codes the fixed partition tree at (r, c): 16x16 blocks
// (split above, forced splits at the frame edges).
func (e *Encoder) codePartition(r, c, bSize int) {
	t := e.t
	f := t.f
	if r >= f.miRows || c >= f.miCols {
		return
	}
	half := num4x4W[bSize] >> 1
	hasRows := r+half < f.miRows
	hasCols := c+half < f.miCols
	part := partNone
	if bSize > block16x16 || !hasRows || !hasCols {
		part = partSplit
	}
	if bSize == block8x8 && part == partSplit {
		// 8x8 at an edge: 4x4 blocks need 4x4 chroma handling; keep NONE
		// (an 8x8 block may extend past the frame).
		part = partNone
	}
	e.writePartition(r, c, bSize, part)
	sub := partitionSubsize[part][bSize]
	switch part {
	case partNone:
		ch := blkChoice{yMode: dcPred, uvMode: dcPred}
		e.codeBlock(r, c, sub, &ch)
	case partSplit:
		for _, o := range [4][2]int{{0, 0}, {0, half}, {half, 0}, {half, half}} {
			e.codePartition(r+o[0], c+o[1], sub)
		}
	}
}

// writePartition mirrors partition()'s symbol.
func (e *Encoder) writePartition(r, c, bSize, part int) {
	t := e.t
	f := t.f
	k := &e.sink
	if bSize < block8x8 {
		return
	}
	half := num4x4W[bSize] >> 1
	hasRows := r+half < f.miRows
	hasCols := c+half < f.miCols
	bsl := miWLog2[bSize]
	ctx := 0
	if t.inside(r-1, c) && miWLog2[t.mi(r-1, c).size] < bsl {
		ctx++
	}
	if t.inside(r, c-1) && miHLog2[t.mi(r, c-1).size] < bsl {
		ctx += 2
	}
	cdf := t.cdf.partition[(bsl-1)*4+ctx][:]
	n := 10
	switch bsl {
	case 1:
		n = 4
	case 5:
		n = 8
	}
	switch {
	case hasRows && hasCols:
		k.sym(cdf, part, n)
	case hasCols:
		p := cdfProb(cdf, partVert) + cdfProb(cdf, partSplit) + cdfProb(cdf, partHorzA) + cdfProb(cdf, partVertA) + cdfProb(cdf, partVertB)
		if bSize != block128x128 {
			p += cdfProb(cdf, partVert4)
		}
		k.boolProb(p, part == partSplit)
	case hasRows:
		p := cdfProb(cdf, partHorz) + cdfProb(cdf, partSplit) + cdfProb(cdf, partHorzA) + cdfProb(cdf, partHorzB) + cdfProb(cdf, partVertA)
		if bSize != block128x128 {
			p += cdfProb(cdf, partHorz4)
		}
		k.boolProb(p, part == partSplit)
	}
}

// codeIntraBlock codes an intra block with the given choice: with the
// sink recording, the mode info is written first and the coefficients as
// the decoder's residual() asks for them. It returns the distortion.
func (e *Encoder) codeIntraBlock(r, c, size int, ch *blkChoice) int64 {
	t := e.t
	b := &t.b
	t.initBlock(r, c, size)
	b.refFrame = [2]int{intraFrame, -1}
	b.segID = 0
	b.lossless = t.h.lossless[0]
	b.yMode, b.angleDeltaY = ch.yMode, ch.angleY
	b.uvMode, b.angleDeltaUV = ch.uvMode, ch.angleUV
	b.cflAlphaU, b.cflAlphaV = ch.cflU, ch.cflV
	b.skip = ch.skip
	b.txSize = maxTxRect[size]
	if b.lossless {
		b.txSize = tx4x4
	}
	for i := 0; i < ch.txDepth; i++ {
		b.txSize = splitTx[b.txSize]
	}
	if e.sink.record {
		e.writeIntraModeInfo(ch.txDepth)
	}
	t.setIntraTxSize()
	if b.skip {
		t.resetBlockContext()
	}
	t.storeModeInfo()
	_ = t.residual()
	return e.blockSSD(r, c, size)
}

// setIntraTxSize records the block's transform size as readBlockTxSize
// does for intra blocks.
func (t *tile) setIntraTxSize() {
	f := t.f
	b := &t.b
	for row := b.miRow; row < min(b.miRow+b.bh4, f.miRows); row++ {
		for col := b.miCol; col < min(b.miCol+b.bw4, f.miCols); col++ {
			f.interTxSize[row*f.miCols+col] = uint8(b.txSize)
		}
	}
}

// writeIntraModeInfo mirrors intraFrameModeInfo (or, in inter frames,
// interFrameModeInfo for an intra block) and the intra transform size
// (readTxSize).
func (e *Encoder) writeIntraModeInfo(txDepth int) {
	t := e.t
	b := &t.b
	k := &e.sink
	if !t.h.intra {
		t.interNeighbors()
		e.writeSkip()
		e.writeCdef()
		e.writeIsInter()
		k.sym(t.cdf.yMode[sizeGroup[b.size]][:], b.yMode, intraModes)
		e.writeIntraModesRest()
		e.writeTxDepth(txDepth)
		return
	}
	// skip
	ctx := 0
	if b.availU && t.mi(b.miRow-1, b.miCol).skip {
		ctx++
	}
	if b.availL && t.mi(b.miRow, b.miCol-1).skip {
		ctx++
	}
	k.flag(t.cdf.skip[ctx][:], b.skip)
	e.writeCdef()
	above, left := dcPred, dcPred
	if b.availU {
		above = int(t.mi(b.miRow-1, b.miCol).yMode)
	}
	if b.availL {
		left = int(t.mi(b.miRow, b.miCol-1).yMode)
	}
	k.sym(t.cdf.kfYMode[intraModeContext[above]][intraModeContext[left]][:], b.yMode, intraModes)
	e.writeIntraModesRest()
	e.writeTxDepth(txDepth)
}

// writeCdef mirrors readCdef: the 64x64's index goes with its first
// non-skip block (a placeholder until pass B picks it).
func (e *Encoder) writeCdef() {
	t := e.t
	b := &t.b
	h := t.h
	if b.skip || h.codedLossless || !t.seq.EnableCDEF || h.allowIntraBC {
		return
	}
	f := t.f
	i := f.cdefIndex(b.miRow&^15, b.miCol&^15)
	if !e.cdefSeen[i] {
		if e.sink.record {
			e.cdefSeen[i] = true
		}
		e.sink.cdefPlaceholder(i, h.cdefBits)
	}
}

// writeIntraModesRest mirrors intraModesRest (no palette, no filter
// intra: the sequence disables them).
func (e *Encoder) writeIntraModesRest() {
	t := e.t
	b := &t.b
	k := &e.sink
	if b.size >= block8x8 && isDirectional(b.yMode) {
		k.sym(t.cdf.angleDelta[b.yMode-vPred][:], b.angleDeltaY+3, 7)
	}
	if b.hasChroma {
		cflAllowed := false
		if b.lossless && chromaSize[b.size] == block4x4 {
			cflAllowed = true
		} else if !b.lossless && max(blockW(b.size), blockH(b.size)) <= 32 {
			cflAllowed = true
		}
		if cflAllowed {
			k.sym(t.cdf.uvMode[1][b.yMode][:], b.uvMode, 14)
		} else {
			k.sym(t.cdf.uvMode[0][b.yMode][:], b.uvMode, 13)
		}
		if b.uvMode == uvCflPred {
			e.writeCflAlphas()
		}
		if b.size >= block8x8 && isDirectional(b.uvMode) {
			k.sym(t.cdf.angleDelta[b.uvMode-vPred][:], b.angleDeltaUV+3, 7)
		}
	}
}

// writeCflAlphas mirrors readCflAlphas.
func (e *Encoder) writeCflAlphas() {
	t := e.t
	b := &t.b
	k := &e.sink
	sign := func(a int) int {
		switch {
		case a == 0:
			return 0
		case a < 0:
			return 1
		}
		return 2
	}
	signU, signV := sign(b.cflAlphaU), sign(b.cflAlphaV)
	k.sym(t.cdf.cflSign[:], signU*3+signV-1, 8)
	if signU != 0 {
		k.sym(t.cdf.cflAlpha[(signU-1)*3+signV][:], abs(b.cflAlphaU)-1, 16)
	}
	if signV != 0 {
		k.sym(t.cdf.cflAlpha[(signV-1)*3+signU][:], abs(b.cflAlphaV)-1, 16)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// writeTxDepth mirrors readTxSize for intra blocks.
func (e *Encoder) writeTxDepth(depth int) {
	t := e.t
	b := &t.b
	if b.lossless || b.size == block4x4 || t.h.txMode != txModeSelect {
		return
	}
	maxRect := maxTxRect[b.size]
	depthMax := maxTxDepth[b.size]
	maxW, maxH := txW[maxRect], txH[maxRect]
	aboveW, leftH := 0, 0
	if b.availU {
		m := t.mi(b.miRow-1, b.miCol)
		if m.isInter {
			aboveW = blockW(int(m.size))
		} else {
			aboveW = t.aboveTxWidth(b.miRow, b.miCol)
		}
	}
	if b.availL {
		m := t.mi(b.miRow, b.miCol-1)
		if m.isInter {
			leftH = blockH(int(m.size))
		} else {
			leftH = t.leftTxHeight(b.miRow, b.miCol)
		}
	}
	ctx := 0
	if aboveW >= maxW {
		ctx++
	}
	if leftH >= maxH {
		ctx++
	}
	n := min(depthMax, 2) + 1
	e.sink.sym(t.cdf.txSize[depthMax-1][ctx][:], depth, n)
}

// blockSSD is the squared error of the block's visible samples.
func (e *Encoder) blockSSD(r, c, size int) int64 {
	t := e.t
	f := t.f
	var ssd int64
	planes := 1
	if t.b.hasChroma {
		planes = 3
	}
	for p := 0; p < planes; p++ {
		ss := min(p, 1)
		pb := &f.cur.planes[p]
		x0, y0 := (c*4)>>ss, (r*4)>>ss
		w := min(blockW(size)>>ss, pb.w-x0)
		h := min(blockH(size)>>ss, pb.h-y0)
		if p > 0 {
			w = max(w, min(4, pb.w-x0))
			h = max(h, min(4, pb.h-y0))
		}
		src, ss2 := e.src[p], e.srcStride[p]
		for y := 0; y < h; y++ {
			a := src[(y0+y)*ss2+x0 : (y0+y)*ss2+x0+w]
			bb := pb.pix8[(y0+y)*pb.stride+x0 : (y0+y)*pb.stride+x0+w]
			for i, v := range a {
				d := int64(v) - int64(bb[i])
				ssd += d * d
			}
		}
	}
	return ssd
}

// txBlock is the encoder's coefficient source (tile.enc): the residual of
// the prediction now in the frame, transformed and quantised; the symbols
// go to the sink.
func (e *Encoder) txBlock(t *tile, plane, startX, startY, tx int) int {
	b := &t.b
	w, h := txW[tx], txH[tx]
	pb := &t.f.cur.planes[plane]
	src, ss := e.src[plane], e.srcStride[plane]
	res := e.tq.res[:w*h]
	for r := 0; r < h; r++ {
		sr := src[(startY+r)*ss+startX:]
		pr := pb.pix8[(startY+r)*pb.stride+startX:]
		for c := 0; c < w; c++ {
			res[r*w+c] = int32(sr[c]) - int32(pr[c])
		}
	}
	q := t.qindex()
	hh := t.h
	var dcq, acq int32
	switch plane {
	case 0:
		dcq, acq = dcQ(8, q+hh.deltaQYDc), acQ(8, q)
	case 1:
		dcq, acq = dcQ(8, q+hh.deltaQUDc), acQ(8, q+hh.deltaQUAc)
	default:
		dcq, acq = dcQ(8, q+hh.deltaQVDc), acQ(8, q+hh.deltaQVAc)
	}
	tq := &txQuant{tx: tx, dcq: dcq, acq: acq}
	switch n := w * h; {
	case n > 1024:
		tq.shift = 2
	case n > 256:
		tq.shift = 1
	}
	a := adjustedTx[tx]
	tw, th := txW[a], txH[a]
	levels := e.tq.levels[:tw*th]
	var typ, eob int
	if plane > 0 {
		typ = t.planeTxType(plane, tx, startX>>2, startY>>2)
		eob = e.quantTx(tq, res, typ, levels)
	} else {
		typ, eob = e.searchTxType(t, tq, startX, startY, res, levels)
	}
	_ = b
	if eob > 0 {
		e.nonZero = true
	}
	t.writeCoeffs(&e.sink, plane, startX, startY, tx, typ, levels, eob)
	t.planeType = typ
	return eob
}

// txQuant are the quantiser parameters of one transform block.
type txQuant struct {
	tx       int
	dcq, acq int32
	shift    uint
}

// quantTx transforms and quantises res with type typ into levels and
// returns the end of block.
func (e *Encoder) quantTx(tq *txQuant, res []int32, typ int, levels []int32) int {
	a := adjustedTx[tq.tx]
	coef := e.tq.coef[:txW[a]*txH[a]]
	fwdTx(res, tq.tx, typ, e.tq.tmp[:], coef)
	return quantize(coef, levels, scanFor(tq.tx, typ), tq.dcq, tq.acq, tq.shift, 0.5, 0.33)
}

// intraTxTypes are the luma transform types tried per intra set.
var intraTxTypes = [3][]int{
	1: {dctDct, adstDct, dctAdst, adstAdst, idtx, vDct, hDct},
	2: {dctDct, adstDct, dctAdst, adstAdst, idtx},
}

// searchTxType picks the luma transform type of a transform block by RD
// (exact coefficient bits from the live CDFs, distortion through the
// decoder's inverse transform) and leaves its levels in levels.
func (e *Encoder) searchTxType(t *tile, tq *txQuant, startX, startY int, res []int32, levels []int32) (int, int) {
	b := &t.b
	tx := tq.tx
	set := t.txSet(tx)
	if set == 0 || t.h.baseQIdx == 0 || b.lossless || e.txTypeSearch == 0 || b.isInter {
		return dctDct, e.quantTx(tq, res, dctDct, levels)
	}
	types := intraTxTypes[set]
	if e.txTypeSearch == 1 {
		types = types[:min(len(types), 4)]
	}
	// Contexts the trial writes touch.
	x4, y4 := startX>>2, startY>>2
	w4, h4 := txW[tx]>>2, txH[tx]>>2
	var aL, aD, lL, lD [16]uint8
	copy(aL[:w4], t.aboveLevel[0][x4:])
	copy(aD[:w4], t.aboveDC[0][x4:])
	for i := 0; i < h4; i++ {
		lL[i], lD[i] = t.leftLevel[0][(y4+i)&31], t.leftDC[0][(y4+i)&31]
	}
	k := &e.sink
	rec := k.record
	k.record = false
	best, bestEOB := dctDct, 0
	bestCost := math.Inf(1)
	bestLevels := e.tq.best[:len(levels)]
	for _, typ := range types {
		eob := e.quantTx(tq, res, typ, levels)
		c0 := k.cost
		t.writeCoeffs(k, 0, startX, startY, tx, typ, levels, eob)
		bits := k.cost - c0
		k.cost = c0
		copy(t.aboveLevel[0][x4:], aL[:w4])
		copy(t.aboveDC[0][x4:], aD[:w4])
		for i := 0; i < h4; i++ {
			t.leftLevel[0][(y4+i)&31], t.leftDC[0][(y4+i)&31] = lL[i], lD[i]
		}
		cost := float64(e.residualSSD(t, tq, res, typ, levels, eob)) + e.lambda*float64(bits)/256
		if cost < bestCost {
			best, bestEOB, bestCost = typ, eob, cost
			copy(bestLevels, levels)
		}
	}
	k.record = rec
	copy(levels, bestLevels)
	return best, bestEOB
}

// residualSSD is the squared error between res and the residual the
// decoder reconstructs from levels.
func (e *Encoder) residualSSD(t *tile, tq *txQuant, res []int32, typ int, levels []int32, eob int) int64 {
	var ssd int64
	if eob == 0 {
		for _, v := range res {
			ssd += int64(v) * int64(v)
		}
		return ssd
	}
	tx := tq.tx
	rs := t.recon
	for i, l := range levels {
		if l == 0 {
			rs.dq[i] = 0
			continue
		}
		dq := tq.acq
		if i == 0 {
			dq = tq.dcq
		}
		v := int32(int64(iabs32(l))*int64(dq)&0xFFFFFF) >> tq.shift
		if l < 0 {
			v = -v
		}
		rs.dq[i] = v
	}
	t.inverseTransform(tx, typ, false)
	for i, v := range res {
		d := int64(v - rs.res[i])
		ssd += d * d
	}
	return ssd
}
