package av1

import "unsafe"

// CDFs are stored inverted (32768 - cdf, as libaom and dav1d do): an
// N-symbol CDF holds N-1 decreasing values, a 0 and the adaptation
// counter at index N; arrays sized for more symbols are zero-padded.

type mvCompCDF struct {
	classes  [12]uint16
	class0FP [2][5]uint16
	fp       [5]uint16
	sign     [3]uint16
	class0HP [3]uint16
	hp       [3]uint16
	class0   [3]uint16
	bits     [10][3]uint16
}

type mvCDF struct {
	joints [5]uint16
	comp   [2]mvCompCDF
}

// cdfContext is the complete adaptive state of the entropy decoder
// (spec 7.20 "save_cdfs": everything a frame saves and loads).
type cdfContext struct {
	kfYMode           [5][5][14]uint16
	yMode             [4][14]uint16
	uvMode            [2][13][15]uint16
	partition         [20][11]uint16
	angleDelta        [8][8]uint16
	paletteYSize      [7][8]uint16
	paletteUVSize     [7][8]uint16
	paletteYMode      [7][3][3]uint16
	paletteUVMode     [2][3]uint16
	paletteYColor     [7][5][9]uint16
	paletteUVColor    [7][5][9]uint16
	compInter         [5][3]uint16
	compRefType       [5][3]uint16
	uniCompRef        [3][3][3]uint16
	compRef           [3][3][3]uint16
	compBwdRef        [3][2][3]uint16
	singleRef         [3][6][3]uint16
	txfmSplit         [21][3]uint16
	compoundIdx       [6][3]uint16
	compGroupIdx      [6][3]uint16
	newMV             [6][3]uint16
	zeroMV            [2][3]uint16
	refMV             [6][3]uint16
	drl               [3][3]uint16
	motionMode        [22][4]uint16
	obmc              [22][3]uint16
	compoundMode      [8][9]uint16
	compoundType      [22][3]uint16
	wedgeIdx          [22][17]uint16
	interIntra        [4][3]uint16
	wedgeInterIntra   [22][3]uint16
	interIntraMode    [4][5]uint16
	segmentPred       [3][3]uint16
	segmentID         [3][9]uint16
	filterIntra       [22][3]uint16
	filterIntraMode   [6]uint16
	switchableRestore [4]uint16
	wienerRestore     [3]uint16
	sgrprojRestore    [3]uint16
	interpFilter      [16][4]uint16
	skipMode          [3][3]uint16
	skip              [3][3]uint16
	isInter           [4][3]uint16
	txSize            [4][3][4]uint16
	deltaQ            [5]uint16
	deltaLF           [5]uint16
	deltaLFMulti      [4][5]uint16
	cflSign           [9]uint16
	cflAlpha          [6][17]uint16
	intraBC           [3]uint16
	intraTxType       [3][4][13][17]uint16
	interTxType       [4][4][17]uint16
	mv                [2]mvCDF // [1] is the intra block copy context

	txbSkip      [5][13][3]uint16
	eobExtra     [5][2][9][3]uint16
	dcSign       [2][3][3]uint16
	coeffBr      [4][2][21][5]uint16
	coeffBase    [5][2][42][5]uint16
	coeffBaseEob [5][2][4][4]uint16
	eobPt16      [2][2][6]uint16
	eobPt32      [2][2][7]uint16
	eobPt64      [2][2][8]uint16
	eobPt128     [2][2][9]uint16
	eobPt256     [2][2][10]uint16
	eobPt512     [2][2][11]uint16
	eobPt1024    [2][2][12]uint16
}

// initNonCoeff loads the default non-coefficient CDFs
// (init_non_coeff_cdfs).
func (c *cdfContext) initNonCoeff() {
	c.kfYMode = defKfYMode
	c.yMode = defYMode
	c.uvMode = defUVMode
	c.partition = defPartition
	c.angleDelta = defAngleDelta
	c.paletteYSize = defPaletteYSize
	c.paletteUVSize = defPaletteUVSize
	c.paletteYMode = defPaletteYMode
	c.paletteUVMode = defPaletteUVMode
	c.paletteYColor = defPaletteYColor
	c.paletteUVColor = defPaletteUVColor
	c.compInter = defCompInter
	c.compRefType = defCompRefType
	c.uniCompRef = defUniCompRef
	c.compRef = defCompRef
	c.compBwdRef = defCompBwdRef
	c.singleRef = defSingleRef
	c.txfmSplit = defTxfmSplit
	c.compoundIdx = defCompoundIdx
	c.compGroupIdx = defCompGroupIdx
	c.newMV = defNewMV
	c.zeroMV = defZeroMV
	c.refMV = defRefMV
	c.drl = defDRL
	c.motionMode = defMotionMode
	c.obmc = defOBMC
	c.compoundMode = defCompoundMode
	c.compoundType = defCompoundType
	c.wedgeIdx = defWedgeIdx
	c.interIntra = defInterIntra
	c.wedgeInterIntra = defWedgeInterIntra
	c.interIntraMode = defInterIntraMode
	c.segmentPred = defSegmentPred
	c.segmentID = defSegmentID
	c.filterIntra = defFilterIntra
	c.filterIntraMode = defFilterIntraMode
	c.switchableRestore = defSwitchableRestore
	c.wienerRestore = defWienerRestore
	c.sgrprojRestore = defSgrprojRestore
	c.interpFilter = defInterpFilter
	c.skipMode = defSkipMode
	c.skip = defSkip
	c.isInter = defIsInter
	c.txSize = defTxSize
	c.deltaQ = defDeltaQ
	c.deltaLF = defDeltaLF
	c.deltaLFMulti = defDeltaLFMulti
	c.cflSign = defCflSign
	c.cflAlpha = defCflAlpha
	c.intraBC = defIntraBC
	c.intraTxType = defIntraTxType
	c.interTxType = defInterTxType
	for i := range c.mv {
		*(*[143]uint16)(unsafe.Pointer(&c.mv[i])) = defMV
	}
}

// initCoeff loads the default coefficient CDFs for a base_q_idx
// (init_coeff_cdfs).
func (c *cdfContext) initCoeff(baseQIdx int) {
	q := 3
	switch {
	case baseQIdx <= 20:
		q = 0
	case baseQIdx <= 60:
		q = 1
	case baseQIdx <= 120:
		q = 2
	}
	c.txbSkip = defTxbSkip[q]
	c.eobExtra = defEobExtra[q]
	c.dcSign = defDcSign[q]
	c.coeffBr = defCoeffBr[q]
	c.coeffBase = defCoeffBase[q]
	c.coeffBaseEob = defCoeffBaseEob[q]
	c.eobPt16 = defEobPt16[q]
	c.eobPt32 = defEobPt32[q]
	c.eobPt64 = defEobPt64[q]
	c.eobPt128 = defEobPt128[q]
	c.eobPt256 = defEobPt256[q]
	c.eobPt512 = defEobPt512[q]
	c.eobPt1024 = defEobPt1024[q]
}

// resetCounts zeroes every adaptation counter. The context is one run of
// inverted CDFs: in each, the first 0 terminates the values (no other
// entry can be 0) and the counter follows it.
func (c *cdfContext) resetCounts() {
	a := unsafe.Slice((*uint16)(unsafe.Pointer(c)), unsafe.Sizeof(*c)/2)
	for i := 0; i < len(a); i++ {
		if a[i] != 0 {
			continue
		}
		if i > 0 && a[i-1] != 0 && i+1 < len(a) {
			a[i+1] = 0
			i++
		}
	}
}
