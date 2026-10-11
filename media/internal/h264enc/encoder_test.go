package h264enc

import (
	"bytes"
	"image"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// sourceFrames decodes a committed decoder vector to use as encoder input.
func sourceFrames(t testing.TB, name string) []*image.YCbCr {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "h264", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	frames, err := h264.DecodeAnnexB(data)
	if err != nil {
		t.Fatal(err)
	}
	var out []*image.YCbCr
	for _, f := range frames {
		out = append(out, toYCbCr(f))
	}
	return out
}

func toYCbCr(f *h264.Frame) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, f.Width, f.Height), image.YCbCrSubsampleRatio420)
	for y := 0; y < f.Height; y++ {
		copy(img.Y[y*img.YStride:], f.Y.Pix[y*f.Y.Stride:y*f.Y.Stride+f.Width])
	}
	for y := 0; y < f.Height/2; y++ {
		copy(img.Cb[y*img.CStride:], f.Cb.Pix[y*f.Cb.Stride:y*f.Cb.Stride+f.Width/2])
		copy(img.Cr[y*img.CStride:], f.Cr.Pix[y*f.Cr.Stride:y*f.Cr.Stride+f.Width/2])
	}
	return img
}

// planes returns the cropped I420 bytes of a frame.
func planes(f *h264.Frame) []byte {
	var b []byte
	for _, p := range []*h264.Plane{f.Y, f.Cb, f.Cr} {
		for y := 0; y < p.H; y++ {
			b = append(b, p.Pix[y*p.Stride:y*p.Stride+p.W]...)
		}
	}
	return b
}

type encResult struct {
	stream []byte
	recon  [][]byte // display order
	bits   int
}

// encodeAll encodes frames and collects the reconstruction of every
// picture in display order.
func encodeAll(t testing.TB, cfg Config, frames []*image.YCbCr) encResult {
	t.Helper()
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.logRecon = true
	var r encResult
	var pkts []Packet
	for _, f := range frames {
		p, err := e.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		pkts = append(pkts, p...)
	}
	p, _ := e.Flush()
	pkts = append(pkts, p...)
	r.recon = e.reconLog
	for _, p := range pkts {
		r.stream = append(r.stream, AnnexB(p.NALs)...)
	}
	r.bits = 8 * len(r.stream)
	return r
}

func srcPlanes(img *image.YCbCr) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var b []byte
	for y := 0; y < h; y++ {
		b = append(b, img.Y[y*img.YStride:y*img.YStride+w]...)
	}
	for _, p := range [][]byte{img.Cb, img.Cr} {
		for y := 0; y < h/2; y++ {
			b = append(b, p[y*img.CStride:y*img.CStride+w/2]...)
		}
	}
	return b
}

func psnr(a, b []byte) float64 {
	var s float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		s += d * d
	}
	if s == 0 {
		return 99
	}
	return 10 * math.Log10(255*255*float64(len(a))/s)
}

