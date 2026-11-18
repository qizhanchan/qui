package vp9

// Tile decoding: the partition tree and mode info (libvpx
// vp9_decodeframe.c decode_partition / decode_block and vp9_decodemv.c).

const (
	block4x4   = 0
	block4x8   = 1
	block8x4   = 2
	block8x8   = 3
	block64x64 = 12

	dcPred   = 0
	vPred    = 1
	hPred    = 2
	d45Pred  = 3
	d135Pred = 4
	d117Pred = 5
	d153Pred = 6
	d207Pred = 7
	d63Pred  = 8
	tmPred   = 9

	nearestMV = 10
	nearMV    = 11
	zeroMV    = 12
	newMV     = 13

	partNone  = 0
	partHorz  = 1
	partVert  = 2
	partSplit = 3
)

type blockInfo struct {
	bsize   uint8
	mode    uint8
	uvMode  uint8
	bmode   [4]uint8
	ref     [2]int8
	mv      [2]mv
	bmv     [4][2]mv
	filter  uint8
	tx      uint8
	skip    bool
	isInter bool
	seg     uint8
	segPred uint8
}

func (b *blockInfo) hasSecond() bool { return b.ref[1] > intraFrame }

// yModeAt is the intra mode of 4x4 sub-block i (or the block's mode).
func (b *blockInfo) yModeAt(i int) uint8 {
	if b.bsize < block8x8 {
		return b.bmode[i]
	}
	return b.mode
}

const slabChunk = 1024

type tileState struct {
	d      *Decoder
	bd     boolDecoder
	counts frameCounts

	miRowStart, miRowEnd, miColStart, miColEnd int

	leftNZ   [3][16]uint8
	leftPart [8]uint8

	slabs [][]blockInfo
	slabN int

	// current block
	miRow, miCol     int
	bi               *blockInfo
	above, left      *blockInfo
	toLeft, toRight  int // mb_to_*_edge, 1/8 pel
	toTop, toBottom  int
	bwl, bhl         int // log2 of the block size in 4x4 units
	bmodeWL, bmodeHL int

	coef     [32 * 32]int32
	itxBuf   [32 * 32]int
	itxIn    [32]int
	itxOut   [32]int
	tokCache [32 * 32]uint8
	mcBuf    [2][64 * 64]uint8
	mcTmp    [(64*2 + 16) * 64]uint8
	edgeBuf  [(64*2 + 16 + 8) * (64*2 + 16 + 8)]uint8
}

func (t *tileState) frameStart(d *Decoder) {
	t.d = d
	t.counts = frameCounts{}
	t.slabN = 0
}

// reset prepares the tile for a new tile row.
func (t *tileState) reset(d *Decoder) { t.d = d }

func (t *tileState) alloc() *blockInfo {
	c, i := t.slabN/slabChunk, t.slabN%slabChunk
	if c == len(t.slabs) {
		t.slabs = append(t.slabs, make([]blockInfo, slabChunk))
	}
	t.slabN++
	b := &t.slabs[c][i]
	*b = blockInfo{}
	return b
}

func (t *tileState) decode() error {
	d := t.d
	for row := t.miRowStart; row < t.miRowEnd; row += 8 {
		t.leftNZ = [3][16]uint8{}
		t.leftPart = [8]uint8{}
		for col := t.miColStart; col < t.miColEnd; col += 8 {
			t.decodePartition(row, col, block64x64, 4)
		}
	}
	_ = d
	return nil
}

func (t *tileState) readPartition(miRow, miCol int, hasRows, hasCols bool, bsl int) int {
	d := t.d
	above := (int(d.abovePart[miCol]) >> bsl) & 1
	left := (int(t.leftPart[miRow&7]) >> bsl) & 1
	ctx := left*2 + above + bsl*4
	var probs []uint8
	if d.hdr.intraOnly() {
		probs = kfPartitionProbs[ctx][:]
	} else {
		probs = d.fc.partition[ctx][:]
	}
	var p int
	switch {
	case hasRows && hasCols:
		p = t.bd.tree(partitionTree[:], probs)
	case !hasRows && hasCols:
		p = partHorz
		if t.bd.read(probs[1]) != 0 {
			p = partSplit
		}
	case hasRows && !hasCols:
		p = partVert
		if t.bd.read(probs[2]) != 0 {
			p = partSplit
		}
	default:
		p = partSplit
	}
	if !d.hdr.intraOnly() {
		t.counts.partition[ctx][p]++
	}
	return p
}

