package av1

// Inter mode info and motion vector prediction (spec 5.11.18-5.11.33,
// 7.10.2, 7.10.4).

// Motion modes.
const (
	motionSimple    = 0
	motionOBMC      = 1
	motionLocalWarp = 2
)

// Compound types.
const (
	compoundWedge    = 0
	compoundDiffWtd  = 1
	compoundAverage  = 2
	compoundIntra    = 3
	compoundDistance = 4
)

const (
	refCatLevel   = 640
	maxRefMvStack = 8
	invalidMv     = -1 << 15
)

var wedgeBits = [blockSizes]int{0, 0, 0, 4, 4, 4, 4, 4, 4, 4, 0, 0, 0, 0, 0, 0, 0, 0, 4, 4, 0, 0}

var compoundModeCtxMap = [3][5]int{{0, 1, 1, 1, 1}, {1, 2, 3, 4, 4}, {4, 4, 5, 6, 7}}

// mvPred is the motion vector prediction state of a block.
type mvPred struct {
	numMvFound, newMvCount int
	refStack               [maxRefMvStack][2]mv
	weight                 [maxRefMvStack]int
	globalMvs              [2]mv
	drlCtx                 [maxRefMvStack]int
	newMvCtx, refMvCtx     int
	zeroMvCtx              int
	foundMatch             bool
	closeMatches           int
	totalMatches           int
	refMvIdx               int
	predMv                 [2]mv

	// Warp samples.
	numSamples, numSamplesScanned int
	candList                      [8][4]int
	localWarp                     [6]int32
	localValid                    bool

	// Neighbour summary.
	leftRef, aboveRef       [2]int
	leftIntra, aboveIntra   bool
	leftSingle, aboveSingle bool
}

func hasNewmv(m int) bool {
	switch m {
	case newMV, newNewMV, nearNewMV, newNearMV, nearestNewMV, newNearestMV:
		return true
	}
	return false
}

// interFrameModeInfo is inter_frame_mode_info (spec 5.11.18).
func (t *tile) interFrameModeInfo() error {
	b := &t.b
	h := t.h
	p := &t.mvp
	p.leftRef, p.aboveRef = [2]int{intraFrame, -1}, [2]int{intraFrame, -1}
	if b.availL {
		m := t.mi(b.miRow, b.miCol-1)
		p.leftRef = [2]int{int(m.refFrame[0]), int(m.refFrame[1])}
	}
	if b.availU {
		m := t.mi(b.miRow-1, b.miCol)
		p.aboveRef = [2]int{int(m.refFrame[0]), int(m.refFrame[1])}
	}
	p.leftIntra, p.aboveIntra = p.leftRef[0] <= intraFrame, p.aboveRef[0] <= intraFrame
	p.leftSingle, p.aboveSingle = p.leftRef[1] <= intraFrame, p.aboveRef[1] <= intraFrame
	b.skip = false
	t.interSegmentID(true)
	t.readSkipMode()
	if b.skipMode {
		b.skip = true
	} else {
		t.readSkip()
	}
	if !h.seg.preSkip {
		t.interSegmentID(false)
	}
	b.lossless = h.lossless[b.segID]
	t.readCdef()
	t.readDeltaQIndex()
	t.readDeltaLF()
	t.readDeltas = false
	t.readIsInter()
	if b.isInter {
		return t.interBlockModeInfo()
	}
	b.refFrame = [2]int{intraFrame, -1}
	b.yMode = t.ms.sym(t.cdf.yMode[sizeGroup[b.size]][:], intraModes)
	t.intraModesRest()
	return nil
}

