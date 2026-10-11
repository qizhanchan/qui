package h264

import (
	"bytes"
	"os"
	"testing"
)

// asoHeader parses a slice header against the parameter sets already fed to d.
func asoHeader(d *Decoder, nal []byte) *sliceHeader {
	rbsp := unescapeRBSP(nil, nal[1:])
	br := &bitReader{}
	br.reset(rbsp)
	h, err := parseSliceHeader(br, nal[0], d.sps[:], d.pps[:])
	if err != nil {
		return nil
	}
	return h
}

func asoPlanesEqual(a, b []*Frame) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		for _, pl := range [][2]*Plane{{a[i].Y, b[i].Y}, {a[i].Cb, b[i].Cb}, {a[i].Cr, b[i].Cr}} {
			x, y := pl[0], pl[1]
			if x.W != y.W || x.H != y.H {
				return false
			}
			for r := 0; r < x.H; r++ {
				if !bytes.Equal(x.Pix[r*x.Stride:r*x.Stride+x.W], y.Pix[r*y.Stride:r*y.Stride+y.W]) {
					return false
				}
			}
		}
	}
	return true
}

// TestArbitrarySliceOrder reverses the slices inside every picture of a
// multi-slice stream (3 slices per picture, first_mb_in_slice 0/6/18) and
// checks the decoded planes are bit-identical to the in-order decode. This is
// the H8 "slices in any order" (ASO) requirement: decoding must not assume
// slices arrive with increasing first_mb_in_slice.
func TestArbitrarySliceOrder(t *testing.T) {
	data, err := os.ReadFile("testdata/cavlc_slices.264")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := DecodeAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	sc := []byte{0, 0, 1}
	var out []byte
	var group [][]byte
	flush := func() {
		for i := len(group) - 1; i >= 0; i-- {
			out = append(out, sc...)
			out = append(out, group[i]...)
		}
		group = nil
	}
	d := New()
	for _, nal := range SplitAnnexB(data) {
		switch nal[0] & 0x1f {
		case 7, 8: // SPS / PPS
			_ = d.DecodeNAL(nal)
			flush()
			out = append(out, sc...)
			out = append(out, nal...)
			continue
		case 6: // SEI
			out = append(out, sc...)
			out = append(out, nal...)
			continue
		}
		h := asoHeader(d, nal)
		if h == nil {
			t.Fatal("slice header parse failed")
		}
		if h.firstMB == 0 && len(group) > 0 {
			flush() // a picture starts at first_mb_in_slice == 0
		}
		group = append(group, nal)
	}
	flush()

	got, err := DecodeAnnexB(out)
	if err != nil {
		t.Fatalf("ASO decode error: %v", err)
	}
	if len(got) != len(ref) {
		t.Fatalf("ASO decoded %d frames, want %d", len(got), len(ref))
	}
	if !asoPlanesEqual(ref, got) {
		t.Errorf("ASO decode differs from the in-order decode")
	}
}
