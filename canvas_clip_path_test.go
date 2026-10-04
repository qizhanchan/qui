package qui

import (
	"image"
	"testing"
)

// Basic ClipPath: draw an opaque rect while a circular clip is active
// — pixels inside the circle turn red, pixels outside stay
// transparent. Guards the mask-multiply-then-composite pipeline in
// compositeLayerMasked.
func TestClipPathCircleMasksFill(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)

	depth := c.Save()
	circle := NewPath().AddCircle(20, 20, 10)
	c.ClipPath(circle)
	c.FillRect(Rect{W: 40, H: 40}, Color{R: 1, A: 1})
	c.RestoreTo(depth)

	// Pixel at circle center — should be red.
	i := img.PixOffset(20, 20)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 240 || got[3] < 240 {
		t.Errorf("center pixel = %v, want ~red", got)
	}
	// Pixel far outside the circle — should stay transparent.
	i = img.PixOffset(2, 2)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("outside pixel = %v, want transparent", got)
	}
	// Pixel just past the circle radius — should also be transparent.
	i = img.PixOffset(35, 20)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("past-radius pixel = %v, want transparent", got)
	}
}

// Restore lifts the clip: draws after restore fill the whole canvas
// without any masking.
func TestClipPathRestoreLiftsMask(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)

	depth := c.Save()
	circle := NewPath().AddCircle(20, 20, 8)
	c.ClipPath(circle)
	c.FillRect(Rect{W: 40, H: 40}, Color{R: 1, A: 1})
	c.RestoreTo(depth)

	// Now draw a green fill without any clip. The whole canvas should
	// go green — even pixels that were transparent under the circle
	// clip should now composite over.
	c.FillRect(Rect{W: 40, H: 40}, Color{G: 1, A: 1})
	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[1] < 240 {
		t.Fatalf("post-restore pixel = %v, want ~green", got)
	}
}

// Nested ClipPath — outer + inner mask compose intersection semantics.
// A pixel outside EITHER mask must not survive.
func TestClipPathNestedIntersects(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)

	// Outer clip: left half.
	outerPath := NewPath().AddRect(Rect{X: 0, Y: 0, W: 20, H: 40})
	// Inner clip: top half.
	innerPath := NewPath().AddRect(Rect{X: 0, Y: 0, W: 40, H: 20})

	outer := c.Save()
	c.ClipPath(outerPath)
	inner := c.Save()
	c.ClipPath(innerPath)
	c.FillRect(Rect{W: 40, H: 40}, Color{B: 1, A: 1})
	c.RestoreTo(inner)
	c.RestoreTo(outer)

	// Pixel in the intersection (top-left quadrant): blue.
	i := img.PixOffset(5, 5)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[2] < 240 {
		t.Errorf("intersection pixel = %v, want ~blue", got)
	}
	// Top-right (outside outer): transparent.
	i = img.PixOffset(35, 5)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("outside-outer pixel = %v, want transparent", got)
	}
	// Bottom-left (outside inner): transparent.
	i = img.PixOffset(5, 35)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("outside-inner pixel = %v, want transparent", got)
	}
	// Bottom-right (outside both): transparent.
	i = img.PixOffset(35, 35)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("outside-both pixel = %v, want transparent", got)
	}
}

// ClipPath under a translation — the mask should follow the canvas
// transform, not stay in absolute framebuffer coords.
func TestClipPathRespectsMatrix(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)

	depth := c.Save()
	c.Translate(20, 0)
	circle := NewPath().AddCircle(10, 20, 8) // logical center = (10, 20); after translate, physical center = (30, 20)
	c.ClipPath(circle)
	c.FillRect(Rect{W: 40, H: 40}, Color{R: 1, A: 1})
	c.RestoreTo(depth)

	// Pixel at physical (30, 20) — should be inside the translated
	// circle, i.e. red.
	i := img.PixOffset(30, 20)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 240 {
		t.Errorf("translated-center pixel = %v, want ~red", got)
	}
	// Physical (10, 20) — outside the translated circle.
	i = img.PixOffset(10, 20)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("pre-translation-center pixel = %v, want transparent", got)
	}
}

// Canvas.Clear must respect the current clip — Window.Step relies on
// this to preserve the previous frame's pixels outside the dirty
// region during partial repaints. Regression for a bug the E2.1
// refactor introduced (backend.Clear was wiping the ENTIRE framebuffer
// instead of just the clip); the html-css example broke visibly on
// hover because non-dirty regions got cleared to the theme surface
// color while the widget kept its old rendering.
func TestClearRespectsCurrentClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	// Pre-fill the framebuffer with red to represent "previous frame".
	c := NewImageCanvas(img)
	c.Clear(Color{R: 1, A: 1})
	// Sanity: every pixel is red.
	i := img.PixOffset(0, 0)
	if img.Pix[i] != 255 {
		t.Fatalf("pre-fill failed at (0,0): %v", img.Pix[i:i+4])
	}

	// Simulate a partial repaint: narrow clip to a small rect, then Clear.
	// Only pixels inside the clip should be overwritten.
	depth := c.Save()
	c.ClipRect(Rect{X: 5, Y: 5, W: 5, H: 5})
	c.Clear(Color{G: 1, A: 1})
	c.RestoreTo(depth)

	// Inside clip: should be green.
	i = img.PixOffset(7, 7)
	if img.Pix[i+1] != 255 || img.Pix[i] != 0 {
		t.Errorf("pixel inside clip = %v, want ~green", img.Pix[i:i+4])
	}
	// Outside clip: should STILL be red (previous frame preserved).
	i = img.PixOffset(1, 1)
	if img.Pix[i] != 255 || img.Pix[i+1] != 0 {
		t.Errorf("pixel outside clip = %v, want red (should be preserved)", img.Pix[i:i+4])
	}
	// Also check the far corner.
	i = img.PixOffset(18, 18)
	if img.Pix[i] != 255 || img.Pix[i+1] != 0 {
		t.Errorf("far-corner pixel = %v, want red (should be preserved)", img.Pix[i:i+4])
	}
}
