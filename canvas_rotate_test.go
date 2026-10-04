package qui

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// Rotated raster rendering (F0). Before DrawImageTransformed existed,
// imageCanvas.DrawText / DrawImage / DrawVector all projected their rect
// with Matrix.TransformRect — an axis-aligned BOUNDING BOX — so rotated
// content was painted upright inside a too-large box instead of rotated.
// Shapes were never affected (DrawShape already converts non-axis-aligned
// rects to a Path), which is why these tests focus on the raster trio.

// opaqueRed / opaqueGreen feed the package's existing solidImage helper
// (renderer_gl_test.go).
var (
	opaqueRed   = color.RGBA{R: 255, A: 255}
	opaqueGreen = color.RGBA{G: 255, A: 255}
)

// halvesImage builds an image whose left half is red and right half blue,
// so a 180° rotation is observable as the halves swapping.
func halvesImage(w, h int) *image.RGBA {
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

func alphaAt(img *image.RGBA, x, y int) uint8 {
	return img.Pix[img.PixOffset(x, y)+3]
}

func rgbaAt(img *image.RGBA, x, y int) [4]uint8 {
	i := img.PixOffset(x, y)
	return [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

// rotateAbout applies a rotation of theta radians around (cx, cy) in
// logical coordinates — the transform a slide editor applies per object.
func rotateAbout(c Canvas, cx, cy, theta float32) {
	c.Translate(cx, cy)
	c.Rotate(theta)
	c.Translate(-cx, -cy)
}

// inkBounds returns the bounding box of all non-transparent pixels.
func inkBounds(img *image.RGBA) image.Rectangle {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if alphaAt(img, x, y) > 16 {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if minX > maxX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

// A 40×40 image rotated 45° about its own center covers a diamond, not a
// square: the axis-aligned box corners must stay empty. This is the exact
// case the old TransformRect path got wrong — it filled the whole box.
func TestDrawImageRotated45CoversDiamondNotBoundingBox(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	c := NewImageCanvas(img)
	src := solidImage(40, 40, opaqueRed)

	depth := c.Save()
	rotateAbout(c, 50, 50, math.Pi/4)
	c.DrawImage(src, Rect{X: 30, Y: 30, W: 40, H: 40})
	c.RestoreTo(depth)

	// Diamond center and top vertex are inside the rotated square.
	if got := rgbaAt(img, 50, 50); got[0] < 240 || got[3] < 240 {
		t.Errorf("diamond center = %v, want ~opaque red", got)
	}
	if a := alphaAt(img, 50, 27); a < 240 {
		t.Errorf("diamond top vertex alpha = %d, want ~255", a)
	}
	// (25,25) sits inside the rotated footprint's bounding box but well
	// outside the diamond itself. Under the old AABB behaviour it was
	// filled; it must now be empty.
	if a := alphaAt(img, 25, 25); a > 8 {
		t.Errorf("bounding-box corner alpha = %d, want transparent — "+
			"rotated image still painting its AABB", a)
	}
	// The unrotated square's own corner is likewise outside the diamond.
	if a := alphaAt(img, 32, 32); a > 8 {
		t.Errorf("unrotated corner alpha = %d, want transparent", a)
	}
}

// 180° is a pure pixel permutation, so it pins down sampler orientation:
// a left-red / right-blue source must come back left-blue / right-red.
func TestDrawImageRotated180SwapsHalves(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	c := NewImageCanvas(img)
	src := halvesImage(40, 40)

	depth := c.Save()
	rotateAbout(c, 30, 30, math.Pi)
	c.DrawImage(src, Rect{X: 10, Y: 10, W: 40, H: 40})
	c.RestoreTo(depth)

	if got := rgbaAt(img, 20, 30); got[2] < 200 || got[0] > 60 {
		t.Errorf("left sample after 180° = %v, want blue", got)
	}
	if got := rgbaAt(img, 40, 30); got[0] < 200 || got[2] > 60 {
		t.Errorf("right sample after 180° = %v, want red", got)
	}
}

// 90° maps a rect exactly onto a rect, so the rotated path must agree
// with the geometry the old AABB path happened to get right here: a wide
// source lands as a tall footprint. Guards against a transposed or
// mirrored inverse map.
func TestDrawImageRotated90ProducesTallFootprint(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	c := NewImageCanvas(img)
	src := solidImage(40, 10, opaqueGreen)

	depth := c.Save()
	rotateAbout(c, 40, 40, math.Pi/2)
	c.DrawImage(src, Rect{X: 20, Y: 35, W: 40, H: 10})
	c.RestoreTo(depth)

	ink := inkBounds(img)
	if ink.Empty() {
		t.Fatal("no ink after 90° rotation")
	}
	if ink.Dy() <= ink.Dx() {
		t.Errorf("ink bounds %v: want taller than wide after 90° rotation", ink)
	}
	// Footprint should be the 10×40 rect centered on (40,40), within a
	// pixel of snapping slack.
	want := image.Rect(35, 20, 45, 60)
	if !nearRect(ink, want, 2) {
		t.Errorf("ink bounds = %v, want ≈%v", ink, want)
	}
}

// The axis-aligned fast path must be untouched: an unrotated blit still
// fills its rect exactly, with no resample fringe bleeding outside.
func TestDrawImageAxisAlignedPathUnchanged(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)
	src := solidImage(20, 20, opaqueRed)

	c.DrawImage(src, Rect{X: 10, Y: 10, W: 20, H: 20})

	if got := rgbaAt(img, 10, 10); got[0] < 240 || got[3] < 240 {
		t.Errorf("rect origin = %v, want opaque red", got)
	}
	if got := rgbaAt(img, 29, 29); got[0] < 240 || got[3] < 240 {
		t.Errorf("rect far corner = %v, want opaque red", got)
	}
	if a := alphaAt(img, 9, 9); a != 0 {
		t.Errorf("pixel outside rect alpha = %d, want 0 (fringe bleed)", a)
	}
	if a := alphaAt(img, 30, 30); a != 0 {
		t.Errorf("pixel past rect alpha = %d, want 0 (fringe bleed)", a)
	}
}

// A scaled-up rotation must sample at the canvas scale, not at logical
// size — this is the HiDPI case (window pushes Scale(dpr,dpr) before any
// widget transform), so the rotated branch has to compose both.
func TestDrawImageRotatedUnderScaleCoversScaledFootprint(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	c := NewImageCanvas(img)
	src := solidImage(20, 20, opaqueRed)

	depth := c.Save()
	c.Scale(2, 2) // logical 60×60 surface at 2× DPR
	rotateAbout(c, 30, 30, math.Pi/4)
	c.DrawImage(src, Rect{X: 20, Y: 20, W: 20, H: 20})
	c.RestoreTo(depth)

	ink := inkBounds(img)
	if ink.Empty() {
		t.Fatal("no ink for rotated+scaled image")
	}
	// Logical center (30,30) → physical (60,60); the diamond's half
	// diagonal is 10·√2 logical = ~28 physical.
	if got := rgbaAt(img, 60, 60); got[0] < 240 || got[3] < 240 {
		t.Errorf("scaled diamond center = %v, want opaque red", got)
	}
	want := image.Rect(60-29, 60-29, 60+29, 60+29)
	if !nearRect(ink, want, 3) {
		t.Errorf("ink bounds = %v, want ≈%v", ink, want)
	}
}

// Rotated text has to end up rotated. The old path scaled the font and
// then handed over an AABB, so glyphs stayed horizontal no matter the
// matrix: a 90° rotation produced wide ink instead of tall ink.
func TestDrawTextRotated90ProducesTallInk(t *testing.T) {
	const label = "Rotate"
	flat := image.NewRGBA(image.Rect(0, 0, 140, 140))
	fc := NewImageCanvas(flat)
	fc.DrawText(label, Rect{X: 20, Y: 60, W: 100, H: 20}, Color{A: 1}, Font{Size: 16})
	flatInk := inkBounds(flat)
	if flatInk.Empty() {
		t.Skip("no text ink — no usable font face in this environment")
	}
	if flatInk.Dx() <= flatInk.Dy() {
		t.Fatalf("baseline (unrotated) ink %v is not wider than tall; "+
			"test premise broken", flatInk)
	}

	rot := image.NewRGBA(image.Rect(0, 0, 140, 140))
	rc := NewImageCanvas(rot)
	depth := rc.Save()
	rotateAbout(rc, 70, 70, math.Pi/2)
	rc.DrawText(label, Rect{X: 20, Y: 60, W: 100, H: 20}, Color{A: 1}, Font{Size: 16})
	rc.RestoreTo(depth)

	rotInk := inkBounds(rot)
	if rotInk.Empty() {
		t.Fatal("rotated text produced no ink")
	}
	if rotInk.Dy() <= rotInk.Dx() {
		t.Errorf("rotated ink %v is not taller than wide — text still "+
			"painting upright inside its bounding box", rotInk)
	}
	// Rotating by 90° should transpose the ink extents, within a couple of
	// pixels of rasterization slack.
	if diff := abs(rotInk.Dy() - flatInk.Dx()); diff > 3 {
		t.Errorf("rotated ink height %d vs flat ink width %d (diff %d), "+
			"want transposed", rotInk.Dy(), flatInk.Dx(), diff)
	}
}

// Rotated text must still respect the clip — the offscreen detour is a
// separate code path from the axis-aligned one, so its clip handling
// needs its own guard.
func TestDrawTextRotatedRespectsClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 140, 140))
	c := NewImageCanvas(img)

	depth := c.Save()
	// Clip to the left half, then draw rotated text spanning the width.
	c.ClipRect(Rect{W: 70, H: 140})
	rotateAbout(c, 70, 70, math.Pi/4)
	c.DrawText("Clipped text sample", Rect{X: 10, Y: 60, W: 120, H: 20}, Color{A: 1}, Font{Size: 16})
	c.RestoreTo(depth)

	for y := 0; y < 140; y++ {
		for x := 70; x < 140; x++ {
			if a := alphaAt(img, x, y); a > 8 {
				t.Fatalf("ink at (%d,%d) alpha=%d escaped the clip", x, y, a)
			}
		}
	}
}

// stripeVector is a VectorSource whose rasterization is opaque on the top
// half only, so rotation is observable in the output.
type stripeVector struct{}

func (stripeVector) Rasterize(w, h int, tint Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h/2; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+3] = 255, 255
		}
	}
	return img
}

