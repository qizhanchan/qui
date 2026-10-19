package chart

import (
	"math"
	"sort"

	. "github.com/qizhanchan/qui"
)

// SunburstSeries renders a hierarchy as concentric annular sectors.
// Values determine angular spans; depth determines ring radius.
type SunburstSeries struct {
	seriesBase
	Roots      []TreemapNode
	Colors     []Color
	Radius     float32 // fraction of half the shorter side; default .75
	ShowLabels bool
	FontFamily string
}

func NewSunburstSeries(name string, roots []TreemapNode) *SunburstSeries {
	return &SunburstSeries{seriesBase: newSeriesBase(name), Roots: roots, Radius: .75, ShowLabels: true}
}

func (s *SunburstSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

type sunburstCell struct {
	node         TreemapNode
	start, end   float32
	inner, outer float32
	depth        int
	color        Color
}

func (s *SunburstSeries) layout(bounds Rect) []sunburstCell {
	radiusRatio := s.Radius
	if radiusRatio <= 0 {
		radiusRatio = .75
	}
	cx, cy := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	radius := minF32(bounds.W, bounds.H) * .5 * radiusRatio
	maxDepth := 0
	var measure func(node TreemapNode, depth int)
	measure = func(node TreemapNode, depth int) {
		if depth > maxDepth {
			maxDepth = depth
		}
		for _, child := range node.Children {
			measure(child, depth+1)
		}
	}
	for _, root := range s.Roots {
		measure(root, 0)
	}
	ring := radius / float32(maxDepth+1)
	var cells []sunburstCell
	var walk func(node TreemapNode, start, end float32, depth int, color Color)
	walk = func(node TreemapNode, start, end float32, depth int, color Color) {
		inner := float32(depth) * ring
		outer := inner + ring
		cells = append(cells, sunburstCell{node: node, start: start, end: end, inner: inner, outer: outer, depth: depth, color: color})
		if len(node.Children) == 0 {
			return
		}
		children := append([]TreemapNode(nil), node.Children...)
		sort.SliceStable(children, func(i, j int) bool { return treemapChildValue(children[i]) > treemapChildValue(children[j]) })
		var sum float32
		for _, child := range children {
			sum += treemapChildValue(child)
		}
		if sum <= 0 {
			return
		}
		angle := start
		span := end - start
		for i, child := range children {
			childSpan := span * treemapChildValue(child) / sum
			childColor := mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, float32(depth+1)*.34+float32(i)*.01)
			walk(child, angle, angle+childSpan, depth+1, childColor)
			angle += childSpan
		}
	}
	var sum float32
	for _, root := range s.Roots {
		sum += treemapChildValue(root)
	}
	if sum <= 0 {
		return nil
	}
	angle := -float32(math.Pi) / 2
	for i, root := range s.Roots {
		span := 2 * float32(math.Pi) * treemapChildValue(root) / sum
		color := s.color
		if i < len(s.Colors) && !isZeroColor(s.Colors[i]) {
			color = s.Colors[i]
		}
		walk(root, angle, angle+span, 0, color)
		angle += span
	}
	_ = cx
	_ = cy
	return cells
}

func (s *SunburstSeries) Draw(canvas Canvas, bounds Rect, _, _ Axis, hot HotSpot) {
	cells := s.layout(bounds)
	if len(cells) == 0 {
		return
	}
	radiusRatio := s.Radius
	if radiusRatio <= 0 {
		radiusRatio = .75
	}
	cx, cy := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	maxDepth := 0
	for _, cell := range cells {
		if cell.depth > maxDepth {
			maxDepth = cell.depth
		}
	}
	ring := minF32(bounds.W, bounds.H) * .5 * radiusRatio / float32(maxDepth+1)
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	for i, cell := range cells {
		color := cell.color
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, .14)
		}
		drawSunburstSector(canvas, cx, cy, cell.inner, cell.outer, cell.start, cell.end, color)
		drawSunburstSectorStroke(canvas, cx, cy, cell.inner, cell.outer, cell.start, cell.end, Color{R: 1, G: 1, B: 1, A: 1}, 1)
		if s.ShowLabels {
			midAngle := (cell.start + cell.end) / 2
			labelRadius := (cell.inner + cell.outer) / 2
			lx := cx + labelRadius*float32(math.Cos(float64(midAngle)))
			ly := cy + labelRadius*float32(math.Sin(float64(midAngle)))
			text := cell.node.Name
			w, _ := TextMetrics(text, font)
			if w <= 0 {
				w = float32(len(text)) * font.Size * .55
			}
			if w < ring*1.6 {
				labelColor := Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
				if cell.depth == 0 {
					labelColor = Color{R: 1, G: 1, B: 1, A: 1}
				}
				canvas.DrawText(text, Rect{X: lx - w/2, Y: ly - font.Size/2, W: w, H: font.Size + 2}, labelColor, font)
			}
		}
	}
}

