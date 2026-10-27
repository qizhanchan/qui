package qui

import (
	"image"
	"image/color"
	"testing"
)

// Direct (internal) tests for the resampler. The CPU buffer is never
// available in test windows (no GLRenderer), so we synthesize a
// source RGBA manually and check the math.

func makeRGBA(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestResample_IdentityCopy(t *testing.T) {
	src := makeRGBA(16, 16, color.RGBA{R: 12, G: 34, B: 56, A: 255})
	dst := resampleRGBA(src, 16, 16)
	if dst == nil {
		t.Fatal("nil dst")
	}
	if dst == src {
		t.Fatal("expected fresh image, got aliased pointer")
	}
	if dst.RGBAAt(5, 5) != src.RGBAAt(5, 5) {
		t.Fatalf("identity copy mismatched")
	}
}

func TestResample_Shrink(t *testing.T) {
	src := makeRGBA(8, 8, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	dst := resampleRGBA(src, 4, 4)
	if dst == nil || dst.Bounds().Dx() != 4 {
		t.Fatalf("dst bounds = %v", dst.Bounds())
	}
	// Uniform source → uniform output.
	want := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if got := dst.RGBAAt(x, y); got != want {
				t.Fatalf("dst[%d,%d] = %+v, want %+v", x, y, got, want)
			}
		}
	}
}

func TestResample_Expand(t *testing.T) {
	src := makeRGBA(2, 2, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	dst := resampleRGBA(src, 6, 6)
	if dst == nil || dst.Bounds().Dx() != 6 {
		t.Fatalf("dst bounds = %v", dst.Bounds())
	}
	want := color.RGBA{R: 10, G: 20, B: 30, A: 255}
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			if got := dst.RGBAAt(x, y); got != want {
				t.Fatalf("expanded uniform mismatch at %d,%d: %+v", x, y, got)
			}
		}
	}
}

func TestResample_AverageMixed(t *testing.T) {
	// Half-white, half-black source shrunk to 1×2 should average to
	// 128 on the side that spans both.
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	src.SetRGBA(1, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	src.SetRGBA(0, 1, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	src.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	dst := resampleRGBA(src, 1, 1)
	if dst == nil {
		t.Fatal("nil dst")
	}
	c := dst.RGBAAt(0, 0)
	if c.R != 127 || c.G != 127 || c.B != 127 {
		t.Fatalf("average = %+v, want ~127", c)
	}
}

func TestResample_InvalidInputs(t *testing.T) {
	if resampleRGBA(nil, 4, 4) != nil {
		t.Errorf("nil src should yield nil")
	}
	src := makeRGBA(2, 2, color.RGBA{A: 255})
	if resampleRGBA(src, 0, 4) != nil {
		t.Errorf("0 width should yield nil")
	}
	if resampleRGBA(src, 4, -1) != nil {
		t.Errorf("negative height should yield nil")
	}
}

func TestAnnotationLabel(t *testing.T) {
	cases := []struct {
		n    *AXNode
		want string
	}{
		{&AXNode{ID: "submit"}, "#submit"},
		{&AXNode{Role: RoleButton, Name: "Save"}, "button:Save"},
		{&AXNode{Role: RoleLabel}, "label"},
	}
	for _, c := range cases {
		if got := annotationLabel(c.n); got != c.want {
			t.Errorf("label(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
}
