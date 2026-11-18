package vp9

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qizhanchan/qui/media/internal/webm"
)

// clips are the VP9 fixtures; hash is the SHA-256 of FFmpeg's decode of
// every frame as I420 at its own size (ffmpeg -fps_mode passthrough
// -noautoscale -f rawvideo -pix_fmt yuv420p). All but the resize clip
// were encoded by FFmpeg's libvpx-vp9 (alt-ref: 2-pass -auto-alt-ref 1
// -lag-in-frames 25; tiles: -tile-columns 2 -tile-rows 1; lossless:
// -lossless 1); vp9_resize_160x90 comes from libvpx's encoder API with
// vpx_codec_enc_config_set changing the size mid-stream (160x90 → 112x64
// → 136x78, no keyframe), so its inter frames predict from scaled
// references.
var clips = []struct {
	name, hash string
	frames     int
}{
	{"vp9p0_opus_160x90", "d7cbdf4d196ad71381cc474aa58590c073e028c13f14b1cb865391720cb84a73", 24},
	{"vp9_altref_160x90", "e90e939d235e0672bf1f1955a5e241f0340510ad142439ed3be7187af8672a85", 48},
	{"vp9_tiles_640x96", "48317512e209ef5f496a928afb5d05d400bfe38aac9c0fb287de2c0e1725ff2b", 12},
	{"vp9_lossless_96x64", "30b91c992fa15940c5cf20947b4fbabd0b11c7683f9aca9b33c52398fac0a5c9", 12},
	{"vp9_odd_75x43", "55f13706309da139c98bafd5d4966cbc73b87c800db601b5c3b6680e1e8aed7f", 24},
	{"vp9_resize_160x90", "e045ecac71f1b0280175059e8beb516ebb1dc576cc4927e2b91566d0950e1071", 24},
}

func readBlocks(t testing.TB, name string) [][]byte {
	return readBlocksPath(t, filepath.Join("..", "..", "testdata", name+".webm"))
}

func readBlocksPath(t testing.TB, path string) [][]byte {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := webm.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	vt := r.VideoTrack()
	var out [][]byte
	for i := 0; i < r.SampleCount(vt); i++ {
		b, err := r.ReadSample(vt, i)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, append([]byte(nil), b...))
	}
	return out
}

func decodeAll(t testing.TB, blocks [][]byte) []*Frame {
	d := New()
	d.stats = &decodeStats{}
	defer func() { t.Logf("%v", d.stats) }()
	var out []*Frame
	for i, b := range blocks {
		d.SetTag(int64(i))
		if err := d.Decode(b); err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
		for f := d.Next(); f != nil; f = d.Next() {
			out = append(out, f)
		}
	}
	return out
}

func frameBytes(f *Frame) []byte {
	var b []byte
	for _, p := range []*struct {
		pix    []byte
		stride int
		w, h   int
	}{
		{f.Y.Pix, f.Y.Stride, f.Y.W, f.Y.H},
		{f.Cb.Pix, f.Cb.Stride, f.Cb.W, f.Cb.H},
		{f.Cr.Pix, f.Cr.Stride, f.Cr.W, f.Cr.H},
	} {
		for r := 0; r < p.h; r++ {
			b = append(b, p.pix[r*p.stride:r*p.stride+p.w]...)
		}
	}
	return b
}

func TestDecodeMatchesFFmpeg(t *testing.T) {
	refDir := os.Getenv("VP9_REF_DIR")
	for _, c := range clips {
		t.Run(c.name, func(t *testing.T) {
			frames := decodeAll(t, readBlocks(t, c.name))
			if len(frames) != c.frames {
				t.Errorf("decoded %d frames, want %d", len(frames), c.frames)
			}
			h := sha256.New()
			var ref []byte
			if refDir != "" {
				ref, _ = os.ReadFile(filepath.Join(refDir, c.name+".yuv"))
			}
			for i, f := range frames {
				fb := frameBytes(f)
				h.Write(fb)
				if ref != nil {
					if len(ref) < len(fb) {
						t.Fatalf("frame %d: reference too short", i)
					}
					want := ref[:len(fb)]
					ref = ref[len(fb):]
					if !bytes.Equal(fb, want) {
						reportDiff(t, i, f, fb, want)
						return
					}
				}
			}
			got := hex.EncodeToString(h.Sum(nil))
			if got != c.hash {
				t.Errorf("hash %s, want %s", got, c.hash)
			}
			t.Logf("hash %s", got)
		})
	}
}

func reportDiff(t *testing.T, i int, f *Frame, got, want []byte) {
	w, h := f.Width, f.Height
	cw, ch := (w+1)/2, (h+1)/2
	sizes := []int{w * h, cw * ch, cw * ch}
	widths := []int{w, cw, cw}
	off := 0
	for p := 0; p < 3; p++ {
		for k := 0; k < sizes[p]; k++ {
			if got[off+k] != want[off+k] {
				n := 0
				for kk := k; kk < sizes[p]; kk++ {
					if got[off+kk] != want[off+kk] {
						n++
					}
				}
				t.Fatalf("frame %d (%dx%d): plane %d first diff at x=%d y=%d got %d want %d (%d diffs)",
					i, w, h, p, k%widths[p], k/widths[p], got[off+k], want[off+k], n)
			}
		}
		off += sizes[p]
	}
}

// A show_existing_frame packet re-outputs a reference slot.
func TestShowExistingFrame(t *testing.T) {
	d := New()
	var last *Frame
	for _, b := range readBlocks(t, "vp9p0_opus_160x90") {
		if err := d.Decode(b); err != nil {
			t.Fatal(err)
		}
		for f := d.Next(); f != nil; f = d.Next() {
			last = f
		}
	}
	// frame_marker 10, profile 0, show_existing_frame 1, slot 0 (LAST).
	if err := d.Decode([]byte{0x88}); err != nil {
		t.Fatal(err)
	}
	f := d.Next()
	if f == nil || !bytes.Equal(frameBytes(f), frameBytes(last)) {
		t.Fatal("show_existing_frame did not repeat the last frame")
	}
	if err := New().Decode([]byte{0x88}); err == nil {
		t.Error("show_existing_frame of an empty slot accepted")
	}
}

// Profile 1 (4:4:4) is detected and rejected; profile 0 is reported.
func TestProfiles(t *testing.T) {
	b := readBlocks(t, "vp9_opus_160x90")
	if p := ProfileOf(b[0]); p != 1 {
		t.Errorf("profile %d, want 1", p)
	}
	if err := New().Decode(b[0]); !errors.Is(err, ErrUnsupported) {
		t.Errorf("profile 1: %v, want ErrUnsupported", err)
	}
	if p := ProfileOf(readBlocks(t, "vp9p0_opus_160x90")[0]); p != 0 {
		t.Errorf("profile %d, want 0", p)
	}
}

func TestSuperframeSplit(t *testing.T) {
	blocks := readBlocks(t, "vp9_altref_160x90")
	n := 0
	for _, b := range blocks {
		if len(SplitSuperframe(b)) > 1 {
			n++
		}
	}
	if n == 0 {
		t.Error("no superframes in the alt-ref clip")
	}
}
