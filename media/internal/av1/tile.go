package av1

import "fmt"

// Tile decoding: the superblock walk, partitions and block mode info
// (spec 5.11).

// mv is a motion vector in 1/8 sample units.
type mv struct{ row, col int16 }

// miInfo is the mode info of one 4x4 luma unit (the spec's per-position
// arrays YModes, RefFrames, Mvs, IsInters, Skips, MiSizes, ...).
type miInfo struct {
	size         uint8
	skip         bool
	skipMode     bool
	isInter      bool
	segID        uint8
	yMode        uint8
	uvMode       uint8
	refFrame     [2]int8
	mv           [2]mv
	interp       [2]uint8
	txSize       uint8
	paletteSize  [2]uint8
	compGroupIdx uint8
	compoundIdx  uint8
	deltaLF      [4]int8
	paletteColor [2][8]uint16
	written      bool // decoded in the current frame
}

// block is the state of the block being decoded.
type block struct {
	miRow, miCol, size int
	bw4, bh4           int
	hasChroma          bool
	availU, availL     bool
	availUC, availLC   bool // chroma availability

	skip, skipMode, isInter, useIntraBC bool
	segID                               int
	lossless                            bool
	yMode, uvMode                       int
	angleDeltaY, angleDeltaUV           int
	cflAlphaU, cflAlphaV                int
	useFilterIntra                      bool
	filterIntraMode                     int
	paletteSizeY, paletteSizeUV         int
	paletteColors                       [3][8]uint16
	refFrame                            [2]int
	mv                                  [2]mv
	interpFilter                        [2]int
	motionMode                          int
	interIntra                          bool
	interIntraMode                      int
	wedgeInterIntra                     bool
	wedgeIndex, wedgeSign               int
	maskType                            int
	compoundType                        int
	compGroupIdx, compoundIdx           int
	numMvFound, newMvCount              int
	txSize                              int
	maxLumaW, maxLumaH                  int

	colorMap [2][64 * 64]uint8 // palette indices (luma, chroma), stride 64
	txTypes  [32 * 32]uint8    // luma transform types per 4x4, stride 32
}

// tile is the per-tile decoding state.
type tile struct {
	f   *frameState
	h   *FrameHeader
	seq *SequenceHeader
	ms  msac
	cdf cdfContext

	miRowStart, miRowEnd int
	miColStart, miColEnd int

	// Above contexts (frame-wide, in 4x4 units of each plane) and left
	// contexts (indexed by position within the superblock row's span).
	aboveLevel, aboveDC [3][]uint8
	aboveSegPred        []bool
	leftLevel, leftDC   [3][32]uint8
	leftSegPred         [32]bool

	deltaLF      [4]int
	refSgrXqd    [3][2]int
	refLrWiener  [3][2][3]int
	currentQ     int
	readDeltas   bool
	blockDecoded [3][34][34]bool // [plane][y+1][x+1] within the superblock

	b   block
	mvp mvPred

	edges     intraEdges
	mc        *mcScratch
	coef      [32 * 32]int32 // Quant[] of the current transform block
	planeType int            // PlaneTxType of the current transform block
	recon     *reconScratch
}

// tileScratch is the large per-goroutine scratch of tile decoding, kept
// by the Decoder across frames.
type tileScratch struct {
	mc    mcScratch
	recon reconScratch
}

func (t *tile) mi(row, col int) *miInfo { return &t.f.mi[row*t.f.miCols+col] }

// inside is is_inside: within the current tile.
func (t *tile) inside(row, col int) bool {
	return col >= t.miColStart && col < t.miColEnd && row >= t.miRowStart && row < t.miRowEnd
}

func (t *tile) sbSize() int {
	if t.seq.Use128x128 {
		return block128x128
	}
	return block64x64
}

// decode is decode_tile (spec 5.11.2).
func (t *tile) decode() error {
	f := t.f
	for p := 0; p < 3; p++ {
		n := (f.miCols+1)>>min(p, 1) + 32
		if cap(t.aboveLevel[p]) < n {
			t.aboveLevel[p] = make([]uint8, n)
			t.aboveDC[p] = make([]uint8, n)
		}
		t.aboveLevel[p] = t.aboveLevel[p][:n]
		t.aboveDC[p] = t.aboveDC[p][:n]
		clear(t.aboveLevel[p])
		clear(t.aboveDC[p])
	}
	if cap(t.aboveSegPred) < f.miCols+32 {
		t.aboveSegPred = make([]bool, f.miCols+32)
	}
	t.aboveSegPred = t.aboveSegPred[:f.miCols+32]
	clear(t.aboveSegPred)
	t.deltaLF = [4]int{}
	for p := 0; p < 3; p++ {
		for pass := 0; pass < 2; pass++ {
			t.refSgrXqd[p][pass] = sgrprojXqdMid[pass]
			t.refLrWiener[p][pass] = wienerTapsMid
		}
	}
	sb := t.sbSize()
	sb4 := num4x4W[sb]
	for r := t.miRowStart; r < t.miRowEnd; r += sb4 {
		for p := range t.leftLevel {
			t.leftLevel[p] = [32]uint8{}
			t.leftDC[p] = [32]uint8{}
		}
		t.leftSegPred = [32]bool{}
		for c := t.miColStart; c < t.miColEnd; c += sb4 {
			t.readDeltas = t.h.deltaQPresent
			f.clearCdef(r, c)
			t.clearBlockDecoded(r, c, sb4)
			t.readLR(r, c, sb)
			if err := t.partition(r, c, sb); err != nil {
				return err
			}
		}
	}
	return nil
}

