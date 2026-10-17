package av1

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPipelinedFilterMatchesInPlace decodes every test stream (and
// AV1_DIR's) with the in-loop filters pipelined behind single-tile
// decoding and after it, and requires identical frames, invisible ones
// included, padding included.
func TestPipelinedFilterMatchesInPlace(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))
	}
	files, _ := filepath.Glob("testdata/*.ivf")
	if dir := os.Getenv("AV1_DIR"); dir != "" {
		more, _ := filepath.Glob(filepath.Join(dir, "*.ivf"))
		files = append(files, more...)
	}
	pipelined := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		tus, _, _, err := ReadIVF(data)
		if err != nil {
			t.Fatal(err)
		}
		decode := func(noPipeline bool) ([]*frame, int) {
			d := New()
			d.outputInvisible, d.noFilmGrain, d.noPipeline = true, true, noPipeline
			var out []*frame
			for i, tu := range tus {
				if err := d.DecodeTemporalUnit(tu); err != nil {
					t.Fatalf("%s TU %d: %v", file, i, err)
				}
				out = append(out, d.out...)
				d.out = d.out[:0]
			}
			return out, d.pipelined
		}
		got, n := decode(false)
		want, _ := decode(true)
		pipelined += n
		if len(got) != len(want) {
			t.Fatalf("%s: %d frames, want %d", file, len(got), len(want))
		}
		for i := range got {
			for p := 0; p < got[i].numPlanes; p++ {
				a, b := &got[i].planes[p], &want[i].planes[p]
				if !bytes.Equal(a.pix8, b.pix8) || !equal16(a.pix16, b.pix16) {
					t.Fatalf("%s frame %d plane %d differs", file, i, p)
				}
			}
		}
	}
	if pipelined == 0 {
		t.Fatal("no frame was pipelined")
	}
	t.Logf("%d frames pipelined", pipelined)
}

func equal16(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
