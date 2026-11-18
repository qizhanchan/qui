package svg

import (
	"bytes"
	"encoding/xml"
	"github.com/qizhanchan/qui"
	"github.com/srwiley/rasterx"
	"io"
	"math"
	"strconv"
	"strings"
)

type paintGradient struct {
	radial    bool
	points    [4]float32
	stops     []qui.GradientStop
	userSpace bool
	transform *Transform
}

type gradientShader struct {
	gradient qui.Shader
	inverse  rasterx.Matrix2D
}

func (g gradientShader) ColorAt(x, y float32) qui.Color {
	px, py := g.inverse.Transform(float64(x), float64(y))
	return g.gradient.ColorAt(float32(px), float32(py))
}
func (g *paintGradient) shader(bounds ViewBox, m rasterx.Matrix2D, opacity float32) qui.Shader {
	local := rasterx.Identity
	if !g.userSpace {
		local = local.Translate(float64(bounds.X), float64(bounds.Y)).Scale(float64(bounds.W), float64(bounds.H))
	}
	local = applyElementMatrix(local, g.transform)
	stops := append([]qui.GradientStop(nil), g.stops...)
	for i := range stops {
		stops[i].Color.A *= opacity
	}
	var shader qui.Shader = qui.LinearGradient{Start: qui.Point{X: g.points[0], Y: g.points[1]}, End: qui.Point{X: g.points[2], Y: g.points[3]}, Stops: stops}
	if g.radial {
		shader = qui.RadialGradient{Center: qui.Point{X: g.points[0], Y: g.points[1]}, Radius: g.points[2], Stops: stops}
	}
	return gradientShader{gradient: shader, inverse: m.Mult(local).Invert()}
}

// Read paint servers separately so a reference may precede its definition.
func collectGradients(data []byte) map[string]*paintGradient {
	out := map[string]*paintGradient{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || (start.Name.Local != "linearGradient" && start.Name.Local != "radialGradient") {
			continue
		}
		g := &paintGradient{points: [4]float32{0, 0, 1, 0}}
		if start.Name.Local == "radialGradient" {
			g.radial = true
			g.points = [4]float32{.5, .5, .5, 0}
		}
		id := ""
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "id":
				id = a.Value
			case "gradientUnits":
				g.userSpace = a.Value == "userSpaceOnUse"
			case "gradientTransform":
				g.transform, _ = parseTransformAttr(a.Value)
			}
		}
		for _, a := range start.Attr {
			i := -1
			switch a.Name.Local {
			case "x1", "cx":
				i = 0
			case "y1", "cy":
				i = 1
			case "x2", "r":
				i = 2
			case "y2":
				i = 3
			}
			if i >= 0 {
				v := strings.TrimSpace(a.Value)
				n, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 32)
				if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && math.Abs(n) <= math.MaxFloat32 {
					if strings.HasSuffix(v, "%") {
						n /= 100
					}
					g.points[i] = float32(n)
				}
			}
		}
		for {
			tok, err = dec.Token()
			if err != nil {
				break
			}
			if end, ok := tok.(xml.EndElement); ok && end.Name.Local == start.Name.Local {
				break
			}
			s, ok := tok.(xml.StartElement)
			if !ok || s.Name.Local != "stop" {
				continue
			}
			attrs := map[string]string{}
			for _, a := range s.Attr {
				attrs[a.Name.Local] = a.Value
			}
			for _, decl := range strings.Split(attrs["style"], ";") {
				kv := strings.SplitN(decl, ":", 2)
				if len(kv) == 2 {
					attrs[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
				}
			}
			c := qui.Color{A: 1}
			if value, err := parseColor(attrs["stop-color"]); err == nil {
				c = value
			}
			if v, err := strconv.ParseFloat(attrs["stop-opacity"], 32); err == nil {
				c.A *= clampUnit(float32(v))
			}
			offset := float32(0)
			v := strings.TrimSpace(attrs["offset"])
			if n, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 32); err == nil {
				if strings.HasSuffix(v, "%") {
					n /= 100
				}
				offset = clampUnit(float32(n))
			}
			if len(g.stops) > 0 && offset < g.stops[len(g.stops)-1].Offset {
				offset = g.stops[len(g.stops)-1].Offset
			}
			g.stops = append(g.stops, qui.GradientStop{Offset: offset, Color: c})
		}
		if id != "" {
			out[id] = g
		}
		if err == io.EOF {
			break
		}
	}
	return out
}
func resolveGradientPaints(root *Group, gradients map[string]*paintGradient) {
	var walk func(Element)
	walk = func(e Element) {
		s := &e.base().Style
		for _, p := range []*Paint{&s.Fill, &s.Stroke} {
			if p.Kind == PaintGradient {
				p.gradient = gradients[p.reference]
				if p.gradient == nil {
					p.Kind = PaintNone
				}
			}
		}
		if g, ok := e.(*Group); ok {
			for _, c := range g.Children {
				walk(c)
			}
		}
	}
	walk(root)
}
