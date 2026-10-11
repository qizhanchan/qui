package av1

import (
	"image"
	"testing"
)

// rcSource returns n frames of real content: a committed vector's frames
// (320x180) ping-ponged.
func rcSource(t testing.TB, n int) []*image.YCbCr {
	t.Helper()
	src := testSource(t, "ra_p4", 12)
	var out []*image.YCbCr
	for i := 0; len(out) < n; i++ {
		k := i % (2*len(src) - 2)
		if k >= len(src) {
			k = 2*len(src) - 2 - k
		}
		out = append(out, src[k])
	}
	return out
}

func encodeSizes(t *testing.T, cfg EncoderConfig, frames []*image.YCbCr) (sizes, qs []int) {
	t.Helper()
	cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
	e, err := NewEncoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		p, err := e.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, 8*len(p.Data))
		qs = append(qs, p.QIndex)
	}
	return
}

func sumInts(v []int) int {
	s := 0
	for _, x := range v {
		s += x
	}
	return s
}

func minMax(v []int) (int, int) {
	lo, hi := v[0], v[0]
	for _, x := range v {
		lo, hi = min(lo, x), max(hi, x)
	}
	return lo, hi
}

func TestABR(t *testing.T) {
	frames := rcSource(t, 48)
	px := frames[0].Rect.Dx() * frames[0].Rect.Dy()
	for _, bpp := range []float64{0.02, 0.06, 0.2} {
		rate := int(bpp * float64(px) * 30)
		sizes, qs := encodeSizes(t, EncoderConfig{Bitrate: rate, KeyInt: 24}, frames)
		got := float64(sumInts(sizes)) * 30 / float64(len(sizes))
		lo, hi := minMax(qs)
		t.Logf("target %d bps: got %.0f (%+.1f%%), qindex %d..%d", rate, got, 100*(got/float64(rate)-1), lo, hi)
		if (got < 0.85*float64(rate) || got > 1.15*float64(rate)) && lo > 1 && hi < 255 {
			t.Errorf("bitrate %.0f is not within 15%% of %d", got, rate)
		}
	}
}

func TestVBV(t *testing.T) {
	frames := rcSource(t, 36)
	px := frames[0].Rect.Dx() * frames[0].Rect.Dy()
	rate := int(0.05 * float64(px) * 30)
	buf := rate / 2
	for _, cfg := range []EncoderConfig{
		{Bitrate: rate, VBVMaxRate: rate, VBVBuffer: buf, KeyInt: 18},
		{CRF: 20, VBVMaxRate: rate, VBVBuffer: buf, KeyInt: 18},
	} {
		sizes, qs := encodeSizes(t, cfg, frames)
		fill := 0.9 * float64(buf)
		for i, s := range sizes {
			fill -= float64(s)
			if fill < 0 {
				t.Fatalf("VBV underflow at frame %d (%d bits, qindex %d)", i, s, qs[i])
			}
			fill = min(fill+float64(rate)/30, float64(buf))
		}
	}
}

func TestCRFMonotonic(t *testing.T) {
	frames := rcSource(t, 12)
	prev := 0
	for _, crf := range []float64{50, 40, 30, 20} {
		sizes, qs := encodeSizes(t, EncoderConfig{CRF: crf}, frames)
		lo, hi := minMax(qs)
		t.Logf("CRF %.0f: %d bits, qindex %d..%d", crf, sumInts(sizes), lo, hi)
		if sumInts(sizes) <= prev {
			t.Errorf("CRF %.0f produced %d bits, not more than the previous %d", crf, sumInts(sizes), prev)
		}
		prev = sumInts(sizes)
	}
}