func (t *tileState) decodePartition(miRow, miCol, bsize, n4l2 int) {
	d := t.d
	if miRow >= d.miRows || miCol >= d.miCols {
		return
	}
	n8l2 := n4l2 - 1
	num8 := 1 << n8l2
	hbs := num8 >> 1
	hasRows := miRow+hbs < d.miRows
	hasCols := miCol+hbs < d.miCols
	p := t.readPartition(miRow, miCol, hasRows, hasCols, n8l2)
	sub := int(subsizeLookup[p][bsize])
	if hbs == 0 {
		t.bmodeWL = 1
		if p&partVert != 0 {
			t.bmodeWL = 0
		}
		t.bmodeHL = 1
		if p&partHorz != 0 {
			t.bmodeHL = 0
		}
		t.decodeBlock(miRow, miCol, sub, 1, 1)
	} else {
		switch p {
		case partNone:
			t.decodeBlock(miRow, miCol, sub, n4l2, n4l2)
		case partHorz:
			t.decodeBlock(miRow, miCol, sub, n4l2, n8l2)
			if hasRows {
				t.decodeBlock(miRow+hbs, miCol, sub, n4l2, n8l2)
			}
		case partVert:
			t.decodeBlock(miRow, miCol, sub, n8l2, n4l2)
			if hasCols {
				t.decodeBlock(miRow, miCol+hbs, sub, n8l2, n4l2)
			}
		default:
			t.decodePartition(miRow, miCol, sub, n8l2)
			t.decodePartition(miRow, miCol+hbs, sub, n8l2)
			t.decodePartition(miRow+hbs, miCol, sub, n8l2)
			t.decodePartition(miRow+hbs, miCol+hbs, sub, n8l2)
		}
	}
	if bsize >= block8x8 && (bsize == block8x8 || p != partSplit) {
		pc := partitionContextLookup[sub]
		for i := 0; i < num8; i++ {
			if miCol+i < len(d.abovePart) {
				d.abovePart[miCol+i] = pc[0]
			}
			t.leftPart[(miRow+i)&7] = pc[1]
		}
	}
}

func (t *tileState) decodeBlock(miRow, miCol, bsize, bwl, bhl int) {
	d := t.d
	bw, bh := 1<<(bwl-1), 1<<(bhl-1)
	xmis, ymis := min(bw, d.miCols-miCol), min(bh, d.miRows-miRow)
	b := t.alloc()
	b.bsize = uint8(bsize)
	for y := 0; y < ymis; y++ {
		row := d.mi[(miRow+y)*d.miStride+miCol:]
		for x := 0; x < xmis; x++ {
			row[x] = b
		}
	}
	t.bi = b
	t.miRow, t.miCol = miRow, miCol
	t.bwl, t.bhl = bwl, bhl
	t.toTop = -(miRow * 8 * 8)
	t.toBottom = (d.miRows - bh - miRow) * 8 * 8
	t.toLeft = -(miCol * 8 * 8)
	t.toRight = (d.miCols - bw - miCol) * 8 * 8
	t.above, t.left = nil, nil
	if miRow > 0 {
		t.above = d.mi[(miRow-1)*d.miStride+miCol]
	}
	if miCol > t.miColStart {
		t.left = d.mi[miRow*d.miStride+miCol-1]
	}

	if d.hdr.intraOnly() {
		t.readIntraFrameModeInfo(xmis, ymis)
	} else {
		t.readInterFrameModeInfo(xmis, ymis)
		mvs := d.cur.mvs
		for y := 0; y < ymis; y++ {
			row := mvs[(miRow+y)*d.miCols+miCol:]
			for x := 0; x < xmis; x++ {
				row[x] = mvRef{ref: b.ref, mv: b.mv}
			}
		}
	}
	if d.hdr.intraOnly() {
		mvs := d.cur.mvs
		for y := 0; y < ymis; y++ {
			row := mvs[(miRow+y)*d.miCols+miCol:]
			for x := 0; x < xmis; x++ {
				row[x] = mvRef{ref: [2]int8{intraFrame, -1}}
			}
		}
	}
	if st := d.stats; st != nil {
		st.block(b)
	}
	if b.skip {
		t.resetSkipContext()
	}
	if !b.isInter {
		t.reconIntra()
	} else {
		t.predictInter()
		if !b.skip {
			eobTotal := t.reconInter()
			if bsize >= block8x8 && eobTotal == 0 {
				b.skip = true
			}
		}
	}
}

