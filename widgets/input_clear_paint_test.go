package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

// The clear glyph must land in the trailing inset, which is OUTSIDE the
// field's text-content clip. It regressed exactly once — drawn after a
// deferred RestoreTo, so the content clip was still in force and the glyph
// was invisible on screen while every state check said it was showing.
func TestInputClearGlyphPaintsPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 30))
	cv := NewImageCanvas(img)
	in := NewInput("s")
	in.TrailingButton = InputTrailingClear
	in.SetText("hello")
	in.Layout(Rect{W: 200, H: 30})
	in.Draw(cv)

	r := in.trailingButtonRect()
	ink := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				ink++
			}
		}
	}
	t.Logf("rect=%+v ink=%d", r, ink)
	if ink == 0 {
		t.Error("no pixels painted in the trailing-button box")
	}
}