// DrawVector shares the rotated branch with DrawImage but has to
// rasterize first, at the canvas scale rather than at logical size.
func TestDrawVectorRotated180FlipsStripe(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	c := NewImageCanvas(img)

	depth := c.Save()
	rotateAbout(c, 30, 30, math.Pi)
	c.DrawVector(stripeVector{}, Rect{X: 10, Y: 10, W: 40, H: 40}, Color{A: 1})
	c.RestoreTo(depth)

	// Stripe started on the top half; after 180° it must be on the bottom.
	if a := alphaAt(img, 30, 40); a < 200 {
		t.Errorf("bottom-half alpha = %d, want opaque after 180° rotation", a)
	}
	if a := alphaAt(img, 30, 20); a > 8 {
		t.Errorf("top-half alpha = %d, want transparent after 180° rotation", a)
	}
}

// A degenerate (zero-scale) matrix has no inverse; the rotated path must
// bail rather than divide by zero or paint garbage.
func TestDrawImageDegenerateMatrixIsNoop(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	c := NewImageCanvas(img)
	src := solidImage(20, 20, opaqueRed)

	depth := c.Save()
	// Rotate (so IsAxisAligned is false) then collapse one axis.
	rotateAbout(c, 20, 20, math.Pi/4)
	c.Scale(0, 1)
	c.DrawImage(src, Rect{X: 10, Y: 10, W: 20, H: 20})
	c.RestoreTo(depth)

	if ink := inkBounds(img); !ink.Empty() {
		t.Errorf("degenerate transform painted %v, want nothing", ink)
	}
}

func nearRect(got, want image.Rectangle, tol int) bool {
	return abs(got.Min.X-want.Min.X) <= tol && abs(got.Min.Y-want.Min.Y) <= tol &&
		abs(got.Max.X-want.Max.X) <= tol && abs(got.Max.Y-want.Max.Y) <= tol
}