func (t *tile) interSegmentID(preSkip bool) {
	b := &t.b
	h := t.h
	f := t.f
	if !h.seg.enabled {
		b.segID = 0
		return
	}
	// get_segment_id: the smallest previous id under the block.
	pred := 7
	if f.prevSeg != nil {
		for y := 0; y < min(f.miRows-b.miRow, b.bh4); y++ {
			for x := 0; x < min(f.miCols-b.miCol, b.bw4); x++ {
				pred = min(pred, int(f.prevSeg[(b.miRow+y)*f.miCols+b.miCol+x]))
			}
		}
	} else {
		pred = 0
	}
	if !h.seg.updateMap {
		b.segID = pred
		return
	}
	if preSkip && !h.seg.preSkip {
		b.segID = 0
		return
	}
	setCtx := func(v bool) {
		for i := 0; i < b.bw4; i++ {
			t.aboveSegPred[b.miCol+i] = v
		}
		for i := 0; i < b.bh4; i++ {
			t.leftSegPred[(b.miRow+i)&31] = v
		}
	}
	if !preSkip && b.skip {
		setCtx(false)
		t.readSegmentID()
		return
	}
	if h.seg.temporalUpdate {
		ctx := 0
		if t.leftSegPred[b.miRow&31] {
			ctx++
		}
		if t.aboveSegPred[b.miCol] {
			ctx++
		}
		predicted := t.ms.flag(t.cdf.segmentPred[ctx][:])
		if predicted {
			b.segID = pred
		} else {
			t.readSegmentID()
		}
		setCtx(predicted)
		return
	}
	t.readSegmentID()
}

func (t *tile) readSkipMode() {
	b := &t.b
	h := t.h
	if t.segFeature(b.segID, segLvlSkip) || t.segFeature(b.segID, segLvlRefFrame) || t.segFeature(b.segID, segLvlGlobalMV) ||
		!h.skipModePresent || blockW(b.size) < 8 || blockH(b.size) < 8 {
		b.skipMode = false
		return
	}
	ctx := 0
	if b.availU && t.mi(b.miRow-1, b.miCol).skipMode {
		ctx++
	}
	if b.availL && t.mi(b.miRow, b.miCol-1).skipMode {
		ctx++
	}
	b.skipMode = t.ms.flag(t.cdf.skipMode[ctx][:])
}

func (t *tile) readIsInter() {
	b := &t.b
	p := &t.mvp
	switch {
	case b.skipMode:
		b.isInter = true
	case t.segFeature(b.segID, segLvlRefFrame):
		b.isInter = t.h.seg.featureData[b.segID][segLvlRefFrame] != intraFrame
	case t.segFeature(b.segID, segLvlGlobalMV):
		b.isInter = true
	default:
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
		b.isInter = t.ms.flag(t.cdf.isInter[ctx][:])
	}
}

func checkBackward(r int) bool { return r >= bwdrefFrame && r <= altrefFrame }

func (t *tile) countRefs(r int) int {
	b := &t.b
	p := &t.mvp
	c := 0
	if b.availU {
		if p.aboveRef[0] == r {
			c++
		}
		if p.aboveRef[1] == r {
			c++
		}
	}
	if b.availL {
		if p.leftRef[0] == r {
			c++
		}
		if p.leftRef[1] == r {
			c++
		}
	}
	return c
}

func refCountCtx(c0, c1 int) int {
	switch {
	case c0 < c1:
		return 0
	case c0 == c1:
		return 1
	}
	return 2
}

func (t *tile) refCtx(a, b []int) int {
	c0, c1 := 0, 0
	for _, r := range a {
		c0 += t.countRefs(r)
	}
	for _, r := range b {
		c1 += t.countRefs(r)
	}
	return refCountCtx(c0, c1)
}

func (t *tile) compModeCtx() int {
	b := &t.b
	p := &t.mvp
	bi := func(x bool) int {
		if x {
			return 1
		}
		return 0
	}
	switch {
	case b.availU && b.availL:
		switch {
		case p.aboveSingle && p.leftSingle:
			return bi(checkBackward(p.aboveRef[0])) ^ bi(checkBackward(p.leftRef[0]))
		case p.aboveSingle:
			return 2 + bi(checkBackward(p.aboveRef[0]) || p.aboveIntra)
		case p.leftSingle:
			return 2 + bi(checkBackward(p.leftRef[0]) || p.leftIntra)
		}
		return 4
	case b.availU:
		if p.aboveSingle {
			return bi(checkBackward(p.aboveRef[0]))
		}
		return 3
	case b.availL:
		if p.leftSingle {
			return bi(checkBackward(p.leftRef[0]))
		}
		return 3
	}
	return 1
}

