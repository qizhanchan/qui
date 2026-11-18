package htmlcss

import (
	"math"

	"github.com/qizhanchan/qui"
)

// applyBackgroundImage loads the first URL layer, including SVG, and resolves
// its tile in the CSS positioning area rather than stretching the border box.
func (e *El) applyBackgroundImage(cs *ComputedStyle) {
	if cs.Gradient != nil && cs.raw["background-size"] != "" {
		props := map[string]string{"mask-image": "url(background)"}
		for _, p := range []string{"-size", "-position", "-repeat", "-origin"} {
			if value, ok := cs.raw["background"+p]; ok {
				props["mask"+p] = value
			}
		}
		spec, _ := parseMask(props, cs.FontSize)
		if spec.origin == "" {
			spec.origin = "padding-box"
		}
		border, padding, margin := boxEdges(cs)
		e.Box.BackgroundShader = func(b qui.Rect) qui.Shader {
			area := referenceBox(b, spec.origin, border, padding, margin)
			w, h := spec.tileSize(area, 0, 0, 0)
			tile := qui.Rect{X: area.X + spec.posX.resolve(area.W-w), Y: area.Y + spec.posY.resolve(area.H-h), W: w, H: h}
			return backgroundTileShader{shader: cs.Gradient.shaderFor(tile), tile: tile, repeatX: spec.repeatX, repeatY: spec.repeatY}
		}
		return
	}
	if layers := backgroundGradients(cs); len(layers) > 0 {
		border, padding, margin := boxEdges(cs)
		e.Box.BackgroundShader = func(b qui.Rect) qui.Shader {
			out := gradientLayersShader{}
			for _, l := range layers {
				area := referenceBox(b, l.box, border, padding, margin)
				radius := max(cs.Radius-(area.X-b.X), 0)
				out.layers = append(out.layers, gradientLayerShader{l.gradient.shaderFor(area), area, radius})
			}
			return out
		}
		return
	}
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

// Each gradient layer has its own painting/positioning box. The first
// authored layer is composited on top, as with background-image.
type backgroundGradient struct {
	gradient *gradientSpec
	box      string
}

func backgroundGradients(cs *ComputedStyle) []backgroundGradient {
	v := cs.raw["background"]
	if v == "" {
		return nil
	}
	parts := splitTopLevel(v, ',')
	var out []backgroundGradient
	for _, part := range parts {
		fields := fieldsTopLevel(part)
		if len(fields) == 0 {
			continue
		}
		g, ok := parseGradient(fields[0])
		if !ok {
			return nil
		}
		box := "border-box"
		for _, f := range fields[1:] {
			if f == "border-box" || f == "padding-box" || f == "content-box" {
				box = f
			}
		}
		out = append(out, backgroundGradient{g, box})
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

type gradientLayerShader struct {
	shader qui.Shader
	area   qui.Rect
	radius float32
}
type gradientLayersShader struct{ layers []gradientLayerShader }

func (s gradientLayersShader) ColorAt(x, y float32) qui.Color {
	var out qui.Color
	for i := len(s.layers) - 1; i >= 0; i-- {
		l := s.layers[i]
		a := l.area
		if x < a.X || x >= a.X+a.W || y < a.Y || y >= a.Y+a.H {
			continue
		}
		r := min(l.radius, min(a.W, a.H)/2)
		cx := max(a.X+r, min(x, a.X+a.W-r))
		cy := max(a.Y+r, min(y, a.Y+a.H-r))
		if (x-cx)*(x-cx)+(y-cy)*(y-cy) > r*r {
			continue
		}
		c := l.shader.ColorAt(x, y)
		alpha := c.A + out.A*(1-c.A)
		if alpha > 0 {
			out = qui.Color{R: (c.R*c.A + out.R*out.A*(1-c.A)) / alpha, G: (c.G*c.A + out.G*out.A*(1-c.A)) / alpha, B: (c.B*c.A + out.B*out.A*(1-c.A)) / alpha, A: alpha}
		}
	}
	return out
}
