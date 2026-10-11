package hevc

import (
	"image"
	"os"
	"testing"
)

// rcSource returns n frames of real content: the committed vector
// b_weighted.265 (176x144, 12 frames) decoded and ping-ponged.
func rcSource(t testing.TB, n int) []*image.YCbCr {
	t.Helper()
	data, err := os.ReadFile("testdata/b_weighted.265")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := DecodeAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	src := make([]*image.YCbCr, len(dec))
	for i, f := range dec {
		img := image.NewYCbCr(image.Rect(0, 0, f.Width, f.Height), image.YCbCrSubsampleRatio420)
		for y := 0; y < f.Height; y++ {
			copy(img.Y[y*img.YStride:], f.Y.Pix[y*f.Y.Stride:y*f.Y.Stride+f.Width])
		}
		for y := 0; y < f.Height/2; y++ {
			copy(img.Cb[y*img.CStride:], f.Cb.Pix[y*f.Cb.Stride:y*f.Cb.Stride+f.Width/2])
			copy(img.Cr[y*img.CStride:], f.Cr.Pix[y*f.Cr.Stride:y*f.Cr.Stride+f.Width/2])
		}
		src[i] = img
	}
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

func encodeSizes(t *testing.T, cfg EncoderConfig, frames []*image.YCbCr) ([]int, []int) {
	t.Helper()
	cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
	e, err := NewEncoder(cfg)
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
	frames := rcSource(t, 90)
	px := frames[0].Rect.Dx() * frames[0].Rect.Dy()
	for _, bpp := range []float64{0.03, 0.1, 0.3} {
		rate := int(bpp * float64(px) * 30)
		sizes, qps := encodeSizes(t, EncoderConfig{Bitrate: rate, KeyInt: 60, BFrames: 2, Preset: 0}, frames)
		got := float64(sumInts(sizes)) * 30 / float64(len(sizes))
		lo, hi := minMax(qps)
		t.Logf("target %d bps: got %.0f (%+.1f%%), QP %d..%d", rate, got, 100*(got/float64(rate)-1), lo, hi)
		if (got < 0.85*float64(rate) || got > 1.15*float64(rate)) && lo > 0 && hi < 51 {
			t.Errorf("bitrate %.0f is not within 15%% of %d", got, rate)
		}
	}
}

func TestVBV(t *testing.T) {
	frames := rcSource(t, 60)
	px := frames[0].Rect.Dx() * frames[0].Rect.Dy()
	rate := int(0.08 * float64(px) * 30)
	buf := rate / 2
	for _, cfg := range []EncoderConfig{
		{Bitrate: rate, VBVMaxRate: rate, VBVBuffer: buf, KeyInt: 30, Preset: 0},
		{CRF: 18, VBVMaxRate: rate, VBVBuffer: buf, KeyInt: 30, Preset: 0},
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
	frames := rcSource(t, 24)
	prev := 0
	for _, crf := range []float64{38, 32, 26, 20} {
		sizes, qps := encodeSizes(t, EncoderConfig{CRF: crf, Preset: 0}, frames)
		lo, hi := minMax(qps)
		t.Logf("CRF %.0f: %d bits, QP %d..%d", crf, sumInts(sizes), lo, hi)
		if sumInts(sizes) <= prev {
			t.Errorf("CRF %.0f produced %d bits, not more than the previous %d", crf, sumInts(sizes), prev)
		}
		prev = sumInts(sizes)
	}
}
