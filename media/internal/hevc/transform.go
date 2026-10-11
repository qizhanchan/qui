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

// inverseTransform turns d (raster n x n, in place) into residual
// samples (8.6.4.2). dst4 selects the 4x4 DST.
func inverseTransform(c []int32, n int, dst4 bool, tmp []int32) {
	// Vertical (columns) first: e[x][y] = sum_j M[j][y] * d[x][j].
	step := 32 / n
	for x := 0; x < n; x++ {
		// Last non-zero row in this column bounds the work.
		last := -1
		for j := n - 1; j >= 0; j-- {
			if c[j*n+x] != 0 {
				last = j
				break
			}
		}
		for y := 0; y < n; y++ {
			var s int32
			for j := 0; j <= last; j++ {
				if dst4 {
					s += dstMatrix[j][y] * c[j*n+x]
				} else {
					s += transMatrix[j*step][y] * c[j*n+x]
				}
			}
			tmp[y*n+x] = clip16((s + 64) >> 7)
		}
	}
	// Horizontal: r[x][y] = sum_j M[j][x] * g[j][y].
	for y := 0; y < n; y++ {
		row := tmp[y*n : y*n+n]
		last := -1
		for j := n - 1; j >= 0; j-- {
			if row[j] != 0 {
				last = j
				break
			}
		}
		for x := 0; x < n; x++ {
			var s int32
			for j := 0; j <= last; j++ {
				if dst4 {
					s += dstMatrix[j][x] * row[j]
				} else {
					s += transMatrix[j*step][x] * row[j]
				}
			}
			c[y*n+x] = (s + 2048) >> 12
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
