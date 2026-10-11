package av1

import (
	"math/rand"
	"testing"
)

// TestMsacRoundTrip codes random symbols of random adaptive CDFs, literal
// bits and fixed-probability bools, and decodes them with msac.
func TestMsacRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for iter := 0; iter < 200; iter++ {
		type item struct {
			kind, n, s, p int
			cdf           int // index of the CDF
		}
		// A few CDFs of 2..16 symbols, twice: one copy for each side.
		const ncdf = 6
		var encCDF, decCDF [ncdf][]uint16
		sizes := [ncdf]int{}
		for i := range encCDF {
			n := 2 + rng.Intn(15)
			sizes[i] = n
			c := make([]uint16, n+1)
			// a random decreasing inverted CDF ending at 0
			v := 32768
			for k := 0; k < n-1; k++ {
				v -= 1 + rng.Intn(max(1, v/(n-k)))
				c[k] = uint16(max(v, 0))
			}
			encCDF[i] = c
			decCDF[i] = append([]uint16(nil), c...)
		}
		var items []item
		var e msacEnc
		e.init()
		for k := 0; k < 1+rng.Intn(3000); k++ {
			switch rng.Intn(3) {
			case 0:
				ci := rng.Intn(ncdf)
				n := sizes[ci]
				s := rng.Intn(n)
				if rng.Intn(2) == 0 { // skewed
					s = 0
				}
				items = append(items, item{kind: 0, cdf: ci, n: n, s: s})
				e.sym(encCDF[ci], s, n)
			case 1:
				b := rng.Intn(2)
				items = append(items, item{kind: 1, s: b})
				e.bit(b)
			default:
				p := 1 + rng.Intn(32766)
				b := rng.Intn(2)
				items = append(items, item{kind: 2, s: b, p: p})
				e.encode([]uint16{uint16(p), 0}, b, 2)
			}
		}
		data := e.done()
		var m msac
		m.init(data, false)
		for i, it := range items {
			var got int
			switch it.kind {
			case 0:
				got = m.sym(decCDF[it.cdf], it.n)
			case 1:
				got = m.bit()
			default:
				got = m.decode([]uint16{uint16(it.p), 0}, 2)
			}
			if got != it.s {
				t.Fatalf("iter %d item %d (kind %d): got %d want %d", iter, i, it.kind, got, it.s)
			}
		}
	}
}