var testConfigs = []struct {
	name string
	cfg  Config
}{
	{"baseline_intra", Config{KeyInt: 1, QP: 28}},
	{"baseline_p", Config{QP: 28}},
	{"baseline_p_fast", Config{QP: 30, Preset: 0}},
	{"baseline_p_parts", Config{QP: 24, Preset: 2}},
	{"baseline_refs3", Config{QP: 26, Refs: 3, Preset: 1}},
	{"baseline_lowqp", Config{QP: 4, KeyInt: 5}},
	{"baseline_highqp", Config{QP: 51}},
	{"baseline_slices", Config{QP: 27, Slices: 3, Preset: 1}},
	{"baseline_nodeblock", Config{QP: 32, NoDeblock: true}},
	{"baseline_deblock_offsets", Config{QP: 36, DeblockAlpha: 3, DeblockBeta: -2, ChromaQPOffset: 2}},
	{"main_cabac_intra", Config{Profile: ProfileMain, CABAC: true, KeyInt: 1, QP: 26}},
	{"main_cabac_p", Config{Profile: ProfileMain, CABAC: true, QP: 27, Preset: 2}},
	{"main_cabac_refs", Config{Profile: ProfileMain, CABAC: true, QP: 25, Refs: 3, Preset: 1}},
	{"main_cabac_lowqp", Config{Profile: ProfileMain, CABAC: true, QP: 2, KeyInt: 4}},
	{"main_cabac_highqp", Config{Profile: ProfileMain, CABAC: true, QP: 50}},
	{"main_cavlc_b", Config{Profile: ProfileMain, BFrames: 2, QP: 27, Preset: 1}},
	{"main_cabac_b", Config{Profile: ProfileMain, CABAC: true, BFrames: 3, QP: 29, Preset: 1}},
	{"main_cabac_slices", Config{Profile: ProfileMain, CABAC: true, BFrames: 1, Slices: 2, QP: 30}},
	{"high_cavlc_8x8", Config{Profile: ProfileHigh, Transform8x8: true, QP: 26, Preset: 1}},
	{"high_cabac_8x8", Config{Profile: ProfileHigh, CABAC: true, Transform8x8: true, QP: 24, Preset: 2}},
	{"high_cabac_8x8_b", Config{Profile: ProfileHigh, CABAC: true, Transform8x8: true, BFrames: 2, Refs: 2, QP: 28, Preset: 2, KeyInt: 7}},
	{"high_crf", Config{Profile: ProfileHigh, CABAC: true, Transform8x8: true, BFrames: 2, CRF: 23, Preset: 1}},
}

// TestReconMatchesDecoder checks the encoder's reconstruction against our
// decoder's output for the stream, frame by frame.
func TestReconMatchesDecoder(t *testing.T) {
	for _, src := range []string{"vt_main_p8x8.264", "crop_offedge.264"} {
		frames := sourceFrames(t, src)
		for _, tc := range testConfigs {
			t.Run(src+"/"+tc.name, func(t *testing.T) {
				cfg := tc.cfg
				cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
				r := encodeAll(t, cfg, frames)
				dec, err := h264.DecodeAnnexB(r.stream)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(dec) != len(frames) {
					t.Fatalf("decoded %d frames, want %d", len(dec), len(frames))
				}
				var ps float64
				for i, f := range dec {
					got := planes(f)
					if !bytes.Equal(got, r.recon[i]) {
						t.Fatalf("frame %d: decoder output differs from encoder reconstruction", i)
					}
					ps += psnr(got, srcPlanes(frames[i]))
				}
				t.Logf("%d bytes, PSNR %.2f dB", len(r.stream), ps/float64(len(dec)))
			})
		}
	}
}

// TestFFmpegConformance decodes every test configuration with FFmpeg and
// requires no errors and output identical to the encoder's reconstruction.
func TestFFmpegConformance(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	dir := t.TempDir()
	for _, src := range []string{"vt_main_p8x8.264", "crop_offedge.264"} {
		frames := sourceFrames(t, src)
		for _, tc := range testConfigs {
			t.Run(src+"/"+tc.name, func(t *testing.T) {
				cfg := tc.cfg
				cfg.Width, cfg.Height = frames[0].Rect.Dx(), frames[0].Rect.Dy()
				r := encodeAll(t, cfg, frames)
				in := filepath.Join(dir, "in.264")
				out := filepath.Join(dir, "out.yuv")
				if err := os.WriteFile(in, r.stream, 0o644); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(ff, "-v", "error", "-y", "-i", in, "-f", "rawvideo", "-pix_fmt", "yuv420p", out)
				msg, err := cmd.CombinedOutput()
				if err != nil || len(msg) > 0 {
					t.Fatalf("ffmpeg: %v %s", err, msg)
				}
				got, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				want := bytes.Join(r.recon, nil)
				if !bytes.Equal(got, want) {
					t.Fatalf("FFmpeg output (%d bytes) differs from the reconstruction (%d bytes)", len(got), len(want))
				}
			})
		}
	}
}
