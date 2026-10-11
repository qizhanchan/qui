package av1

import (
	"math"
	"math/bits"
	"sync"
)

// Forward transforms, quantisation and coefficient coding.
//
// The forward transforms are the exact inverses of the decoder's: each
// 1-D inverse (tx1D) is sampled into a matrix once, inverted in float64,
// and the 2-D forward undoes the decoder's scaling (rectangular factor,
// row shift, column shift 4) and flips. Coefficients therefore come out
// on the decoder's dequantised scale.

// fwdMat[kind][log2n] is the inverse of tx1D's n x n matrix (row-major),
// built on first use.
var (
	fwdMat     [3][7][]float64
	fwdMatOnce sync.Once
)

func initFwdMats() {
	for kind := kDCT; kind <= kIdentity; kind++ {
		for l := 2; l <= 6; l++ {
			if kind == kADST && l > 4 || kind == kIdentity && l > 5 {
				continue
			}
			n := 1 << uint(l)
			const amp = 1 << 12
			m := make([]float64, n*n) // m[i*n+j]: output i of input j
			v := make([]int32, n)
			for j := 0; j < n; j++ {
				clear(v)
				v[j] = amp
				tx1D(v, l, kind)
				for i := 0; i < n; i++ {
					m[i*n+j] = float64(v[i]) / amp
				}
			}
			fwdMat[kind][l] = invertMatrix(m, n)
		}
	}
}

// invertMatrix inverts an n x n matrix by Gauss-Jordan elimination with
// partial pivoting.
func invertMatrix(m []float64, n int) []float64 {
	a := append([]float64(nil), m...)
	inv := make([]float64, n*n)
	for i := 0; i < n; i++ {
		inv[i*n+i] = 1
	}
	for col := 0; col < n; col++ {
		p := col
		for r := col + 1; r < n; r++ {
			if math.Abs(a[r*n+col]) > math.Abs(a[p*n+col]) {
				p = r
			}
		}
		if p != col {
			for k := 0; k < n; k++ {
				a[p*n+k], a[col*n+k] = a[col*n+k], a[p*n+k]
				inv[p*n+k], inv[col*n+k] = inv[col*n+k], inv[p*n+k]
			}
		}
		d := a[col*n+col]
		for k := 0; k < n; k++ {
			a[col*n+k] /= d
			inv[col*n+k] /= d
		}
		for r := 0; r < n; r++ {
			if r == col || a[r*n+col] == 0 {
				continue
			}
			f := a[r*n+col]
			for k := 0; k < n; k++ {
				a[r*n+k] -= f * a[col*n+k]
				inv[r*n+k] -= f * inv[col*n+k]
			}
		}
	}
	return inv
}

// fwdTx transforms the h x w residual res (row-major) for transform size
// tx and type typ into out (th x tw row-major, tw = min(w, 32), th =
// min(h, 32)): the coefficients the decoder's inverse maps back to res.
func fwdTx(res []int32, tx, typ int, tmp, out []float64) {
	fwdMatOnce.Do(initFwdMats)
	w, h := txW[tx], txH[tx]
	lw, lh := txWLog2[tx], txHLog2[tx]
	tw, th := min(w, 32), min(h, 32)
	rowT, colT, flipLR, flipUD := txKinds(typ)
	fr, fc := fwdMat[rowT][lw], fwdMat[colT][lh]
	// tmp = Fc * flip(res): h x w.
	for i := 0; i < th; i++ {
		frow := fc[i*h : i*h+h]
		dst := tmp[i*w : i*w+w]
		clear(dst)
		for k := 0; k < h; k++ {
			f := frow[k]
			if f == 0 {
				continue
			}
			sr := k
			if flipUD {
				sr = h - 1 - k
			}
			src := res[sr*w : sr*w+w]
			if flipLR {
				for j := range dst {
					dst[j] += f * float64(src[w-1-j])
				}
			} else {
				for j := range dst {
					dst[j] += f * float64(src[j])
				}
			}
		}
	}
	// out = tmp * Fr^T / g, g = 2^-(4+rowShift) (x 2896/4096 when 2:1).
	scale := float64(int(1) << uint(4+txRowShift[tx]))
	if lw-lh == 1 || lh-lw == 1 {
		scale *= 4096.0 / 2896
	}
	for i := 0; i < th; i++ {
		src := tmp[i*w : i*w+w]
		for j := 0; j < tw; j++ {
			frow := fr[j*w : j*w+w]
			s := 0.0
			for k, v := range src {
				s += frow[k] * v
			}
			out[i*tw+j] = s * scale
		}
	}
}

