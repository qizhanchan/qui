package opus

import (
	"math"
	"sync"
)

// Inverse MDCT with CELT's low-overlap window (celt/mdct.c, backward
// direction) on top of a small mixed-radix complex FFT.

type fftPlan struct {
	n       int
	factors []int
	tw      []complex128 // e^{+2*pi*i*k/n}
}

func newFFTPlan(n int) *fftPlan {
	p := &fftPlan{n: n}
	m := n
	for _, r := range []int{4, 2, 3, 5} {
		for m%r == 0 {
			p.factors = append(p.factors, r)
			m /= r
		}
	}
	if m != 1 {
		p.factors = append(p.factors, m)
	}
	p.tw = make([]complex128, n)
	for k := range p.tw {
		a := 2 * math.Pi * float64(k) / float64(n)
		p.tw[k] = complex(math.Cos(a), math.Sin(a))
	}
	return p
}

// inverse computes out[k] = sum_j in[j] e^{+2 pi i jk/n} (unscaled).
func (p *fftPlan) inverse(in, out, scratch []complex128) {
	p.rec(in, 1, out, scratch, p.n, 0)
}

func (p *fftPlan) rec(in []complex128, stride int, out, scratch []complex128, n, fi int) {
	if n == 1 {
		out[0] = in[0]
		return
	}
	r := p.factors[fi]
	m := n / r
	// Sub-transforms of the decimated sequences.
	for q := 0; q < r; q++ {
		p.rec(in[q*stride:], stride*r, out[q*m:], scratch, m, fi+1)
	}
	twStep := p.n / n
	var tmp [8]complex128
	for k := 0; k < m; k++ {
		for q := 0; q < r; q++ {
			tmp[q] = out[q*m+k] * p.tw[(q*k*twStep)%p.n]
		}
		for s := 0; s < r; s++ {
			var acc complex128
			for q := 0; q < r; q++ {
				acc += tmp[q] * p.tw[((q*s*m)%n)*twStep]
			}
			scratch[s*m+k] = acc
		}
	}
	copy(out[:n], scratch[:n])
}

type mdctState struct {
	n     int // full MDCT size (2x number of coefficients)
	fft   *fftPlan
	trig  []float64 // trig[i] = cos(2*pi*i/n), i <= n/4
	sineC float64   // cos/sin of the 1/8-bin rotation
	sineS float64
}

// mdctScratch holds the per-decoder work buffers of the IMDCT.
type mdctScratch struct {
	f, f2, sc [480]complex128
	tmp, g    [960]float64
}

var (
	mdctOnce   sync.Once
	mdctStates [maxLM + 1]*mdctState // index by shift: n = 1920>>shift
)

func getMDCT(shift int) *mdctState {
	mdctOnce.Do(func() {
		for s := 0; s <= maxLM; s++ {
			n := 1920 >> uint(s)
			m := &mdctState{n: n, fft: newFFTPlan(n / 4)}
			m.trig = make([]float64, n/4+1)
			for i := range m.trig {
				m.trig[i] = math.Cos(2 * math.Pi * float64(i) / float64(n))
			}
			th := 2 * math.Pi * 0.125 / float64(n)
			m.sineC, m.sineS = math.Cos(th), math.Sin(th)
			mdctStates[s] = m
		}
	})
	return mdctStates[shift]
}

// backward computes the IMDCT of n/2 coefficients read from in with the
// given stride and overlap-adds the windowed result into out[pos:], where
// the first overlap samples are accumulated (TDAC) and the rest assigned.
func (l *mdctState) backward(in []float32, stride int, out []float32, pos int, window []float32, overlap int, s *mdctScratch) {
	N := l.n
	N2 := N >> 1
	N4 := N >> 2
	f := s.f[:N4]
	f2 := s.f2[:N4]
	sc := s.sc[:N4]
	t := l.trig
	cs, sn := l.sineC, l.sineS
	for i := 0; i < N4; i++ {
		x1 := float64(in[2*i*stride])
		x2 := float64(in[stride*(N2-1)-2*i*stride])
		c, s := t[i], t[N4-i]
		yr := -x2*c + x1*s
		yi := -x2*s - x1*c
		f2[i] = complex(yr*cs-yi*sn, yi*cs+yr*sn)
	}
	l.fft.inverse(f2, f, sc)
	tmp := s.tmp[:N2]
	for i := 0; i < N4; i++ {
		re, im := real(f[i]), imag(f[i])
		c, s := t[i], t[N4-i]
		yr := re*c - im*s
		yi := im*c + re*s
		tmp[2*i] = yr*cs - yi*sn
		tmp[2*i+1] = yi*cs + yr*sn
	}
	// De-shuffle.
	g := s.g[:N2]
	for i := 0; i < N4; i++ {
		g[2*i] = -tmp[2*i]
		g[2*i+1] = tmp[N2-1-2*i]
	}
	base := pos - ((N2 - overlap) >> 1)
	// Mirror on both sides for TDAC.
	{
		fp1 := N4 - 1
		xp1 := base + N2 - 1
		yp1 := base + N4 - overlap/2
		i := 0
		for ; i < N4-overlap/2; i++ {
			out[xp1] = float32(g[fp1])
			xp1--
			fp1--
		}
		for k := 0; i < N4; i, k = i+1, k+1 {
			x1 := g[fp1]
			fp1--
			out[yp1] += -window[k] * float32(x1)
			out[xp1] += window[overlap-1-k] * float32(x1)
			yp1++
			xp1--
		}
	}
	{
		fp2 := N4
		xp2 := base + N2
		yp2 := base + N - 1 - (N4 - overlap/2)
		i := 0
		for ; i < N4-overlap/2; i++ {
			out[xp2] = float32(g[fp2])
			xp2++
			fp2++
		}
		for k := 0; i < N4; i, k = i+1, k+1 {
			x2 := g[fp2]
			fp2++
			out[yp2] = window[k] * float32(x2)
			out[xp2] = window[overlap-1-k] * float32(x2)
			yp2--
			xp2++
		}
	}
}