func (t *tile) compRefTypeCtx() int {
	b := &t.b
	p := &t.mvp
	bi := func(x bool) int {
		if x {
			return 1
		}
		return 0
	}
	sameDir := func(r0, r1 int) bool { return (r0 >= bwdrefFrame) == (r1 >= bwdrefFrame) }
	above0, above1 := p.aboveRef[0], p.aboveRef[1]
	left0, left1 := p.leftRef[0], p.leftRef[1]
	aboveComp := b.availU && !p.aboveIntra && !p.aboveSingle
	leftComp := b.availL && !p.leftIntra && !p.leftSingle
	aboveUni := aboveComp && sameDir(above0, above1)
	leftUni := leftComp && sameDir(left0, left1)
	switch {
	case b.availU && !p.aboveIntra && b.availL && !p.leftIntra:
		same := bi(sameDir(above0, left0))
		switch {
		case !aboveComp && !leftComp:
			return 1 + 2*same
		case !aboveComp:
			if !leftUni {
				return 1
			}
			return 3 + same
		case !leftComp:
			if !aboveUni {
				return 1
			}
			return 3 + same
		}
		switch {
		case !aboveUni && !leftUni:
			return 0
		case !aboveUni || !leftUni:
			return 2
		}
		return 3 + bi((above0 == bwdrefFrame) == (left0 == bwdrefFrame))
	case b.availU && b.availL:
		switch {
		case aboveComp:
			return 1 + 2*bi(aboveUni)
		case leftComp:
			return 1 + 2*bi(leftUni)
		}
		return 2
	case aboveComp:
		return 4 * bi(aboveUni)
	case leftComp:
		return 4 * bi(leftUni)
	}
	return 2
}

var (
	refsFwd  = []int{lastFrame, last2Frame, last3Frame, goldenFrame}
	refsBwd  = []int{bwdrefFrame, altref2Frame, altrefFrame}
	refsLL2  = []int{lastFrame, last2Frame}
	refsL3G  = []int{last3Frame, goldenFrame}
	refsBA2  = []int{bwdrefFrame, altref2Frame}
	refsAlt  = []int{altrefFrame}
	refsLast = []int{lastFrame}
	refsL2   = []int{last2Frame}
	refsL3   = []int{last3Frame}
	refsGold = []int{goldenFrame}
	refsBwd1 = []int{bwdrefFrame}
	refsAlt2 = []int{altref2Frame}
)

