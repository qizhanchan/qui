package av1

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// TestDecodeAPI decodes the 8-bit vectors through Decode / Next (pooled
// I420 output, tags) and checks the same hashes as TestVectors.
func TestDecodeAPI(t *testing.T) {
	f, err := os.Open("testdata/vectors.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 3 || strings.HasPrefix(fields[0], "#") || strings.Contains(fields[0], "10bit") {
			continue
		}
		name, want := fields[0], fields[2]
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
			n := 0
			for i, tu := range tus {
				d.SetTag(int64(i))
				if err := d.Decode(tu); err != nil {
					t.Fatalf("TU %d: %v", i, err)
				}
				for fr := d.Next(); fr != nil; fr = d.Next() {
					if fr.Tag != int64(i) {
						t.Fatalf("frame tag %d in TU %d", fr.Tag, i)
					}
					if n == 0 && !fr.KeyFrame {
						t.Errorf("first frame is not a key frame")
					}
					for _, pl := range []*planeView{{fr.Y.Pix, fr.Y.Stride, fr.Y.W, fr.Y.H}, {fr.Cb.Pix, fr.Cb.Stride, fr.Cb.W, fr.Cb.H}, {fr.Cr.Pix, fr.Cr.Stride, fr.Cr.W, fr.Cr.H}} {
						for r := 0; r < pl.h; r++ {
							h.Write(pl.pix[r*pl.stride : r*pl.stride+pl.w])
						}
					}
					n++
				}
			}
			if got := hex.EncodeToString(h.Sum(nil)); got != want {
				t.Errorf("planes differ from the reference:\n got  %s\n want %s", got, want)
			}
		})
	}
}

type planeView struct {
	pix       []byte
	stride, w int
	h         int
}

// TestDecodeCorrupt feeds damaged temporal units: Decode must return
// errors, never panic.
func TestDecodeCorrupt(t *testing.T) {
	data, err := os.ReadFile("testdata/ra_p4.ivf")
	if err != nil {
		t.Fatal(err)
	}
	tus, _, _, err := ReadIVF(data)
	if err != nil {
		t.Fatal(err)
	}
	for seed := 0; seed < 200; seed++ {
		d := New()
		for i, tu := range tus {
			b := append([]byte(nil), tu...)
			if i > 0 || seed%2 == 1 {
				k := (seed*7919 + i*104729) % len(b)
				b[k] ^= byte(1 + seed%255)
			}
			_ = d.Decode(b)
			for d.Next() != nil {
			}
		}
	}
}