func (t *tileState) resetSkipContext() {
	for p := 0; p < 3; p++ {
		ss := 0
		if p > 0 {
			ss = 1
		}
		n4w := (1 << t.bwl) >> ss
		n4h := (1 << t.bhl) >> ss
		if n4w < 1 {
			n4w = 1
		}
		if n4h < 1 {
			n4h = 1
		}
		a := t.d.aboveNZ[p]
		x := (t.miCol * 2) >> ss
		for i := 0; i < n4w && x+i < len(a); i++ {
			a[x+i] = 0
		}
		y := ((t.miRow & 7) * 2) >> ss
		for i := 0; i < n4h && y+i < 16; i++ {
			t.leftNZ[p][y+i] = 0
		}
	}
}

// --- segment ids ---

func (t *tileState) predSegID(xmis, ymis int) int {
	d := t.d
	seg := 7
	for y := 0; y < ymis; y++ {
		for x := 0; x < xmis; x++ {
			seg = min(seg, int(d.segMapLast[(t.miRow+y)*d.miCols+t.miCol+x]))
		}
	}
	return seg
}

func (t *tileState) setSegID(xmis, ymis, id int) {
	d := t.d
	for y := 0; y < ymis; y++ {
		row := d.segMapCur[(t.miRow+y)*d.miCols+t.miCol:]
		for x := 0; x < xmis; x++ {
			row[x] = uint8(id)
		}
	}
}

func (t *tileState) copySegID(xmis, ymis int) {
	d := t.d
	for y := 0; y < ymis; y++ {
		o := (t.miRow+y)*d.miCols + t.miCol
		copy(d.segMapCur[o:o+xmis], d.segMapLast[o:o+xmis])
	}
}

func (t *tileState) readIntraSegID(xmis, ymis int) int {
	s := &t.d.seg
	if !s.enabled {
		return 0
	}
	if !s.updateMap {
		t.copySegID(xmis, ymis)
		return 0
	}
	id := t.bd.tree(segmentTree[:], s.treeProbs[:])
	t.setSegID(xmis, ymis, id)
	return id
}

func (t *tileState) readInterSegID(xmis, ymis int) int {
	s := &t.d.seg
	if !s.enabled {
		return 0
	}
	pred := t.predSegID(xmis, ymis)
	if !s.updateMap {
		t.copySegID(xmis, ymis)
		return pred
	}
	var id int
	if s.temporalUpdate {
		ctx := 0
		if t.above != nil {
			ctx += int(t.above.segPred)
		}
		if t.left != nil {
			ctx += int(t.left.segPred)
		}
		sp := t.bd.read(s.predProbs[ctx])
		t.bi.segPred = uint8(sp)
		if sp != 0 {
			id = pred
		} else {
			id = t.bd.tree(segmentTree[:], s.treeProbs[:])
		}
	} else {
		id = t.bd.tree(segmentTree[:], s.treeProbs[:])
	}
	t.setSegID(xmis, ymis, id)
	return id
}

func (t *tileState) readSkip() bool {
	d := t.d
	if d.seg.active(int(t.bi.seg), segLvlSkip) {
		return true
	}
	ctx := 0
	if t.above != nil && t.above.skip {
		ctx++
	}
	if t.left != nil && t.left.skip {
		ctx++
	}
	s := t.bd.read(d.fc.skip[ctx])
	t.counts.skip[ctx][s]++
	return s != 0
}

