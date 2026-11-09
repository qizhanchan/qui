package render

import (
	"bytes"
	"image"
	"math/rand"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Deferred fills must leave exactly the pixels immediate painting does,
// whatever mix of replacing writes, blending draws and layers a frame
// holds.
func TestDeferredFillsMatchImmediatePainting(t *testing.T) {
	const w, h = 97, 71
	rng := rand.New(rand.NewSource(1))
	rect := func() image.Rectangle {
		x0, y0 := rng.Intn(w+10)-5, rng.Intn(h+10)-5
		return image.Rect(x0, y0, x0+rng.Intn(w), y0+rng.Intn(h))
	}
	frect := func() gfx.Rect {
		r := rect()
		return gfx.Rect{X: float32(r.Min.X) + 0.3, Y: float32(r.Min.Y) + 0.6, W: float32(r.Dx()), H: float32(r.Dy())}
	}
	col := func() gfx.Color {
		a := float32(1)
		if rng.Intn(3) == 0 {
			a = rng.Float32()
		}
		return gfx.Color{R: rng.Float32(), G: rng.Float32(), B: rng.Float32(), A: a}
	}
	for frame := 0; frame < 300; frame++ {
		type op func(b *CPUBackend)
		var ops []op
		depth := 0
		for i := 0; i < 2+rng.Intn(25); i++ {
			r, fr, fc, c, c2 := rect(), frect(), frect(), col(), col()
			cov := make([]float32, r.Dx()*r.Dy()+1)
			for j := range cov {
				cov[j] = rng.Float32()
			}
			rad := float32(rng.Intn(3) * 4)
			switch rng.Intn(12) {
			case 0, 1, 2:
				ops = append(ops, func(b *CPUBackend) { b.FillRect(r, c) })
			case 3:
				ops = append(ops, func(b *CPUBackend) { b.ClearRect(r) })
			case 4:
				ops = append(ops, func(b *CPUBackend) { b.Clear(c) })
			case 5:
				ops = append(ops, func(b *CPUBackend) { b.FillRoundedRect(fr, rad, fc, c) })
			case 6:
				ops = append(ops, func(b *CPUBackend) {
					b.blendCoverage(cov, r, image.Rect(0, 0, w, h), c2)
				})
			case 7:
				ops = append(ops, func(b *CPUBackend) {
					b.DrawLine(gfx.Point{X: fr.X, Y: fr.Y}, gfx.Point{X: fr.X + fr.W, Y: fr.Y + fr.H}, gfx.Rect{W: w, H: h}, c2, 2, 0)
				})
			case 9:
				ops = append(ops, func(b *CPUBackend) { b.StrokeRoundedRect(fr, rad, fc, c, 2) })
			case 10:
				tri := [][]gfx.Point{{{X: fr.X, Y: fr.Y}, {X: fr.X + fr.W, Y: fr.Y}, {X: fr.X, Y: fr.Y + fr.H}, {X: fr.X, Y: fr.Y}}}
				ops = append(ops, func(b *CPUBackend) { b.FillPath(tri, r, gfx.Paint{Color: c, AntiAlias: true}, gfx.IdentityMatrix()) })
			case 11:
				ops = append(ops, func(b *CPUBackend) { b.StrokeRect(fr, fc, c, 3) })
			case 8:
				if depth == 0 {
					depth++
					ops = append(ops, func(b *CPUBackend) { b.PushLayer(r, image.Rect(0, 0, w, h), gfx.Paint{}) })
				} else {
					depth--
					ops = append(ops, func(b *CPUBackend) { b.PopLayer(nil) })
				}
			}
		}
		for ; depth > 0; depth-- {
			ops = append(ops, func(b *CPUBackend) { b.PopLayer(nil) })
		}
		run := func(deferred bool) []byte {
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			for i := range img.Pix {
				img.Pix[i] = uint8(i * 7)
			}
			b := newCPUBackend(img)
			b.SetDeferFills(deferred)
			b.Begin(gfx.Size{W: w, H: h})
			for _, o := range ops {
				o(b)
			}
			b.End()
			return img.Pix
		}
		if want, got := run(false), run(true); !bytes.Equal(want, got) {
			t.Fatalf("frame %d (%d ops): deferred fills changed the pixels", frame, len(ops))
		}
	}
}

func TestDeferredFillsSkipCoveredPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	b := newCPUBackend(img)
	b.SetDeferFills(true)
	b.Begin(gfx.Size{W: 100, H: 100})
	red, blue := gfx.Color{R: 1, A: 1}, gfx.Color{B: 1, A: 1}
	b.FillRect(image.Rect(0, 0, 100, 100), red)
	b.FillRect(image.Rect(10, 10, 90, 90), blue)
	b.FillRect(image.Rect(0, 0, 100, 100), blue) // covers both
	if len(b.pending) != 1 || b.pending[0].r != image.Rect(0, 0, 100, 100) {
		t.Fatalf("pending = %v, want the last fill only", b.pending)
	}
	b.End()
	if img.Pix[0] != 0 || img.Pix[2] != 255 {
		t.Fatalf("pixel = %v", img.Pix[:4])
	}
}
