package hevc

// Scaling (dequantisation) and inverse transforms (spec 8.6.2-8.6.4).

import "math/bits"

var levelScale = [6]int32{40, 45, 51, 57, 64, 72}

// dctBasis[k] approximates 64*sqrt(2)*cos(k*pi/64) as fixed by the
// standard; transMatrix[m][n] = c((2n+1)*m mod 128) with the symmetric
// extension of dctBasis, and row 0 = 64.
var dctBasis = [32]int32{
	64, 90, 90, 90, 89, 88, 87, 85, 83, 82, 80, 78, 75, 73, 70, 67,
	64, 61, 57, 54, 50, 46, 43, 38, 36, 31, 25, 22, 18, 13, 9, 4,
}

// transMatrix is the 32x32 DCT matrix (row = frequency); smaller sizes use
// rows m*32/n and the first n columns.
var transMatrix [32][32]int32

var dstMatrix = [4][4]int32{
	{29, 55, 74, 84},
	{74, 74, 0, -74},
	{84, -29, -74, 55},
	{55, -84, 74, -29},
}

func init() {
	c := func(k int) int32 {
		k &= 127
		switch {
		case k == 32 || k == 96:
			return 0
		case k < 32:
			return dctBasis[k]
		case k < 64:
			return -dctBasis[64-k]
		case k < 96:
			return -dctBasis[k-64]
		}
		return dctBasis[128-k]
	}
	for m := 0; m < 32; m++ {
		for n := 0; n < 32; n++ {
			if m == 0 {
				transMatrix[m][n] = 64
			} else {
				transMatrix[m][n] = c((2*n + 1) * m)
			}
		}
	}
	initBasis()
}

// dequant scales levels in place (raster, n x n): coefficients from
// TransCoeffLevel to d[x][y] (8.6.3) for qP and the scaling factors m
// (nil = flat 16). nz, when not nil, lists every non-zero position; nil
// scans the block. It returns the columns holding a non-zero level (bit
// x for column x) and the last row holding one.
func dequant(c []int32, n, qp int, m []uint8, nz []int16) (cols uint32, by int) {
	lg := log2(n)
	bdShift := uint(8 + lg - 5)
	ls := int64(levelScale[qp%6] << uint(qp/6))
	add := int64(1) << (bdShift - 1)
	scale := func(i int) {
		f := int64(16)
		if m != nil {
			f = int64(m[i])
		}
		d := (int64(c[i])*f*ls + add) >> bdShift
		c[i] = int32(min(max(d, -32768), 32767))
		cols |= 1 << uint(i&(n-1))
		by = max(by, i>>lg)
	}
	if nz != nil {
		for _, i := range nz {
			scale(int(i))
		}
		return cols, by
	}
	for i := 0; i < n*n; i++ {
		if c[i] != 0 {
			scale(i)
		}
	}
	return cols, by
}

func clip16(v int32) int32 {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return v
}

// basis[k] is the n x n inverse-transform matrix for n = 4 << k
// (basis[k][j*n+y] = transMatrix[j*32/n][y]); dstBasis is the 4x4 DST.
// Both are filled by init after transMatrix.
var (
	basis    [4][]int32
	dstBasis []int32
)

func initBasis() {
	for k := 0; k < 4; k++ {
		n := 4 << k
		basis[k] = make([]int32, n*n)
		for j := 0; j < n; j++ {
			for y := 0; y < n; y++ {
				basis[k][j*n+y] = transMatrix[j*32/n][y]
			}
		}
	}
	dstBasis = make([]int32, 16)
	for j := 0; j < 4; j++ {
		for y := 0; y < 4; y++ {
			dstBasis[j*4+y] = dstMatrix[j][y]
		}
	}
}