func (t *tileState) readTxSize(allowSelect bool) uint8 {
	d := t.d
	bs := int(t.bi.bsize)
	maxTx := int(maxTxsizeLookup[bs])
	if allowSelect && d.hdr.txMode == txModeSelect && bs >= block8x8 {
		aboveCtx, leftCtx := maxTx, maxTx
		if t.above != nil && !t.above.skip {
			aboveCtx = int(t.above.tx)
		}
		if t.left != nil && !t.left.skip {
			leftCtx = int(t.left.tx)
		}
		if t.left == nil {
			leftCtx = aboveCtx
		}
		if t.above == nil {
			aboveCtx = leftCtx
		}
		ctx := 0
		if aboveCtx+leftCtx > maxTx {
			ctx = 1
		}
		var probs []uint8
		switch maxTx {
		case 1:
			probs = d.fc.tx8[ctx][:]
		case 2:
			probs = d.fc.tx16[ctx][:]
		default:
			probs = d.fc.tx32[ctx][:]
		}
		tx := t.bd.read(probs[0])
		if tx != 0 && maxTx >= 2 {
			tx += t.bd.read(probs[1])
			if tx != 1 && maxTx >= 3 {
				tx += t.bd.read(probs[2])
			}
		}
		switch maxTx {
		case 1:
			t.counts.tx8[ctx][tx]++
		case 2:
			t.counts.tx16[ctx][tx]++
		default:
			t.counts.tx32[ctx][tx]++
		}
		return uint8(tx)
	}
	return uint8(min(maxTx, txModeToBiggest[d.hdr.txMode]))
}

// --- intra frames ---

func leftBlockMode(cur, left *blockInfo, b int) uint8 {
	if b == 0 || b == 2 {
		if left == nil || left.isInter {
			return dcPred
		}
		return left.yModeAt(b + 1)
	}
	return cur.bmode[b-1]
}

func aboveBlockMode(cur, above *blockInfo, b int) uint8 {
	if b == 0 || b == 1 {
		if above == nil || above.isInter {
			return dcPred
		}
		return above.yModeAt(b + 2)
	}
	return cur.bmode[b-2]
}

func (t *tileState) readKFMode(b int) uint8 {
	a := aboveBlockMode(t.bi, t.above, b)
	l := leftBlockMode(t.bi, t.left, b)
	return uint8(t.bd.tree(intraModeTree[:], kfYModeProb[a][l][:]))
}

func (t *tileState) readIntraFrameModeInfo(xmis, ymis int) {
	b := t.bi
	b.seg = uint8(t.readIntraSegID(xmis, ymis))
	b.skip = t.readSkip()
	b.tx = t.readTxSize(true)
	b.ref = [2]int8{intraFrame, -1}
	switch b.bsize {
	case block4x4:
		for i := 0; i < 4; i++ {
			b.bmode[i] = t.readKFMode(i)
		}
		b.mode = b.bmode[3]
	case block4x8:
		b.bmode[0] = t.readKFMode(0)
		b.bmode[2] = b.bmode[0]
		b.bmode[1] = t.readKFMode(1)
		b.bmode[3] = b.bmode[1]
		b.mode = b.bmode[1]
	case block8x4:
		b.bmode[0] = t.readKFMode(0)
		b.bmode[1] = b.bmode[0]
		b.bmode[2] = t.readKFMode(2)
		b.bmode[3] = b.bmode[2]
		b.mode = b.bmode[2]
	default:
		b.mode = t.readKFMode(0)
	}
	b.uvMode = uint8(t.bd.tree(intraModeTree[:], kfUVModeProb[b.mode][:]))
}

// --- inter frames ---

func (t *tileState) readInterFrameModeInfo(xmis, ymis int) {
	d := t.d
	b := t.bi
	b.seg = uint8(t.readInterSegID(xmis, ymis))
	b.skip = t.readSkip()
	// is_inter
	if d.seg.active(int(b.seg), segLvlRefFrame) {
		b.isInter = d.seg.featureData[b.seg][segLvlRefFrame] != intraFrame
	} else {
		ctx := 0
		a, l := t.above, t.left
		switch {
		case a != nil && l != nil:
			ai, li := !a.isInter, !l.isInter
			if ai && li {
				ctx = 3
			} else if ai || li {
				ctx = 1
			}
		case a != nil || l != nil:
			e := a
			if e == nil {
				e = l
			}
			if !e.isInter {
				ctx = 2
			}
		}
		v := t.bd.read(d.fc.intraInter[ctx])
		t.counts.intraInter[ctx][v]++
		b.isInter = v != 0
	}
	b.tx = t.readTxSize(!b.skip || !b.isInter)
	if b.isInter {
		t.readInterBlockModeInfo()
	} else {
		t.readIntraBlockModeInfo()
	}
}

