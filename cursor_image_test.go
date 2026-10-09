package qui

import (
	"image"
	"image/color"
	"testing"
)

func solidCursorImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestNewImageCursorValidation(t *testing.T) {
	if _, err := NewImageCursor(nil, 0, 0); err == nil {
		t.Fatal("nil image accepted")
	}
	if _, err := NewImageCursor(image.NewNRGBA(image.Rect(0, 0, 0, 0)), 0, 0); err == nil {
		t.Fatal("empty image accepted")
	}
	if _, err := NewImageCursorScaled(solidCursorImage(4, 4, color.NRGBA{A: 255}), 0, 0, 0); err == nil {
		t.Fatal("zero scale accepted")
	}
}

func TestImageCursorRegistryAndHotSpotClamp(t *testing.T) {
	a, err := NewImageCursor(solidCursorImage(16, 16, color.NRGBA{R: 255, A: 255}), 40, -3)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewImageCursorScaled(solidCursorImage(32, 32, color.NRGBA{A: 255}), 2, 8, 8)
	if a == b || a < imageCursorBase || !IsImageCursor(a) || !IsImageCursor(b) {
		t.Fatalf("ids %d %d not distinct registered image cursors", a, b)
	}
	ic := lookupImageCursor(a)
	if ic.hotX != 15 || ic.hotY != 0 {
		t.Fatalf("hot spot not clamped into the image: (%v,%v)", ic.hotX, ic.hotY)
	}
	if w, h := lookupImageCursor(b).pointSize(); w != 16 || h != 16 {
		t.Fatalf("2x cursor point size = %vx%v, want 16x16", w, h)
	}
	ReleaseCursor(a)
	ReleaseCursor(b)
	if IsImageCursor(a) || isSupportedCursorShape(a) {
		t.Fatal("released cursor still registered")
	}
	ReleaseCursor(a)          // double release is a no-op
	ReleaseCursor(CursorHand) // built-ins are never released
	if !isSupportedCursorShape(CursorHand) || !isSupportedCursorShape(CursorHidden) {
		t.Fatal("built-in shapes must stay supported")
	}
}

func TestImageCursorAt1xDownsample(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	// Left 2x2 block: one opaque red pixel, three transparent ones.
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	// Right 2x2 block: all opaque blue.
	for y := 0; y < 2; y++ {
		for x := 2; x < 4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{B: 200, A: 255})
		}
	}
	ic := &imageCursor{img: img, scale: 2}
	out := ic.at1x()
	if out.Rect.Dx() != 2 || out.Rect.Dy() != 1 {
		t.Fatalf("1x size = %v", out.Rect)
	}
	// Transparent neighbours must not darken the red (alpha-weighted).
	if got := out.NRGBAAt(0, 0); got.R != 255 || got.A != 63 {
		t.Fatalf("left = %+v, want pure red at quarter alpha", got)
	}
	if got := out.NRGBAAt(1, 0); got.B != 200 || got.A != 255 {
		t.Fatalf("right = %+v", got)
	}
	if one := (&imageCursor{img: img, scale: 1}).at1x(); one != img {
		t.Fatal("scale 1 must reuse the image")
	}
}

func TestWindowAppliesHiddenAndImageCursors(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	shape, err := NewImageCursor(solidCursorImage(8, 8, color.NRGBA{G: 255, A: 255}), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer ReleaseCursor(shape)

	w.SetCursor(CursorHidden)
	if f.cursor != CursorHidden {
		t.Fatalf("hidden not forwarded, platform has %v", f.cursor)
	}
	w.SetCursor(shape)
	if f.cursor != shape || w.Cursor() != shape {
		t.Fatalf("image cursor not forwarded, platform has %v", f.cursor)
	}
	// An id that was never registered degrades to the arrow.
	w.SetCursor(shape + 1000)
	if f.cursor != CursorDefault {
		t.Fatalf("unknown id should fall back to default, got %v", f.cursor)
	}
	// Dropping the cursor a window shows resets that window.
	w.SetCursor(shape)
	w.dropCursor(shape)
	if f.cursor != CursorDefault || w.Cursor() != CursorDefault {
		t.Fatalf("dropCursor left %v / %v", f.cursor, w.Cursor())
	}
}

func TestDeclaredImageCursorResolves(t *testing.T) {
	shape, _ := NewImageCursor(solidCursorImage(4, 4, color.NRGBA{A: 255}), 0, 0)
	defer ReleaseCursor(shape)
	w := NewTestWindow(Size{W: 200, H: 200})
	probe := &cursorProbe{BaseWidget: NewBaseWidget()}
	probe.SetSelf(probe)
	w.SetRoot(probe)
	probe.Layout(Rect{W: 100, H: 100})
	probe.SetCursorShape(shape)
	w.SetHoverPath([]Widget{probe})
	if got := w.resolveCursor(Point{X: 10, Y: 10}); got != shape {
		t.Fatalf("resolved %v, want the declared image cursor", got)
	}
	probe.SetCursorShape(CursorHidden)
	if got := w.resolveCursor(Point{X: 10, Y: 10}); got != CursorHidden {
		t.Fatalf("resolved %v, want hidden", got)
	}
}
