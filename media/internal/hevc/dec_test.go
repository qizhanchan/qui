package hevc

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func framePlanes(f *Frame) []byte {
	var b []byte
	for _, p := range []*Plane{f.Y, f.Cb, f.Cr} {
		for y := 0; y < p.H; y++ {
			b = append(b, p.Pix[y*p.Stride:y*p.Stride+p.W]...)
		}
	}
	return b
}

// TestCorpusDir decodes every *.265 in HEVC_DIR and compares with the
// matching FFmpeg *.yuv.
func TestCorpusDir(t *testing.T) {
	dir := os.Getenv("HEVC_DIR")
	if dir == "" {
		t.Skip("HEVC_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.265"))
	for _, f := range files {
		name := filepath.Base(f)
		if m := os.Getenv("HEVC_ONLY"); m != "" && !bytes.Contains([]byte(name), []byte(m)) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, _ := os.ReadFile(f)
			want, err := os.ReadFile(f[:len(f)-4] + ".yuv")
			if err != nil {
				t.Skip("no reference")
			}
			frames, err := DecodeAnnexB(data)
			if err != nil {
				t.Errorf("decode error: %v", err)
			}
			if len(frames) == 0 {
				t.Fatal("no frames")
			}
			fs := len(framePlanes(frames[0]))
			if len(frames)*fs != len(want) {
				t.Errorf("decoded %d frames, reference has %d", len(frames), len(want)/fs)
			}
			for i, fr := range frames {
				got := framePlanes(fr)
				if (i+1)*fs > len(want) {
					break
				}
				ref := want[i*fs : (i+1)*fs]
				if !bytes.Equal(got, ref) {
					w := fr.Width
					for j := range got {
						if got[j] != ref[j] {
							plane, off := "Y", j
							if j >= w*fr.Height {
								plane, off = "C", j-w*fr.Height
							}
							t.Fatalf("frame %d (POC %d) differs: first at %s offset %d (x %d y %d): got %d want %d", i, fr.POC, plane, off, off%w, off/w, got[j], ref[j])
						}
					}
				}
			}
			fmt.Printf("%s: %d frames OK\n", name, len(frames))
		})
	}
}
