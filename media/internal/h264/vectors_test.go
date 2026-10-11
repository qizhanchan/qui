package h264

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestVectors decodes the committed streams listed in testdata/vectors.txt
// ("name frames sha256") and checks the SHA-256 of all output planes (I420,
// display order) against FFmpeg's decode of the same stream.
func TestVectors(t *testing.T) {
	f, err := os.Open("testdata/vectors.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 {
			continue
		}
		name, want := fields[0], fields[2]
		n, _ := strconv.Atoi(fields[1])
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name + ".264")
			if err != nil {
				t.Fatal(err)
			}
			frames, err := DecodeAnnexB(data)
			if err != nil {
				t.Errorf("decode error: %v", err)
			}
			if len(frames) != n {
				t.Fatalf("decoded %d frames, want %d", len(frames), n)
			}
			h := sha256.New()
			for _, fr := range frames {
				for _, p := range []*Plane{fr.Y, fr.Cb, fr.Cr} {
					for y := 0; y < p.H; y++ {
						h.Write(p.Pix[y*p.Stride : y*p.Stride+p.W])
					}
				}
			}
			if got := hex.EncodeToString(h.Sum(nil)); got != want {
				t.Errorf("planes differ from FFmpeg:\n got  %s\n want %s", got, want)
			}
		})
	}
}