func (t *tileState) readIntraY(group int) uint8 {
	m := t.bd.tree(intraModeTree[:], t.d.fc.yMode[group][:])
	t.counts.yMode[group][m]++
	return uint8(m)
}

func (t *tileState) readIntraBlockModeInfo() {
	b := t.bi
	switch b.bsize {
	case block4x4:
		for i := 0; i < 4; i++ {
			b.bmode[i] = t.readIntraY(0)
		}
		b.mode = b.bmode[3]
	case block4x8:
		b.bmode[0] = t.readIntraY(0)
		b.bmode[2] = b.bmode[0]
		b.bmode[1] = t.readIntraY(0)
		b.bmode[3] = b.bmode[1]
		b.mode = b.bmode[1]
	case block8x4:
		b.bmode[0] = t.readIntraY(0)
		b.bmode[1] = b.bmode[0]
		b.bmode[2] = t.readIntraY(0)
		b.bmode[3] = b.bmode[2]
		b.mode = b.bmode[2]
	default:
		b.mode = t.readIntraY(int(sizeGroupLookup[b.bsize]))
	}
	uv := t.bd.tree(intraModeTree[:], t.d.fc.uvMode[b.mode][:])
	t.counts.uvMode[b.mode][uv]++
	b.uvMode = uint8(uv)
	b.filter = 3 // SWITCHABLE_FILTERS
	b.ref = [2]int8{intraFrame, -1}
}

func (t *tileState) readRefFrames() {
	d := t.d
	h := d.hdr
	b := t.bi
	if d.seg.active(int(b.seg), segLvlRefFrame) {
		b.ref = [2]int8{int8(d.seg.featureData[b.seg][segLvlRefFrame]), -1}
		return
	}
	mode := h.refMode
	if mode == referenceModeSelect {
		ctx := t.refModeCtx()
		mode = t.bd.read(d.fc.compInter[ctx])
		t.counts.compInter[ctx][mode]++
	}
	if mode == compoundReference {
		idx := 0
		if h.signBias[h.compFixedRef] {
			idx = 1
		}
		ctx := t.compRefCtx()
		bit := t.bd.read(d.fc.compRef[ctx])
		t.counts.compRef[ctx][bit]++
		b.ref[idx] = int8(h.compFixedRef)
		b.ref[1-idx] = int8(h.compVarRef[bit])
		return
	}
	ctx0 := t.singleRefP1Ctx()
	bit0 := t.bd.read(d.fc.singleRef[ctx0][0])
	t.counts.singleRef[ctx0][0][bit0]++
	if bit0 != 0 {
		ctx1 := t.singleRefP2Ctx()
		bit1 := t.bd.read(d.fc.singleRef[ctx1][1])
		t.counts.singleRef[ctx1][1][bit1]++
		if bit1 != 0 {
			b.ref[0] = altrefFrame
		} else {
			b.ref[0] = goldenFrame
		}
	} else {
		b.ref[0] = lastFrame
	}
	b.ref[1] = -1
}

func (t *tileState) readInterMode(ctx int) uint8 {
	m := t.bd.tree(interModeTree[:], t.d.fc.interMode[ctx][:])
	t.counts.interMode[ctx][m]++
	return uint8(nearestMV + m)
}

func (t *tileState) readSwitchableFilter() uint8 {
	lt, at := 3, 3
	if t.left != nil && t.left.isInter {
		lt = int(t.left.filter)
	}
	if t.above != nil && t.above.isInter {
		at = int(t.above.filter)
	}
	var ctx int
	switch {
	case lt == at:
		ctx = lt
	case lt == 3:
		ctx = at
	case at == 3:
		ctx = lt
	default:
		ctx = 3
	}
	f := t.bd.tree(switchableInterpTree[:], t.d.fc.switchableInterp[ctx][:])
	t.counts.switchableInterp[ctx][f]++
	return uint8(f)
}

