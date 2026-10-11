package av1

// Block sizes.
const (
	block4x4 = iota
	block4x8
	block8x4
	block8x8
	block8x16
	block16x8
	block16x16
	block16x32
	block32x16
	block32x32
	block32x64
	block64x32
	block64x64
	block64x128
	block128x64
	block128x128
	block4x16
	block16x4
	block8x32
	block32x8
	block16x64
	block64x16
	blockSizes
	blockInvalid = blockSizes
)

var (
	num4x4W    = [blockSizes]int{1, 1, 2, 2, 2, 4, 4, 4, 8, 8, 8, 16, 16, 16, 32, 32, 1, 4, 2, 8, 4, 16}
	num4x4H    = [blockSizes]int{1, 2, 1, 2, 4, 2, 4, 8, 4, 8, 16, 8, 16, 32, 16, 32, 4, 1, 8, 2, 16, 4}
	miWLog2    = [blockSizes]int{0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5, 0, 2, 1, 3, 2, 4}
	miHLog2    = [blockSizes]int{0, 1, 0, 1, 2, 1, 2, 3, 2, 3, 4, 3, 4, 5, 4, 5, 2, 0, 3, 1, 4, 2}
	sizeGroup  = [blockSizes]int{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 3, 3, 3, 3, 0, 0, 1, 1, 2, 2}
	maxTxDepth = [blockSizes]int{0, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4, 4, 4, 4, 2, 2, 3, 3, 4, 4}
)

func blockW(b int) int { return num4x4W[b] * 4 }
func blockH(b int) int { return num4x4H[b] * 4 }

// sizeOf returns the block size of w x h samples, or blockInvalid.
func sizeOf(w, h int) int {
	for b := 0; b < blockSizes; b++ {
		if blockW(b) == w && blockH(b) == h {
			return b
		}
	}
	return blockInvalid
}

// Partition types.
const (
	partNone = iota
	partHorz
	partVert
	partSplit
	partHorzA
	partHorzB
	partVertA
	partVertB
	partHorz4
	partVert4
)

var partitionSubsize [10][blockSizes]int

// planeResidual is get_plane_residual_size for 4:2:0 chroma
// (Subsampled_Size[b][1][1]).
var chromaSize [blockSizes]int

// TX sizes.
const (
	tx4x4 = iota
	tx8x8
	tx16x16
	tx32x32
	tx64x64
	tx4x8
	tx8x4
	tx8x16
	tx16x8
	tx16x32
	tx32x16
	tx32x64
	tx64x32
	tx4x16
	tx16x4
	tx8x32
	tx32x8
	tx16x64
	tx64x16
	txSizesAll
)

var (
	txW     = [txSizesAll]int{4, 8, 16, 32, 64, 4, 8, 8, 16, 16, 32, 32, 64, 4, 16, 8, 32, 16, 64}
	txH     = [txSizesAll]int{4, 8, 16, 32, 64, 8, 4, 16, 8, 32, 16, 64, 32, 16, 4, 32, 8, 64, 16}
	txWLog2 = [txSizesAll]int{2, 3, 4, 5, 6, 2, 3, 3, 4, 4, 5, 5, 6, 2, 4, 3, 5, 4, 6}
	txHLog2 = [txSizesAll]int{2, 3, 4, 5, 6, 3, 2, 4, 3, 5, 4, 6, 5, 4, 2, 5, 3, 6, 4}
	// txSqr / txSqrUp: the square size of the smaller / larger side.
	txSqr      = [txSizesAll]int{0, 1, 2, 3, 4, 0, 0, 1, 1, 2, 2, 3, 3, 0, 0, 1, 1, 2, 2}
	txSqrUp    = [txSizesAll]int{0, 1, 2, 3, 4, 1, 1, 2, 2, 3, 3, 4, 4, 2, 2, 3, 3, 4, 4}
	splitTx    = [txSizesAll]int{tx4x4, tx4x4, tx8x8, tx16x16, tx32x32, tx4x4, tx4x4, tx8x8, tx8x8, tx16x16, tx16x16, tx32x32, tx32x32, tx4x8, tx8x4, tx8x16, tx16x8, tx16x32, tx32x16}
	adjustedTx = [txSizesAll]int{tx4x4, tx8x8, tx16x16, tx32x32, tx32x32, tx4x8, tx8x4, tx8x16, tx16x8, tx16x32, tx32x16, tx32x32, tx32x32, tx4x16, tx16x4, tx8x32, tx32x8, tx16x32, tx32x16}
)

// maxTxRect is Max_Tx_Size_Rect: the largest transform of a block.
var maxTxRect [blockSizes]int

