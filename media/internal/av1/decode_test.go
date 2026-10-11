package av1

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// planes returns a frame's visible samples as FFmpeg's rawvideo output
// lays them out (yuv420p / yuv420p10le).
func (f *frame) planesBytes() []byte {
	var out []byte
	for p := 0; p < f.numPlanes; p++ {
		pb := &f.planes[p]
		for y := 0; y < pb.h; y++ {
			if pb.pix16 != nil {
				for _, v := range pb.pix16[y*pb.stride : y*pb.stride+pb.w] {
					out = append(out, byte(v), byte(v>>8))
				}
			} else {
				out = append(out, pb.pix8[y*pb.stride:y*pb.stride+pb.w]...)
			}
		}
	}
	return out
}

// decodeIVF decodes an IVF file and returns the shown frames.
func decodeIVF(data []byte) ([]*frame, error) {
	tus, _, _, err := ReadIVF(data)
	if err != nil {
		return nil, err
	}
	d := New()
	var out []*frame
	for i, tu := range tus {
		if err := d.DecodeTemporalUnit(tu); err != nil {
			return append(out, d.out...), fmt.Errorf("TU %d: %w", i, err)
		}
		out = append(out, d.out...)
		d.out = d.out[:0]
	}
	return out, nil
}

// TestCompareFFmpeg (opt-in: AV1_DIR with *.ivf, AV1_ONLY to filter)
// decodes each stream and compares every frame with FFmpeg's libdav1d
// output.
func TestCompareFFmpeg(t *testing.T) {
	dir := os.Getenv("AV1_DIR")
	if dir == "" {
		t.Skip("set AV1_DIR")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.ivf"))
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".ivf")
		if only := os.Getenv("AV1_ONLY"); only != "" && !strings.Contains(name, only) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			frames, derr := decodeIVF(data)
			if len(frames) == 0 {
				t.Fatalf("no frames: %v", derr)
			}
			pf := "yuv420p"
			if frames[0].bitDepth > 8 {
				pf = "yuv420p10le"
			}
			want, err := exec.Command("ffmpeg", "-v", "error", "-c:v", "libdav1d", "-i", file, "-f", "rawvideo", "-pix_fmt", pf, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			off := 0
			for i, fr := range frames {
				got := fr.planesBytes()
				if off+len(got) > len(want) {
					t.Fatalf("frame %d: FFmpeg has fewer frames", i)
				}
				if w := want[off : off+len(got)]; !bytes.Equal(got, w) {
					t.Fatalf("frame %d differs: %s", i, firstDiff(fr, got, w))
				}
				off += len(got)
			}
			if derr != nil {
				t.Fatalf("after %d matching frames: %v", len(frames), derr)
			}
			if off != len(want) {
				t.Errorf("decoded %d frames; FFmpeg has more", len(frames))
			}
		})
	}
}

func firstDiff(f *frame, got, want []byte) string {
	sz := 1
	if f.bitDepth > 8 {
		sz = 2
	}
	off := 0
	for p := 0; p < f.numPlanes; p++ {
		pb := &f.planes[p]
		n := pb.w * pb.h * sz
		for i := 0; i < n; i++ {
			if got[off+i] != want[off+i] {
				x, y := (i/sz)%pb.w, (i/sz)/pb.w
				return fmt.Sprintf("plane %d at (%d,%d): got %d want %d", p, x, y, got[off+i], want[off+i])
			}
		}
		off += n
	}
	return "?"
}
