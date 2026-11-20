package render

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// pictureScene draws one of everything the frontend can turn into backend
// calls: fills, rounded and stroked rects, a gradient path, strokes,
// lines, shaped and legacy text, scaled and rotated images, a vector, a
// shadow, an alpha layer, a blurred layer, a clip path and a backdrop
// filter.
func pictureScene(c gfx.Canvas) {
	c.Scale(1.5, 1.5)
	c.FillRect(gfx.Rect{X: 2, Y: 3, W: 200, H: 150}, gfx.Color{R: 0.95, G: 0.96, B: 0.97, A: 1})
	c.FillRoundedRect(gfx.Rect{X: 10.3, Y: 12.7, W: 80, H: 40}, 9, gfx.Color{R: 0.2, G: 0.5, B: 0.9, A: 0.8})
	c.StrokeRoundedRect(gfx.Rect{X: 100, Y: 10, W: 60, H: 30}, 6, gfx.Color{R: 1, A: 1}, 1.5)
	c.StrokeRect(gfx.Rect{X: 5, Y: 60, W: 30, H: 30}, gfx.Color{G: 0.6, A: 1}, 2)
	p := gfx.NewPath()
	p.MoveTo(40, 60)
	p.LineTo(90, 70)
	p.LineTo(60, 110)
	p.Close()
	c.DrawShape(gfx.ShapePath{Path: p}, gfx.Paint{Shader: gfx.LinearGradient{
		Start: gfx.Point{X: 40, Y: 60}, End: gfx.Point{X: 90, Y: 110},
		Stops: []gfx.GradientStop{{Offset: 0, Color: gfx.Color{R: 1, A: 1}}, {Offset: 1, Color: gfx.Color{B: 1, A: 1}}},
	}, AntiAlias: true})
	c.DrawShape(gfx.ShapePath{Path: p}, gfx.Paint{Style: gfx.PaintStroke, StrokeWidth: 2, Color: gfx.Color{A: 1}, AntiAlias: true})
	c.DrawLine(gfx.Point{X: 100, Y: 50}, gfx.Point{X: 180, Y: 90}, gfx.Color{R: 0.5, A: 1}, 1.2)
	c.DrawPolyline([]gfx.Point{{X: 100, Y: 100}, {X: 120, Y: 90}, {X: 140, Y: 110}}, gfx.Color{G: 0.4, B: 0.4, A: 1}, 1)
	c.DrawText("Picture 显示列表 ok", gfx.Rect{X: 8, Y: 118, W: 190, H: 20}, gfx.Color{A: 1}, gfx.Font{Size: 13})
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 8), 128, 255})
		}
	}
	c.DrawImage(img, gfx.Rect{X: 150, Y: 100, W: 50, H: 37})
	s := c.Save()
	c.Translate(170, 40)
	c.Rotate(0.3)
	c.DrawImage(img, gfx.Rect{X: -20, Y: -15, W: 40, H: 30})
	c.DrawText("rot", gfx.Rect{X: -10, Y: 16, W: 40, H: 14}, gfx.Color{R: 0.3, A: 1}, gfx.Font{Size: 11})
	c.RestoreTo(s)
	c.DrawShadow(gfx.Rect{X: 20, Y: 150, W: 60, H: 20}, 4, gfx.ElevationSpec{Y: 2, Blur: 6, Opacity: 0.4}, gfx.Color{A: 1})
	l := c.SaveLayer(gfx.Rect{X: 90, Y: 140, W: 60, H: 40}, gfx.Paint{Alpha: 0.5})
	c.FillRect(gfx.Rect{X: 95, Y: 145, W: 40, H: 25}, gfx.Color{R: 1, G: 0.5, A: 1})
	c.RestoreTo(l)
	l = c.SaveLayer(gfx.Rect{X: 150, Y: 140, W: 50, H: 40}, gfx.Paint{ImageFilter: gfx.BlurImageFilter{Radius: 3}})
	c.FillRect(gfx.Rect{X: 160, Y: 150, W: 25, H: 15}, gfx.Color{B: 0.7, A: 1})
	c.RestoreTo(l)
	s = c.Save()
	circ := gfx.NewPath()
	circ.AddCircle(40, 200, 18)
	c.ClipPath(circ)
	c.FillRect(gfx.Rect{X: 0, Y: 170, W: 100, H: 60}, gfx.Color{R: 0.1, G: 0.7, B: 0.3, A: 1})
	c.RestoreTo(s)
	if bf, ok := c.(interface {
		BackdropFilter(gfx.Rect, *gfx.Path, gfx.ImageFilter)
	}); ok {
		bf.BackdropFilter(gfx.Rect{X: 120, Y: 190, W: 60, H: 30}, nil, gfx.BlurImageFilter{Radius: 2})
	}
}

