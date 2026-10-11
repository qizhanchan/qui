package h264

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// decodeAll decodes an Annex-B stream with deblocking pipelined (the
// default) or in place.
func decodeAll(t *testing.T, data []byte, sync bool) []*Frame {
	t.Helper()
	d := New()
	d.syncDeblock = sync
	var out []*Frame
	for _, nal := range SplitAnnexB(data) {
		_ = d.DecodeNAL(nal)
		for f := d.Next(); f != nil; f = d.Next() {
			out = append(out, f)
		}
	}
	d.Flush()
	for f := d.Next(); f != nil; f = d.Next() {
		out = append(out, f)
	}
	return out
}

// TestPipelinedDeblockMatchesInPlace pins the pipelined deblocking
// (pipeline.go) to the in-place result on every committed stream.
func TestPipelinedDeblockMatchesInPlace(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.264")
	more, _ := filepath.Glob("testdata/*.h264")
	for _, f := range append(files, more...) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		a, b := decodeAll(t, data, true), decodeAll(t, data, false)
		if len(a) != len(b) {
			t.Fatalf("%s: %d frames in place, %d pipelined", f, len(a), len(b))
		}
		for i := range a {
			if !bytes.Equal(a[i].Y.Pix, b[i].Y.Pix) || !bytes.Equal(a[i].Cb.Pix, b[i].Cb.Pix) || !bytes.Equal(a[i].Cr.Pix, b[i].Cr.Pix) {
				t.Fatalf("%s: frame %d differs", f, i)
			}
		}
	}
}
