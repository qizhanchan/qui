package htmlcss

import (
	"math"

	"github.com/qizhanchan/qui"
)

// applyBackgroundImage loads the first URL layer, including SVG, and resolves
// its tile in the CSS positioning area rather than stretching the border box.
func (e *El) applyBackgroundImage(cs *ComputedStyle) {
	if cs.Gradient != nil || cs.BackgroundImageURL == "" {
		e.bgImg, e.bgImgSrc = nil, ""
		return
	}
	if e.bgImg == nil || e.bgImgSrc != cs.BackgroundImageURL {
		src := e.loadMaskSource(cs.BackgroundImageURL)
		w, h, ratio := src.intrinsic()
		e.bgImgRatio = ratio
		e.bgImgNatural = qui.Size{W: w, H: h}
		e.bgImg = src.raster
		if src.vector != nil {
			e.bgImg = rasterizeBackgroundSVG(src.vector)
		}
		e.bgImgSrc = cs.BackgroundImageURL
	}
	if e.bgImg == nil {
		return
	}
	// Background and mask images share size/position/repeat syntax. Reuse the
	// same resolver, with background's initial padding-box positioning area.
	props := map[string]string{}
	for _, p := range []string{"", "-image", "-size", "-position", "-repeat", "-origin"} {
		if v, ok := cs.raw["background"+p]; ok {
			props["mask"+p] = v
		}
	}
	// A non-parsed sentinel URL lets the shared parser resolve geometry;
	// loading has already happened using the original CSS URL.
	props["mask-image"] = "url(background)"
	spec, _ := parseMask(props, cs.FontSize)
	if spec.origin == "" {
		spec.origin = "padding-box"
	}
	border, padding, margin := boxEdges(cs)
	img, natural, ratio := e.bgImg, e.bgImgNatural, e.bgImgRatio
	e.Box.BackgroundShader = func(b qui.Rect) qui.Shader {
		area := referenceBox(b, spec.origin, border, padding, margin)
		w, h := spec.tileSize(area, natural.W, natural.H, ratio)
		tile := qui.Rect{X: area.X + spec.posX.resolve(area.W-w), Y: area.Y + spec.posY.resolve(area.H-h), W: w, H: h}
		return backgroundTileShader{shader: qui.ImageShader(img, tile, qui.ImageStretch), tile: tile, repeatX: spec.repeatX, repeatY: spec.repeatY}
	}
}

type backgroundTileShader struct {
	shader           qui.Shader
	tile             qui.Rect
	repeatX, repeatY bool
}

func (s backgroundTileShader) ColorAt(x, y float32) qui.Color {
	if s.tile.W <= 0 || s.tile.H <= 0 {
		return qui.Color{}
	}
	wrap := func(v, start, size float32, repeat bool) (float32, bool) {
		if repeat {
			return v - float32(math.Floor(float64((v-start)/size)))*size, true
		}
		return v, v >= start && v < start+size
	}
	var ok bool
	if x, ok = wrap(x, s.tile.X, s.tile.W, s.repeatX); !ok {
		return qui.Color{}
	}
	if y, ok = wrap(y, s.tile.Y, s.tile.H, s.repeatY); !ok {
		return qui.Color{}
	}
	return s.shader.ColorAt(x, y)
}