// encTQ is the scratch of transform coding.
type encTQ struct {
	res    [64 * 64]int32
	tmp    [64 * 64]float64
	coef   [32 * 32]float64
	levels [32 * 32]int32
}

// txTypeIndex maps a transform type to its symbol in each set:
// [inter][set][type], -1 when absent.
var txTypeIndex [2][4][16]int8

func init() {
	for i := range txTypeIndex {
		for j := range txTypeIndex[i] {
			for k := range txTypeIndex[i][j] {
				txTypeIndex[i][j][k] = -1
			}
		}
	}
	for i, t := range txTypeIntraInv1 {
		txTypeIndex[0][1][t] = int8(i)
	}
	for i, t := range txTypeIntraInv2 {
		txTypeIndex[0][2][t] = int8(i)
	}
	for i, t := range txTypeInterInv1 {
		txTypeIndex[1][1][t] = int8(i)
	}
	for i, t := range txTypeInterInv2 {
		txTypeIndex[1][2][t] = int8(i)
	}
	for i, t := range txTypeInterInv3 {
		txTypeIndex[1][3][t] = int8(i)
	}
}

// writeTxType mirrors readTxType.
func (t *tile) writeTxType(k *symSink, tx, typ int) {
	b := &t.b
	set := t.txSet(tx)
	q := t.h.baseQIdx
	if t.h.seg.enabled {
		q = t.h.qindex(true, b.segID, 0)
	}
	if set == 0 || q == 0 {
		return
	}
	sqr := txSqr[tx]
	if b.isInter {
		idx := int(txTypeIndex[1][set][typ])
		switch set {
		case 1:
			k.sym(t.cdf.interTxType[1][sqr][:], idx, 16)
		case 2:
			k.sym(t.cdf.interTxType[2][sqr][:], idx, 12)
		default:
			k.sym(t.cdf.interTxType[3][sqr][:], idx, 2)
		}
		return
	}
	dir := b.yMode
	if b.useFilterIntra {
		dir = filterIntraModeToIntraDir[b.filterIntraMode]
	}
	idx := int(txTypeIndex[0][set][typ])
	if set == 1 {
		k.sym(t.cdf.intraTxType[1][sqr][dir][:], idx, 7)
	} else {
		k.sym(t.cdf.intraTxType[2][sqr][dir][:], idx, 5)
	}
}

