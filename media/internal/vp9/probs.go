package vp9

// Probability contexts, their defaults, the compressed header's delta
// updates and the backward adaptation at the end of a frame (libvpx
// vp9_entropy*.c, vp9_decodeframe.c read_compressed_header).

type mvComponentProbs struct {
	sign     uint8
	classes  [10]uint8
	class0   [1]uint8
	bits     [10]uint8
	class0FP [2][3]uint8
	fp       [3]uint8
	class0HP uint8
	hp       uint8
}

type frameContext struct {
	yMode            [4][9]uint8
	uvMode           [10][9]uint8
	partition        [16][3]uint8
	coef             [4][2][2][6][6][3]uint8
	switchableInterp [4][2]uint8
	interMode        [7][3]uint8
	intraInter       [4]uint8
	compInter        [5]uint8
	singleRef        [5][2]uint8
	compRef          [5]uint8
	tx8              [2][1]uint8
	tx16             [2][2]uint8
	tx32             [2][3]uint8
	skip             [3]uint8
	mvJoints         [3]uint8
	mvComp           [2]mvComponentProbs
}

type mvComponentCounts struct {
	sign     [2]uint32
	classes  [11]uint32
	class0   [2]uint32
	bits     [10][2]uint32
	class0FP [2][4]uint32
	fp       [4]uint32
	class0HP [2]uint32
	hp       [2]uint32
}

type frameCounts struct {
	yMode            [4][10]uint32
	uvMode           [10][10]uint32
	partition        [16][4]uint32
	coef             [4][2][2][6][6][4]uint32
	eobBranch        [4][2][2][6][6]uint32
	switchableInterp [4][3]uint32
	interMode        [7][4]uint32
	intraInter       [4][2]uint32
	compInter        [5][2]uint32
	singleRef        [5][2][2]uint32
	compRef          [5][2]uint32
	tx32             [2][4]uint32
	tx16             [2][3]uint32
	tx8              [2][2]uint32
	skip             [3][2]uint32
	mvJoints         [4]uint32
	mvComp           [2]mvComponentCounts
}

func (c *frameCounts) add(o *frameCounts) {
	addU32(c.yMode[:], o.yMode[:])
	addU32(c.uvMode[:], o.uvMode[:])
	addU32(c.partition[:], o.partition[:])
	for a := range c.coef {
		for b := range c.coef[a] {
			for x := range c.coef[a][b] {
				for y := range c.coef[a][b][x] {
					for z := range c.coef[a][b][x][y] {
						for t := range c.coef[a][b][x][y][z] {
							c.coef[a][b][x][y][z][t] += o.coef[a][b][x][y][z][t]
						}
						c.eobBranch[a][b][x][y][z] += o.eobBranch[a][b][x][y][z]
					}
				}
			}
		}
	}
	addU32(c.switchableInterp[:], o.switchableInterp[:])
	addU32(c.interMode[:], o.interMode[:])
	addU32(c.intraInter[:], o.intraInter[:])
	addU32(c.compInter[:], o.compInter[:])
	for i := range c.singleRef {
		addU32(c.singleRef[i][:], o.singleRef[i][:])
	}
	addU32(c.compRef[:], o.compRef[:])
	addU32(c.tx32[:], o.tx32[:])
	addU32(c.tx16[:], o.tx16[:])
	addU32(c.tx8[:], o.tx8[:])
	addU32(c.skip[:], o.skip[:])
	for i := range c.mvJoints {
		c.mvJoints[i] += o.mvJoints[i]
	}
	for i := range c.mvComp {
		a, b := &c.mvComp[i], &o.mvComp[i]
		for j := range a.sign {
			a.sign[j] += b.sign[j]
			a.class0[j] += b.class0[j]
			a.class0HP[j] += b.class0HP[j]
			a.hp[j] += b.hp[j]
		}
		for j := range a.classes {
			a.classes[j] += b.classes[j]
		}
		addU32(a.bits[:], b.bits[:])
		addU32(a.class0FP[:], b.class0FP[:])
		for j := range a.fp {
			a.fp[j] += b.fp[j]
		}
	}
}

func addU32[T ~[2]uint32 | ~[3]uint32 | ~[4]uint32 | ~[10]uint32](dst, src []T) {
	for i := range dst {
		for j := range len(dst[i]) {
			dst[i][j] += src[i][j]
		}
	}
}

var defIntraInterProb = [4]uint8{9, 102, 187, 225}
var defSwitchableInterpProb = [4][2]uint8{{235, 162}, {36, 255}, {34, 3}, {149, 144}}

