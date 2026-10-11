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
	e, err := New(Config{Width: w, Height: h, QP: 26, Preset: 0, KeyInt: 1 << 30})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Encode(frames[i%n]); err != nil {
			b.Fatal(err)
		}
	}
}
