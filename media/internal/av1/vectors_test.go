package av1

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestVectors decodes the streams in testdata/vectors.txt ("name frames
// sha256") and checks the SHA-256 of all shown frames' planes (yuv420p or
// yuv420p10le, film grain applied). The hashes are libdav1d's output for
// the same files; the streams were made with libsvtav1 to cover intra and
// screen-content tools, random access at presets 0 and 4 with tiles,
// S-frames, superres, reference scaling, film grain, quantiser matrices
// with segmentation, lossless and 10-bit.
func TestVectors(t *testing.T) {
	f, err := os.Open("testdata/vectors.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		name, want := fields[0], fields[2]
		n, _ := strconv.Atoi(fields[1])
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name + ".ivf")
			if err != nil {
				t.Fatal(err)
			}
			tus, _, _, err := ReadIVF(data)
			if err != nil {
				t.Fatal(err)
			}
			d := New()
			h := sha256.New()
			count := 0
			for i, tu := range tus {
				if err := d.DecodeTemporalUnit(tu); err != nil {
					t.Fatalf("TU %d: %v", i, err)
				}
				for _, fr := range d.out {
					h.Write(fr.planesBytes())
					count++
				}
				d.out = d.out[:0]
			}
			if count != n {
				t.Fatalf("decoded %d frames, want %d", count, n)
			}
			if got := hex.EncodeToString(h.Sum(nil)); got != want {
				t.Errorf("planes differ from the reference:\n got  %s\n want %s", got, want)
			}
		})
	}
}
