package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

func paintTextArea(t *testing.T, ta *TextArea, w, h int) *image.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 255, 255, 255
	}
	ta.Layout(Rect{X: 20, Y: 20, W: float32(w - 40), H: float32(h - 40)})
	ta.Draw(NewImageCanvas(img))
	return img
}

// A textarea with only a bottom border paints that edge and leaves the
// others unpainted; the text inset follows the per-side widths.
func TestTextAreaPerSideBorders(t *testing.T) {
	ta := NewTextArea("")
	ta.BoxModel = true
	st := ta.Style()
	st.Background = Color{R: 1, G: 1, B: 1, A: 1}
	st.BorderWidths = Insets{Bottom: 4}
	st.BorderColors = SideColors{Bottom: Color{R: 1, A: 1}}
	st.Radius = 0
	img := paintTextArea(t, ta, 200, 100)
	if c := img.RGBAAt(100, 78); c.R < 200 || c.G > 60 {
		t.Errorf("bottom edge = %v, want red", c)
	}
	if c := img.RGBAAt(100, 20); c.R < 240 || c.G < 240 {
		t.Errorf("top edge = %v, want unpainted (white)", c)
	}
	if in := ta.insets(); in.Bottom < 4 || in.Top != max(htmlControlPadY, 0) {
		t.Errorf("insets = %+v, want bottom ≥ 4 and top = padding only", in)
	}
}

// Per-corner radii leave the square corners unpainted where rounded; a
// box-shadow paints outside the box.
func TestTextAreaCornersAndShadow(t *testing.T) {
	ta := NewTextArea("")
	ta.BoxModel = true
	st := ta.Style()
	st.Background = Color{B: 1, A: 1}
	st.BorderSize = 0
	st.Corners = CornerRadii{TL: 16}
	st.Shadow = ShadowStyle{Y: 6, Color: Color{A: 1}}
	img := paintTextArea(t, ta, 200, 100)
	if c := img.RGBAAt(21, 21); c.B > 200 && c.R < 50 {
		t.Errorf("rounded top-left corner painted %v, want background unpainted", c)
	}
	if c := img.RGBAAt(178, 21); c.B < 200 || c.R > 50 {
		t.Errorf("square top-right corner = %v, want blue fill", c)
	}
	if c := img.RGBAAt(100, 83); c.R > 80 {
		t.Errorf("below the box = %v, want the shadow", c)
	}
}
