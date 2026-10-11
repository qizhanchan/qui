package hevc

import (
	"fmt"
	"image"
	"os"
	"testing"
)

// BenchmarkEncodeFile encodes HEVC_ENC_YUV (HEVC_ENC_SIZE, first 10
// frames) at QP 30 with HEVC_ENC_PRESET and reports ms per frame.
func BenchmarkEncodeFile(b *testing.B) {
	path := os.Getenv("HEVC_ENC_YUV")
	if path == "" {
		b.Skip("HEVC_ENC_YUV not set")
	}
	var w, h, preset int
	fmt.Sscanf(os.Getenv("HEVC_ENC_SIZE"), "%dx%d", &w, &h)
	fmt.Sscan(os.Getenv("HEVC_ENC_PRESET"), &preset)
	raw, _ := os.ReadFile(path)
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
		e, _ := NewEncoder(EncoderConfig{Width: w, Height: h, QP: 30, Preset: preset})
		for _, f := range frames {
			e.Encode(f)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/1000/float64(b.N*n), "ms/frame")
}
