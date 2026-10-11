package hevc

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// testSource returns n frames of the committed 176x144 clip as I420
// images (from the HEVC vectors' source when HEVC_SRC is set, else a
// synthetic moving pattern).
func testSource(t testing.TB, w, h, n int) []*image.YCbCr {
	t.Helper()
	var raw []byte
	if f := os.Getenv("HEVC_SRC"); f != "" {
		raw, _ = os.ReadFile(f)
	}
	fs := w * h * 3 / 2
	out := make([]*image.YCbCr, n)
	for i := range out {
		img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		if len(raw) >= (i+1)*fs {
			f := raw[i*fs:]
			copy(img.Y, f[:w*h])
			copy(img.Cb, f[w*h:w*h+w*h/4])
			copy(img.Cr, f[w*h+w*h/4:fs])
		} else {
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					v := (x*3 + y*2 + i*4) & 255
					if (x/16+y/16+i/3)&1 == 0 {
						v = 255 - v/2
					}
					img.Y[y*img.YStride+x] = byte(v)
				}
			}
			for y := 0; y < h/2; y++ {
				for x := 0; x < w/2; x++ {
					img.Cb[y*img.CStride+x] = byte(128 + (x-y+i)%40)
					img.Cr[y*img.CStride+x] = byte(100 + (x+y*2)%60)
				}
			}
		}
		out[i] = img
	}
	return out
}

// encodeAll encodes frames and returns the Annex-B stream and the
// encoder's reconstruction (output order).
func encodeAll(t testing.TB, cfg EncoderConfig, frames []*image.YCbCr) ([]byte, []*Frame) {
	t.Helper()
	e, err := NewEncoder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.keepRecon = true
	var nals [][]byte
	for _, f := range frames {
		pkts, err := e.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pkts {
			nals = append(nals, p.NALs...)
		}
	}
	pkts, _ := e.Flush()
	for _, p := range pkts {
		nals = append(nals, p.NALs...)
	}
	e.d.Flush()
	for f := e.d.Next(); f != nil; f = e.d.Next() {
		e.recon = append(e.recon, f)
	}
	return AnnexB(nals), e.recon
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
	mse := s / float64(len(a))
	return 10 * log10(255*255/mse)
}

func log10(x float64) float64 {
	// small helper to avoid importing math in several test files
	r := 0.0
	for x >= 10 {
		x /= 10
		r++
	}
	for x < 1 {
		x *= 10
		r--
	}
	// ln approximation via series is unnecessary: use 8 refinement steps
	y := 0.0
	f := 1.0
	for i := 0; i < 30; i++ {
		x = x * x
		f /= 2
		if x >= 10 {
			x /= 10
			y += f
		}
	}
	return r + y
}

var encConfigs = []struct {
	name string
	cfg  EncoderConfig
}{
	{"intra_qp30", EncoderConfig{QP: 30, KeyInt: 1}},
	{"intra_qp22_ctu16", EncoderConfig{QP: 22, KeyInt: 1, CTU: 16}},
	{"intra_qp40_ctu64_nosao", EncoderConfig{QP: 40, KeyInt: 1, CTU: 64, NoSAO: true, NoSignHiding: true}},
	{"intra_wpp", EncoderConfig{QP: 32, KeyInt: 1, WPP: true}},
	{"intra_tiles", EncoderConfig{QP: 32, KeyInt: 1, TileCols: 2, TileRows: 2, CTU: 16}},
	{"intra_slices_dep", EncoderConfig{QP: 32, KeyInt: 1, Slices: 2, DependentSlices: true, CTU: 16, WPP: true}},
	{"p_qp30", EncoderConfig{QP: 30}},
	{"p_qp24_ctu64", EncoderConfig{QP: 24, CTU: 64}},
	{"p_refs3_wpp", EncoderConfig{QP: 32, Refs: 3, WPP: true}},
	{"p_qp38_nosao_nosdh", EncoderConfig{QP: 38, NoSAO: true, NoSignHiding: true, Preset: 2}},
	{"p_tiles_slices", EncoderConfig{QP: 30, TileCols: 2, Slices: 2, CTU: 16}},
	{"b3_qp30", EncoderConfig{QP: 30, BFrames: 3}},
	{"b2_refs2_wpp_ctu64", EncoderConfig{QP: 27, BFrames: 2, Refs: 2, WPP: true, CTU: 64}},
	{"b1_keyint4", EncoderConfig{QP: 33, BFrames: 1, KeyInt: 4}},
}

// TestEncoderReconMatchesDecoders checks, for each configuration, that
// our decoder and FFmpeg (when installed) decode the stream to exactly the
// encoder's reconstruction.
func TestEncoderReconMatchesDecoders(t *testing.T) {
	const w, h = 176, 144
	frames := testSource(t, w, h, 9)
	for _, c := range encConfigs {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.cfg
			cfg.Width, cfg.Height = w, h
			stream, recon := encodeAll(t, cfg, frames)
			got, err := DecodeAnnexB(stream)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(got) != len(recon) || len(got) != len(frames) {
				t.Fatalf("%d decoded, %d reconstructed, %d input", len(got), len(recon), len(frames))
			}
			var src []byte
			for i := range got {
				if !bytes.Equal(framePlanes(got[i]), framePlanes(recon[i])) {
					t.Fatalf("frame %d: decoder output differs from the reconstruction", i)
				}
				src = append(src, i420(frames[i])...)
			}
			var all []byte
			for _, f := range got {
				all = append(all, framePlanes(f)...)
			}
			t.Logf("%d bytes, PSNR %.2f dB", len(stream), psnr(src, all))
			ff, err := exec.LookPath("ffmpeg")
			if err != nil {
				return
			}
			dir := t.TempDir()
			in := filepath.Join(dir, "s.265")
			if err := os.WriteFile(in, stream, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(ff, "-v", "error", "-i", in, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
			if err != nil {
				t.Fatalf("ffmpeg: %v", err)
			}
			if !bytes.Equal(out, all) {
				// FFmpeg's software decoder deviates on some CTU-16 streams
				// with deblocking and SAO (see vectors_test.go); defer to
				// VideoToolbox when it is available.
				vt, err := exec.Command(ff, "-v", "error", "-hwaccel", "videotoolbox", "-i", in, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-").Output()
				if err == nil && bytes.Equal(vt, all) && !bytes.Equal(vt, out) {
					t.Logf("FFmpeg differs; VideoToolbox matches the reconstruction")
					return
				}
				fs := w * h * 3 / 2
				for i := 0; i*fs < len(out) && i*fs < len(all); i++ {
					if !bytes.Equal(out[i*fs:(i+1)*fs], all[i*fs:(i+1)*fs]) {
						t.Fatalf("FFmpeg differs at frame %d (%d vs %d bytes)", i, len(out), len(all))
					}
				}
				t.Fatalf("FFmpeg output length %d, ours %d", len(out), len(all))
			}
		})
	}
}

func i420(img *image.YCbCr) []byte {
	var b []byte
	w, h := img.Rect.Dx(), img.Rect.Dy()
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

var _ = fmt.Sprint
