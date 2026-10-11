package av1

import (
	"fmt"
	"slices"
)

// decodeBlock is decode_block (spec 5.11.5).
func (t *tile) decodeBlock(r, c, size int) error {
	f := t.f
	b := &t.b
	*b = block{colorMap: b.colorMap, txTypes: b.txTypes}
	b.miRow, b.miCol, b.size = r, c, size
	b.bw4, b.bh4 = num4x4W[size], num4x4H[size]
	switch {
	case b.bh4 == 1 && (r&1) == 0, b.bw4 == 1 && (c&1) == 0:
		b.hasChroma = false
	default:
		b.hasChroma = t.seq.NumPlanes() > 1
	}
	b.availU = t.inside(r-1, c)
	b.availL = t.inside(r, c-1)
	if b.hasChroma {
		b.availUC, b.availLC = b.availU, b.availL
		if b.bh4 == 1 {
			b.availUC = t.inside(r-2, c)
		}
		if b.bw4 == 1 {
			b.availLC = t.inside(r, c-2)
		}
	}
	var err error
	if t.h.intra {
		err = t.intraFrameModeInfo()
	} else {
		err = t.interFrameModeInfo()
	}
	if err != nil {
		return err
	}
	t.paletteTokens()
	t.readBlockTxSize()
	if b.skip {
		t.resetBlockContext()
	}
	// Store the mode info of every covered 4x4 unit.
	rows, cols := min(b.bh4, f.miRows-r), min(b.bw4, f.miCols-c)
	m := miInfo{
		size: uint8(size), skip: b.skip, skipMode: b.skipMode, isInter: b.isInter,
		segID: uint8(b.segID), yMode: uint8(b.yMode), uvMode: uint8(b.uvMode),
		refFrame: [2]int8{int8(b.refFrame[0]), int8(b.refFrame[1])},
		mv:       b.mv, interp: [2]uint8{uint8(b.interpFilter[0]), uint8(b.interpFilter[1])},
		txSize: uint8(b.txSize), paletteSize: [2]uint8{uint8(b.paletteSizeY), uint8(b.paletteSizeUV)},
		compGroupIdx: uint8(b.compGroupIdx), compoundIdx: uint8(b.compoundIdx),
	}
	for i := 0; i < 4; i++ {
		m.deltaLF[i] = int8(t.deltaLF[i])
	}
	if b.paletteSizeY > 0 {
		m.paletteColor[0] = b.paletteColors[0]
	}
	if b.paletteSizeUV > 0 {
		m.paletteColor[1] = b.paletteColors[1]
	}
	// UVModes is only written by intra blocks with chroma.
	keepUV := b.refFrame[0] != intraFrame || !b.hasChroma
	for y := 0; y < rows; y++ {
		row := f.mi[(r+y)*f.miCols+c:]
		for x := 0; x < cols; x++ {
			if keepUV {
				m.uvMode = row[x].uvMode
			}
			row[x] = m
		}
	}
	if err := t.computePrediction(); err != nil {
		return err
	}
	return t.residual()
}

func (t *tile) segFeature(id, feature int) bool {
	return t.h.seg.enabled && t.h.seg.featureEnabled[id][feature]
}

// intraFrameModeInfo is intra_frame_mode_info (spec 5.11.7).
func (t *tile) intraFrameModeInfo() error {
	b := &t.b
	h := t.h
	if h.seg.preSkip {
		t.intraSegmentID()
	}
	t.readSkip()
	if !h.seg.preSkip {
		t.intraSegmentID()
	}
	t.readCdef()
	t.readDeltaQIndex()
	t.readDeltaLF()
	t.readDeltas = false
	b.refFrame = [2]int{intraFrame, -1}
	if h.allowIntraBC {
		b.useIntraBC = t.ms.flag(t.cdf.intraBC[:])
	}
	if b.useIntraBC {
		b.isInter = true
		b.interpFilter = [2]int{filterBilinear, filterBilinear}
		return t.intraBCMode()
	}
	above, left := dcPred, dcPred
	if b.availU {
		above = int(t.mi(b.miRow-1, b.miCol).yMode)
	}
	if b.availL {
		left = int(t.mi(b.miRow, b.miCol-1).yMode)
	}
	b.yMode = t.ms.sym(t.cdf.kfYMode[intraModeContext[above]][intraModeContext[left]][:], intraModes)
	t.intraModesRest()
	return nil
}

