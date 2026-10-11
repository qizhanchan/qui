package h264

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func TestBitReaderUE(t *testing.T) {
	// Codes and their expected values, concatenated MSB-first.
	cases := []struct {
		bits string
		want uint32
	}{
		{"1", 0}, {"010", 1}, {"011", 2}, {"00100", 3},
		{"00101", 4}, {"00110", 5}, {"00111", 6},
	}
	for _, c := range cases {
		r := newBitReader(packBits(c.bits))
		if got := r.ue(); got != c.want || r.err() != nil {
			t.Errorf("ue(%q) = %d (err %v), want %d", c.bits, got, r.err(), c.want)
		}
	}
}

func TestBitReaderSE(t *testing.T) {
	cases := []struct {
		bits string
		want int32
	}{
		{"1", 0}, {"010", 1}, {"011", -1}, {"00100", 2}, {"00101", -2},
	}
	for _, c := range cases {
		r := newBitReader(packBits(c.bits))
		if got := r.se(); got != c.want || r.err() != nil {
			t.Errorf("se(%q) = %d (err %v), want %d", c.bits, got, r.err(), c.want)
		}
	}
}

func TestBitReaderU(t *testing.T) {
	r := newBitReader([]byte{0b1011_0011})
	if got := r.u(3); got != 0b101 {
		t.Errorf("u(3) = %b, want 101", got)
	}
	if got := r.u(5); got != 0b1_0011 {
		t.Errorf("u(5) = %b, want 10011", got)
	}
	if r.err() != nil {
		t.Errorf("unexpected err %v", r.err())
	}
	// Overrun latches the error.
	r2 := newBitReader([]byte{0xff})
	r2.u(9)
	if r2.err() == nil {
		t.Error("expected error on overrun")
	}
}

func TestUnescape(t *testing.T) {
	got := unescapeRBSP([]byte{0x00, 0x00, 0x03, 0x01})
	if !bytes.Equal(got, []byte{0x00, 0x00, 0x01}) {
		t.Errorf("unescape = % x", got)
	}
	got = unescapeRBSP([]byte{0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x02})
	if !bytes.Equal(got, []byte{0x00, 0x00, 0x00, 0x00, 0x02}) {
		t.Errorf("unescape = % x", got)
	}
}

func TestSplitAnnexB(t *testing.T) {
	stream := []byte{
		0, 0, 0, 1, 0x67, 0xAA, // SPS
		0, 0, 1, 0x68, 0xBB, // PPS (3-byte start code)
		0, 0, 0, 1, 0x65, 0xCC, // IDR
	}
	nalus := splitAnnexB(stream)
	if len(nalus) != 3 {
		t.Fatalf("got %d NAL units, want 3", len(nalus))
	}
	if !bytes.Equal(nalus[0], []byte{0x67, 0xAA}) ||
		!bytes.Equal(nalus[1], []byte{0x68, 0xBB}) ||
		!bytes.Equal(nalus[2], []byte{0x65, 0xCC}) {
		t.Errorf("NAL units = % x", nalus)
	}
	if nalType(nalus[0][0]) != nalSPS || nalType(nalus[1][0]) != nalPPS || nalType(nalus[2][0]) != nalIDR {
		t.Errorf("types = %d %d %d", nalType(nalus[0][0]), nalType(nalus[1][0]), nalType(nalus[2][0]))
	}
}

// Real SPS/PPS captured from H.264 MP4s (see tools/media-poc/cmd/dump-avcc).
var (
	realSPS1080x1080 = mustHexStatic("67640020ac d9404402 27965c04 40000003 00400000 0f03c60c 6580")
	realSPS1920x1080 = mustHexStatic("27640028ac 56807802 27e59a80 808081")
	realPPS1         = mustHexStatic("68ebe3cb22c0")
	realPPS2         = mustHexStatic("28ee3cb0")
)

func mustHexStatic(s string) []byte {
	clean := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' {
			clean = append(clean, s[i])
		}
	}
	b, err := hex.DecodeString(string(clean))
	if err != nil {
		panic(err)
	}
	return b
}

func TestParseSPS(t *testing.T) {
	cases := []struct {
		name    string
		nal     []byte
		w, h    int
		profile uint8
		level   uint8
	}{
		{"1080x1080", realSPS1080x1080, 1080, 1080, 100, 32},
		{"1920x1080", realSPS1920x1080, 1920, 1080, 100, 40},
	}
	for _, c := range cases {
		sps, err := ParseSPS(c.nal)
		if err != nil {
			t.Fatalf("%s: ParseSPS: %v", c.name, err)
		}
		if sps.Width != c.w || sps.Height != c.h {
			t.Errorf("%s: size = %dx%d, want %dx%d", c.name, sps.Width, sps.Height, c.w, c.h)
		}
		if sps.ProfileIDC != c.profile {
			t.Errorf("%s: profile = %d, want %d", c.name, sps.ProfileIDC, c.profile)
		}
		if sps.LevelIDC != c.level {
			t.Errorf("%s: level = %d, want %d", c.name, sps.LevelIDC, c.level)
		}
		if sps.ChromaFormatIDC != 1 {
			t.Errorf("%s: chroma_format_idc = %d, want 1 (4:2:0)", c.name, sps.ChromaFormatIDC)
		}
		if sps.BitDepthLuma != 8 || sps.BitDepthChroma != 8 {
			t.Errorf("%s: bit depths = %d/%d, want 8/8", c.name, sps.BitDepthLuma, sps.BitDepthChroma)
		}
	}
}

func TestParsePPS(t *testing.T) {
	for _, p := range [][]byte{realPPS1, realPPS2} {
		pps, err := ParsePPS(p)
		if err != nil {
			t.Fatalf("ParsePPS(% x): %v", p, err)
		}
		if pps.NumSliceGroups != 1 {
			t.Errorf("NumSliceGroups = %d, want 1", pps.NumSliceGroups)
		}
	}
}

// packBits turns a bit string into a byte slice, MSB-first, zero padded.
func packBits(bits string) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i := 0; i < len(bits); i++ {
		if bits[i] == '1' {
			out[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	return out
}