func (t *tile) readRefFrames() {
	b := &t.b
	h := t.h
	c := &t.cdf
	switch {
	case b.skipMode:
		b.refFrame = h.skipModeFrame
		return
	case t.segFeature(b.segID, segLvlRefFrame):
		b.refFrame = [2]int{h.seg.featureData[b.segID][segLvlRefFrame], -1}
		return
	case t.segFeature(b.segID, segLvlSkip), t.segFeature(b.segID, segLvlGlobalMV):
		b.refFrame = [2]int{lastFrame, -1}
		return
	}
	compound := false
	if h.referenceSelect && min(b.bw4, b.bh4) >= 2 {
		compound = t.ms.flag(c.compInter[t.compModeCtx()][:])
	}
	if !compound {
		if t.ms.flag(c.singleRef[t.refCtx(refsFwd, refsBwd)][0][:]) {
			if !t.ms.flag(c.singleRef[t.refCtx(refsBA2, refsAlt)][1][:]) {
				if t.ms.flag(c.singleRef[t.refCtx(refsBwd1, refsAlt2)][5][:]) {
					b.refFrame[0] = altref2Frame
				} else {
					b.refFrame[0] = bwdrefFrame
				}
			} else {
				b.refFrame[0] = altrefFrame
			}
		} else if t.ms.flag(c.singleRef[t.refCtx(refsLL2, refsL3G)][2][:]) {
			if t.ms.flag(c.singleRef[t.refCtx(refsL3, refsGold)][4][:]) {
				b.refFrame[0] = goldenFrame
			} else {
				b.refFrame[0] = last3Frame
			}
		} else if t.ms.flag(c.singleRef[t.refCtx(refsLast, refsL2)][3][:]) {
			b.refFrame[0] = last2Frame
		} else {
			b.refFrame[0] = lastFrame
		}
		b.refFrame[1] = -1
		return
	}
	if !t.ms.flag(c.compRefType[t.compRefTypeCtx()][:]) { // UNIDIR_COMP_REFERENCE
		if t.ms.flag(c.uniCompRef[t.refCtx(refsFwd, refsBwd)][0][:]) {
			b.refFrame = [2]int{bwdrefFrame, altrefFrame}
		} else if t.ms.flag(c.uniCompRef[t.refCtx(refsL2, refsL3G)][1][:]) {
			if t.ms.flag(c.uniCompRef[t.refCtx(refsL3, refsGold)][2][:]) {
				b.refFrame = [2]int{lastFrame, goldenFrame}
			} else {
				b.refFrame = [2]int{lastFrame, last3Frame}
			}
		} else {
			b.refFrame = [2]int{lastFrame, last2Frame}
		}
		return
	}
	if !t.ms.flag(c.compRef[t.refCtx(refsLL2, refsL3G)][0][:]) {
		if t.ms.flag(c.compRef[t.refCtx(refsLast, refsL2)][1][:]) {
			b.refFrame[0] = last2Frame
		} else {
			b.refFrame[0] = lastFrame
		}
	} else if t.ms.flag(c.compRef[t.refCtx(refsL3, refsGold)][2][:]) {
		b.refFrame[0] = goldenFrame
	} else {
		b.refFrame[0] = last3Frame
	}
	if !t.ms.flag(c.compBwdRef[t.refCtx(refsBA2, refsAlt)][0][:]) {
		if t.ms.flag(c.compBwdRef[t.refCtx(refsBwd1, refsAlt2)][1][:]) {
			b.refFrame[1] = altref2Frame
		} else {
			b.refFrame[1] = bwdrefFrame
		}
	} else {
		b.refFrame[1] = altrefFrame
	}
}

// interBlockModeInfo is inter_block_mode_info (spec 5.11.23).
func (t *tile) interBlockModeInfo() error {
	b := &t.b
	h := t.h
	p := &t.mvp
	c := &t.cdf
	b.paletteSizeY, b.paletteSizeUV = 0, 0
	t.readRefFrames()
	compound := b.refFrame[1] > intraFrame
	t.findMvStack(compound)
	switch {
	case b.skipMode:
		b.yMode = nearestNearestMV
	case t.segFeature(b.segID, segLvlSkip), t.segFeature(b.segID, segLvlGlobalMV):
		b.yMode = globalMV
	case compound:
		ctx := compoundModeCtxMap[p.refMvCtx>>1][min(p.newMvCtx, 4)]
		b.yMode = nearestNearestMV + t.ms.sym(c.compoundMode[ctx][:], 8)
	default:
		if !t.ms.flag(c.newMV[p.newMvCtx][:]) {
			b.yMode = newMV
		} else if !t.ms.flag(c.zeroMV[p.zeroMvCtx][:]) {
			b.yMode = globalMV
		} else if !t.ms.flag(c.refMV[p.refMvCtx][:]) {
			b.yMode = nearestMV
		} else {
			b.yMode = nearMV
		}
	}
	p.refMvIdx = 0
	switch {
	case b.yMode == newMV || b.yMode == newNewMV:
		for idx := 0; idx < 2; idx++ {
			if p.numMvFound > idx+1 {
				if !t.ms.flag(c.drl[p.drlCtx[idx]][:]) {
					p.refMvIdx = idx
					break
				}
				p.refMvIdx = idx + 1
			}
		}
	case b.yMode == nearMV || b.yMode == nearNearMV || b.yMode == nearNewMV || b.yMode == newNearMV:
		p.refMvIdx = 1
		for idx := 1; idx < 3; idx++ {
			if p.numMvFound > idx+1 {
				if !t.ms.flag(c.drl[p.drlCtx[idx]][:]) {
					p.refMvIdx = idx
					break
				}
				p.refMvIdx = idx + 1
			}
		}
	}
	if err := t.assignMv(compound); err != nil {
		return err
	}
	t.readInterIntra(compound)
	t.readMotionMode(compound)
	t.readCompoundType(compound)
	if h.interpFilter == filterSwitchable {
		dirs := 1
		if t.seq.EnableDualFilter {
			dirs = 2
		}
		for dir := 0; dir < dirs; dir++ {
			if t.needsInterpFilter() {
				b.interpFilter[dir] = t.ms.sym(c.interpFilter[t.interpCtx(dir)][:], 3)
			} else {
				b.interpFilter[dir] = filterEightTap
			}
		}
		if !t.seq.EnableDualFilter {
			b.interpFilter[1] = b.interpFilter[0]
		}
	} else {
		b.interpFilter = [2]int{h.interpFilter, h.interpFilter}
	}
	return nil
}