// intraModesRest reads what follows the luma mode of an intra block: the
// angle delta, chroma mode, CfL, palette and filter intra.
func (t *tile) intraModesRest() {
	b := &t.b
	if b.size >= block8x8 && isDirectional(b.yMode) {
		b.angleDeltaY = t.ms.sym(t.cdf.angleDelta[b.yMode-vPred][:], 7) - 3
	}
	if b.hasChroma {
		cflAllowed := false
		if b.lossless && chromaSize[b.size] == block4x4 {
			cflAllowed = true
		} else if !b.lossless && max(blockW(b.size), blockH(b.size)) <= 32 {
			cflAllowed = true
		}
		if cflAllowed {
			b.uvMode = t.ms.sym(t.cdf.uvMode[1][b.yMode][:], 14)
		} else {
			b.uvMode = t.ms.sym(t.cdf.uvMode[0][b.yMode][:], 13)
		}
		if b.uvMode == uvCflPred {
			t.readCflAlphas()
		}
		if b.size >= block8x8 && isDirectional(b.uvMode) {
			b.angleDeltaUV = t.ms.sym(t.cdf.angleDelta[b.uvMode-vPred][:], 7) - 3
		}
	}
	if b.size >= block8x8 && blockW(b.size) <= 64 && blockH(b.size) <= 64 && t.h.allowScreenContent {
		t.paletteModeInfo()
	}
	t.filterIntraModeInfo()
}

func (t *tile) readCflAlphas() {
	b := &t.b
	signs := t.ms.sym(t.cdf.cflSign[:], 8)
	signU, signV := (signs+1)/3, (signs+1)%3
	if signU != 0 {
		b.cflAlphaU = 1 + t.ms.sym(t.cdf.cflAlpha[(signU-1)*3+signV][:], 16)
		if signU == 1 {
			b.cflAlphaU = -b.cflAlphaU
		}
	}
	if signV != 0 {
		b.cflAlphaV = 1 + t.ms.sym(t.cdf.cflAlpha[(signV-1)*3+signU][:], 16)
		if signV == 1 {
			b.cflAlphaV = -b.cflAlphaV
		}
	}
}

func (t *tile) filterIntraModeInfo() {
	b := &t.b
	if t.seq.EnableFilterIntra && b.yMode == dcPred && b.paletteSizeY == 0 && max(blockW(b.size), blockH(b.size)) <= 32 {
		b.useFilterIntra = t.ms.flag(t.cdf.filterIntra[b.size][:])
		if b.useFilterIntra {
			b.filterIntraMode = t.ms.sym(t.cdf.filterIntraMode[:], 5)
		}
	}
}

func (t *tile) intraSegmentID() {
	b := &t.b
	if t.h.seg.enabled {
		t.readSegmentID()
	}
	b.lossless = t.h.lossless[b.segID]
}

// segPred returns the spatial segment id prediction and its context.
func (t *tile) segPred() (pred, ctx int) {
	b := &t.b
	prevUL, prevU, prevL := -1, -1, -1
	if b.availU && b.availL {
		prevUL = int(t.mi(b.miRow-1, b.miCol-1).segID)
	}
	if b.availU {
		prevU = int(t.mi(b.miRow-1, b.miCol).segID)
	}
	if b.availL {
		prevL = int(t.mi(b.miRow, b.miCol-1).segID)
	}
	switch {
	case prevU == -1:
		pred = max(prevL, 0)
	case prevL == -1:
		pred = prevU
	case prevUL == prevU:
		pred = prevU
	default:
		pred = prevL
	}
	switch {
	case prevUL < 0:
		ctx = 0
	case prevUL == prevU && prevUL == prevL:
		ctx = 2
	case prevUL == prevU || prevUL == prevL || prevU == prevL:
		ctx = 1
	}
	return
}

