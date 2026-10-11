package h264

import (
	"os"
	"testing"
)

// TestMissingRefConcealed decodes the first 60 NAL units of the OpenH264
// error-concealment stream MR2_TANDBERG_E. It resets frame_num without an IDR,
// so a ref_pic_list_modification names a picture that is not in the DPB.
// The decoder must conceal (keep the reference already in that position) and
// keep going rather than returning an error; before the H8 fix this failed at
// NAL 55 with "ref_pic_list_modification names a missing picture".
func TestMissingRefConcealed(t *testing.T) {
	data, err := os.ReadFile("testdata/conceal_missing_ref.264")
	if err != nil {
		t.Fatal(err)
	}
	d := New()
	n := 0
	for _, nal := range SplitAnnexB(data) {
		if err := d.DecodeNAL(nal); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		for d.Next() != nil {
			n++
		}
	}
	d.Flush()
	for d.Next() != nil {
		n++
	}
	if n == 0 {
		t.Fatal("decoded no frames")
	}
	if d.concealedRefs == 0 {
		t.Errorf("expected the missing reference to be concealed, concealedRefs=0")
	}
	t.Logf("frames=%d concealedRefs=%d", n, d.concealedRefs)
}