// txSizeOf is find_tx_size.
func txSizeOf(w, h int) int {
	for t := 0; t < txSizesAll; t++ {
		if txW[t] == w && txH[t] == h {
			return t
		}
	}
	return tx4x4
}

func init() {
	for b := 0; b < blockSizes; b++ {
		w, h := blockW(b), blockH(b)
		maxTxRect[b] = txSizeOf(min(w, 64), min(h, 64))
		chromaSize[b] = sizeOf(max(w/2, 4), max(h/2, 4))
		for p := range partitionSubsize {
			partitionSubsize[p][b] = blockInvalid
		}
		partitionSubsize[partNone][b] = b
		if w != h || w < 8 {
			continue
		}
		ps := &partitionSubsize
		ps[partNone][b] = b
		ps[partHorz][b] = sizeOf(w, h/2)
		ps[partVert][b] = sizeOf(w/2, h)
		ps[partSplit][b] = sizeOf(w/2, h/2)
		if w >= 16 {
			ps[partHorzA][b], ps[partHorzB][b] = sizeOf(w, h/2), sizeOf(w, h/2)
			ps[partVertA][b], ps[partVertB][b] = sizeOf(w/2, h), sizeOf(w/2, h)
			if w <= 64 {
				ps[partHorz4][b] = sizeOf(w, h/4)
				ps[partVert4][b] = sizeOf(w/4, h)
			}
		}
	}
}

// Intra prediction modes.
const (
	dcPred = iota
	vPred
	hPred
	d45Pred
	d135Pred
	d113Pred
	d157Pred
	d203Pred
	d67Pred
	smoothPred
	smoothVPred
	smoothHPred
	paethPred
	uvCflPred
	intraModes = 13
)

// Inter prediction modes (YMode values of inter blocks).
const (
	nearestMV = 13 + iota
	nearMV
	globalMV
	newMV
	nearestNearestMV
	nearNearMV
	nearestNewMV
	newNearestMV
	nearNewMV
	newNearMV
	globalGlobalMV
	newNewMV
)

var intraModeContext = [intraModes]int{0, 1, 2, 3, 4, 4, 4, 4, 3, 0, 1, 2, 0}

func isDirectional(m int) bool { return m >= vPred && m <= d67Pred }

// Transform types.
const (
	dctDct = iota
	adstDct
	dctAdst
	adstAdst
	flipadstDct
	dctFlipadst
	flipadstFlipadst
	adstFlipadst
	flipadstAdst
	idtx
	vDct
	hDct
	vAdst
	hAdst
	vFlipadst
	hFlipadst
)

var (
	txTypeIntraInv1 = []int{idtx, dctDct, vDct, hDct, adstAdst, adstDct, dctAdst}
	txTypeIntraInv2 = []int{idtx, dctDct, adstAdst, adstDct, dctAdst}
	txTypeInterInv1 = []int{idtx, vDct, hDct, vAdst, hAdst, vFlipadst, hFlipadst, dctDct, adstDct, dctAdst, flipadstDct, dctFlipadst, adstAdst, flipadstFlipadst, adstFlipadst, flipadstAdst}
	txTypeInterInv2 = []int{idtx, vDct, hDct, dctDct, adstDct, dctAdst, flipadstDct, dctFlipadst, adstAdst, flipadstFlipadst, adstFlipadst, flipadstAdst}
	txTypeInterInv3 = []int{idtx, dctDct}

	// txTypeInSet[inter][set] is a bit mask of the set's types.
	txTypeInSet [2][4]uint32

	modeToTxfm = [14]int{dctDct, adstDct, dctAdst, dctDct, adstAdst, adstDct, dctAdst, dctAdst, adstDct, adstAdst, adstDct, dctAdst, adstAdst, dctDct}
)

func init() {
	mask := func(s []int) (m uint32) {
		for _, t := range s {
			m |= 1 << t
		}
		return
	}
	txTypeInSet[0] = [4]uint32{1 << dctDct, mask(txTypeIntraInv1), mask(txTypeIntraInv2), 0}
	txTypeInSet[1] = [4]uint32{1 << dctDct, mask(txTypeInterInv1), mask(txTypeInterInv2), mask(txTypeInterInv3)}
}

// Transform classes.
const (
	txClass2D    = 0
	txClassHoriz = 1
	txClassVert  = 2
)

func txClass(t int) int {
	switch t {
	case vDct, vAdst, vFlipadst:
		return txClassVert
	case hDct, hAdst, hFlipadst:
		return txClassHoriz
	}
	return txClass2D
}

var filterIntraModeToIntraDir = [5]int{dcPred, vPred, hPred, d157Pred, dcPred}