func (t *tile) readSegmentID() {
	b := &t.b
	pred, ctx := t.segPred()
	if b.skip {
		b.segID = pred
		return
	}
	last := t.h.seg.lastActive
	v := t.ms.sym(t.cdf.segmentID[ctx][:], maxSegments)
	b.segID = clip3(0, last, negDeinterleave(v, pred, last+1))
}

func negDeinterleave(diff, ref, max int) int {
	if ref == 0 {
		return diff
	}
	if ref >= max-1 {
		return max - diff - 1
	}
	if 2*ref < max {
		if diff <= 2*ref {
			if diff&1 != 0 {
				return ref + (diff+1)>>1
			}
			return ref - diff>>1
		}
		return diff
	}
	if diff <= 2*(max-ref-1) {
		if diff&1 != 0 {
			return ref + (diff+1)>>1
		}
		return ref - diff>>1
	}
	return max - (diff + 1)
}

func (t *tile) readSkip() {
	b := &t.b
	if t.h.seg.preSkip && t.segFeature(b.segID, segLvlSkip) {
		b.skip = true
		return
	}
	ctx := 0
	if b.availU && t.mi(b.miRow-1, b.miCol).skip {
		ctx++
	}
	if b.availL && t.mi(b.miRow, b.miCol-1).skip {
		ctx++
	}
	b.skip = t.ms.flag(t.cdf.skip[ctx][:])
}

func (t *tile) readCdef() {
	b := &t.b
	h := t.h
	if b.skip || h.codedLossless || !t.seq.EnableCDEF || h.allowIntraBC {
		return
	}
	f := t.f
	r, c := b.miRow&^15, b.miCol&^15
	i := f.cdefIndex(r, c)
	if f.cdefIdx[i] == -1 {
		v := int8(t.ms.lit(h.cdefBits))
		for y := r; y < r+b.bh4; y += 16 {
			for x := c; x < c+b.bw4; x += 16 {
				if y < f.miRows && x < f.miCols {
					f.cdefIdx[f.cdefIndex(y, x)] = v
				}
			}
		}
	}
}

func (t *tile) readDeltaQIndex() {
	b := &t.b
	if b.size == t.sbSize() && b.skip {
		return
	}
	if t.readDeltas {
		abs := t.ms.sym(t.cdf.deltaQ[:], 4)
		if abs == 3 {
			n := t.ms.lit(3) + 1
			abs = t.ms.lit(n) + (1 << n) + 1
		}
		if abs != 0 {
			if t.ms.bit() != 0 {
				abs = -abs
			}
			t.currentQ = clip3(1, 255, t.currentQ+abs<<t.h.deltaQRes)
		}
	}
}

func (t *tile) readDeltaLF() {
	b := &t.b
	h := t.h
	if b.size == t.sbSize() && b.skip {
		return
	}
	if !t.readDeltas || !h.deltaLFPresent {
		return
	}
	count := 1
	if h.deltaLFMulti {
		count = 4
		if t.seq.NumPlanes() == 1 {
			count = 2
		}
	}
	for i := 0; i < count; i++ {
		cdf := t.cdf.deltaLF[:]
		if h.deltaLFMulti {
			cdf = t.cdf.deltaLFMulti[i][:]
		}
		abs := t.ms.sym(cdf, 4)
		if abs == 3 {
			n := t.ms.lit(3) + 1
			abs = t.ms.lit(n) + (1 << n) + 1
		}
		if abs != 0 {
			if t.ms.bit() != 0 {
				abs = -abs
			}
			t.deltaLF[i] = clip3(-63, 63, t.deltaLF[i]+abs<<h.deltaLFRes)
		}
	}
}