func defaultFrameContext() frameContext {
	var fc frameContext
	fc.yMode = defIfYProbs
	fc.uvMode = defIfUVProbs
	fc.partition = defPartitionProbs
	fc.coef[0] = defCoef4
	fc.coef[1] = defCoef8
	fc.coef[2] = defCoef16
	fc.coef[3] = defCoef32
	fc.switchableInterp = defSwitchableInterpProb
	fc.interMode = defInterModeProbs
	fc.intraInter = defIntraInterProb
	fc.compInter = defCompInterProb
	fc.singleRef = defSingleRefProb
	fc.compRef = defCompRefProb
	fc.tx8 = [2][1]uint8{{100}, {66}}
	fc.tx16 = [2][2]uint8{{20, 152}, {15, 101}}
	fc.tx32 = [2][3]uint8{{3, 136, 37}, {5, 52, 13}}
	fc.skip = defSkipProb
	n := defNMV[:]
	copy(fc.mvJoints[:], n[:3])
	n = n[3:]
	for i := range fc.mvComp {
		c := &fc.mvComp[i]
		c.sign = n[0]
		copy(c.classes[:], n[1:11])
		c.class0[0] = n[11]
		copy(c.bits[:], n[12:22])
		copy(c.class0FP[0][:], n[22:25])
		copy(c.class0FP[1][:], n[25:28])
		copy(c.fp[:], n[28:31])
		c.class0HP = n[31]
		c.hp = n[32]
		n = n[33:]
	}
	return fc
}

// --- compressed header delta updates ---

func invRecenterNonneg(v, m int) int {
	if v > 2*m {
		return v
	}
	if v&1 != 0 {
		return m - ((v + 1) >> 1)
	}
	return m + (v >> 1)
}

func invRemapProb(v int, m uint8) uint8 {
	if v >= len(invMapTable) {
		v = len(invMapTable) - 1
	}
	v = int(invMapTable[v])
	mm := int(m) - 1
	if mm<<1 <= 255 {
		return uint8(1 + invRecenterNonneg(v, mm))
	}
	return uint8(255 - invRecenterNonneg(v, 254-mm))
}

func (d *boolDecoder) decodeTermSubexp() int {
	if d.readBit() == 0 {
		return d.literal(4)
	}
	if d.readBit() == 0 {
		return d.literal(4) + 16
	}
	if d.readBit() == 0 {
		return d.literal(5) + 32
	}
	v := d.literal(7)
	if v < 65 {
		return v + 64
	}
	return (v << 1) - 65 + d.readBit() + 64
}

func (d *boolDecoder) diffUpdate(p *uint8) {
	if d.read(252) != 0 {
		*p = invRemapProb(d.decodeTermSubexp(), *p)
	}
}

func (d *boolDecoder) mvUpdate(p *uint8) {
	if d.read(252) != 0 {
		*p = uint8(d.literal(7)<<1 | 1)
	}
}

// readCompressedHeader parses the compressed header into h and fc.
func (dec *Decoder) readCompressedHeader(d *boolDecoder, h *frameHeader, fc *frameContext) {
	// tx_mode
	if h.lossless {
		h.txMode = only4x4
	} else {
		h.txMode = d.literal(2)
		if h.txMode == allow32x32 {
			h.txMode += d.readBit()
		}
	}
	if h.txMode == txModeSelect {
		for i := 0; i < 2; i++ {
			d.diffUpdate(&fc.tx8[i][0])
		}
		for i := 0; i < 2; i++ {
			for j := 0; j < 2; j++ {
				d.diffUpdate(&fc.tx16[i][j])
			}
		}
		for i := 0; i < 2; i++ {
			for j := 0; j < 3; j++ {
				d.diffUpdate(&fc.tx32[i][j])
			}
		}
	}
	maxTx := txModeToBiggest[h.txMode]
	for tx := 0; tx <= maxTx; tx++ {
		if d.readBit() == 0 {
			continue
		}
		for i := 0; i < 2; i++ {
			for j := 0; j < 2; j++ {
				for k := 0; k < 6; k++ {
					nctx := 6
					if k == 0 {
						nctx = 3
					}
					for l := 0; l < nctx; l++ {
						for m := 0; m < 3; m++ {
							d.diffUpdate(&fc.coef[tx][i][j][k][l][m])
						}
					}
				}
			}
		}
	}
	for i := range fc.skip {
		d.diffUpdate(&fc.skip[i])
	}
	if h.intraOnly() {
		return
	}
	for i := range fc.interMode {
		for j := range fc.interMode[i] {
			d.diffUpdate(&fc.interMode[i][j])
		}
	}
	if h.interpFilter == filterSwitchable {
		for j := range fc.switchableInterp {
			for i := range fc.switchableInterp[j] {
				d.diffUpdate(&fc.switchableInterp[j][i])
			}
		}
	}
	for i := range fc.intraInter {
		d.diffUpdate(&fc.intraInter[i])
	}
	// reference mode
	h.refMode = singleReference
	if h.compoundAllowed() {
		if d.readBit() != 0 {
			h.refMode = compoundReference
			if d.readBit() != 0 {
				h.refMode = referenceModeSelect
			}
		}
	}
	h.setupCompoundRefs()
	if h.refMode == referenceModeSelect {
		for i := range fc.compInter {
			d.diffUpdate(&fc.compInter[i])
		}
	}
	if h.refMode != compoundReference {
		for i := range fc.singleRef {
			d.diffUpdate(&fc.singleRef[i][0])
			d.diffUpdate(&fc.singleRef[i][1])
		}
	}
	if h.refMode != singleReference {
		for i := range fc.compRef {
			d.diffUpdate(&fc.compRef[i])
		}
	}
	for j := range fc.yMode {
		for i := range fc.yMode[j] {
			d.diffUpdate(&fc.yMode[j][i])
		}
	}
	for j := range fc.partition {
		for i := range fc.partition[j] {
			d.diffUpdate(&fc.partition[j][i])
		}
	}
	// mv probs
	for i := range fc.mvJoints {
		d.mvUpdate(&fc.mvJoints[i])
	}
	for i := 0; i < 2; i++ {
		c := &fc.mvComp[i]
		d.mvUpdate(&c.sign)
		for j := range c.classes {
			d.mvUpdate(&c.classes[j])
		}
		d.mvUpdate(&c.class0[0])
		for j := range c.bits {
			d.mvUpdate(&c.bits[j])
		}
	}
	for i := 0; i < 2; i++ {
		c := &fc.mvComp[i]
		for j := 0; j < 2; j++ {
			for k := 0; k < 3; k++ {
				d.mvUpdate(&c.class0FP[j][k])
			}
		}
		for k := 0; k < 3; k++ {
			d.mvUpdate(&c.fp[k])
		}
	}
	if h.allowHP {
		for i := 0; i < 2; i++ {
			c := &fc.mvComp[i]
			d.mvUpdate(&c.class0HP)
			d.mvUpdate(&c.hp)
		}
	}
}

