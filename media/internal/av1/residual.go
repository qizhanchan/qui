package av1

import "fmt"

// Residual decoding: transform block layout, coefficients, dequantization
// and reconstruction (spec 5.11.34-5.11.39, 7.12.3, 7.13.3).

type reconScratch struct {
	dq  [64 * 64]int32 // dequantized coefficients, row-major (width min(w, 32))
	res [64 * 64]int32 // residual, row-major width w
	tmp [64]int32
}

// planeTxSize is get_tx_size.
func (t *tile) planeTxSize(plane, tx int) int {
	if plane == 0 {
		return tx
	}
	uv := maxTxRect[chromaSize[t.b.size]]
	if txW[uv] == 64 || txH[uv] == 64 {
		switch {
		case txW[uv] == 16:
			return tx16x32
		case txH[uv] == 16:
			return tx32x16
		}
		return tx32x32
	}
	return uv
}

// residual is residual() (spec 5.11.34).
func (t *tile) residual() error {
	b := &t.b
	sbMask := 15
	if t.seq.Use128x128 {
		sbMask = 31
	}
	_ = sbMask
	widthChunks := max(1, blockW(b.size)>>6)
	heightChunks := max(1, blockH(b.size)>>6)
	sizeChunk := b.size
	if widthChunks > 1 || heightChunks > 1 {
		sizeChunk = block64x64
	}
	planes := 1
	if b.hasChroma {
		planes = 3
	}
	for cy := 0; cy < heightChunks; cy++ {
		for cx := 0; cx < widthChunks; cx++ {
			rowChunk := b.miRow + cy<<4
			colChunk := b.miCol + cx<<4
			for p := 0; p < planes; p++ {
				tx := tx4x4
				if !b.lossless {
					tx = t.planeTxSize(p, b.txSize)
				}
				stepX, stepY := txW[tx]>>2, txH[tx]>>2
				ss := min(p, 1)
				psz := sizeChunk
				if p > 0 {
					psz = chromaSize[sizeChunk]
				}
				n4w, n4h := num4x4W[psz], num4x4H[psz]
				baseX := (colChunk >> ss) * 4
				baseY := (rowChunk >> ss) * 4
				if b.isInter && !b.lossless && p == 0 {
					if err := t.transformTree(baseX, baseY, n4w*4, n4h*4); err != nil {
						return err
					}
					continue
				}
				baseXB := (b.miCol >> ss) * 4
				baseYB := (b.miRow >> ss) * 4
				for y := 0; y < n4h; y += stepY {
					for x := 0; x < n4w; x += stepX {
						err := t.transformBlock(p, baseXB, baseYB, tx, x+((cx<<4)>>ss), y+((cy<<4)>>ss))
						if err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func (t *tile) transformTree(startX, startY, w, h int) error {
	f := t.f
	if startX >= f.miCols*4 || startY >= f.miRows*4 {
		return nil
	}
	row, col := startY>>2, startX>>2
	tx := int(f.interTxSize[row*f.miCols+col])
	if w <= txW[tx] && h <= txH[tx] {
		return t.transformBlock(0, startX, startY, txSizeOf(w, h), 0, 0)
	}
	var err error
	switch {
	case w > h:
		err = t.transformTree(startX, startY, w/2, h)
		if err == nil {
			err = t.transformTree(startX+w/2, startY, w/2, h)
		}
	case w < h:
		err = t.transformTree(startX, startY, w, h/2)
		if err == nil {
			err = t.transformTree(startX, startY+h/2, w, h/2)
		}
	default:
		for _, o := range [4][2]int{{0, 0}, {w / 2, 0}, {0, h / 2}, {w / 2, h / 2}} {
			if err == nil {
				err = t.transformTree(startX+o[0], startY+o[1], w/2, h/2)
			}
		}
	}
	return err
}

// transformBlock is transform_block (spec 5.11.35); (x, y) are in 4x4
// units relative to (baseX, baseY), the block origin in the plane.
func (t *tile) transformBlock(plane, baseX, baseY, tx, x, y int) error {
	b := &t.b
	f := t.f
	startX, startY := baseX+4*x, baseY+4*y
	ss := min(plane, 1)
	row := (startY << ss) >> 2
	col := (startX << ss) >> 2
	sbMask := 15
	if t.seq.Use128x128 {
		sbMask = 31
	}
	sbRow, sbCol := row&sbMask, col&sbMask
	stepX, stepY := txW[tx]>>2, txH[tx]>>2
	maxX := (f.miCols * 4) >> ss
	maxY := (f.miRows * 4) >> ss
	if startX >= maxX || startY >= maxY {
		return nil
	}
	if !b.isInter {
		if (plane == 0 && b.paletteSizeY > 0) || (plane != 0 && b.paletteSizeUV > 0) {
			t.predictPalette(plane, startX, startY, x, y, tx)
		} else {
			isCfl := plane > 0 && b.uvMode == uvCflPred
			mode := b.yMode
			if plane > 0 {
				mode = b.uvMode
				if isCfl {
					mode = dcPred
				}
			}
			availL, availU := b.availL, b.availU
			if plane > 0 {
				availL, availU = b.availLC, b.availUC
			}
			bd := &t.blockDecoded[plane]
			haveAboveRt := bd[(sbRow>>ss)-1+1][(sbCol>>ss)+stepX+1]
			haveBelowLt := bd[(sbRow>>ss)+stepY+1][(sbCol>>ss)-1+1]
			t.predictIntra(plane, startX, startY, availL || x > 0, availU || y > 0, haveAboveRt, haveBelowLt, mode, txWLog2[tx], txHLog2[tx])
			if isCfl {
				t.predictCfl(plane, startX, startY, tx)
			}
		}
		if plane == 0 {
			b.maxLumaW = startX + stepX*4
			b.maxLumaH = startY + stepY*4
		}
	}
	if !b.skip {
		eob, err := t.coeffs(plane, startX, startY, tx)
		if err != nil {
			return err
		}
		if eob > 0 {
			t.reconstruct(plane, startX, startY, tx)
		}
	}
	lf := f.lfTxSize[plane]
	stride := f.lfStride[plane]
	bd := &t.blockDecoded[plane]
	for i := 0; i < stepY; i++ {
		for j := 0; j < stepX; j++ {
			r, c := (row>>ss)+i, (col>>ss)+j
			if r < len(lf)/stride && c < stride {
				lf[r*stride+c] = uint8(tx)
			}
			if yy, xx := (sbRow>>ss)+i+1, (sbCol>>ss)+j+1; yy < 34 && xx < 34 {
				bd[yy][xx] = true
			}
		}
	}
	return nil
}

// txSet is get_tx_set.
func (t *tile) txSet(tx int) int {
	b := &t.b
	sqr, up := txSqr[tx], txSqrUp[tx]
	if up > tx32x32 {
		return 0
	}
	if b.isInter {
		switch {
		case t.h.reducedTxSet || up == tx32x32:
			return 3
		case sqr == tx16x16:
			return 2
		}
		return 1
	}
	switch {
	case up == tx32x32:
		return 0
	case t.h.reducedTxSet, sqr == tx16x16:
		return 2
	}
	return 1
}

func (t *tile) qindex() int { return t.h.qindex(false, t.b.segID, t.currentQ) }

// readTxType is transform_type (spec 5.11.47); it returns the type and
// records it for the 4x4 units of the transform block.
func (t *tile) readTxType(x4, y4, tx int) int {
	b := &t.b
	set := t.txSet(tx)
	typ := dctDct
	q := t.h.baseQIdx
	if t.h.seg.enabled {
		q = t.h.qindex(true, b.segID, 0)
	}
	if set > 0 && q > 0 {
		sqr := txSqr[tx]
		if b.isInter {
			switch set {
			case 1:
				typ = txTypeInterInv1[t.ms.sym(t.cdf.interTxType[1][sqr][:], 16)]
			case 2:
				typ = txTypeInterInv2[t.ms.sym(t.cdf.interTxType[2][sqr][:], 12)]
			default:
				typ = txTypeInterInv3[t.ms.sym(t.cdf.interTxType[3][sqr][:], 2)]
			}
		} else {
			dir := b.yMode
			if b.useFilterIntra {
				dir = filterIntraModeToIntraDir[b.filterIntraMode]
			}
			if set == 1 {
				typ = txTypeIntraInv1[t.ms.sym(t.cdf.intraTxType[1][sqr][dir][:], 7)]
			} else {
				typ = txTypeIntraInv2[t.ms.sym(t.cdf.intraTxType[2][sqr][dir][:], 5)]
			}
		}
	}
	t.setTxTypes(x4, y4, tx, typ)
	return typ
}

func (t *tile) setTxTypes(x4, y4, tx, typ int) {
	b := &t.b
	bx, by := x4-b.miCol, y4-b.miRow
	for j := 0; j < txH[tx]>>2; j++ {
		for i := 0; i < txW[tx]>>2; i++ {
			if by+j < 32 && bx+i < 32 && by+j >= 0 && bx+i >= 0 {
				b.txTypes[(by+j)*32+bx+i] = uint8(typ)
			}
		}
	}
}

// planeTxType is compute_tx_type (spec 5.11.40).
func (t *tile) planeTxType(plane, tx, x4, y4 int) int {
	b := &t.b
	if b.lossless || txSqrUp[tx] > tx32x32 {
		return dctDct
	}
	set := t.txSet(tx)
	if plane == 0 {
		return int(b.txTypes[(y4-b.miRow)*32+x4-b.miCol])
	}
	inter := 0
	var typ int
	if b.isInter {
		inter = 1
		lx := max(b.miCol, x4<<1)
		ly := max(b.miRow, y4<<1)
		typ = int(b.txTypes[(ly-b.miRow)*32+lx-b.miCol])
	} else {
		typ = modeToTxfm[b.uvMode]
	}
	if txTypeInSet[inter][set]>>typ&1 == 0 {
		return dctDct
	}
	return typ
}

// scan returns the coefficient scan of a transform (get_scan).
func scanFor(tx, typ int) []int16 {
	switch tx {
	case tx16x64:
		return scan16x32[:]
	case tx64x16:
		return scan32x16[:]
	}
	if txSqrUp[tx] == tx64x64 {
		return scan32x32[:]
	}
	switch txClass(typ) {
	case txClassVert:
		return mrowScan[tx]
	case txClassHoriz:
		return mcolScan[tx]
	}
	return defaultScan[tx]
}

var defaultScan, mrowScan, mcolScan [txSizesAll][]int16

// nzMap[tx] is the per-position coefficient base context offset.
var nzMap [txSizesAll][]int8

func init() {
	defaultScan = [txSizesAll][]int16{
		tx4x4: scan4x4[:], tx8x8: scan8x8[:], tx16x16: scan16x16[:], tx32x32: scan32x32[:],
		tx4x8: scan4x8[:], tx8x4: scan8x4[:], tx8x16: scan8x16[:], tx16x8: scan16x8[:],
		tx16x32: scan16x32[:], tx32x16: scan32x16[:], tx4x16: scan4x16[:], tx16x4: scan16x4[:],
		tx8x32: scan8x32[:], tx32x8: scan32x8[:],
	}
	for tx := 0; tx < txSizesAll; tx++ {
		w, h := txW[tx], txH[tx]
		if w > 16 || h > 16 {
			continue // 1-D transforms exist up to 16 samples
		}
		row := make([]int16, w*h)
		col := make([]int16, w*h)
		for i := range row {
			row[i] = int16(i)
			col[i] = int16((i%h)*w + i/h)
		}
		mrowScan[tx], mcolScan[tx] = row, col
	}
	nzMap = [txSizesAll][]int8{
		tx4x4: nzMap4x4[:], tx8x8: nzMap8x8[:], tx16x16: nzMap16x16[:], tx32x32: nzMap32x32[:],
		tx64x64: nzMap32x32[:], tx4x8: nzMap4x16[:], tx8x4: nzMap8x4[:], tx8x16: nzMap8x32[:],
		tx16x8: nzMap16x8[:], tx16x32: nzMap16x32[:], tx32x16: nzMap32x16[:], tx32x64: nzMap32x64[:],
		tx64x32: nzMap64x32[:], tx4x16: nzMap4x16[:], tx16x4: nzMap16x4[:], tx8x32: nzMap8x32[:],
		tx32x8: nzMap32x8[:], tx16x64: nzMap16x32[:], tx64x16: nzMap32x16[:],
	}
}

var (
	sigRefDiffOffset = [3][5][2]int{
		{{0, 1}, {1, 0}, {1, 1}, {0, 2}, {2, 0}},
		{{0, 1}, {1, 0}, {0, 2}, {0, 3}, {0, 4}},
		{{0, 1}, {1, 0}, {2, 0}, {3, 0}, {4, 0}},
	}
	magRefOffset = [3][3][2]int{
		{{0, 1}, {1, 0}, {1, 1}},
		{{0, 1}, {1, 0}, {0, 2}},
		{{0, 1}, {1, 0}, {2, 0}},
	}
)

const sigCoefContexts2D = 26

// coeffs is coeffs() (spec 5.11.39): it reads the coefficients of one
// transform block into t.coef (Quant[]) and returns the end of block.
func (t *tile) coeffs(plane, startX, startY, tx int) (int, error) {
	b := &t.b
	f := t.f
	x4, y4 := startX>>2, startY>>2
	w4, h4 := txW[tx]>>2, txH[tx]>>2
	txSzCtx := (txSqr[tx] + txSqrUp[tx] + 1) >> 1
	ptype := min(plane, 1)
	ss := min(plane, 1)
	maxX4, maxY4 := f.miCols>>ss, f.miRows>>ss
	a := adjustedTx[tx]
	bwl := txWLog2[a]
	tw, th := txW[a], txH[a]
	quant := t.coef[:tw*th]
	clear(quant)

	// all_zero context.
	var ctx int
	if plane == 0 {
		top, left := 0, 0
		for k := 0; k < w4; k++ {
			if x4+k < maxX4 {
				top = max(top, int(t.aboveLevel[0][x4+k]))
			}
		}
		for k := 0; k < h4; k++ {
			if y4+k < maxY4 {
				left = max(left, int(t.leftLevel[0][(y4+k)&31]))
			}
		}
		top, left = min(top, 255), min(left, 255)
		switch {
		case blockW(b.size) == txW[tx] && blockH(b.size) == txH[tx]:
			ctx = 0
		case top == 0 && left == 0:
			ctx = 1
		case top == 0 || left == 0:
			ctx = 2
			if max(top, left) > 3 {
				ctx = 3
			}
		case max(top, left) <= 3:
			ctx = 4
		case min(top, left) <= 3:
			ctx = 5
		default:
			ctx = 6
		}
	} else {
		above, left := 0, 0
		for k := 0; k < w4; k++ {
			if x4+k < maxX4 {
				above |= int(t.aboveLevel[plane][x4+k]) | int(t.aboveDC[plane][x4+k])
			}
		}
		for k := 0; k < h4; k++ {
			if y4+k < maxY4 {
				left |= int(t.leftLevel[plane][(y4+k)&31]) | int(t.leftDC[plane][(y4+k)&31])
			}
		}
		ctx = 7
		if above != 0 {
			ctx++
		}
		if left != 0 {
			ctx++
		}
		cs := chromaSize[b.size]
		if blockW(cs)*blockH(cs) > txW[tx]*txH[tx] {
			ctx += 3
		}
	}
	allZero := t.ms.flag(t.cdf.txbSkip[txSzCtx][ctx][:])
	eob := 0
	culLevel, dcCategory := 0, 0
	if allZero {
		if plane == 0 {
			t.setTxTypes(x4, y4, tx, dctDct)
		}
	} else {
		if plane == 0 {
			t.readTxType(x4, y4, tx)
		}
		typ := t.planeTxType(plane, tx, x4, y4)
		t.planeType = typ
		scan := scanFor(tx, typ)
		class := txClass(typ)
		eobCtx := 0
		if class != txClass2D {
			eobCtx = 1
		}
		var eobPt int
		switch min(txWLog2[tx], 5) + min(txHLog2[tx], 5) - 4 {
		case 0:
			eobPt = t.ms.sym(t.cdf.eobPt16[ptype][eobCtx][:], 5) + 1
		case 1:
			eobPt = t.ms.sym(t.cdf.eobPt32[ptype][eobCtx][:], 6) + 1
		case 2:
			eobPt = t.ms.sym(t.cdf.eobPt64[ptype][eobCtx][:], 7) + 1
		case 3:
			eobPt = t.ms.sym(t.cdf.eobPt128[ptype][eobCtx][:], 8) + 1
		case 4:
			eobPt = t.ms.sym(t.cdf.eobPt256[ptype][eobCtx][:], 9) + 1
		case 5:
			eobPt = t.ms.sym(t.cdf.eobPt512[ptype][0][:], 10) + 1
		default:
			eobPt = t.ms.sym(t.cdf.eobPt1024[ptype][0][:], 11) + 1
		}
		eob = eobPt
		if eobPt >= 2 {
			eob = 1<<(eobPt-2) + 1
		}
		if shift := eobPt - 3; shift >= 0 {
			if t.ms.flag(t.cdf.eobExtra[txSzCtx][ptype][eobPt-3][:]) {
				eob += 1 << shift
			}
			for i := 1; i < max(0, eobPt-2); i++ {
				shift = max(0, eobPt-2) - 1 - i
				if t.ms.bit() != 0 {
					eob += 1 << shift
				}
			}
		}
		if eob > len(scan) {
			return 0, fmt.Errorf("%w: eob %d of tx %d at (%d,%d) plane %d", ErrInvalid, eob, tx, startX, startY, plane)
		}
		for c := eob - 1; c >= 0; c-- {
			pos := int(scan[c])
			var level int
			if c == eob-1 {
				// coeff_base_eob context.
				cctx := 3
				switch {
				case c == 0:
					cctx = 0
				case c <= (th<<bwl)/8:
					cctx = 1
				case c <= (th<<bwl)/4:
					cctx = 2
				}
				level = t.ms.sym(t.cdf.coeffBaseEob[txSzCtx][ptype][cctx][:], 3) + 1
			} else {
				level = t.ms.sym(t.cdf.coeffBase[txSzCtx][ptype][t.coeffBaseCtx(tx, class, bwl, tw, th, pos)][:], 4)
			}
			if level > 2 {
				brCtx := t.coeffBrCtx(class, bwl, tw, th, pos)
				for idx := 0; idx < 12/3; idx++ {
					br := t.ms.sym(t.cdf.coeffBr[min(txSzCtx, 3)][ptype][brCtx][:], 4)
					level += br
					if br < 3 {
						break
					}
				}
			}
			quant[pos] = int32(level)
		}
		for c := 0; c < eob; c++ {
			pos := int(scan[c])
			sign := 0
			if quant[pos] != 0 {
				if c == 0 {
					sign = t.ms.sym(t.cdf.dcSign[ptype][t.dcSignCtx(plane, x4, y4, w4, h4, maxX4, maxY4)][:], 2)
				} else {
					sign = t.ms.bit()
				}
			}
			if quant[pos] > 14 {
				length := 0
				for {
					length++
					if t.ms.bit() != 0 {
						break
					}
					if length > 20 {
						return 0, ErrInvalid
					}
				}
				x := 1
				for i := length - 2; i >= 0; i-- {
					x = x<<1 | t.ms.bit()
				}
				quant[pos] = int32(x + 14)
			}
			if pos == 0 && quant[pos] > 0 {
				dcCategory = 2
				if sign != 0 {
					dcCategory = 1
				}
			}
			quant[pos] &= 0xFFFFF
			culLevel += int(quant[pos])
			if sign != 0 {
				quant[pos] = -quant[pos]
			}
		}
		culLevel = min(63, culLevel)
	}
	for i := 0; i < w4; i++ {
		t.aboveLevel[plane][x4+i] = uint8(culLevel)
		t.aboveDC[plane][x4+i] = uint8(dcCategory)
	}
	for i := 0; i < h4; i++ {
		t.leftLevel[plane][(y4+i)&31] = uint8(culLevel)
		t.leftDC[plane][(y4+i)&31] = uint8(dcCategory)
	}
	return eob, nil
}

func iabs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (t *tile) coeffBaseCtx(tx, class, bwl, tw, th, pos int) int {
	row := pos >> bwl
	col := pos - row<<bwl
	mag := 0
	for _, o := range sigRefDiffOffset[class] {
		r, c := row+o[0], col+o[1]
		if r < th && c < tw {
			mag += int(min(iabs32(t.coef[r<<bwl+c]), 3))
		}
	}
	ctx := min((mag+1)>>1, 4)
	if class == txClass2D {
		if pos == 0 {
			return 0
		}
		return ctx + int(nzMap[tx][pos])
	}
	idx := col
	if class == txClassVert {
		idx = row
	}
	return ctx + sigCoefContexts2D + 5*min(idx, 2)
}

func (t *tile) coeffBrCtx(class, bwl, tw, th, pos int) int {
	row := pos >> bwl
	col := pos - row<<bwl
	mag := 0
	for _, o := range magRefOffset[class] {
		r, c := row+o[0], col+o[1]
		if r < th && c < tw {
			mag += int(min(t.coef[r*tw+c], 15))
		}
	}
	mag = min((mag+1)>>1, 6)
	switch {
	case pos == 0:
		return mag
	case class == txClass2D && row < 2 && col < 2,
		class == txClassHoriz && col == 0,
		class == txClassVert && row == 0:
		return mag + 7
	}
	return mag + 14
}

func (t *tile) dcSignCtx(plane, x4, y4, w4, h4, maxX4, maxY4 int) int {
	s := 0
	for k := 0; k < w4; k++ {
		if x4+k < maxX4 {
			switch t.aboveDC[plane][x4+k] {
			case 1:
				s--
			case 2:
				s++
			}
		}
	}
	for k := 0; k < h4; k++ {
		if y4+k < maxY4 {
			switch t.leftDC[plane][(y4+k)&31] {
			case 1:
				s--
			case 2:
				s++
			}
		}
	}
	switch {
	case s < 0:
		return 1
	case s > 0:
		return 2
	}
	return 0
}
