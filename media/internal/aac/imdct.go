package aac

import "math"

// Filterbank: IMDCT via a DCT-IV computed with a complex FFT, and the sine /
// Kaiser-Bessel-derived windows (ISO/IEC 14496-3 4.6.11).

type fft struct {
	n       int
	rev     []int
	twRe    []float64
	twIm    []float64
	preRe   []float64 // DCT-IV pre/post twiddles
	preIm   []float64
	postRe  []float64
	postIm  []float64
	bufRe   []float64
	bufIm   []float64
	scratch []float64
}

// newDCT4 prepares a DCT-IV of size m (m/2-point complex FFT).
func newDCT4(m int) *fft {
	n := m / 2
	f := &fft{n: n, rev: make([]int, n), twRe: make([]float64, n/2), twIm: make([]float64, n/2),
		preRe: make([]float64, n), preIm: make([]float64, n), bufRe: make([]float64, n), bufIm: make([]float64, n),
		scratch: make([]float64, m)}
	bitsN := 0
	for 1<<uint(bitsN) < n {
		bitsN++
	}
	for i := 0; i < n; i++ {
		r := 0
		for b := 0; b < bitsN; b++ {
			if i&(1<<uint(b)) != 0 {
				r |= 1 << uint(bitsN-1-b)
			}
		}
		f.rev[i] = r
	}
	for i := 0; i < n/2; i++ {
		a := -2 * math.Pi * float64(i) / float64(n)
		f.twRe[i], f.twIm[i] = math.Cos(a), math.Sin(a)
	}
	for k := 0; k < n; k++ {
		a := -math.Pi * float64(4*k+1) / float64(4*m)
		f.preRe[k], f.preIm[k] = math.Cos(a), math.Sin(a)
	}
	f.postRe, f.postIm = make([]float64, n), make([]float64, n)
	for k := 0; k < n; k++ {
		a := -math.Pi * float64(k) / float64(m)
		f.postRe[k], f.postIm[k] = math.Cos(a), math.Sin(a)
	}
	return f
}

func (f *fft) transform(re, im []float64) {
	n := f.n
	for i := 0; i < n; i++ {
		if j := f.rev[i]; j > i {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		step := n / size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				wr, wi := f.twRe[k*step], f.twIm[k*step]
				a, b := start+k, start+k+half
				tr := re[b]*wr - im[b]*wi
				ti := re[b]*wi + im[b]*wr
				re[b], im[b] = re[a]-tr, im[a]-ti
				re[a] += tr
				im[a] += ti
			}
		}
	}
}

// dct4 computes out[n] = sum_k in[k] cos(pi/M (n+1/2)(k+1/2)), M = len(in).
func (f *fft) dct4(in, out []float64) {
	m := len(in)
	n := m / 2
	re, im := f.bufRe, f.bufIm
	for k := 0; k < n; k++ {
		a, b := in[2*k], in[m-1-2*k]
		re[k] = a*f.preRe[k] - b*f.preIm[k]
		im[k] = a*f.preIm[k] + b*f.preRe[k]
	}
	f.transform(re, im)
	for k := 0; k < n; k++ {
		wr := re[k]*f.postRe[k] - im[k]*f.postIm[k]
		wi := re[k]*f.postIm[k] + im[k]*f.postRe[k]
		out[2*k] = wr
		out[m-1-2*k] = -wi
	}
}

// imdct computes the N = 2*len(spec) output samples of the inverse MDCT,
// scaled by 2/N, into out.
func (f *fft) imdct(spec []float64, out []float64) {
	m := len(spec)
	u := f.scratch[:m]
	f.dct4(spec, u)
	scale := 2.0 / float64(2*m)
	h := m / 2
	for i := 0; i < h; i++ {
		out[i] = u[i+h] * scale
	}
	for i := h; i < 3*h; i++ {
		out[i] = -u[3*h-1-i] * scale
	}
	for i := 3 * h; i < 4*h; i++ {
		out[i] = -u[i-3*h] * scale
	}
}

// Windows: [shape][n] for shape 0 = sine, 1 = KBD; long has 1024 rising
// samples, short 128.
var (
	winLong  [2][1024]float64
	winShort [2][128]float64
)

func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 50; k++ {
		term *= (x / 2 / float64(k)) * (x / 2 / float64(k))
		sum += term
	}
	return sum
}

func kbd(out []float64, alpha float64) {
	n := len(out) // half window length
	w := make([]float64, n+1)
	for i := 0; i <= n; i++ {
		r := 2*float64(i)/float64(n) - 1 // over the N/2+1 Kaiser points
		w[i] = besselI0(math.Pi * alpha * math.Sqrt(1-r*r))
	}
	total := 0.0
	for i := 0; i <= n; i++ {
		total += w[i]
	}
	acc := 0.0
	for i := 0; i < n; i++ {
		acc += w[i]
		out[i] = math.Sqrt(acc / total)
	}
}

func init() {
	for i := 0; i < 1024; i++ {
		winLong[0][i] = math.Sin(math.Pi / 2048 * (float64(i) + 0.5))
	}
	for i := 0; i < 128; i++ {
		winShort[0][i] = math.Sin(math.Pi / 256 * (float64(i) + 0.5))
	}
	kbd(winLong[1][:], 4)
	kbd(winShort[1][:], 6)
}