// inverseTransform turns d (raster n x n, in place) into residual
// samples (8.6.4.2). dst4 selects the 4x4 DST. cols has bit x set when
// column x holds a non-zero input, and by is the last row holding one.
//
// Most blocks are very sparse (a few levels spread over a large area),
// so the default accumulates basis rows scaled by the non-zero inputs of
// the non-zero columns only: the vertical stage fills those columns of
// tmp, and the horizontal stage reads only them. Blocks with many
// non-zero columns run HM's partial butterflies instead. Both equal the
// matrix product exactly (integer arithmetic, rounding after each stage).
func inverseTransform(c []int32, n int, dst4 bool, tmp []int32, cols uint32, by int) {
	if dst4 {
		inverseDST4(c, tmp)
		return
	}
	if cols <= 1 && by == 0 {
		// DC only: both stages are a scale by 64.
		v := (64*clip16((64*c[0]+64)>>7) + 2048) >> 12
		for i := range c[:n*n] {
			c[i] = v
		}
		return
	}
	if bits.OnesCount32(cols) > n/4 {
		inverseButterfly(c, n, tmp, bits.Len32(cols)-1, by)
		return
	}
	m := basis[log2(n)-2]
	var acc [32]int32
	for cm := cols; cm != 0; cm &= cm - 1 {
		x := bits.TrailingZeros32(cm)
		a := acc[:n]
		clear(a)
		for j := 0; j <= by; j++ {
			cj := c[j*n+x]
			if cj == 0 {
				continue
			}
			row := m[j*n : j*n+n]
			for y, v := range row {
				a[y] += v * cj
			}
		}
		for y, v := range a {
			tmp[y*n+x] = clip16((v + 64) >> 7)
		}
	}
	for y := 0; y < n; y++ {
		a := acc[:n]
		clear(a)
		g := tmp[y*n : y*n+n]
		for cm := cols; cm != 0; cm &= cm - 1 {
			j := bits.TrailingZeros32(cm)
			gj := g[j]
			if gj == 0 {
				continue
			}
			row := m[j*n : j*n+n]
			for x, v := range row {
				a[x] += v * gj
			}
		}
		out := c[y*n : y*n+n]
		for x, v := range a {
			out[x] = (v + 2048) >> 12
		}
	}
}

// inverseButterfly is inverseTransform by partial butterflies over the
// inputs at columns <= bx and rows <= by.
func inverseButterfly(c []int32, n int, tmp []int32, bx, by int) {
	var in, out [32]int32
	// Vertical (columns), into tmp; columns beyond bx are zero.
	for x := 0; x < n; x++ {
		if x > bx {
			for y := 0; y < n; y++ {
				tmp[y*n+x] = 0
			}
			continue
		}
		for j := 0; j <= by; j++ {
			in[j] = c[j*n+x]
		}
		invButterfly(in[:], n, by+1, out[:])
		for y := 0; y < n; y++ {
			tmp[y*n+x] = clip16((out[y] + 64) >> 7)
		}
	}
	// Horizontal: only the first bx+1 inputs of each row can be non-zero.
	for y := 0; y < n; y++ {
		invButterfly(tmp[y*n:y*n+bx+1], n, bx+1, out[:])
		r := c[y*n : y*n+n]
		for x := range r {
			r[x] = (out[x] + 2048) >> 12
		}
	}
}

// invButterfly sets out[k] = sum_{j<lim} M_n[j][k] * in[j] for k < n,
// splitting into the even rows (the n/2 transform) and the odd rows,
// which are symmetric and antisymmetric about n/2.
func invButterfly(in []int32, n, lim int, out []int32) {
	if n == 4 {
		var s0, s1, s2, s3 int32
		s0 = in[0]
		if lim > 1 {
			s1 = in[1]
		}
		if lim > 2 {
			s2 = in[2]
		}
		if lim > 3 {
			s3 = in[3]
		}
		e0, e1 := 64*(s0+s2), 64*(s0-s2)
		o0, o1 := 83*s1+36*s3, 36*s1-83*s3
		out[0], out[1], out[2], out[3] = e0+o0, e1+o1, e1-o1, e0-o0
		return
	}
	h := n / 2
	var even, e, o [16]int32
	for j := 0; 2*j < lim; j++ {
		even[j] = in[2*j]
	}
	invButterfly(even[:], h, (lim+1)/2, e[:])
	m := basis[log2(n)-2]
	for j := 1; j < lim; j += 2 {
		v := in[j]
		if v == 0 {
			continue
		}
		row := m[j*n : j*n+h]
		for k, b := range row {
			o[k] += b * v
		}
	}
	for k := 0; k < h; k++ {
		out[k], out[n-1-k] = e[k]+o[k], e[k]-o[k]
	}
}

// inverseDST4 is the 4x4 DST of intra luma (8.6.4.2 with trType 1).
func inverseDST4(c, tmp []int32) {
	m := dstBasis
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			var a int32
			for j := 0; j < 4; j++ {
				a += m[j*4+y] * c[j*4+x]
			}
			tmp[y*4+x] = clip16((a + 64) >> 7)
		}
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			var a int32
			for j := 0; j < 4; j++ {
				a += m[j*4+x] * tmp[y*4+j]
			}
			c[y*4+x] = (a + 2048) >> 12
		}
	}
}

// addResidual adds r (raster n x n) to the prediction in dst.
func addResidual(dst []byte, ds int, r []int32, n int) {
	for y := 0; y < n; y++ {
		row := dst[y*ds : y*ds+n]
		rr := r[y*n : y*n+n]
		for x := range row {
			row[x] = clipU8(int(row[x]) + int(rr[x]))
		}
	}
}
