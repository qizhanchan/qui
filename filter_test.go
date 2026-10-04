package qui

import (
	"image"
	"image/color"
	"testing"
)

// GrayscaleColorFilter maps a red pixel to its luminance value. The
// filter applies inside a SaveLayer scope so the color transform
// runs at composite time.
func TestColorFilterGrayscale(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	c := NewImageCanvas(img)

	depth := c.SaveLayer(Rect{W: 4, H: 4}, Paint{ColorFilter: GrayscaleColorFilter()})
	c.FillRect(Rect{W: 4, H: 4}, Color{R: 1, A: 1})
	c.RestoreTo(depth)

	// Luminance of pure red under BT.601 = 0.299 → ~76.
	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 70 || got[0] > 82 {
		t.Errorf("red-grayscale luminance = %v, want ~76", got)
	}
	// Grayscale means all channels equal.
	if got[0] != got[1] || got[1] != got[2] {
		t.Errorf("grayscale channels not equal: %v", got)
	}
	if got[3] < 250 {
		t.Errorf("alpha lost during color filter: %v", got)
	}
}

// InvertColorFilter flips every channel except alpha.
func TestColorFilterInvert(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	c := NewImageCanvas(img)

	depth := c.SaveLayer(Rect{W: 4, H: 4}, Paint{ColorFilter: InvertColorFilter()})
	// Pure green in → inverted = (1-0, 1-1, 1-0, 1) = magenta.
	c.FillRect(Rect{W: 4, H: 4}, Color{G: 1, A: 1})
	c.RestoreTo(depth)

	i := img.PixOffset(2, 2)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	// Expected: magenta (255, 0, 255, 255).
	if got[0] < 240 || got[1] > 10 || got[2] < 240 || got[3] < 240 {
		t.Errorf("inverted-green = %v, want ~magenta", got)
	}
}

