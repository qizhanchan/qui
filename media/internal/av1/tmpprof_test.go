package av1

import (
	"image"
	"os"
	"testing"
)

func TestTmpEncProf(t *testing.T) {
	raw, _ := os.ReadFile(os.Getenv("Y"))
	w, h := 640, 360
	fs := w * h * 3 / 2
	e, _ := NewEncoder(EncoderConfig{Width: w, Height: h, QIndex: 128})
	for i := 0; i < 2; i++ {
		img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
		f := raw[i*fs:]
		copy(img.Y, f[:w*h])
		copy(img.Cb, f[w*h:w*h+w*h/4])
		copy(img.Cr, f[w*h+w*h/4:fs])
		e.Encode(img)
	}
}