func (t *tileState) readInterBlockModeInfo() {
	d := t.d
	h := d.hdr
	b := t.bi
	t.readRefFrames()
	compound := b.hasSecond()
	nrefs := 1
	if compound {
		nrefs = 2
	}
	var refList [2][2]mv
	var modeCtx int
	for r := 0; r < nrefs; r++ {
		modeCtx = t.findMVRefs(int(b.ref[r]), &refList[r], -1)
	}
	bs := int(b.bsize)
	if d.seg.active(int(b.seg), segLvlSkip) {
		b.mode = zeroMV
		if bs < block8x8 {
			panic("vp9: segment skip on sub8x8 block")
		}
	} else if bs >= block8x8 {
		b.mode = t.readInterMode(modeCtx)
	}
	var nearest, near [2]mv
	if bs < block8x8 || b.mode != zeroMV {
		for r := 0; r < nrefs; r++ {
			l := refList[r]
			for i := range l {
				lowerMVPrecision(&l[i], h.allowHP)
				t.clampMV2(&l[i])
			}
			nearest[r], near[r] = l[0], l[1]
		}
	}
	if h.interpFilter == filterSwitchable {
		b.filter = t.readSwitchableFilter()
	} else {
		b.filter = uint8(h.interpFilter)
	}
	if bs < block8x8 {
		n4w, n4h := 1<<t.bmodeWL, 1<<t.bmodeHL
		var bm uint8
		for idy := 0; idy < 2; idy += n4h {
			for idx := 0; idx < 2; idx += n4w {
				j := idy*2 + idx
				bm = t.readInterMode(modeCtx)
				var ns, nr [2]mv
				if bm == nearestMV || bm == nearMV {
					for r := 0; r < nrefs; r++ {
						ns[r], nr[r] = t.appendSub8x8(j, r)
					}
				}
				var out [2]mv
				t.assignMV(bm, &out, nearest, ns, nr, nrefs)
				b.bmv[j] = out
				if n4h == 2 {
					b.bmv[j+2] = out
				}
				if n4w == 2 {
					b.bmv[j+1] = out
				}
			}
		}
		b.mode = bm
		b.mv = b.bmv[3]
	} else {
		t.assignMV(b.mode, &b.mv, nearest, nearest, near, nrefs)
		for i := range b.bmv {
			b.bmv[i] = b.mv
		}
	}
}

func (t *tileState) assignMV(mode uint8, out *[2]mv, best, nearest, near [2]mv, nrefs int) {
	switch mode {
	case newMV:
		for r := 0; r < nrefs; r++ {
			out[r] = t.readMV(best[r])
		}
	case nearestMV:
		out[0], out[1] = nearest[0], nearest[1]
	case nearMV:
		out[0], out[1] = near[0], near[1]
	default:
		out[0], out[1] = mv{}, mv{}
	}
	if nrefs == 1 {
		out[1] = mv{}
	}
}