func TestPicturePlaybackMatchesDirectRaster(t *testing.T) {
	const w, h = 320, 360
	direct := image.NewRGBA(image.Rect(0, 0, w, h))
	pictureScene(NewImageCanvas(direct))

	c, rec := NewPictureCanvas(image.Rect(0, 0, w, h))
	pictureScene(c)
	pic := rec.Finish()
	if pic.Len() == 0 {
		t.Fatal("nothing recorded")
	}
	played := pic.Rasterize(image.Rect(0, 0, w, h))
	if !bytes.Equal(direct.Pix, played.Pix) {
		t.Fatalf("playback differs from direct raster in %d pixels", diffPixels(direct, played))
	}
	if !pic.WorkerSafe() {
		t.Fatal("legacy DrawText marks the picture UI-thread only; the scene should shape text instead")
	}
	// Played twice onto the same buffer the result is stable (no op
	// mutates the picture).
	again := pic.Rasterize(image.Rect(0, 0, w, h))
	if !bytes.Equal(played.Pix, again.Pix) {
		t.Fatal("second playback differs from the first")
	}
}

// A tile of the picture played at an offset equals the same rectangle of
// the whole raster, away from filtered layers (which read beyond a tile).
func TestPictureTilePlayback(t *testing.T) {
	const w, h = 320, 360
	c, rec := NewPictureCanvas(image.Rect(0, 0, w, h))
	pictureScene(c)
	pic := rec.Finish()
	full := pic.Rasterize(image.Rect(0, 0, w, h))
	for _, tile := range []image.Rectangle{
		image.Rect(0, 0, 128, 128),
		image.Rect(128, 0, 256, 128),
		image.Rect(64, 128, 192, 256),
		image.Rect(13, 77, 90, 150),
	} {
		got := pic.Rasterize(tile)
		want := full.SubImage(tile).(*image.RGBA)
		// Rotated images resample through an inverse computed from the
		// moved matrix: ±1 per channel, never more.
		if n := diffPixelsAtTol(want, got, tile.Min, 1); n > 0 {
			t.Errorf("tile %v differs from the full raster in %d pixels", tile, n)
		}
	}
}

func TestPictureRecordingCost(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	const w, h = 320, 360
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	direct := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			clear(img.Pix)
			pictureScene(NewImageCanvas(img))
		}
	})
	record := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c, rec := NewPictureCanvas(image.Rect(0, 0, w, h))
			pictureScene(c)
			rec.Finish()
		}
	})
	ratio := float64(record.NsPerOp()) / float64(direct.NsPerOp())
	t.Logf("direct %v/op, record %v/op (%.0f%%)", direct.NsPerOp(), record.NsPerOp(), 100*ratio)
	if ratio > 0.5 && !math.IsNaN(ratio) {
		t.Errorf("recording costs %.0f%% of rasterizing", 100*ratio)
	}
}

func diffPixels(a, b *image.RGBA) int { return diffPixelsAtTol(a, b, a.Rect.Min, 0) }

// diffPixelsAtTol counts pixels of a (any rectangle) that differ by more
// than tol in a channel from b (origin (0,0)) placed at at.
func diffPixelsAtTol(a, b *image.RGBA, at image.Point, tol int) int {
	n := 0
	r := a.Rect
	d := func(x, y uint8) bool { v := int(x) - int(y); return v > tol || v < -tol }
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p, q := a.RGBAAt(x, y), b.RGBAAt(x-at.X, y-at.Y)
			if d(p.R, q.R) || d(p.G, q.G) || d(p.B, q.B) || d(p.A, q.A) {
				n++
			}
		}
	}
	return n
}
