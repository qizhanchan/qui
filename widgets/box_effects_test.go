package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

func whiteCanvas(w, h int) (*image.RGBA, Canvas) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	return img, NewImageCanvas(img)
}

func TestBoxClipPathClipsPaintAndHits(t *testing.T) {
	box := NewBox(nil)
	box.Style().Background = Color{B: 1, A: 1}
	box.Layout(Rect{W: 40, H: 40})
	box.ClipPath = func(b Rect) *Path { return NewPath().AddOval(b) }
	if hit := box.HitTest(Point{X: 1, Y: 1}); hit != nil {
		t.Errorf("corner outside the clip hit %v", hit)
	}
	if hit := box.HitTest(Point{X: 20, Y: 20}); hit != box {
		t.Errorf("center hit %v, want box", hit)
	}
	img, c := whiteCanvas(40, 40)
	box.Draw(c)
	if p := img.RGBAAt(1, 1); p.B != 255 || p.R != 255 {
		t.Errorf("clipped corner = %v, want white", p)
	}
	if p := img.RGBAAt(20, 20); p.R != 0 || p.B != 255 {
		t.Errorf("center = %v, want blue", p)
	}
}

func TestBoxMaskKeepsCoverageOnly(t *testing.T) {
	box := NewBox(nil)
	box.Style().Background = Color{R: 1, A: 1}
	box.Layout(Rect{W: 40, H: 20})
	box.Mask = func(c Canvas, b Rect) { c.FillRect(Rect{X: b.X, Y: b.Y, W: b.W / 2, H: b.H}, Color{A: 1}) }
	img, c := whiteCanvas(40, 20)
	box.Draw(c)
	if p := img.RGBAAt(5, 10); p.G != 0 {
		t.Errorf("masked-in = %v, want red", p)
	}
	if p := img.RGBAAt(35, 10); p.G != 255 {
		t.Errorf("masked-out = %v, want white", p)
	}
}

func TestLabelTextShadowPaints(t *testing.T) {
	l := NewLabel("WWWW")
	l.Style().Foreground = Color{R: 1, G: 1, B: 1, A: 1}
	l.Style().Font.Size = 30
	l.TextShadows = []ShadowStyle{{X: 3, Y: 3, Color: Color{R: 1, A: 1}}}
	l.Layout(Rect{W: 120, H: 50})
	img, c := whiteCanvas(160, 60)
	l.Draw(c)
	red := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 200 && img.Pix[i+1] < 80 {
			red++
		}
	}
	if red == 0 {
		t.Error("text-shadow painted no red pixels")
	}
	if pb := l.PaintBounds(); pb.W <= 120 {
		t.Errorf("PaintBounds %v should cover the shadow offset", pb)
	}
}