func useMVHP(m mv) bool {
	return abs(int(m.row))>>3 < 8 && abs(int(m.col))>>3 < 8
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func lowerMVPrecision(m *mv, allowHP bool) {
	if allowHP && useMVHP(*m) {
		return
	}
	if m.row&1 != 0 {
		if m.row > 0 {
			m.row--
		} else {
			m.row++
		}
	}
	if m.col&1 != 0 {
		if m.col > 0 {
			m.col--
		} else {
			m.col++
		}
	}
}

func (t *tileState) readMVComponent(i int, useHP bool) int {
	c := &t.d.fc.mvComp[i]
	sign := t.bd.read(c.sign)
	class := t.bd.tree(mvClassTree[:], c.classes[:])
	var mag, dd, fr, hp int
	if class == 0 {
		dd = t.bd.read(c.class0[0])
		fr = t.bd.tree(mvFPTree[:], c.class0FP[dd][:])
		hp = 1
		if useHP {
			hp = t.bd.read(c.class0HP)
		}
	} else {
		for j := 0; j < class; j++ { // mv_class + CLASS0_BITS - 1 bits
			dd |= t.bd.read(c.bits[j]) << j
		}
		mag = 2 << (class + 2)
		fr = t.bd.tree(mvFPTree[:], c.fp[:])
		hp = 1
		if useHP {
			hp = t.bd.read(c.hp)
		}
	}
	mag += (dd<<3 | fr<<1 | hp) + 1
	if sign != 0 {
		return -mag
	}
	return mag
}

func mvClassOf(z int) (int, int) {
	c := 10
	if z < 2*4096 {
		c = log2Floor(z >> 3)
	}
	base := 0
	if c > 0 {
		base = 2 << (c + 2)
	}
	return c, z - base
}

func log2Floor(n int) int {
	r := 0
	for n > 1 {
		n >>= 1
		r++
	}
	return r
}

func (t *tileState) incMVComponent(v int, i int) {
	c := &t.counts.mvComp[i]
	s := 0
	if v < 0 {
		s = 1
		v = -v
	}
	c.sign[s]++
	z := v - 1
	class, o := mvClassOf(z)
	c.classes[class]++
	dd := o >> 3
	f := (o >> 1) & 3
	e := o & 1
	if class == 0 {
		c.class0[dd]++
		c.class0FP[dd][f]++
		c.class0HP[e]++
	} else {
		for j := 0; j < class; j++ {
			c.bits[j][(dd>>j)&1]++
		}
		c.fp[f]++
		c.hp[e]++
	}
}

func (t *tileState) readMV(ref mv) mv {
	d := t.d
	j := t.bd.tree(mvJointTree[:], d.fc.mvJoints[:])
	useHP := d.hdr.allowHP && useMVHP(ref)
	var dr, dc int
	if j == 2 || j == 3 {
		dr = t.readMVComponent(0, useHP)
	}
	if j == 1 || j == 3 {
		dc = t.readMVComponent(1, useHP)
	}
	t.counts.mvJoints[j]++
	if j == 2 || j == 3 {
		t.incMVComponent(dr, 0)
	}
	if j == 1 || j == 3 {
		t.incMVComponent(dc, 1)
	}
	r, c := int(ref.row)+dr, int(ref.col)+dc
	if r <= -(1<<14) || r >= 1<<14 || c <= -(1<<14) || c >= 1<<14 {
		panic("vp9: invalid motion vector")
	}
	return mv{int16(r), int16(c)}
}

// --- contexts (vp9_pred_common.c) ---

func (t *tileState) refModeCtx() int {
	h := t.d.hdr
	a, l := t.above, t.left
	fixed := int8(h.compFixedRef)
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	switch {
	case a != nil && l != nil:
		switch {
		case !a.hasSecond() && !l.hasSecond():
			return b2i(a.ref[0] == fixed) ^ b2i(l.ref[0] == fixed)
		case !a.hasSecond():
			return 2 + b2i(a.ref[0] == fixed || !a.isInter)
		case !l.hasSecond():
			return 2 + b2i(l.ref[0] == fixed || !l.isInter)
		default:
			return 4
		}
	case a != nil || l != nil:
		e := a
		if e == nil {
			e = l
		}
		if !e.hasSecond() {
			return b2i(e.ref[0] == fixed)
		}
		return 3
	}
	return 1
}

func (t *tileState) compRefCtx() int {
	h := t.d.hdr
	a, l := t.above, t.left
	fixIdx := 0
	if h.signBias[h.compFixedRef] {
		fixIdx = 1
	}
	varIdx := 1 - fixIdx
	var1 := int8(h.compVarRef[1])
	var0 := int8(h.compVarRef[0])
	fixed := int8(h.compFixedRef)
	switch {
	case a != nil && l != nil:
		ai, li := !a.isInter, !l.isInter
		switch {
		case ai && li:
			return 2
		case ai || li:
			e := a
			if ai {
				e = l
			}
			if !e.hasSecond() {
				if e.ref[0] != var1 {
					return 3
				}
				return 1
			}
			if e.ref[varIdx] != var1 {
				return 3
			}
			return 1
		default:
			lsg, asg := !l.hasSecond(), !a.hasSecond()
			vrfa := a.ref[varIdx]
			if asg {
				vrfa = a.ref[0]
			}
			vrfl := l.ref[varIdx]
			if lsg {
				vrfl = l.ref[0]
			}
			switch {
			case vrfa == vrfl && var1 == vrfa:
				return 0
			case lsg && asg:
				if (vrfa == fixed && vrfl == var0) || (vrfl == fixed && vrfa == var0) {
					return 4
				}
				if vrfa == vrfl {
					return 3
				}
				return 1
			case lsg || asg:
				vrfc := vrfl
				if lsg {
					vrfc = vrfa
				}
				rfs := vrfl
				if asg {
					rfs = vrfa
				}
				if vrfc == var1 && rfs != var1 {
					return 1
				}
				if rfs == var1 && vrfc != var1 {
					return 2
				}
				return 4
			case vrfa == vrfl:
				return 4
			default:
				return 2
			}
		}
	case a != nil || l != nil:
		e := a
		if e == nil {
			e = l
		}
		if !e.isInter {
			return 2
		}
		if e.hasSecond() {
			if e.ref[varIdx] != var1 {
				return 4
			}
			return 0
		}
		if e.ref[0] != var1 {
			return 3
		}
		return 0
	}
	return 2
}

func (t *tileState) singleRefP1Ctx() int {
	a, l := t.above, t.left
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	switch {
	case a != nil && l != nil:
		ai, li := !a.isInter, !l.isInter
		switch {
		case ai && li:
			return 2
		case ai || li:
			e := a
			if ai {
				e = l
			}
			if !e.hasSecond() {
				return 4 * b2i(e.ref[0] == lastFrame)
			}
			return 1 + b2i(e.ref[0] == lastFrame || e.ref[1] == lastFrame)
		default:
			as, ls := a.hasSecond(), l.hasSecond()
			a0, a1, l0, l1 := a.ref[0], a.ref[1], l.ref[0], l.ref[1]
			switch {
			case as && ls:
				return 1 + b2i(a0 == lastFrame || a1 == lastFrame || l0 == lastFrame || l1 == lastFrame)
			case as || ls:
				rfs := l0
				if !as {
					rfs = a0
				}
				crf1, crf2 := l0, l1
				if as {
					crf1, crf2 = a0, a1
				}
				if rfs == lastFrame {
					return 3 + b2i(crf1 == lastFrame || crf2 == lastFrame)
				}
				return b2i(crf1 == lastFrame || crf2 == lastFrame)
			default:
				return 2*b2i(a0 == lastFrame) + 2*b2i(l0 == lastFrame)
			}
		}
	case a != nil || l != nil:
		e := a
		if e == nil {
			e = l
		}
		if !e.isInter {
			return 2
		}
		if !e.hasSecond() {
			return 4 * b2i(e.ref[0] == lastFrame)
		}
		return 1 + b2i(e.ref[0] == lastFrame || e.ref[1] == lastFrame)
	}
	return 2
}

func (t *tileState) singleRefP2Ctx() int {
	a, l := t.above, t.left
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	switch {
	case a != nil && l != nil:
		ai, li := !a.isInter, !l.isInter
		switch {
		case ai && li:
			return 2
		case ai || li:
			e := a
			if ai {
				e = l
			}
			if !e.hasSecond() {
				if e.ref[0] == lastFrame {
					return 3
				}
				return 4 * b2i(e.ref[0] == goldenFrame)
			}
			return 1 + 2*b2i(e.ref[0] == goldenFrame || e.ref[1] == goldenFrame)
		default:
			as, ls := a.hasSecond(), l.hasSecond()
			a0, a1, l0, l1 := a.ref[0], a.ref[1], l.ref[0], l.ref[1]
			switch {
			case as && ls:
				if a0 == l0 && a1 == l1 {
					return 3 * b2i(a0 == goldenFrame || a1 == goldenFrame || l0 == goldenFrame || l1 == goldenFrame)
				}
				return 2
			case as || ls:
				rfs := l0
				if !as {
					rfs = a0
				}
				crf1, crf2 := l0, l1
				if as {
					crf1, crf2 = a0, a1
				}
				switch rfs {
				case goldenFrame:
					return 3 + b2i(crf1 == goldenFrame || crf2 == goldenFrame)
				case altrefFrame:
					return b2i(crf1 == goldenFrame || crf2 == goldenFrame)
				default:
					return 1 + 2*b2i(crf1 == goldenFrame || crf2 == goldenFrame)
				}
			default:
				switch {
				case a0 == lastFrame && l0 == lastFrame:
					return 3
				case a0 == lastFrame || l0 == lastFrame:
					e0 := a0
					if a0 == lastFrame {
						e0 = l0
					}
					return 4 * b2i(e0 == goldenFrame)
				default:
					return 2*b2i(a0 == goldenFrame) + 2*b2i(l0 == goldenFrame)
				}
			}
		}
	case a != nil || l != nil:
		e := a
		if e == nil {
			e = l
		}
		if !e.isInter || (e.ref[0] == lastFrame && !e.hasSecond()) {
			return 2
		}
		if !e.hasSecond() {
			return 4 * b2i(e.ref[0] == goldenFrame)
		}
		return 3 * b2i(e.ref[0] == goldenFrame || e.ref[1] == goldenFrame)
	}
	return 2
}
