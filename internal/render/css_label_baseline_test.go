package render

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/typeset"
)

func TestCSSLabelBaselineMatchesInlineRun(t *testing.T) {
	for _, scale := range []float32{.8, 1.2, 2} {
		f := gfx.Font{Size: 96}
		origin := gfx.Rect{X: 10, Y: 10, W: 600, H: 260}
		plain := image.NewRGBA(image.Rect(0, 0, 640, 320))
		inline := image.NewRGBA(plain.Rect)
		reference := image.NewRGBA(plain.Rect)
		shaped, ok := typeset.ShapeSingleLine([]rune("Rust"), f, typeset.TextDirectionAuto)
		if !ok {
			t.Fatal("font did not shape")
		}
		above, _ := typeset.CSSInlineMetrics(f, scale)
		NewImageCanvasForTest(reference).DrawShapedLineAtBaseline(shaped, origin.X, origin.Y+above, gfx.Color{A: 1}, f)
		layout := typeset.BuildTextLayout("Rust", f, typeset.TextLayoutOptions{LineHeightScale: scale, CSSBaseline: true})
		typeset.DrawTextLayout(NewImageCanvasForTest(plain), layout, origin, gfx.Color{A: 1}, f)
		run := typeset.BuildInlineLayout([]typeset.InlineItem{{Text: "Rust", Font: f, Color: gfx.Color{A: 1}}}, typeset.InlineLayoutOptions{BaseFont: f, LineHeightScale: scale, RoundNormalMetrics: true, Strut: true})
		typeset.DrawInlineText(NewImageCanvasForTest(inline), run, origin)
		ink := func(img *image.RGBA) (top, bottom int) {
			top = img.Rect.Max.Y
			for y := 0; y < img.Rect.Max.Y; y++ {
				for x := 0; x < img.Rect.Max.X; x++ {
					if img.RGBAAt(x, y).A > 100 {
						top, bottom = min(top, y), max(bottom, y)
					}
				}
			}
			return
		}
		p0, p1 := ink(plain)
		i0, i1 := ink(inline)
		r0, r1 := ink(reference)
		if p0 < r0-1 || p0 > r0+1 || p1 < r1-1 || p1 > r1+1 || i0 < r0-1 || i0 > r0+1 || i1 < r1-1 || i1 > r1+1 {
			t.Fatalf("line height %v: label ink [%d,%d], inline ink [%d,%d], CSS baseline ink [%d,%d]", scale, p0, p1, i0, i1, r0, r1)
		}
	}
}
