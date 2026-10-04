package qui

import (
	"image"
	"testing"
)

// inkRightEdge scans img for the rightmost pixel with any alpha.
func inkRightEdge(img *image.RGBA) int {
	b := img.Bounds()
	for x := b.Max.X - 1; x >= b.Min.X; x-- {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				return x
			}
		}
	}
	return -1
}

// Letter/word-spacing tracking must scale with the canvas transform like
// glyph sizes do. On a 2× canvas an unscaled WordSpacing paints at half
// strength, so justified lines (whose stretch rides on WordSpacing) fall
// short of the right edge by a per-line-varying amount.
func TestTrackingScalesWithCanvas(t *testing.T) {
	spec := Font{Size: 16, WordSpacing: 12}
	text := "aa bb cc dd"
	logicalW, _ := TextMetrics(text, spec) // includes 3×12px word spacing

	draw := func(scale float32) int {
		img := image.NewRGBA(image.Rect(0, 0, 800, 100))
		c := NewImageCanvas(img)
		c.Save()
		c.Scale(scale, scale)
		c.DrawText(text, Rect{X: 0, Y: 0, W: 600, H: 40}, ColorBlack, spec)
		c.Restore()
		return inkRightEdge(img)
	}

	edge1x := draw(1)
	edge2x := draw(2)
	if edge1x <= 0 || edge2x <= 0 {
		t.Fatalf("no ink painted: 1x=%d 2x=%d", edge1x, edge2x)
	}
	// The 2× render's ink must end at ~double the 1× extent. Without
	// tracking scaling it ends 3×12 = 36px short.
	want := 2 * edge1x
	diff := edge2x - want
	if diff < -8 || diff > 8 {
		t.Errorf("2x ink right edge = %d, want ≈ %d (2× the 1x edge %d); tracking not scaled?", edge2x, want, edge1x)
	}
	// And the drawn advance must match the measured logical width (×scale).
	if d := float32(edge2x) - 2*logicalW; d < -10 || d > 10 {
		t.Errorf("2x ink edge %d vs measured 2×%v — draw/measure divergence %v", edge2x, logicalW, d)
	}
}
