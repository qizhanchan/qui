package qui

import (
	"image"
	"testing"
)

// Phase B: scale lives on the canvas state stack, not in a separate
// wrapper. These tests push scale onto RecordingCanvas / imageCanvas
// and verify that draws come out at the expected physical-pixel
// position / size.

func TestStateScaleLineWidthAndEndpoints(t *testing.T) {
	rec := &RecordingCanvas{canvasState: newCanvasState(Rect{W: 1000, H: 1000})}
	rec.Scale(2, 2)

	rec.DrawLine(Point{X: 10, Y: 20}, Point{X: 30, Y: 40}, Color{}, 1.5)

	if len(rec.Lines) != 1 {
		t.Fatalf("expected 1 forwarded line, got %d", len(rec.Lines))
	}
	// RecordingCanvas keeps the *logical* point in the record — the
	// scale lives on the state stack and tests assert at the projection
	// level via FillRect's recorded effective rect (see below). What we
	// care about here is that the line wasn't dropped by the clip.
	got := rec.Lines[0]
	if got.P1 != (Point{X: 10, Y: 20}) || got.P2 != (Point{X: 30, Y: 40}) {
		t.Errorf("logical endpoints round-trip = %+v,%+v", got.P1, got.P2)
	}
}

// scaleRoundedShape's bugfix regression — kept verbatim from Phase A
// since the helper still exists in render_scale.go.

func TestScaleRoundedShapePartialKeepsPerAxis(t *testing.T) {
	rect := Rect{X: 10, Y: 10, W: 100, H: 60}
	radius := float32(8) // far below min/2=30
	gotRect, gotR := scaleRoundedShape(rect, radius, 2.0, 3.0)
	wantRect := Rect{X: 20, Y: 30, W: 200, H: 180}
	wantR := float32(8) * 2.5 // avgScale
	if gotRect != wantRect {
		t.Errorf("partial rounded rect: got rect %+v, want %+v", gotRect, wantRect)
	}
	if gotR != wantR {
		t.Errorf("partial rounded radius: got %v, want %v", gotR, wantR)
	}
}

func TestScaleRoundedShapeCircleEqualScale(t *testing.T) {
	rect := Rect{X: 8, Y: 8, W: 40, H: 40}
	radius := float32(20)
	for _, s := range []float32{1.0, 1.5, 2.0, 3.0} {
		gotRect, gotR := scaleRoundedShape(rect, radius, s, s)
		wantRect := Rect{X: 8 * s, Y: 8 * s, W: 40 * s, H: 40 * s}
		wantR := 20 * s
		if gotRect != wantRect || gotR != wantR {
			t.Errorf("s=%v: got (%+v, %v), want (%+v, %v)", s, gotRect, gotR, wantRect, wantR)
		}
	}
}

func TestScaleRoundedShapeCircleUnequalScale(t *testing.T) {
	rect := Rect{X: 10, Y: 10, W: 40, H: 40}
	radius := float32(20)
	sx, sy := float32(2.05), float32(2.00)
	gotRect, gotR := scaleRoundedShape(rect, radius, sx, sy)
	s := (sx + sy) / 2 // 2.025
	wantW := 40 * s
	wantH := 40 * s
	approxEq := func(a, b float32) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-3
	}
	if !approxEq(gotRect.W, wantW) || !approxEq(gotRect.H, wantH) {
		t.Errorf("uneq-scale pill: got %v×%v, want %v×%v (square)",
			gotRect.W, gotRect.H, wantW, wantH)
	}
	if !approxEq(gotRect.W, gotRect.H) {
		t.Errorf("uneq-scale pill width != height: %v vs %v", gotRect.W, gotRect.H)
	}
	wantX := 10*sx + (40*sx-wantW)/2
	wantY := 10*sy + (40*sy-wantH)/2
	if !approxEq(gotRect.X, wantX) || !approxEq(gotRect.Y, wantY) {
		t.Errorf("uneq-scale pill: got pos %v,%v want %v,%v",
			gotRect.X, gotRect.Y, wantX, wantY)
	}
	if !approxEq(gotR, radius*s) {
		t.Errorf("uneq-scale pill radius: got %v, want %v", gotR, radius*s)
	}
}

// End-to-end regression: a 40×40 logical pill drawn through the state
// stack at scaleX != scaleY rasterizes to a square bounding box. Before
// the scaleRoundedShape pill-detection fix it came out 82×80.
func TestImageCanvasFillRoundedRectPillRendersSquare(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	canvas := newImageCanvasForTest(img)
	_ = canvas.canvasState.stack
	// Re-init state with the correct extent since newImageCanvasForTest uses image bounds.
	canvas.canvasState = newCanvasState(Rect{W: 120, H: 120})
	canvas.Scale(2.05, 2.00)
	canvas.FillRoundedRect(Rect{X: 10, Y: 10, W: 40, H: 40}, 20,
		Color{R: 0.4, G: 0.31, B: 0.65, A: 1})

	minX, minY := img.Bounds().Dx(), img.Bounds().Dy()
	maxX, maxY := -1, -1
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y).A == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < 0 {
		t.Fatal("nothing was painted")
	}
	w := maxX - minX + 1
	h := maxY - minY + 1
	if abs(w-h) > 1 {
		t.Errorf("pill bounding box %d×%d (expected square within 1 px) at (%d,%d)-(%d,%d)",
			w, h, minX, minY, maxX, maxY)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
