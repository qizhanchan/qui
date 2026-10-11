package aac

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// TestCorruptionNoPanic feeds bit-flipped and truncated access units.
func TestCorruptionNoPanic(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.aac")
	rng := rand.New(rand.NewSource(7))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		h, _, err := ParseADTS(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		aus, _ := RawBlocks(data)
		for iter := 0; iter < 50; iter++ {
			d, _ := NewDecoder(&Config{ObjectType: 2, SampleRateIndex: h.SampleRateIndex, Channels: h.Channels, FrameLength: 1024})
			for _, au := range aus {
				c := append([]byte(nil), au...)
				for k := 0; k < 1+rng.Intn(6); k++ {
					c[rng.Intn(len(c))] ^= byte(1 << uint(rng.Intn(8)))
				}
				if rng.Intn(5) == 0 {
					c = c[:rng.Intn(len(c))]
				}
				out, _ := d.DecodeInt16(c, nil)
				if len(out) != 1024*d.Channels() {
					t.Fatalf("frame size %d", len(out))
				}
			}
		}
	}
}