// clearBlockDecoded is clear_block_decoded_flags (spec 5.11.3): marks
// which neighbouring samples count as decoded for intra edge
// availability. Index [y+1][x+1] holds BlockDecoded[plane][y][x].
func (t *tile) clearBlockDecoded(r, c, sb4 int) {
	for p := 0; p < t.seq.NumPlanes(); p++ {
		ss := min(p, 1)
		sbW4 := (t.miColEnd - c) >> ss
		sbH4 := (t.miRowEnd - r) >> ss
		bd := &t.blockDecoded[p]
		for y := -1; y <= sb4>>ss; y++ {
			for x := -1; x <= sb4>>ss; x++ {
				bd[y+1][x+1] = (y < 0 && x < sbW4) || (x < 0 && y < sbH4)
			}
		}
		bd[sb4>>ss+1][0] = false
	}
}

// partition is decode_partition (spec 5.11.4).
func (t *tile) partition(r, c, bSize int) error {
	f := t.f
	if r >= f.miRows || c >= f.miCols {
		return nil
	}
	availU := t.inside(r-1, c)
	availL := t.inside(r, c-1)
	num4x4 := num4x4W[bSize]
	half := num4x4 >> 1
	quarter := half >> 1
	hasRows := r+half < f.miRows
	hasCols := c+half < f.miCols
	part := partSplit
	if bSize < block8x8 {
		part = partNone
	} else {
		bsl := miWLog2[bSize]
		ctx := 0
		if availU && miWLog2[t.mi(r-1, c).size] < bsl {
			ctx++
		}
		if availL && miHLog2[t.mi(r, c-1).size] < bsl {
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
			part = t.ms.sym(cdf, n)
		case hasCols:
			// split_or_horz: at the bottom edge every partition with a
			// vertical split becomes SPLIT.
			p := cdfProb(cdf, partVert) + cdfProb(cdf, partSplit) + cdfProb(cdf, partHorzA) + cdfProb(cdf, partVertA) + cdfProb(cdf, partVertB)
			if bSize != block128x128 {
				p += cdfProb(cdf, partVert4)
			}
			part = partHorz
			if t.ms.decode([]uint16{uint16(p), 0}, 2) == 1 {
				part = partSplit
			}
		case hasRows:
			// split_or_vert: at the right edge every partition with a
			// horizontal split becomes SPLIT.
			p := cdfProb(cdf, partHorz) + cdfProb(cdf, partSplit) + cdfProb(cdf, partHorzA) + cdfProb(cdf, partHorzB) + cdfProb(cdf, partVertA)
			if bSize != block128x128 {
				p += cdfProb(cdf, partHorz4)
			}
			part = partVert
			if t.ms.decode([]uint16{uint16(p), 0}, 2) == 1 {
				part = partSplit
			}
		}
	}
	sub := partitionSubsize[part][bSize]
	split := partitionSubsize[partSplit][bSize]
	if sub == blockInvalid {
		return fmt.Errorf("%w: partition %d of block size %d at (%d,%d)", ErrInvalid, part, bSize, r, c)
	}
	var err error
	dec := func(r, c, s int) {
		if err == nil {
			err = t.decodeBlock(r, c, s)
		}
	}
	switch part {
	case partNone:
		dec(r, c, sub)
	case partHorz:
		dec(r, c, sub)
		if hasRows {
			dec(r+half, c, sub)
		}
	case partVert:
		dec(r, c, sub)
		if hasCols {
			dec(r, c+half, sub)
		}
	case partSplit:
		for _, o := range [4][2]int{{0, 0}, {0, half}, {half, 0}, {half, half}} {
			if err == nil {
				err = t.partition(r+o[0], c+o[1], sub)
			}
		}
	case partHorzA:
		dec(r, c, split)
		dec(r, c+half, split)
		dec(r+half, c, sub)
	case partHorzB:
		dec(r, c, sub)
		dec(r+half, c, split)
		dec(r+half, c+half, split)
	case partVertA:
		dec(r, c, split)
		dec(r+half, c, split)
		dec(r, c+half, sub)
	case partVertB:
		dec(r, c, sub)
		dec(r, c+half, split)
		dec(r+half, c+half, split)
	case partHorz4:
		for i := 0; i < 4; i++ {
			if i < 3 || r+quarter*3 < f.miRows {
				dec(r+quarter*i, c, sub)
			}
		}
	case partVert4:
		for i := 0; i < 4; i++ {
			if i < 3 || c+quarter*3 < f.miCols {
				dec(r, c+quarter*i, sub)
			}
		}
	}
	return err
}

// cdfProb is the probability (out of 32768) of symbol s of an inverted
// CDF.
func cdfProb(cdf []uint16, s int) int {
	if s == 0 {
		return 32768 - int(cdf[0])
	}
	return int(cdf[s-1]) - int(cdf[s])
}
