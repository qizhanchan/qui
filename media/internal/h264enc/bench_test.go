package h264enc

import (
	"image"
	"io"
	"os"
	"testing"
)

// BenchmarkEncode1080 encodes frames of a raw 1080p I420 clip
// (H264ENC_YUV1080) with the fast preset.
func BenchmarkEncode1080(b *testing.B) {
	path := os.Getenv("H264ENC_YUV1080")
	if path == "" {
		b.Skip("H264ENC_YUV1080 not set")
	}
	const w, h = 1920, 1080
	fsz := w * h * 3 / 2
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	raw := make([]byte, 20*fsz)
	m, _ := io.ReadFull(f, raw)
	n := m / fsz
	frames := make([]*image.YCbCr, n)
	for i := range frames {
		f := raw[i*fsz : (i+1)*fsz]
		frames[i] = &image.YCbCr{Y: f[:w*h], Cb: f[w*h : w*h*5/4], Cr: f[w*h*5/4:],
			YStride: w, CStride: w / 2, SubsampleRatio: image.YCbCrSubsampleRatio420, Rect: image.Rect(0, 0, w, h)}
	}
	for _, bc := range []struct {
		name string
		cfg  Config
	}{
		{"fast", Config{Preset: 0}},
		{"medium", Config{Preset: 1}},
		{"high_cabac_b", Config{Preset: 2, Profile: ProfileHigh, CABAC: true, Transform8x8: true, BFrames: 2}},
		{"fast_4slices", Config{Preset: 0, Slices: 4}},
		{"high_cabac_b_4slices", Config{Preset: 2, Profile: ProfileHigh, CABAC: true, Transform8x8: true, BFrames: 2, Slices: 4}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			cfg := bc.cfg
			cfg.Width, cfg.Height, cfg.QP, cfg.KeyInt = w, h, 26, 250
			e, err := New(cfg)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < n; i++ { // warm up references and pools
				e.Encode(frames[i])
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := e.Encode(frames[i%n]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
