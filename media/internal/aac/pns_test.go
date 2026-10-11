package aac

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
)

// TestPNSEnegry decodes an LC stream that uses perceptual noise substitution
// heavily (111 noise bands over 23 frames, produced with FFmpeg's native AAC
// encoder and -aac_pns 1) and checks the decoded energy matches FFmpeg.
//
// PNS replaces a band with pseudo-random noise whose *energy* is signalled in
// the bitstream; the actual sample values depend on the decoder's noise
// generator, which ISO/IEC 14496-3 does not pin down for conformance. The
// conformance criterion is therefore band energy, not sample-exactness — so
// this stream is kept out of TestVectors (which requires ±1 LSB) and is checked
// by overall RMS instead.
func TestPNSEnegry(t *testing.T) {
	data, err := os.ReadFile("testdata/pns/lc_44k_pns.aac")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/pns/lc_44k_pns.s16")
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := ParseADTS(data)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDecoder(&Config{ObjectType: h.ObjectType, SampleRateIndex: h.SampleRateIndex,
		SampleRate: h.SampleRate, ChannelConfig: h.Channels, Channels: h.Channels, FrameLength: 1024})
	if err != nil {
		t.Fatal(err)
	}
	aus, err := RawBlocks(data)
	if err != nil {
		t.Fatal(err)
	}
	var out []int16
	for i, au := range aus {
		if out, err = d.DecodeInt16(au, out); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	ref := make([]int16, len(raw)/2)
	for i := range ref {
		ref[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	if len(out) != len(ref) {
		t.Fatalf("decoded %d samples, want %d", len(out), len(ref))
	}
	ours, want := rmsInt16(out), rmsInt16(ref)
	if want == 0 {
		t.Fatal("reference is silent")
	}
	if rel := math.Abs(ours-want) / want; rel > 0.01 {
		t.Errorf("PNS energy off by %.2f%%: ours rms=%.2f, FFmpeg rms=%.2f", rel*100, ours, want)
	} else {
		t.Logf("PNS energy matches FFmpeg: rms %.2f vs %.2f (%.3f%%)", ours, want, rel*100)
	}
}

func rmsInt16(x []int16) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}
