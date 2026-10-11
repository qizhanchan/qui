package yuv

import (
	"image"
	"testing"
)

func TestI420BlackWhite(t *testing.T) {
	// Limited-range black and white.
	y := []byte{16, 235, 16, 235}
	cb := []byte{128, 128}
	cr := []byte{128, 128}
	dst := image.NewRGBA(image.Rect(0, 0, 2, 2))
	I420ToRGBA(dst, y, cb, cr, 2, 2, 2, 1)

	check := func(x, y int, r, g, b byte) {
		o := dst.PixOffset(x, y)
		if dst.Pix[o] != r || dst.Pix[o+1] != g || dst.Pix[o+2] != b || dst.Pix[o+3] != 255 {
			t.Errorf("pixel(%d,%d) = %v, want (%d,%d,%d,255)",
				x, y, dst.Pix[o:o+4], r, g, b)
		}
	}
	check(0, 0, 0, 0, 0)
	check(1, 0, 255, 255, 255)
	check(0, 1, 0, 0, 0)
	check(1, 1, 255, 255, 255)
}

func TestI420Chroma(t *testing.T) {
	// A 2x2 frame with a strong red-ish chroma: full-range red ≈ Y=81,Cb=90,Cr=240.
	y := []byte{81, 81, 81, 81}
	cb := []byte{90, 90}
	cr := []byte{240, 240}
	dst := image.NewRGBA(image.Rect(0, 0, 2, 2))
	I420ToRGBA(dst, y, cb, cr, 2, 2, 2, 1)
	o := dst.PixOffset(0, 0)
	// Red should dominate.
	if dst.Pix[o] <= dst.Pix[o+1] || dst.Pix[o] <= dst.Pix[o+2] {
		t.Errorf("expected red-dominant pixel, got %v", dst.Pix[o:o+4])
	}
}

func TestI420Stride(t *testing.T) {
	// A 2x2 image stored in a 4-wide Y plane and 2-wide chroma plane.
	y := []byte{
		16, 16, 99, 99, // row 0, padding after
		235, 235, 99, 99, // row 1
	}
	cb := []byte{128, 99, 128, 99}
	cr := []byte{128, 99, 128, 99}
	dst := image.NewRGBA(image.Rect(0, 0, 2, 2))
	I420ToRGBA(dst, y, cb, cr, 2, 2, 4, 2)
	o := dst.PixOffset(0, 1)
	if dst.Pix[o] != 255 || dst.Pix[o+1] != 255 || dst.Pix[o+2] != 255 {
		t.Errorf("row1 should be white, got %v", dst.Pix[o:o+4])
	}
}

func TestShadersPresent(t *testing.T) {
	if I420ShaderVertex == "" || I420ShaderFragment == "" {
		t.Fatal("shader sources must be non-empty")
	}
}