// paletteModeInfo is palette_mode_info (spec 5.11.46).
func (t *tile) paletteModeInfo() {
	b := &t.b
	bd := t.seq.BitDepth
	bsizeCtx := miWLog2[b.size] + miHLog2[b.size] - 2
	if b.yMode == dcPred {
		ctx := 0
		if b.availU && t.mi(b.miRow-1, b.miCol).paletteSize[0] > 0 {
			ctx++
		}
		if b.availL && t.mi(b.miRow, b.miCol-1).paletteSize[0] > 0 {
			ctx++
		}
		if t.ms.flag(t.cdf.paletteYMode[bsizeCtx][ctx][:]) {
			b.paletteSizeY = t.ms.sym(t.cdf.paletteYSize[bsizeCtx][:], 7) + 2
			t.readPaletteColors(0, b.paletteSizeY, bd)
		}
	}
	if b.hasChroma && b.uvMode == dcPred {
		ctx := 0
		if b.paletteSizeY > 0 {
			ctx = 1
		}
		if t.ms.flag(t.cdf.paletteUVMode[ctx][:]) {
			n := t.ms.sym(t.cdf.paletteUVSize[bsizeCtx][:], 7) + 2
			b.paletteSizeUV = n
			t.readPaletteColors(1, n, bd)
			v := &b.paletteColors[2]
			if t.ms.bit() != 0 { // delta_encode_palette_colors_v
				minBits := bd - 4
				maxVal := 1 << bd
				bits := minBits + t.ms.lit(2)
				v[0] = uint16(t.ms.lit(bd))
				for i := 1; i < n; i++ {
					d := t.ms.lit(bits)
					if d != 0 && t.ms.bit() != 0 {
						d = -d
					}
					val := int(v[i-1]) + d
					if val < 0 {
						val += maxVal
					}
					if val >= maxVal {
						val -= maxVal
					}
					v[i] = uint16(clip3(0, maxVal-1, val))
				}
			} else {
				for i := 0; i < n; i++ {
					v[i] = uint16(t.ms.lit(bd))
				}
			}
		}
	}
}

// readPaletteColors reads the luma (plane 0) or Cb (plane 1) palette.
func (t *tile) readPaletteColors(plane, n, bd int) {
	b := &t.b
	cache := t.paletteCache(plane)
	col := &b.paletteColors[plane]
	idx := 0
	for i := 0; i < len(cache) && idx < n; i++ {
		if t.ms.bit() != 0 {
			col[idx] = cache[i]
			idx++
		}
	}
	if idx < n {
		col[idx] = uint16(t.ms.lit(bd))
		idx++
	}
	bits := 0
	if idx < n {
		bits = bd - 3 + t.ms.lit(2)
	}
	for ; idx < n; idx++ {
		d := t.ms.lit(bits)
		if plane == 0 {
			d++
		}
		v := min(int(col[idx-1])+d, 1<<bd-1)
		col[idx] = uint16(v)
		rng := 1<<bd - v
		if plane == 0 {
			rng--
		}
		bits = min(bits, ceilLog2(rng))
	}
	slices.Sort(col[:n])
}

func ceilLog2(x int) int {
	if x < 2 {
		return 0
	}
	i, p := 1, 2
	for p < x {
		i++
		p <<= 1
	}
	return i
}