func drawSunburstSector(canvas Canvas, cx, cy, inner, outer, start, end float32, color Color) {
	if outer <= inner || end <= start {
		return
	}
	steps := int((end-start)*outer/2) + 1
	if steps < 8 {
		steps = 8
	}
	path := NewPath()
	for i := 0; i <= steps; i++ {
		a := start + (end-start)*float32(i)/float32(steps)
		x := cx + outer*float32(math.Cos(float64(a)))
		y := cy + outer*float32(math.Sin(float64(a)))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	for i := steps; i >= 0; i-- {
		a := start + (end-start)*float32(i)/float32(steps)
		path.LineTo(cx+inner*float32(math.Cos(float64(a))), cy+inner*float32(math.Sin(float64(a))))
	}
	path.Close()
	canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
}

func drawSunburstSectorStroke(canvas Canvas, cx, cy, inner, outer, start, end float32, color Color, width float32) {
	steps := int((end-start)*outer/2) + 1
	if steps < 8 {
		steps = 8
	}
	path := NewPath()
	for i := 0; i <= steps; i++ {
		a := start + (end-start)*float32(i)/float32(steps)
		x := cx + outer*float32(math.Cos(float64(a)))
		y := cy + outer*float32(math.Sin(float64(a)))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, Style: PaintStroke, StrokeWidth: width, AntiAlias: true})
	if inner > 0 {
		path = NewPath()
		for i := 0; i <= steps; i++ {
			a := start + (end-start)*float32(i)/float32(steps)
			x := cx + inner*float32(math.Cos(float64(a)))
			y := cy + inner*float32(math.Sin(float64(a)))
			if i == 0 {
				path.MoveTo(x, y)
			} else {
				path.LineTo(x, y)
			}
		}
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, Style: PaintStroke, StrokeWidth: width, AntiAlias: true})
	}
}

func (s *SunburstSeries) HitTest(p Point, bounds Rect, _, _ Axis) int {
	cells := s.layout(bounds)
	if len(cells) == 0 {
		return -1
	}
	cx, cy := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	dx, dy := p.X-cx, p.Y-cy
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	angle := float32(math.Atan2(float64(dy), float64(dx)))
	for i := len(cells) - 1; i >= 0; i-- {
		cell := cells[i]
		if dist >= cell.inner && dist <= cell.outer && angle >= normalizeRadians(cell.start) && angle <= normalizeRadians(cell.end) {
			return i
		}
		if cell.end-cell.start >= 2*float32(math.Pi)-.001 && dist >= cell.inner && dist <= cell.outer {
			return i
		}
	}
	return -1
}

func normalizeRadians(v float32) float32 {
	for v < -float32(math.Pi) {
		v += 2 * float32(math.Pi)
	}
	for v > float32(math.Pi) {
		v -= 2 * float32(math.Pi)
	}
	return v
}

func (s *SunburstSeries) LegendMarker(canvas Canvas, swatch Rect) {
	color := s.color
	if len(s.Colors) > 0 && !isZeroColor(s.Colors[0]) {
		color = s.Colors[0]
	}
	canvas.FillRoundedRect(swatch, 2, color)
}