// --- backward adaptation ---

func clipProb(p int) uint8 {
	if p > 255 {
		return 255
	}
	if p < 1 {
		return 1
	}
	return uint8(p)
}

func getProb(num, den uint32) uint8 {
	if den == 0 {
		return 128
	}
	return clipProb(int((uint64(num)*256 + uint64(den>>1)) / uint64(den)))
}

func weightedProb(a, b uint8, factor int) uint8 {
	return uint8((int(a)*(256-factor) + int(b)*factor + 128) >> 8)
}

func mergeProbs(pre uint8, c0, c1 uint32, countSat, maxUpdate int) uint8 {
	prob := getProb(c0, c0+c1)
	count := min(int(c0+c1), countSat)
	factor := maxUpdate * count / countSat
	return weightedProb(pre, prob, factor)
}

var countToUpdateFactor = [21]int{0, 6, 12, 19, 25, 32, 38, 44, 51, 57, 64, 70, 76, 83, 89, 96, 102, 108, 115, 121, 128}

func modeMVMergeProbs(pre uint8, c0, c1 uint32) uint8 {
	den := c0 + c1
	if den == 0 {
		return pre
	}
	count := min(int(den), 20)
	return weightedProb(pre, getProb(c0, den), countToUpdateFactor[count])
}

func treeMergeProbs(tree []int8, pre []uint8, counts []uint32, probs []uint8) {
	treeMergeImpl(0, tree, pre, counts, probs)
}

func treeMergeImpl(i int, tree []int8, pre []uint8, counts []uint32, probs []uint8) uint32 {
	var lc, rc uint32
	if l := int(tree[i]); l <= 0 {
		lc = counts[-l]
	} else {
		lc = treeMergeImpl(l, tree, pre, counts, probs)
	}
	if r := int(tree[i+1]); r <= 0 {
		rc = counts[-r]
	} else {
		rc = treeMergeImpl(r, tree, pre, counts, probs)
	}
	probs[i>>1] = modeMVMergeProbs(pre[i>>1], lc, rc)
	return lc + rc
}

func adaptCoefProbs(fc, pre *frameContext, c *frameCounts, frameIsIntra, lastWasKey bool) {
	countSat, update := 24, 112
	if !frameIsIntra && lastWasKey {
		update = 128
	}
	for t := 0; t < 4; t++ {
		for i := 0; i < 2; i++ {
			for j := 0; j < 2; j++ {
				for k := 0; k < 6; k++ {
					for l := 0; l < 6; l++ {
						if k == 0 && l >= 3 {
							continue
						}
						cc := &c.coef[t][i][j][k][l]
						n0, n1, n2, neob := cc[0], cc[1], cc[2], cc[3]
						eb := c.eobBranch[t][i][j][k][l]
						p := &fc.coef[t][i][j][k][l]
						pp := &pre.coef[t][i][j][k][l]
						p[0] = mergeProbs(pp[0], neob, eb-neob, countSat, update)
						p[1] = mergeProbs(pp[1], n0, n1+n2, countSat, update)
						p[2] = mergeProbs(pp[2], n1, n2, countSat, update)
					}
				}
			}
		}
	}
}

