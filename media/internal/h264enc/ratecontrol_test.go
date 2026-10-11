package h264enc

import (
	"image"
	"os"
	"testing"
)

// longSource ping-pongs a committed clip to n frames.
func longSource(t testing.TB, name string, n int) []*image.YCbCr {
	f := sourceFrames(t, name)
	var out []*image.YCbCr
	for i := 0; len(out) < n; i++ {
		k := i % (2*len(f) - 2)
		if k >= len(f) {
			k = 2*len(f) - 2 - k
		}
		out = append(out, f[k])
	}
	return out
}

// rcSource returns the opt-in real clip (H264ENC_YUV / H264ENC_SIZE) or the
// committed fallback.
func rcSource(t testing.TB, n int) []*image.YCbCr {
	if os.Getenv("H264ENC_YUV") != "" {
		return loadYUV(t, n)
	}
	return longSource(t, "vt_main_p8x8.264", n)
}

func encodeSizes(t *testing.T, cfg Config, frames []*image.YCbCr) ([]int, []int) {
	t.Helper()
	cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var sizes, qps []int
	collect := func(ps []Packet) {
		for _, p := range ps {
			n := 0
			for _, x := range p.NALs {
				n += 8 * len(x)
			}
			sizes = append(sizes, n)
			qps = append(qps, p.QP)
		}
	}
	for _, f := range frames {
		p, err := e.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		collect(p)
	}
	p, _ := e.Flush()
	collect(p)
	return sizes, qps
}

func sum(v []int) int {
	s := 0
	for _, x := range v {
		s += x
	}
	return s
}

func TestABR(t *testing.T) {
	frames := rcSource(t, 120)
	px := frames[0].Rect.Dx() * frames[0].Rect.Dy()
	for _, bpp := range []float64{0.05, 0.15, 0.4} {
		rate := int(bpp * float64(px) * 30)
		sizes, qps := encodeSizes(t, Config{Bitrate: rate, KeyInt: 60}, frames)
		got := float64(sum(sizes)) * 30 / float64(len(sizes))
		t.Logf("target %d bps: got %.0f (%.1f%%), QP %d..%d", rate, got, 100*(got/float64(rate)-1), minOf(qps), maxOf(qps))
		if got < 0.85*float64(rate) || got > 1.15*float64(rate) {
			if minOf(qps) > 0 && maxOf(qps) < 51 {
				t.Errorf("bitrate %.0f is not within 15%% of %d", got, rate)
			}
		}
	}
}

func TestVBV(t *testing.T) {
	frames := rcSource(t, 90)
	px := frames[0].Rect.Dx() * frames[0].Rect.Dy()
	rate := int(0.1 * float64(px) * 30)
	buf := rate / 2 // half a second
	for _, cfg := range []Config{
		{Bitrate: rate, VBVMaxRate: rate, VBVBuffer: buf, KeyInt: 30},
		{CRF: 18, VBVMaxRate: rate, VBVBuffer: buf, KeyInt: 30},
	} {
		sizes, qps := encodeSizes(t, cfg, frames)
		fill := 0.9 * float64(buf)
		for i, s := range sizes {
			fill -= float64(s)
			if fill < 0 {
				t.Fatalf("VBV underflow at picture %d (%d bits, QP %d)", i, s, qps[i])
			}
			fill = min(fill+float64(rate)/30, float64(buf))
		}
	}
}

func TestCRFMonotonic(t *testing.T) {
	frames := rcSource(t, 30)
	prev := 0
	for _, crf := range []float64{36, 30, 24, 18} {
		sizes, qps := encodeSizes(t, Config{CRF: crf}, frames)
		cq, _ := encodeSizes(t, Config{QP: int(crf)}, frames)
		t.Logf("CRF %.0f: %d bits (CQP %d bits), QP %d..%d", crf, sum(sizes), sum(cq), minOf(qps), maxOf(qps))
		if sum(sizes) <= prev {
			t.Errorf("CRF %.0f produced %d bits, not more than the previous %d", crf, sum(sizes), prev)
		}
		prev = sum(sizes)
	}
}

func minOf(v []int) int {
	m := v[0]
	for _, x := range v {
		m = min(m, x)
	}
	return m
}

func maxOf(v []int) int {
	m := v[0]
	for _, x := range v {
		m = max(m, x)
	}
	return m
}