// writeCoeffs mirrors coeffs(): it codes the levels (signed, row-major
// width min(w, 32), at most eob in scan order non-zero) of one transform
// block of type typ and updates the coefficient contexts. t.coef is used
// as the decoder uses it while parsing and holds the levels afterwards.
func (t *tile) writeCoeffs(k *symSink, plane, startX, startY, tx, typ int, levels []int32, eob int) {
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
	ctx := t.txbSkipCtx(plane, x4, y4, w4, h4, maxX4, maxY4, tx)
	k.flag(t.cdf.txbSkip[txSzCtx][ctx][:], eob == 0)
	culLevel, dcCategory := 0, 0
	if eob == 0 {
		if plane == 0 {
			t.setTxTypes(x4, y4, tx, dctDct)
		}
	} else {
		if plane == 0 {
			t.writeTxType(k, tx, typ)
			t.setTxTypes(x4, y4, tx, typ)
		}
		scan := scanFor(tx, typ)
		class := txClass(typ)
		eobCtx := 0
		if class != txClass2D {
			eobCtx = 1
		}
		eobPt := bits.Len(uint(eob-1)) + 1
		switch min(txWLog2[tx], 5) + min(txHLog2[tx], 5) - 4 {
		case 0:
			k.sym(t.cdf.eobPt16[ptype][eobCtx][:], eobPt-1, 5)
		case 1:
			k.sym(t.cdf.eobPt32[ptype][eobCtx][:], eobPt-1, 6)
		case 2:
			k.sym(t.cdf.eobPt64[ptype][eobCtx][:], eobPt-1, 7)
		case 3:
			k.sym(t.cdf.eobPt128[ptype][eobCtx][:], eobPt-1, 8)
		case 4:
			k.sym(t.cdf.eobPt256[ptype][eobCtx][:], eobPt-1, 9)
		case 5:
			k.sym(t.cdf.eobPt512[ptype][0][:], eobPt-1, 10)
		default:
			k.sym(t.cdf.eobPt1024[ptype][0][:], eobPt-1, 11)
		}
		if eobPt >= 3 {
			extra := eob - (1<<uint(eobPt-2) + 1)
			shift := eobPt - 3
			k.flag(t.cdf.eobExtra[txSzCtx][ptype][eobPt-3][:], extra>>uint(shift)&1 != 0)
			for i := 1; i < eobPt-2; i++ {
				shift = eobPt - 3 - i
				k.lit(1, extra>>uint(shift)&1)
			}
		}
		for c := eob - 1; c >= 0; c-- {
			pos := int(scan[c])
			level := int(min(iabs32(levels[pos]), 15))
			if c == eob-1 {
				cctx := 3
				switch {
				case c == 0:
					cctx = 0
				case c <= (th<<bwl)/8:
					cctx = 1
				case c <= (th<<bwl)/4:
					cctx = 2
				}
				k.sym(t.cdf.coeffBaseEob[txSzCtx][ptype][cctx][:], min(level, 3)-1, 3)
			} else {
				k.sym(t.cdf.coeffBase[txSzCtx][ptype][t.coeffBaseCtx(tx, class, bwl, tw, th, pos)][:], min(level, 3), 4)
			}
			if level > 2 {
				brCtx := t.coeffBrCtx(class, bwl, tw, th, pos)
				rem := level - 3
				for idx := 0; idx < 4; idx++ {
					br := min(rem, 3)
					k.sym(t.cdf.coeffBr[min(txSzCtx, 3)][ptype][brCtx][:], br, 4)
					rem -= br
					if br < 3 {
						break
					}
				}
			}
			quant[pos] = int32(level)
		}
		for c := 0; c < eob; c++ {
			pos := int(scan[c])
			v := levels[pos]
			if v != 0 {
				if c == 0 {
					s := 0
					if v < 0 {
						s = 1
					}
					k.sym(t.cdf.dcSign[ptype][t.dcSignCtx(plane, x4, y4, w4, h4, maxX4, maxY4)][:], s, 2)
				} else {
					s := 0
					if v < 0 {
						s = 1
					}
					k.lit(1, s)
				}
			}
			av := int(iabs32(v))
			if av > 14 {
				x := av - 14
				length := bits.Len(uint(x))
				for i := 0; i < length-1; i++ {
					k.lit(1, 0)
				}
				k.lit(1, 1)
				for i := length - 2; i >= 0; i-- {
					k.lit(1, x>>uint(i)&1)
				}
			}
			if pos == 0 && v != 0 {
				dcCategory = 2
				if v < 0 {
					dcCategory = 1
				}
			}
			culLevel += av & 0xFFFFF
			quant[pos] = v
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
}

// quantize turns coefficients (dequantised scale, row-major tw x th)
// into levels with a dead zone, returning the end of block in scan order.
// shift is the dequantisation shift of large transforms.
func quantize(coef []float64, levels []int32, scan []int16, dcq, acq int32, shift uint, dcBias, acBias float64) int {
	eob := 0
	for c, p := range scan {
		pos := int(p)
		q := float64(acq)
		bias := acBias
		if pos == 0 {
			q, bias = float64(dcq), dcBias
		}
		v := coef[pos] * float64(int(1)<<shift) / q
		l := int32(math.Abs(v) + bias)
		if l > 0 {
			eob = c + 1
		}
		if v < 0 {
			l = -l
		}
		levels[pos] = l
	}
	return eob
}