func adaptModeProbs(fc, pre *frameContext, c *frameCounts, h *frameHeader) {
	for i := range fc.intraInter {
		fc.intraInter[i] = modeMVMergeProbs(pre.intraInter[i], c.intraInter[i][0], c.intraInter[i][1])
	}
	for i := range fc.compInter {
		fc.compInter[i] = modeMVMergeProbs(pre.compInter[i], c.compInter[i][0], c.compInter[i][1])
	}
	for i := range fc.compRef {
		fc.compRef[i] = modeMVMergeProbs(pre.compRef[i], c.compRef[i][0], c.compRef[i][1])
	}
	for i := range fc.singleRef {
		for j := 0; j < 2; j++ {
			fc.singleRef[i][j] = modeMVMergeProbs(pre.singleRef[i][j], c.singleRef[i][j][0], c.singleRef[i][j][1])
		}
	}
	for i := range fc.interMode {
		treeMergeProbs(interModeTree[:], pre.interMode[i][:], c.interMode[i][:], fc.interMode[i][:])
	}
	for i := range fc.yMode {
		treeMergeProbs(intraModeTree[:], pre.yMode[i][:], c.yMode[i][:], fc.yMode[i][:])
	}
	for i := range fc.uvMode {
		treeMergeProbs(intraModeTree[:], pre.uvMode[i][:], c.uvMode[i][:], fc.uvMode[i][:])
	}
	for i := range fc.partition {
		treeMergeProbs(partitionTree[:], pre.partition[i][:], c.partition[i][:], fc.partition[i][:])
	}
	if h.interpFilter == filterSwitchable {
		for i := range fc.switchableInterp {
			treeMergeProbs(switchableInterpTree[:], pre.switchableInterp[i][:], c.switchableInterp[i][:], fc.switchableInterp[i][:])
		}
	}
	if h.txMode == txModeSelect {
		for i := 0; i < 2; i++ {
			t8 := c.tx8[i]
			fc.tx8[i][0] = modeMVMergeProbs(pre.tx8[i][0], t8[0], t8[1])
			t16 := c.tx16[i]
			fc.tx16[i][0] = modeMVMergeProbs(pre.tx16[i][0], t16[0], t16[1]+t16[2])
			fc.tx16[i][1] = modeMVMergeProbs(pre.tx16[i][1], t16[1], t16[2])
			t32 := c.tx32[i]
			fc.tx32[i][0] = modeMVMergeProbs(pre.tx32[i][0], t32[0], t32[1]+t32[2]+t32[3])
			fc.tx32[i][1] = modeMVMergeProbs(pre.tx32[i][1], t32[1], t32[2]+t32[3])
			fc.tx32[i][2] = modeMVMergeProbs(pre.tx32[i][2], t32[2], t32[3])
		}
	}
	for i := range fc.skip {
		fc.skip[i] = modeMVMergeProbs(pre.skip[i], c.skip[i][0], c.skip[i][1])
	}
}

func adaptMVProbs(fc, pre *frameContext, c *frameCounts, allowHP bool) {
	treeMergeProbs(mvJointTree[:], pre.mvJoints[:], c.mvJoints[:], fc.mvJoints[:])
	for i := 0; i < 2; i++ {
		p, pp, cc := &fc.mvComp[i], &pre.mvComp[i], &c.mvComp[i]
		p.sign = modeMVMergeProbs(pp.sign, cc.sign[0], cc.sign[1])
		treeMergeProbs(mvClassTree[:], pp.classes[:], cc.classes[:], p.classes[:])
		treeMergeProbs(mvClass0Tree[:], pp.class0[:], cc.class0[:], p.class0[:])
		for j := range p.bits {
			p.bits[j] = modeMVMergeProbs(pp.bits[j], cc.bits[j][0], cc.bits[j][1])
		}
		for j := 0; j < 2; j++ {
			treeMergeProbs(mvFPTree[:], pp.class0FP[j][:], cc.class0FP[j][:], p.class0FP[j][:])
		}
		treeMergeProbs(mvFPTree[:], pp.fp[:], cc.fp[:], p.fp[:])
		if allowHP {
			p.class0HP = modeMVMergeProbs(pp.class0HP, cc.class0HP[0], cc.class0HP[1])
			p.hp = modeMVMergeProbs(pp.hp, cc.hp[0], cc.hp[1])
		}
	}
}
