package pdf

import (
	"image"
	"os"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/qizhanchan/qui/internal/typeset"
)

// testHalvesImage is a w×h image whose left half is red and right half blue.
func testHalvesImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			if x < w/2 {
				img.Pix[i], img.Pix[i+3] = 255, 255 // red
			} else {
				img.Pix[i+2], img.Pix[i+3] = 255, 255 // blue
			}
		}
	}
	return img
}

// testLoadShapingFont installs the font at path as the default for the
// test, skipping when it is absent.
func testLoadShapingFont(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("shaping test font %s not present; skipping", path)
		}
		t.Fatalf("read shaping test font: %v", err)
	}
	if err := typeset.LoadFontFromBytes(data); err != nil {
		t.Fatalf("load shaping test font: %v", err)
	}
	t.Cleanup(func() { _ = typeset.LoadFontFromBytes(goregular.TTF) })
}
