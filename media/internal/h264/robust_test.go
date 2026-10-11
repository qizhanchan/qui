package h264

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// TestCorruptionNoPanic flips random bytes / truncates NAL units of every
// corpus stream and checks the decoder never panics.
func TestCorruptionNoPanic(t *testing.T) {
	dir := os.Getenv("AVC_CORPUS")
	if dir == "" {
		dir = "testdata"
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.264"))
	more, _ := filepath.Glob(filepath.Join(dir, "*.h264"))
	files = append(files, more...)
	rng := rand.New(rand.NewSource(1))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		for iter := 0; iter < 20; iter++ {
			nals := SplitAnnexB(data)
			d := New()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s iter %d: panic: %v", filepath.Base(f), iter, r)
					}
				}()
				for _, n := range nals {
					c := append([]byte(nil), n...)
					if len(c) > 2 && rng.Intn(4) == 0 {
						for k := 0; k < 1+rng.Intn(4); k++ {
							c[1+rng.Intn(len(c)-1)] ^= byte(1 << uint(rng.Intn(8)))
						}
					}
					if len(c) > 4 && rng.Intn(10) == 0 {
						c = c[:1+rng.Intn(len(c)-1)]
					}
					_ = d.DecodeNAL(c)
					for d.Next() != nil {
					}
				}
				d.Flush()
			}()
		}
	}
}
