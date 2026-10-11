package h264

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestCorpus decodes every *.264 in $AVC_CORPUS and compares it plane-exact
// with the sibling *.yuv (I420, display order) produced by FFmpeg:
//
//	ffmpeg -i x.264 -f rawvideo -pix_fmt yuv420p x.yuv
func TestCorpus(t *testing.T) {
	dir := os.Getenv("AVC_CORPUS")
	if dir == "" {
		t.Skip("AVC_CORPUS not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.264"))
	sort.Strings(files)
	only := os.Getenv("AVC_ONLY")
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".264")
		if only != "" && !strings.Contains(name, only) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := os.ReadFile(strings.TrimSuffix(f, ".264") + ".yuv")
			if err != nil {
				t.Skip("no reference yuv")
			}
			frames, err := DecodeAnnexB(data)
			if err != nil {
				t.Errorf("decode error: %v", err)
			}
			if len(frames) == 0 {
				t.Fatal("no frames")
			}
			w, h := frames[0].Width, frames[0].Height
			fs := w*h + 2*((w+1)/2)*((h+1)/2)
			if len(ref)%fs != 0 {
				t.Fatalf("reference size %d not a multiple of frame size %d", len(ref), fs)
			}
			nref := len(ref) / fs
			if len(frames) != nref {
				t.Errorf("got %d frames, want %d", len(frames), nref)
			}
			bad := 0
			for i := 0; i < min(len(frames), nref); i++ {
				if msg := diffFrame(frames[i], ref[i*fs:(i+1)*fs]); msg != "" {
					if bad < 4 {
						t.Errorf("frame %d (poc %d): %s", i, frames[i].POC, msg)
					}
					bad++
				}
			}
			if bad > 0 {
				t.Errorf("%d/%d frames differ", bad, len(frames))
			}
		})
	}
}

func diffFrame(f *Frame, ref []byte) string {
	w, h := f.Width, f.Height
	cw, ch := (w+1)/2, (h+1)/2
	planes := []struct {
		name string
		p    *Plane
		w, h int
		ref  []byte
		mb   int
	}{
		{"Y", f.Y, w, h, ref[:w*h], 16},
		{"Cb", f.Cb, cw, ch, ref[w*h : w*h+cw*ch], 8},
		{"Cr", f.Cr, cw, ch, ref[w*h+cw*ch:], 8},
	}
	var out []string
	for _, pl := range planes {
		n := 0
		first := -1
		for y := 0; y < pl.h; y++ {
			for x := 0; x < pl.w; x++ {
				if pl.p.Pix[y*pl.p.Stride+x] != pl.ref[y*pl.w+x] {
					if first < 0 {
						first = y*pl.w + x
					}
					n++
				}
			}
		}
		if n > 0 {
			fx, fy := first%pl.w, first/pl.w
			out = append(out, fmt.Sprintf("%s: %d px differ, first at (%d,%d) mb (%d,%d)", pl.name, n, fx, fy, fx/pl.mb, fy/pl.mb))
		}
	}
	return strings.Join(out, "; ")
}

// TestBigFile decodes $AVC_BIG (+ .yuv reference if present) and reports
// throughput.
func TestBigFile(t *testing.T) {
	f := os.Getenv("AVC_BIG")
	if f == "" {
		t.Skip("AVC_BIG not set")
	}
	data, err := os.ReadFile(f + ".264")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	frames, err := DecodeAnnexB(data)
	el := time.Since(start)
	if err != nil {
		t.Errorf("decode error: %v", err)
	}
	t.Logf("%d frames in %v (%.1f fps)", len(frames), el, float64(len(frames))/el.Seconds())
	ref, err := os.ReadFile(f + ".yuv")
	if err != nil {
		return
	}
	w, h := frames[0].Width, frames[0].Height
	fs := w*h + 2*((w+1)/2)*((h+1)/2)
	bad := 0
	for i := 0; i < min(len(frames), len(ref)/fs); i++ {
		if msg := diffFrame(frames[i], ref[i*fs:(i+1)*fs]); msg != "" {
			if bad < 3 {
				t.Errorf("frame %d: %s", i, msg)
			}
			bad++
		}
	}
	if len(frames) != len(ref)/fs {
		t.Errorf("got %d frames, want %d", len(frames), len(ref)/fs)
	}
}