func (t *tile) needsInterpFilter() bool {
	b := &t.b
	h := t.h
	large := min(blockW(b.size), blockH(b.size)) >= 8
	switch {
	case b.skipMode || b.motionMode == motionLocalWarp:
		return false
	case large && b.yMode == globalMV:
		return h.gmType[b.refFrame[0]] == gmTranslation
	case large && b.yMode == globalGlobalMV:
		return h.gmType[b.refFrame[0]] == gmTranslation || h.gmType[b.refFrame[1]] == gmTranslation
	}
	return true
}

func (t *tile) interpCtx(dir int) int {
	b := &t.b
	ctx := (dir & 1) * 2 * 4
	if b.refFrame[1] > intraFrame {
		ctx += 4
	}
	leftType, aboveType := 3, 3
	if b.availL {
		m := t.mi(b.miRow, b.miCol-1)
		if int(m.refFrame[0]) == b.refFrame[0] || int(m.refFrame[1]) == b.refFrame[0] {
			leftType = int(m.interp[dir])
		}
	}
	if b.availU {
		m := t.mi(b.miRow-1, b.miCol)
		if int(m.refFrame[0]) == b.refFrame[0] || int(m.refFrame[1]) == b.refFrame[0] {
			aboveType = int(m.interp[dir])
		}
	}
	switch {
	case leftType == aboveType:
		ctx += leftType
	case leftType == 3:
		ctx += aboveType
	case aboveType == 3:
		ctx += leftType
	default:
		ctx += 3
	}
	return ctx
}

// compMode is get_mode: the single-reference mode of list i.
func compMode(yMode, i int) int {
	if i == 0 {
		switch {
		case yMode < nearestNearestMV:
			return yMode
		case yMode == newNewMV || yMode == newNearestMV || yMode == newNearMV:
			return newMV
		case yMode == nearestNearestMV || yMode == nearestNewMV:
			return nearestMV
		case yMode == nearNearMV || yMode == nearNewMV:
			return nearMV
		}
		return globalMV
	}
	switch {
	case yMode == newNewMV || yMode == nearestNewMV || yMode == nearNewMV:
		return newMV
	case yMode == nearestNearestMV || yMode == newNearestMV:
		return nearestMV
	case yMode == nearNearMV || yMode == newNearMV:
		return nearMV
	}
	return globalMV
}

func (t *tile) assignMv(compound bool) error {
	b := &t.b
	p := &t.mvp
	n := 1
	if compound {
		n = 2
	}
	for i := 0; i < n; i++ {
		var mode int
		if b.useIntraBC {
			mode = newMV
			p.predMv[0] = p.refStack[0][0]
			if p.predMv[0] == (mv{}) {
				p.predMv[0] = p.refStack[1][0]
			}
			if p.predMv[0] == (mv{}) {
				sb4 := num4x4H[t.sbSize()]
				if b.miRow-sb4 < t.miRowStart {
					p.predMv[0] = mv{0, int16(-(sb4*4 + 256) * 8)}
				} else {
					p.predMv[0] = mv{int16(-(sb4 * 4 * 8)), 0}
				}
			}
		} else {
			mode = compMode(b.yMode, i)
			if mode == globalMV {
				p.predMv[i] = p.globalMvs[i]
			} else {
				pos := p.refMvIdx
				if mode == nearestMV {
					pos = 0
				}
				if mode == newMV && p.numMvFound <= 1 {
					pos = 0
				}
				p.predMv[i] = p.refStack[pos][i]
			}
		}
		if mode == newMV {
			t.readMv(i)
		} else {
			b.mv[i] = p.predMv[i]
		}
	}
	return nil
}

