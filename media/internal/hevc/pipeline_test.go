package hevc

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// decodeAll decodes an Annex-B stream with the in-loop filters pipelined
// (the default) or in place, draining output as a player would.
func decodeAll(data []byte, sync bool) []*Frame {
	d := New()
	d.syncFilter = sync
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

// TestPipelinedFilterMatchesInPlace pins the pipelined in-loop filters to
// the in-place result on every committed stream.
func TestPipelinedFilterMatchesInPlace(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.265")
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		a, b := decodeAll(data, true), decodeAll(data, false)
		if len(a) != len(b) {
			t.Fatalf("%s: %d frames in place, %d pipelined", f, len(a), len(b))
		}
		for i := range a {
			if !bytes.Equal(framePlanes(a[i]), framePlanes(b[i])) || a[i].POC != b[i].POC {
				t.Fatalf("%s: frame %d differs", f, i)
			}
		}
	}
}