// paletteCache is get_palette_cache (spec 7.11.4.1 / 5.11.46).
func (t *tile) paletteCache(plane int) []uint16 {
	b := &t.b
	var above, left []uint16
	if (b.miRow*4)%64 != 0 && b.availU {
		m := t.mi(b.miRow-1, b.miCol)
		above = m.paletteColor[plane][:m.paletteSize[plane]]
	}
	if b.availL {
		m := t.mi(b.miRow, b.miCol-1)
		left = m.paletteColor[plane][:m.paletteSize[plane]]
	}
	out := make([]uint16, 0, 16)
	push := func(v uint16) {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	ai, li := 0, 0
	for ai < len(above) && li < len(left) {
		ac, lc := above[ai], left[li]
		if lc < ac {
			push(lc)
			li++
		} else {
			push(ac)
			ai++
			if lc == ac {
				li++
			}
		}
	}
	for ; ai < len(above); ai++ {
		push(above[ai])
	}
	for ; li < len(left); li++ {
		push(left[li])
	}
	return out
}

var paletteColorContext = [9]int{-1, -1, 0, -1, -1, 4, 3, 2, 1}

// paletteTokens is palette_tokens (spec 5.11.49).
func (t *tile) paletteTokens() {
	b := &t.b
	f := t.f
	bh, bw := blockH(b.size), blockW(b.size)
	onH := min(bh, (f.miRows-b.miRow)*4)
	onW := min(bw, (f.miCols-b.miCol)*4)
	if b.paletteSizeY > 0 {
		t.readColorMap(0, b.paletteSizeY, bw, bh, onW, onH, t.cdf.paletteYColor[b.paletteSizeY-2][:])
	}
	if b.paletteSizeUV > 0 {
		bw, bh, onW, onH = bw>>1, bh>>1, onW>>1, onH>>1
		if bw < 4 {
			bw += 2
			onW += 2
		}
		if bh < 4 {
			bh += 2
			onH += 2
		}
		t.readColorMap(1, b.paletteSizeUV, bw, bh, onW, onH, t.cdf.paletteUVColor[b.paletteSizeUV-2][:])
	}
}

func (t *tile) readColorMap(p, n, bw, bh, onW, onH int, cdfs [][9]uint16) {
	m := &t.b.colorMap[p]
	const s = 64
	m[0] = uint8(t.ms.ns(n))
	for i := 1; i < onH+onW-1; i++ {
		for j := min(i, onW-1); j >= max(0, i-onH+1); j-- {
			r, c := i-j, j
			// get_palette_color_context
			var scores [8]int
			order := [8]uint8{0, 1, 2, 3, 4, 5, 6, 7}
			if c > 0 {
				scores[m[r*s+c-1]] += 2
			}
			if r > 0 && c > 0 {
				scores[m[(r-1)*s+c-1]]++
			}
			if r > 0 {
				scores[m[(r-1)*s+c]] += 2
			}
			for k := 0; k < 3; k++ {
				maxScore, maxIdx := scores[k], k
				for l := k + 1; l < n; l++ {
					if scores[l] > maxScore {
						maxScore, maxIdx = scores[l], l
					}
				}
				if maxIdx != k {
					mo := order[maxIdx]
					for l := maxIdx; l > k; l-- {
						scores[l] = scores[l-1]
						order[l] = order[l-1]
					}
					scores[k] = maxScore
					order[k] = mo
				}
			}
			hash := scores[0]*1 + scores[1]*2 + scores[2]*2
			ctx := paletteColorContext[hash]
			idx := t.ms.sym(cdfs[ctx][:], n)
			m[r*s+c] = order[idx]
		}
	}
	for i := 0; i < onH; i++ {
		for j := onW; j < bw; j++ {
			m[i*s+j] = m[i*s+onW-1]
		}
	}
	for i := onH; i < bh; i++ {
		copy(m[i*s:i*s+bw], m[(onH-1)*s:])
	}
}

// readBlockTxSize is read_block_tx_size (spec 5.11.15).
func (t *tile) readBlockTxSize() {
	b := &t.b
	f := t.f
	if t.h.txMode == txModeSelect && b.size > block4x4 && b.isInter && !b.skip && !b.lossless {
		maxTx := maxTxRect[b.size]
		w4, h4 := txW[maxTx]/4, txH[maxTx]/4
		for row := b.miRow; row < b.miRow+b.bh4; row += h4 {
			for col := b.miCol; col < b.miCol+b.bw4; col += w4 {
				t.readVarTxSize(row, col, maxTx, 0)
			}
		}
		return
	}
	t.readTxSize(!b.skip || !b.isInter)
	for row := b.miRow; row < min(b.miRow+b.bh4, f.miRows); row++ {
		for col := b.miCol; col < min(b.miCol+b.bw4, f.miCols); col++ {
			f.interTxSize[row*f.miCols+col] = uint8(b.txSize)
		}
	}
}

func (t *tile) readVarTxSize(row, col, tx, depth int) {
	f := t.f
	b := &t.b
	if row >= f.miRows || col >= f.miCols {
		return
	}
	split := false
	if tx != tx4x4 && depth != 2 {
		split = t.ms.flag(t.cdf.txfmSplit[t.txfmSplitCtx(row, col, tx)][:])
	}
	w4, h4 := txW[tx]/4, txH[tx]/4
	if split {
		sub := splitTx[tx]
		sw, sh := txW[sub]/4, txH[sub]/4
		for i := 0; i < h4; i += sh {
			for j := 0; j < w4; j += sw {
				t.readVarTxSize(row+i, col+j, sub, depth+1)
			}
		}
		return
	}
	for i := 0; i < h4 && row+i < f.miRows; i++ {
		for j := 0; j < w4 && col+j < f.miCols; j++ {
			f.interTxSize[(row+i)*f.miCols+col+j] = uint8(tx)
		}
	}
	b.txSize = tx
}

func (t *tile) aboveTxWidth(row, col int) int {
	b := &t.b
	if row == b.miRow {
		if !b.availU {
			return 64
		}
		if m := t.mi(row-1, col); m.skip && m.isInter {
			return blockW(int(m.size))
		}
	}
	return txW[t.f.interTxSize[(row-1)*t.f.miCols+col]]
}

func (t *tile) leftTxHeight(row, col int) int {
	b := &t.b
	if col == b.miCol {
		if !b.availL {
			return 64
		}
		if m := t.mi(row, col-1); m.skip && m.isInter {
			return blockH(int(m.size))
		}
	}
	return txH[t.f.interTxSize[row*t.f.miCols+col-1]]
}

func (t *tile) txfmSplitCtx(row, col, tx int) int {
	b := &t.b
	above := 0
	if t.aboveTxWidth(row, col) < txW[tx] {
		above = 1
	}
	left := 0
	if t.leftTxHeight(row, col) < txH[tx] {
		left = 1
	}
	size := min(64, max(blockW(b.size), blockH(b.size)))
	maxTx := txSizeOf(size, size)
	ctx := (4-maxTx)*6 + above + left
	if txSqrUp[tx] != maxTx {
		ctx += 3
	}
	return ctx
}

func (t *tile) readTxSize(allowSelect bool) {
	b := &t.b
	if b.lossless {
		b.txSize = tx4x4
		return
	}
	maxRect := maxTxRect[b.size]
	depthMax := maxTxDepth[b.size]
	b.txSize = maxRect
	if b.size > block4x4 && allowSelect && t.h.txMode == txModeSelect {
		// tx_depth context (spec 8.3.2).
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
		depth := t.ms.sym(t.cdf.txSize[depthMax-1][ctx][:], n)
		for i := 0; i < depth; i++ {
			b.txSize = splitTx[b.txSize]
		}
	}
}

// resetBlockContext is reset_block_context (spec 5.11.12).
func (t *tile) resetBlockContext() {
	b := &t.b
	planes := 1
	if b.hasChroma {
		planes = 3
	}
	for p := 0; p < planes; p++ {
		ss := min(p, 1)
		for i := b.miCol >> ss; i < (b.miCol+b.bw4)>>ss; i++ {
			t.aboveLevel[p][i] = 0
			t.aboveDC[p][i] = 0
		}
		for i := b.miRow >> ss; i < (b.miRow+b.bh4)>>ss; i++ {
			t.leftLevel[p][i&31] = 0
			t.leftDC[p][i&31] = 0
		}
	}
}

var errNotImplemented = fmt.Errorf("%w: not implemented yet", ErrUnsupported)
