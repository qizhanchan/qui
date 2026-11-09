package render

import (
	"image"
	"testing"
)

func TestScrollImageMovesRowsAndColumns(t *testing.T) {
	mk := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 6, 6))
		for y := 0; y < 6; y++ {
			for x := 0; x < 6; x++ {
				img.Pix[img.PixOffset(x, y)] = byte(y*10 + x)
			}
		}
		return img
	}
	at := func(img *image.RGBA, x, y int) byte { return img.Pix[img.PixOffset(x, y)] }

	img := mk()
	if !scrollImage(img, image.Rect(1, 1, 5, 5), 0, 2) {
		t.Fatal("declined")
	}
	// Row 4 of the rect now holds old row 2; outside the rect untouched.
	if at(img, 2, 4) != 22 || at(img, 2, 3) != 12 || at(img, 0, 4) != 40 || at(img, 5, 4) != 45 {
		t.Fatalf("down move wrong: %d %d %d %d", at(img, 2, 4), at(img, 2, 3), at(img, 0, 4), at(img, 5, 4))
	}

	img = mk()
	scrollImage(img, image.Rect(1, 1, 5, 5), -1, -1)
	if at(img, 1, 1) != 22 || at(img, 3, 3) != 44 {
		t.Fatalf("up-left move wrong: %d %d", at(img, 1, 1), at(img, 3, 3))
	}

	if scrollImage(mk(), image.Rect(0, 0, 6, 6), 0, 6) {
		t.Fatal("a move past the rect must be declined")
	}
	if scrollImage(mk(), image.Rect(0, 0, 7, 6), 0, 1) {
		t.Fatal("a rect outside the image must be declined")
	}
}
