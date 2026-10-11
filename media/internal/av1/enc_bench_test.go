package av1

import (
	"fmt"
	"image"
	"os"
	"testing"
)

// BenchmarkEncodeFile encodes the first 10 frames of an I420 clip
// (AV1_ENC_YUV, AV1_ENC_SIZE=WxH, AV1_ENC_PRESET, AV1_ENC_MINIGOP).
func BenchmarkEncodeFile(b *testing.B) {
	path := os.Getenv("AV1_ENC_YUV")
	if path == "" {
		b.Skip("AV1_ENC_YUV not set")
	}
	var w, h, preset, miniGOP int
	fmt.Sscanf(os.Getenv("AV1_ENC_SIZE"), "%dx%d", &w, &h)
	fmt.Sscan(os.Getenv("AV1_ENC_PRESET"), &preset)
	fmt.Sscan(os.Getenv("AV1_ENC_MINIGOP"), &miniGOP)
	raw, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	fs := w * h * 3 / 2
	n := min(10, len(raw)/fs)
	frames := make([]*image.YCbCr, n)
	for i := range frames {
		img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		f := raw[i*fs:]
		copy(img.Y, f[:w*h])
		copy(img.Cb, f[w*h:w*h+w*h/4])
		copy(img.Cr, f[w*h+w*h/4:fs])
		frames[i] = img
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, err := NewEncoder(EncoderConfig{Width: w, Height: h, QIndex: 128, Preset: preset, MiniGOP: miniGOP})
		if err != nil {
			b.Fatal(err)
		}
		for _, f := range frames {
			if _, err := e.Encode(f); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := e.Flush(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/1000/float64(b.N*n), "ms/frame")
}
