package av1

import "math"

// Fast forward transforms. The decoder's integer DCTs are scaled
// cosine transforms up to the rounding of their 12-bit constants, so the
// forward DCT is a recursive DCT-II (Lee's algorithm, O(n log n)) with
// each output scaled to match fwdMat; ADST (n <= 16) keeps the matrix
// and the identity is a scale.

var (
	dctScale [7][]float64 // per log2 size and coefficient: fwdMat / cos basis
	leeCos   [7][]float64 // 1 / (2 cos(pi (2i+1) / 2N)) for Lee's step
	idScale  [7]float64
)

func initFastFwd() {
	for l := 2; l <= 6; l++ {
		n := 1 << uint(l)
		m := fwdMat[kDCT][l]
		s := make([]float64, n)
		for k := 0; k < n; k++ {
			// Least-squares fit of row k to the cosine basis.
			var num, den float64
			for i := 0; i < n; i++ {
				c := math.Cos(math.Pi * float64((2*i+1)*k) / float64(2*n))
				num += m[k*n+i] * c
				den += c * c
			}
			s[k] = num / den
		}
		dctScale[l] = s
		lc := make([]float64, n/2)
		for i := range lc {
			lc[i] = 1 / (2 * math.Cos(math.Pi*float64(2*i+1)/float64(2*n)))
		}
		leeCos[l] = lc
		if l <= 5 {
			idScale[l] = fwdMat[kIdentity][l][0]
		}
	}
}

var (
	cosPi4 = math.Cos(math.Pi / 4)
	c8a    = 1 / (2 * math.Cos(math.Pi/8))
	c8b    = 1 / (2 * math.Cos(3*math.Pi/8))
)

// dct4 is dctII of 4 samples (Lee's step unrolled).
func dct4(x []float64) {
	x = x[:4]
	a0, a1 := x[0]+x[3], x[1]+x[2]
	b0, b1 := (x[0]-x[3])*c8a, (x[1]-x[2])*c8b
	A0, A1 := a0+a1, (a0-a1)*cosPi4
	B0, B1 := b0+b1, (b0-b1)*cosPi4
	x[0], x[1], x[2], x[3] = A0, B0+B1, A1, B1
}

// dctII computes the unnormalised DCT-II of x (length 2^l) into x, using
// tmp (same length) as scratch.
func dctII(x, tmp []float64, l int) {
	n := len(x)
	switch n {
	case 1:
		return
	case 2:
		a, b := x[0], x[1]
		x[0] = a + b
		x[1] = (a - b) * cosPi4
		return
	case 4:
		dct4(x)
		return
	}
	h := n / 2
	a, b := tmp[:h], tmp[h:n]
	lc := leeCos[l]
	for i := 0; i < h; i++ {
		a[i] = x[i] + x[n-1-i]
		b[i] = (x[i] - x[n-1-i]) * lc[i]
	}
	dctII(a, x[:h], l-1)
	dctII(b, x[h:], l-1)
	for k := 0; k < h; k++ {
		x[2*k] = a[k]
	}
	for k := 0; k < h-1; k++ {
		x[2*k+1] = b[k] + b[k+1]
	}
	x[n-1] = b[h-1]
}

// fwd1D transforms v (length 2^l) in place with kind: the coefficients the
// decoder's tx1D maps back to v.
func fwd1D(v, tmp []float64, l, kind int) {
	n := len(v)
	switch kind {
	case kDCT:
		dctII(v, tmp, l)
		s := dctScale[l]
		for k := range v {
			v[k] *= s[k]
		}
	case kIdentity:
		sc := idScale[l]
		for i := range v {
			v[i] *= sc
		}
	default:
		m := fwdMat[kADST][l]
		copy(tmp[:n], v)
		for k := 0; k < n; k++ {
			row := m[k*n : k*n+n]
			s := 0.0
			for i, x := range tmp[:n] {
				s += row[i] * x
			}
			v[k] = s
		}
	}
}