func (t *tile) readMv(ref int) {
	b := &t.b
	ctx := 0
	if b.useIntraBC {
		ctx = 1
	}
	mc := &t.cdf.mv[ctx]
	var diff mv
	joint := t.ms.sym(mc.joints[:], 4)
	if joint == 2 || joint == 3 {
		diff.row = int16(t.readMvComponent(&mc.comp[0]))
	}
	if joint == 1 || joint == 3 {
		diff.col = int16(t.readMvComponent(&mc.comp[1]))
	}
	pm := t.mvp.predMv[ref]
	b.mv[ref] = mv{pm.row + diff.row, pm.col + diff.col}
}

func (t *tile) readMvComponent(c *mvCompCDF) int {
	h := t.h
	sign := t.ms.flag(c.sign[:])
	class := t.ms.sym(c.classes[:], 11)
	var mag int
	if class == 0 {
		c0 := t.ms.sym(c.class0[:], 2)
		fr, hp := 3, 1
		if !h.forceIntegerMV {
			fr = t.ms.sym(c.class0FP[c0][:], 4)
		}
		if h.allowHighPrecisionMV {
			hp = t.ms.sym(c.class0HP[:], 2)
		}
		mag = (c0<<3 | fr<<1 | hp) + 1
	} else {
		d := 0
		for i := 0; i < class; i++ {
			d |= t.ms.sym(c.bits[i][:], 2) << i
		}
		mag = 2 << (class + 2)
		fr, hp := 3, 1
		if !h.forceIntegerMV {
			fr = t.ms.sym(c.fp[:], 4)
		}
		if h.allowHighPrecisionMV {
			hp = t.ms.sym(c.hp[:], 2)
		}
		mag += (d<<3 | fr<<1 | hp) + 1
	}
	if sign {
		return -mag
	}
	return mag
}

func (t *tile) readInterIntra(compound bool) {
	b := &t.b
	c := &t.cdf
	b.interIntra = false
	if !b.skipMode && t.seq.EnableInterIntra && !compound && b.size >= block8x8 && b.size <= block32x32 {
		ctx := sizeGroup[b.size]
		b.interIntra = t.ms.flag(c.interIntra[ctx][:])
		if b.interIntra {
			b.interIntraMode = t.ms.sym(c.interIntraMode[ctx][:], 4)
			b.refFrame[1] = intraFrame
			b.angleDeltaY, b.angleDeltaUV = 0, 0
			b.useFilterIntra = false
			b.wedgeInterIntra = t.ms.flag(c.wedgeInterIntra[b.size][:])
			if b.wedgeInterIntra {
				b.wedgeIndex = t.ms.sym(c.wedgeIdx[b.size][:], 16)
				b.wedgeSign = 0
			}
		}
	}
}

func (t *tile) readMotionMode(compound bool) {
	b := &t.b
	h := t.h
	b.motionMode = motionSimple
	if b.skipMode || !h.switchableMotionMode || min(blockW(b.size), blockH(b.size)) < 8 {
		return
	}
	if !h.forceIntegerMV && (b.yMode == globalMV || b.yMode == globalGlobalMV) && h.gmType[b.refFrame[0]] > gmTranslation {
		return
	}
	if compound || b.refFrame[1] == intraFrame || !t.hasOverlappableCandidates() {
		return
	}
	t.findWarpSamples()
	p := &t.mvp
	if h.forceIntegerMV || p.numSamples == 0 || !h.allowWarpedMotion || t.f.scaled[b.refFrame[0]] {
		if t.ms.flag(t.cdf.obmc[b.size][:]) {
			b.motionMode = motionOBMC
		}
		return
	}
	b.motionMode = t.ms.sym(t.cdf.motionMode[b.size][:], 3)
}

