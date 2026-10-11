package h264enc

import (
	"math/rand"
	"testing"
)

// TestNEONMatchesGo pins the NEON SAD/SATD kernels to the Go code.
func TestNEONMatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON")
	}
	rng := rand.New(rand.NewSource(2))
	a := make([]byte, 64*40)
	b := make([]byte, 64*40)
	for i := 0; i < 3000; i++ {
		for j := range a {
			if i%3 == 0 {
				a[j], b[j] = byte(rng.Intn(256)), byte(rng.Intn(256))
			} else {
				a[j], b[j] = byte(100+rng.Intn(20)), byte(100+rng.Intn(20))
			}
		}
		w := []int{4, 8, 16}[rng.Intn(3)]
		h := []int{4, 8, 16}[rng.Intn(3)]
		as, bs := w+rng.Intn(40), w+rng.Intn(40)
		var got int
		neonSATD(&a[0], as, &b[0], bs, w, h, &got)
		want := 0
		for y := 0; y < h; y += 4 {
			for x := 0; x < w; x += 4 {
				want += satd4Raw(a[y*as+x:], as, b[y*bs+x:], bs)
			}
		}
		if got != want {
			t.Fatalf("SATD %dx%d: NEON %d, Go %d", w, h, got, want)
		}
		if w >= 8 {
			neonSAD(&a[0], as, &b[0], bs, w, h, &got)
			if want := sadGo(a, as, b, bs, w, h); got != want {
				t.Fatalf("SAD %dx%d: NEON %d, Go %d", w, h, got, want)
			}
		}
	}
}

func TestDCTQuant8MatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON")
	}
	rng := rand.New(rand.NewSource(3))
	src := make([]byte, 40*12)
	pred := make([]byte, 40*12)
	for i := 0; i < 5000; i++ {
		spread := []int{2, 10, 60, 256}[i%4]
		for j := range src {
			src[j] = byte(min(255, max(0, 128+rng.Intn(spread)-spread/2)))
			pred[j] = byte(min(255, max(0, 128+rng.Intn(spread)-spread/2)))
		}
		qp := rng.Intn(52)
		intra := i&1 == 1
		ss, ps := 8+rng.Intn(30), 8+rng.Intn(30)
		var got, want [64]int32
		mask := 0
		f := 1 << (15 + qp/6) / 6
		if intra {
			f = 1 << (15 + qp/6) / 3
		}
		neonDCTQuant8(&src[0], ss, &pred[0], ps, &mfRows[qp], f, 15+qp/6, &got, &mask)
		wantMask := 0
		for k := 0; k < 4; k++ {
			bx, by := (k&1)*4, (k>>1)*4
			b := (*[16]int32)(want[16*k : 16*k+16])
			fdct4(b, src[by*ss+bx:], ss, pred[by*ps+bx:], ps)
			if quant4(b, qp, intra, 0) > 0 {
				wantMask |= 1 << uint(k)
			}
		}
		if got != want || mask != wantMask {
			t.Fatalf("qp %d intra %v: NEON %v mask %b, Go %v mask %b", qp, intra, got, mask, want, wantMask)
		}
	}
}
