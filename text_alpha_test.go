package qui

import "testing"

// Semi-transparent text.
//
// Color is straight (unassociated) alpha throughout this package, but
// image/draw — which the glyph rasterizer composites through — reads
// color.RGBA as PREMULTIPLIED. Feeding it the straight colour made every
// channel above the alpha an invalid premultiplied value, so text with
// alpha < 1 came out dark and hue-shifted: 50% grey painted near-black,
// 50% red painted grey. Opaque text was unaffected, which is why it went
// unnoticed until a watermark asked for faint ink.

func TestDrawTextHonoursInkAlpha(t *testing.T) {
	cases := []struct {
		name string
		ink  Color
		want [3]uint8 // expected glyph colour over a white background
	}{
		{"opaque grey", Color{R: 0.72, G: 0.72, B: 0.72, A: 1}, [3]uint8{183, 183, 183}},
		{"half black", Color{A: 0.5}, [3]uint8{127, 127, 127}},
		{"half grey", Color{R: 0.72, G: 0.72, B: 0.72, A: 0.5}, [3]uint8{219, 219, 219}},
		{"half red", Color{R: 1, A: 0.5}, [3]uint8{255, 127, 127}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cv, img := NewOffscreenCanvas(400, 160)
			cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
			cv.DrawText("HELLO", Rect{X: 10, Y: 20, W: 380, H: 120}, tc.ink, Font{Size: 90})

			counts := map[[3]uint8]int{}
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					counts[[3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}]++
				}
			}
			best, bestN := [3]uint8{}, 0
			for c, n := range counts {
				if c != [3]uint8{255, 255, 255} && n > bestN {
					best, bestN = c, n
				}
			}
			if bestN == 0 {
				t.Fatal("nothing was drawn")
			}
			// One count of slack per channel: the glyph body is flat, but
			// rounding through 8-bit premultiplication can land a step off.
			for i := range best {
				if diff := int(best[i]) - int(tc.want[i]); diff > 1 || diff < -1 {
					t.Fatalf("glyph colour %v, want %v", best, tc.want)
				}
			}
		})
	}
}
