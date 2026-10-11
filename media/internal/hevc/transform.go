package hevc

// Scaling (dequantisation) and inverse transforms (spec 8.6.2-8.6.4).

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
// (nil = flat 16).
func dequant(c []int32, n, qp int, m []uint8) {
	bdShift := uint(8 + log2(n) - 5)
	ls := levelScale[qp%6] << uint(qp/6)
	add := int64(1) << (bdShift - 1)
	for i := 0; i < n*n; i++ {
		v := c[i]
		if v == 0 {
			continue
		}
		f := int64(16)
		if m != nil {
			f = int64(m[i])
		}
		d := (int64(v)*f*int64(ls) + add) >> bdShift
		if d < -32768 {
			d = -32768
		} else if d > 32767 {
			d = 32767
		}
		c[i] = int32(d)
	}
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
// samples (8.6.4.2). dst4 selects the 4x4 DST. Both stages accumulate
// basis rows scaled by the non-zero inputs only, which keeps sparse
// blocks (the common case) cheap.
func inverseTransform(c []int32, n int, dst4 bool, tmp []int32) {
	var m []int32
	switch {
	case dst4:
		m = dstBasis
	default:
		m = basis[log2(n)-2]
	}
	var acc [32]int32
	// Vertical (columns): e[y][x] = sum_j M[j][y] * d[j][x].
	maxCol := -1
	for x := 0; x < n; x++ {
		a := acc[:n]
		clear(a)
		any := false
		for j := 0; j < n; j++ {
			cj := c[j*n+x]
			if cj == 0 {
				continue
			}
			any = true
			row := m[j*n : j*n+n]
			for y, v := range row {
				a[y] += v * cj
			}
		}
		if !any {
			for y := 0; y < n; y++ {
				tmp[y*n+x] = 0
			}
			continue
		}
		maxCol = x
		for y, v := range a {
			tmp[y*n+x] = clip16((v + 64) >> 7)
		}
	}
	// Horizontal: r[y][x] = sum_j M[j][x] * g[y][j], j <= maxCol.
	for y := 0; y < n; y++ {
		g := tmp[y*n : y*n+maxCol+1]
		a := acc[:n]
		clear(a)
		for j, gj := range g {
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