func (t *tile) hasOverlappableCandidates() bool {
	b := &t.b
	f := t.f
	if b.availU {
		for x4 := b.miCol; x4 < min(f.miCols, b.miCol+b.bw4); x4 += 2 {
			x5 := min(x4|1, f.miCols-1)
			if t.mi(b.miRow-1, x5).refFrame[0] > intraFrame {
				return true
			}
		}
	}
	if b.availL {
		for y4 := b.miRow; y4 < min(f.miRows, b.miRow+b.bh4); y4 += 2 {
			y5 := min(y4|1, f.miRows-1)
			if t.mi(y5, b.miCol-1).refFrame[0] > intraFrame {
				return true
			}
		}
	}
	return false
}

func (t *tile) readCompoundType(compound bool) {
	b := &t.b
	c := &t.cdf
	b.compGroupIdx, b.compoundIdx = 0, 1
	if b.skipMode {
		b.compoundType = compoundAverage
		return
	}
	if !compound {
		switch {
		case b.interIntra && b.wedgeInterIntra:
			b.compoundType = compoundWedge
		case b.interIntra:
			b.compoundType = compoundIntra
		default:
			b.compoundType = compoundAverage
		}
		return
	}
	n := wedgeBits[b.size]
	if t.seq.EnableMaskedCompound {
		b.compGroupIdx = t.ms.sym(c.compGroupIdx[t.compGroupIdxCtx()][:], 2)
	}
	if b.compGroupIdx == 0 {
		b.compoundType = compoundAverage
		if t.seq.EnableJntComp {
			b.compoundIdx = t.ms.sym(c.compoundIdx[t.compoundIdxCtx()][:], 2)
			if b.compoundIdx == 0 {
				b.compoundType = compoundDistance
			}
		}
	} else if n == 0 {
		b.compoundType = compoundDiffWtd
	} else {
		b.compoundType = t.ms.sym(c.compoundType[b.size][:], 2)
	}
	switch b.compoundType {
	case compoundWedge:
		b.wedgeIndex = t.ms.sym(c.wedgeIdx[b.size][:], 16)
		b.wedgeSign = t.ms.bit()
	case compoundDiffWtd:
		b.maskType = t.ms.bit()
	}
}

func (t *tile) compGroupIdxCtx() int {
	b := &t.b
	p := &t.mvp
	ctx := 0
	if b.availU {
		if !p.aboveSingle {
			ctx += int(t.mi(b.miRow-1, b.miCol).compGroupIdx)
		} else if p.aboveRef[0] == altrefFrame {
			ctx += 3
		}
	}
	if b.availL {
		if !p.leftSingle {
			ctx += int(t.mi(b.miRow, b.miCol-1).compGroupIdx)
		} else if p.leftRef[0] == altrefFrame {
			ctx += 3
		}
	}
	return min(5, ctx)
}

func (t *tile) compoundIdxCtx() int {
	b := &t.b
	h := t.h
	p := &t.mvp
	s := t.seq
	fwd := iabs(s.relDist(h.orderHints[b.refFrame[0]], h.OrderHint))
	bck := iabs(s.relDist(h.orderHints[b.refFrame[1]], h.OrderHint))
	ctx := 0
	if fwd == bck {
		ctx = 3
	}
	if b.availU {
		if !p.aboveSingle {
			ctx += int(t.mi(b.miRow-1, b.miCol).compoundIdx)
		} else if p.aboveRef[0] == altrefFrame {
			ctx++
		}
	}
	if b.availL {
		if !p.leftSingle {
			ctx += int(t.mi(b.miRow, b.miCol-1).compoundIdx)
		} else if p.leftRef[0] == altrefFrame {
			ctx++
		}
	}
	return ctx
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// intraBCMode is the intra block copy part of intra_frame_mode_info.
func (t *tile) intraBCMode() error {
	b := &t.b
	b.motionMode = motionSimple
	b.compoundType = compoundAverage
	b.paletteSizeY, b.paletteSizeUV = 0, 0
	b.yMode, b.uvMode = dcPred, dcPred
	t.findMvStack(false)
	return t.assignMv(false)
}
