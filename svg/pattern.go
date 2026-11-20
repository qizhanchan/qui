package svg

import (
	"bytes"
	"encoding/xml"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/srwiley/rasterx"
)

// paintPattern is a <pattern> paint server: its content, drawn once into a
// tile and repeated across the painted shape. GitHub draws its hero grid
// with one (`<pattern width="1" height="1" patternUnits="userSpaceOnUse">`
// holding a single grid-line path).
type paintPattern struct {
	x, y, w, h float32
	userSpace  bool // patternUnits="userSpaceOnUse" (default objectBoundingBox)
	transform  *Transform
	content    *Document // the pattern's children, in a w×h viewBox
}

// maxPatternTile bounds the tile raster per side (device pixels).
const maxPatternTile = 1024

type patternShader struct {
	tile         *image.RGBA
	inverse      rasterx.Matrix2D // device → pattern space
	x, y, w, h   float32          // tile rect in pattern space
	sx, sy       float32          // tile pixels per pattern unit
	opacity      float32
	tileW, tileH int
}

func (p patternShader) ColorAt(x, y float32) qui.Color {
	px, py := p.inverse.Transform(float64(x), float64(y))
	u := math.Mod(px-float64(p.x), float64(p.w))
	if u < 0 {
		u += float64(p.w)
	}
	v := math.Mod(py-float64(p.y), float64(p.h))
	if v < 0 {
		v += float64(p.h)
	}
	ix := min(int(u*float64(p.sx)), p.tileW-1)
	iy := min(int(v*float64(p.sy)), p.tileH-1)
	i := p.tile.PixOffset(ix, iy)
	a := p.tile.Pix[i+3]
	if a == 0 {
		return qui.Color{}
	}
	fa := float32(a) / 255
	return qui.Color{
		R: float32(p.tile.Pix[i]) / 255 / fa,
		G: float32(p.tile.Pix[i+1]) / 255 / fa,
		B: float32(p.tile.Pix[i+2]) / 255 / fa,
		A: fa * p.opacity,
	}
}

// shader rasterizes the tile at the device scale of m and returns a shader
// repeating it; nil when the pattern has no area.
func (p *paintPattern) shader(bounds ViewBox, m rasterx.Matrix2D, opacity float32) qui.Shader {
	x, y, w, h := p.x, p.y, p.w, p.h
	if !p.userSpace {
		x, y = bounds.X+x*bounds.W, bounds.Y+y*bounds.H
		w, h = w*bounds.W, h*bounds.H
	}
	if w <= 0 || h <= 0 || p.content == nil {
		return nil
	}
	full := applyElementMatrix(m, p.transform)
	scaleX := float32(math.Hypot(full.A, full.B))
	scaleY := float32(math.Hypot(full.C, full.D))
	tw := min(max(int(math.Ceil(float64(w*scaleX))), 1), maxPatternTile)
	th := min(max(int(math.Ceil(float64(h*scaleY))), 1), maxPatternTile)
	// The content keeps userSpaceOnUse units: a w×h viewBox at (0,0),
	// stretched to the tile (no aspect fitting).
	tile := rasterizeDocument(&Document{ViewBox: ViewBox{W: w, H: h}, Root: p.content.Root}, tw, th)
	return patternShader{
		tile: tile, inverse: full.Invert(),
		x: x, y: y, w: w, h: h,
		sx: float32(tw) / w, sy: float32(th) / h,
		opacity: opacity, tileW: tw, tileH: th,
	}
}

// collectPatterns reads every <pattern> of the document by id; its
// content is re-parsed as a document of its own.
func collectPatterns(data []byte) map[string]*paintPattern {
	if !bytes.Contains(data, []byte("<pattern")) {
		return nil
	}
	out := map[string]*paintPattern{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "pattern" {
			continue
		}
		p := &paintPattern{}
		id := ""
		for _, a := range start.Attr {
			v := strings.TrimSpace(a.Value)
			num := func() float32 {
				n, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 32)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return 0
				}
				if strings.HasSuffix(v, "%") {
					n /= 100
				}
				return float32(n)
			}
			switch a.Name.Local {
			case "id":
				id = v
			case "x":
				p.x = num()
			case "y":
				p.y = num()
			case "width":
				p.w = num()
			case "height":
				p.h = num()
			case "patternUnits":
				p.userSpace = v == "userSpaceOnUse"
			case "patternTransform":
				p.transform, _ = parseTransformAttr(v)
			}
		}
		begin := dec.InputOffset()
		end, depth := begin, 0
		for depth >= 0 {
			end = dec.InputOffset()
			t, err := dec.Token()
			if err != nil {
				break
			}
			switch t.(type) {
			case xml.StartElement:
				depth++
			case xml.EndElement:
				depth--
			}
		}
		if id == "" || end < begin {
			continue
		}
		var src bytes.Buffer
		src.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
		src.Write(data[begin:end])
		src.WriteString(`</svg>`)
		if doc, err := ParseBytes(src.Bytes()); err == nil {
			p.content = doc
			out[id] = p
		}
	}
	return out
}