// DropShadowImageFilter renders a soft-ish shadow that extends past
// the source rect. Pixels outside the source but within halo distance
// should carry shadow color at reduced alpha.
func TestDropShadowImageFilterExtendsPastSource(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	c := NewImageCanvas(img)

	// Draw a filled circle centered at (30, 30) with a drop shadow.
	// Offset 3, blur 3 — shadow halo extends 3 + 3*3 = ~12 px past the
	// circle at the bottom-right.
	depth := c.SaveLayer(Rect{X: 10, Y: 10, W: 40, H: 40}, Paint{
		ImageFilter: DropShadowImageFilter{
			Offset: Point{X: 3, Y: 3},
			Blur:   3,
			Color:  Color{A: 0.5},
		},
	})
	path := NewPath().AddCircle(30, 30, 10)
	c.DrawShape(ShapePath{Path: path}, Paint{Color: Color{R: 1, A: 1}, AntiAlias: true})
	c.RestoreTo(depth)

	// Under the circle center: should be red.
	i := img.PixOffset(30, 30)
	got := [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[0] < 200 {
		t.Errorf("circle center = %v, want ~red", got)
	}
	// Beside the circle at the shadow direction (~x=42, y=33) — should
	// have some shadow tint (alpha > 0). Threshold intentionally low:
	// the test just proves the shadow extends past the source, not a
	// specific perceptual intensity.
	i = img.PixOffset(42, 33)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] < 5 {
		t.Errorf("shadow halo pixel = %v, want translucent black-ish", got)
	}
	// Far outside both (top-left corner) — untouched.
	i = img.PixOffset(2, 2)
	got = [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
	if got[3] > 8 {
		t.Errorf("far-corner pixel = %v, want transparent", got)
	}
}

// MatrixColorFilter identity is a passthrough.
func TestMatrixColorFilterIdentity(t *testing.T) {
	f := IdentityMatrixColorFilter()
	in := Color{R: 0.3, G: 0.5, B: 0.7, A: 0.9}
	out := f.ApplyColor(in)
	if out != in {
		t.Fatalf("identity filter changed color: %v → %v", in, out)
	}
}

// A partial repaint must produce the same pixels a full repaint would,
// and must not touch a pixel outside the dirty region. A filtered layer
// used to break both halves at once: the layer was allocated to
// bounds ∩ clip, so the blur ran over a slice of the shape cut at the
// dirty rect, and the grown result composited back with no clip at all —
// painting a blurred rim OUTSIDE the invalidated region, where nothing
// ever repaints it — a dark border hugging whatever was last redrawn
// over a shadowed shape (a hovered menu row), darkening on every hover.
func TestFilteredLayerPartialRepaintMatchesFullRepaint(t *testing.T) {
	const w, h = 80, 80
	shadow := func() Paint {
		return Paint{ImageFilter: DropShadowImageFilter{
			Offset: Point{X: 3, Y: 3}, Blur: 3, Color: Color{A: 0.6},
		}}
	}
	paintScene := func(c Canvas) {
		depth := c.SaveLayer(Rect{X: 5, Y: 5, W: 50, H: 50}, shadow())
		c.DrawShape(ShapePath{Path: NewPath().AddCircle(30, 30, 15)},
			Paint{Color: Color{R: 1, A: 1}, AntiAlias: true})
		c.RestoreTo(depth)
	}

	full := image.NewRGBA(image.Rect(0, 0, w, h))
	cv := NewImageCanvas(full)
	cv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	paintScene(cv)

	// Same scene, but repainted one dirty band at a time — the frame loop's
	// dirty-region path.
	partial := image.NewRGBA(image.Rect(0, 0, w, h))
	pv := NewImageCanvas(partial)
	pv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
	for y := 0; y < h; y += 9 {
		band := Rect{X: 0, Y: float32(y), W: w, H: 9}
		d := pv.Save()
		pv.ClipRect(band)
		pv.Clear(Color{R: 1, G: 1, B: 1, A: 1})
		paintScene(pv)
		pv.RestoreTo(d)
	}

	worst, wx, wy := 0, 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a, b := full.RGBAAt(x, y), partial.RGBAAt(x, y)
			for _, d := range []int{
				int(a.R) - int(b.R), int(a.G) - int(b.G),
				int(a.B) - int(b.B), int(a.A) - int(b.A),
			} {
				if d < 0 {
					d = -d
				}
				if d > worst {
					worst, wx, wy = d, x, y
				}
			}
		}
	}
	// 1/255 of slack for band-boundary AA rounding; the bug produced
	// differences of ~80.
	if worst > 1 {
		t.Errorf("band-by-band repaint differs from a full repaint by %d at (%d,%d)", worst, wx, wy)
	}
}

// The composite of a filtered layer is confined to the clip in effect
// when the layer was pushed — the blur may grow the buffer, but not onto
// pixels the frame is not repainting.
func TestFilteredLayerCompositeStaysInsideClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 80))
	c := NewImageCanvas(img)
	c.Clear(Color{R: 1, G: 1, B: 1, A: 1})

	clip := Rect{X: 20, Y: 30, W: 40, H: 20}
	d := c.Save()
	c.ClipRect(clip)
	depth := c.SaveLayer(Rect{X: 5, Y: 5, W: 60, H: 60}, Paint{
		ImageFilter: DropShadowImageFilter{Blur: 5, Color: Color{A: 1}},
	})
	c.DrawShape(ShapePath{Path: NewPath().AddCircle(35, 40, 12)},
		Paint{Color: Color{A: 1}, AntiAlias: true})
	c.RestoreTo(depth)
	c.RestoreTo(d)

	for y := 0; y < 80; y++ {
		for x := 0; x < 80; x++ {
			inside := float32(x) >= clip.X && float32(x) < clip.X+clip.W &&
				float32(y) >= clip.Y && float32(y) < clip.Y+clip.H
			if inside {
				continue
			}
			if px := img.RGBAAt(x, y); px != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
				t.Fatalf("pixel (%d,%d) outside the clip was painted: %v", x, y, px)
			}
		}
	}
}
