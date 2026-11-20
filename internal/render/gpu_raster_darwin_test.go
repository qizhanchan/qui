//go:build darwin && cgo

package render

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/wgpu"
)

// gpuScene is pictureScene without what the GPU path declines (backdrop
// filter, blurred layer): fills, rounded rects, a gradient path, strokes,
// lines, text, scaled and rotated images, a shadow, an alpha layer and a
// clip path.
func gpuScene(c gfx.Canvas) {
	c.Scale(1.5, 1.5)
	c.FillRect(gfx.Rect{X: 2, Y: 3, W: 200, H: 150}, gfx.Color{R: 0.95, G: 0.96, B: 0.97, A: 1})
	c.FillRoundedRect(gfx.Rect{X: 10.3, Y: 12.7, W: 80, H: 40}, 9, gfx.Color{R: 0.2, G: 0.5, B: 0.9, A: 0.8})
	c.StrokeRoundedRect(gfx.Rect{X: 100, Y: 10, W: 60, H: 30}, 6, gfx.Color{R: 1, A: 1}, 1.5)
	p := gfx.NewPath()
	p.MoveTo(40, 60)
	p.LineTo(90, 70)
	p.LineTo(60, 110)
	p.Close()
	c.DrawShape(gfx.ShapePath{Path: p}, gfx.Paint{Shader: gfx.LinearGradient{
		Start: gfx.Point{X: 40, Y: 60}, End: gfx.Point{X: 90, Y: 110},
		Stops: []gfx.GradientStop{{Offset: 0, Color: gfx.Color{R: 1, A: 1}}, {Offset: 1, Color: gfx.Color{B: 1, A: 1}}},
	}, AntiAlias: true})
	c.DrawLine(gfx.Point{X: 100, Y: 50}, gfx.Point{X: 180, Y: 90}, gfx.Color{R: 0.5, A: 1}, 1.2)
	c.DrawText("GPU raster 光栅化", gfx.Rect{X: 8, Y: 118, W: 190, H: 20}, gfx.Color{A: 1}, gfx.Font{Size: 13})
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
	c.RestoreTo(s)
	c.DrawShadow(gfx.Rect{X: 20, Y: 150, W: 60, H: 20}, 4, gfx.ElevationSpec{Y: 2, Blur: 6, Opacity: 0.4}, gfx.Color{A: 1})
	l := c.SaveLayer(gfx.Rect{X: 90, Y: 140, W: 60, H: 40}, gfx.Paint{Alpha: 0.5})
	c.FillRect(gfx.Rect{X: 95, Y: 145, W: 40, H: 25}, gfx.Color{R: 1, G: 0.5, A: 1})
	c.RestoreTo(l)
	s = c.Save()
	circ := gfx.NewPath()
	circ.AddCircle(40, 200, 18)
	c.ClipPath(circ)
	c.FillRect(gfx.Rect{X: 0, Y: 170, W: 100, H: 60}, gfx.Color{R: 0.1, G: 0.7, B: 0.3, A: 1})
	// A rotated photo inside a rounded clip: a marquee card.
	c.Translate(40, 200)
	c.Rotate(0.05)
	c.DrawImage(img, gfx.Rect{X: -30, Y: -20, W: 60, H: 40})
	c.RestoreTo(s)
}

func gpuRasterDevice(t *testing.T) *wgpu.Device {
	t.Helper()
	dev, err := wgpu.Shared()
	if err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	wgpu.SetErrorHandler(func(kind int, msg string) { t.Errorf("WebGPU error %d: %s", kind, msg) })
	t.Cleanup(func() { wgpu.SetErrorHandler(nil) })
	return dev
}

func gpuRasterRead(t *testing.T, dev *wgpu.Device, pic *Picture, r image.Rectangle) (*image.RGBA, bool) {
	t.Helper()
	dst := newGPUTex(dev, "test", r.Dx(), r.Dy(), wgpu.FormatRGBA8Unorm)
	defer dst.release()
	if !gpuRasterPicture(dev, pic, r, dst) {
		return nil, false
	}
	pix, err := dev.ReadTexture(dst.tex, 0, 0, r.Dx(), r.Dy())
	if err != nil {
		t.Fatal(err)
	}
	return &image.RGBA{Pix: pix, Stride: r.Dx() * 4, Rect: image.Rect(0, 0, r.Dx(), r.Dy())}, true
}

func TestGPURasterMatchesCPU(t *testing.T) {
	dev := gpuRasterDevice(t)
	const w, h = 320, 360
	c, rec := NewPictureCanvas(image.Rect(0, 0, w, h))
	gpuScene(c)
	pic := rec.Finish()
	cpu := pic.Rasterize(image.Rect(0, 0, w, h))
	for _, r := range []image.Rectangle{image.Rect(64, 96, 256, 288), image.Rect(0, 0, w, h)} {
		gpu, ok := gpuRasterRead(t, dev, pic, r)
		if !ok {
			t.Fatal("the GPU declined a picture it supports")
		}
		// Antialiased edges and bilinear sampling differ in rounding; a
		// wrong primitive differs over whole areas.
		bad, worst := 0, 0
		for y := 0; y < r.Dy(); y++ {
			for x := 0; x < r.Dx(); x++ {
				a, b := cpu.RGBAAt(x+r.Min.X, y+r.Min.Y), gpu.RGBAAt(x, y)
				d := max(absInt(int(a.R)-int(b.R)), absInt(int(a.G)-int(b.G)), absInt(int(a.B)-int(b.B)), absInt(int(a.A)-int(b.A)))
				worst = max(worst, d)
				if d > 24 {
					bad++
				}
			}
		}
		t.Logf("rect %v: %d pixels differ by more than 24, worst %d", r, bad, worst)
		if dir := os.Getenv("QUI_GPU_RASTER_DUMP"); dir != "" {
			savePNG(t, filepath.Join(dir, "cpu.png"), cpu)
			savePNG(t, filepath.Join(dir, "gpu.png"), gpu)
		}
		if frac := float64(bad) / float64(r.Dx()*r.Dy()); frac > 0.01 {
			t.Errorf("rect %v: %.2f%% of pixels differ by more than 24 (worst %d)", r, 100*frac, worst)
		}
	}
}

func TestGPURasterDeclinesBackdropFilter(t *testing.T) {
	dev := gpuRasterDevice(t)
	c, rec := NewPictureCanvas(image.Rect(0, 0, 100, 100))
	pictureScene(c) // has a backdrop filter and a blurred layer
	if _, ok := gpuRasterRead(t, dev, rec.Finish(), image.Rect(0, 0, 100, 100)); ok {
		t.Fatal("a backdrop filter must make the GPU decline the picture")
	}
}

func savePNG(t *testing.T, path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
