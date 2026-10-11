package h264

import (
	"math/rand"
	"testing"
)

// TestNEONMatchesGo checks every MC kernel against its Go reference on
// random data, for each block size and fraction.
func TestNEONMatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON kernels in this build")
	}
	rng := rand.New(rand.NewSource(3))
	const ss = 96
	src := make([]byte, ss*48)
	s := &sliceCtx{}
	for it := 0; it < 300; it++ {
		rng.Read(src)
		for _, wh := range [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}} {
			w, h := wh[0], wh[1]
			o := (4+rng.Intn(8))*ss + 4 + rng.Intn(40)
			var a, b [16 * 20]byte
			lumaHGo(a[:], 20, src, o, ss, w, h)
			lumaH(b[:], 20, src, o, ss, w, h)
			if a != b {
				t.Fatalf("lumaH %dx%d differs", w, h)
			}
			lumaVGo(a[:], 20, src, o, ss, w, h)
			lumaV(b[:], 20, src, o, ss, w, h)
			if a != b {
				t.Fatalf("lumaV %dx%d differs", w, h)
			}
			s.lumaHVGo(a[:], 20, src, o, ss, w, h)
			s.lumaHV(b[:], 20, src, o, ss, w, h)
			if a != b {
				t.Fatalf("lumaHV %dx%d differs", w, h)
			}
		}
		for _, wh := range [][2]int{{8, 8}, {8, 4}, {4, 8}, {4, 4}, {4, 2}} {
			w, h := wh[0], wh[1]
			fx, fy := rng.Intn(8), rng.Intn(8)
			if fx == 0 && fy == 0 {
				fx = 1
			}
			x, y := 2+rng.Intn(40), 2+rng.Intn(30)
			var a, b [16 * 20]byte
			s.mcChromaGoForTest(a[:], 20, src, ss, ss, 48, x, y, fx, fy, w, h)
			s.mcChroma(b[:], 20, src, ss, ss, 48, x, y, fx, fy, w, h)
			if a != b {
				t.Fatalf("mcChroma %dx%d (%d,%d) differs", w, h, fx, fy)
			}
		}
	}
}

// mcChromaGoForTest is the scalar bilinear path of mcChroma.
func (s *sliceCtx) mcChromaGoForTest(dst []byte, ds int, src []byte, ss, pw, ph, x, y, fx, fy, w, h int) {
	o := y*ss + x
	a := int32((8 - fx) * (8 - fy))
	b := int32(fx * (8 - fy))
	c := int32((8 - fx) * fy)
	dd := int32(fx * fy)
	for j := 0; j < h; j++ {
		r0 := src[o+j*ss : o+j*ss+w+1]
		r1 := src[o+(j+1)*ss : o+(j+1)*ss+w+1]
		d := dst[j*ds : j*ds+w]
		for i := range d {
			d[i] = byte((a*int32(r0[i]) + b*int32(r0[i+1]) + c*int32(r1[i]) + dd*int32(r1[i+1]) + 32) >> 6)
		}
	}
}

// TestNEONLumaEdgeMatchesGo checks the NEON luma loop filter against the
// scalar one for both edge directions and every bS mix it handles.
func TestNEONLumaEdgeMatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON kernels in this build")
	}
	rng := rand.New(rand.NewSource(4))
	const stride = 64
	for it := 0; it < 20000; it++ {
		a := make([]byte, stride*40)
		base := rng.Intn(200)
		spread := 1 + rng.Intn(60)
		for i := range a {
			a[i] = byte(base + rng.Intn(spread))
		}
		b := append([]byte(nil), a...)
		var bs [4]uint8
		if rng.Intn(3) == 0 {
			bs = [4]uint8{4, 4, 4, 4}
		} else {
			for k := range bs {
				bs[k] = uint8(rng.Intn(4))
			}
		}
		si := &sliceInfo{alphaOffset: rng.Intn(25) - 12, betaOffset: rng.Intn(25) - 12}
		qp := rng.Intn(52)
		off := 8*stride + 8 + rng.Intn(16)
		if it&1 == 0 {
			filterLumaGo(a, off, 1, stride, &bs, qp, si)
			filterLuma(b, off, 1, stride, &bs, qp, si)
		} else {
			filterLumaGo(a, off, stride, 1, &bs, qp, si)
			filterLuma(b, off, stride, 1, &bs, qp, si)
		}
		if string(a) != string(b) {
			for i := range a {
				if a[i] != b[i] {
					t.Fatalf("iteration %d (vertical=%v bs=%v qp=%d): first diff at %d,%d: go %d neon %d",
						it, it&1 == 0, bs, qp, i%stride, i/stride, a[i], b[i])
				}
			}
		}
	}
}

// TestNEONChromaEdgeMatchesGo checks the two-plane NEON chroma filter
// against the scalar one.
func TestNEONChromaEdgeMatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON kernels in this build")
	}
	rng := rand.New(rand.NewSource(5))
	const stride = 48
	for it := 0; it < 20000; it++ {
		mk := func() []byte {
			a := make([]byte, stride*24)
			base, spread := rng.Intn(200), 1+rng.Intn(60)
			for i := range a {
				a[i] = byte(base + rng.Intn(spread))
			}
			return a
		}
		cb, cr := mk(), mk()
		cb2, cr2 := append([]byte(nil), cb...), append([]byte(nil), cr...)
		var bs [4]uint8
		if rng.Intn(3) == 0 {
			bs = [4]uint8{4, 4, 4, 4}
		} else {
			for k := range bs {
				bs[k] = uint8(rng.Intn(4))
			}
		}
		si := &sliceInfo{alphaOffset: rng.Intn(25) - 12, betaOffset: rng.Intn(25) - 12}
		qp := [2]int{rng.Intn(52), rng.Intn(52)}
		off := 6*stride + 6 + rng.Intn(16)
		step, along := 1, stride
		if it&1 == 1 {
			step, along = stride, 1
		}
		filterChroma(cb, off, step, along, &bs, qp[0], si)
		filterChroma(cr, off, step, along, &bs, qp[1], si)
		filterChroma2(cb2, cr2, off, step, along, &bs, qp, si)
		if string(cb) != string(cb2) || string(cr) != string(cr2) {
			t.Fatalf("iteration %d (vertical=%v bs=%v qp=%v) differs", it, step == 1, bs, qp)
		}
	}
}

func TestNEONIdct4AddMatchesGo(t *testing.T) {
	if !haveNEON {
		t.Skip("no NEON kernels in this build")
	}
	rng := rand.New(rand.NewSource(6))
	for it := 0; it < 50000; it++ {
		var blk, blk2 [16]int32
		scale := []int32{64, 1024, 1 << 14, 1 << 22}[rng.Intn(4)]
		for i := range blk {
			if rng.Intn(3) > 0 {
				blk[i] = rng.Int31n(2*scale) - scale
			}
		}
		blk2 = blk
		a := make([]byte, 4*20)
		rng.Read(a)
		b := append([]byte(nil), a...)
		idct4AddGo(a, 20, &blk)
		idct4Add(b, 20, &blk2)
		if string(a) != string(b) || blk2 != ([16]int32{}) {
			t.Fatalf("iteration %d differs", it)
		}
	}
}
