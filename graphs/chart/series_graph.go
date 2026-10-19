package chart

import (
	"math"
	"strconv"

	. "github.com/qizhanchan/qui"
)

// GraphNode is one graph/network node. Name is also the link endpoint
// key. X/Y are used by layout:"none"; circular layout derives positions
// from the chart bounds.
type GraphNode struct {
	Name        string
	X, Y, Value float32
	SymbolSize  float32
}

// GraphSeries renders a node-link network. Supported layouts are
// "none" (explicit X/Y), "circular" (default fallback) and "force"
// (currently rendered with a deterministic circular seed).
type GraphSeries struct {
	seriesBase
	Nodes      []GraphNode
	Links      []GraphLinkOption
	NodeColors []Color
	Layout     string
	SymbolSize float32
	ShowLabels bool
	LabelColor Color
	FontFamily string
	LineColor  Color
}

func NewGraphSeries(name string, nodes []GraphNode) *GraphSeries {
	return &GraphSeries{
		seriesBase: newSeriesBase(name),
		Nodes:      nodes,
		SymbolSize: 10,
		LineColor:  Color{R: 0x86 / 255.0, G: 0x87 / 255.0, B: 0x8c / 255.0, A: .5},
		LabelColor: Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1},
	}
}

func (s *GraphSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func (s *GraphSeries) nodeSize(node GraphNode) float32 {
	if node.SymbolSize > 0 {
		return node.SymbolSize
	}
	if s.SymbolSize > 0 {
		return s.SymbolSize
	}
	return 10
}

func (s *GraphSeries) positions(bounds Rect) []Point {
	if len(s.Nodes) == 0 {
		return nil
	}
	layout := s.Layout
	if layout == "" {
		layout = "circular"
	}
	if layout == "none" {
		minX, maxX := s.Nodes[0].X, s.Nodes[0].X
		minY, maxY := s.Nodes[0].Y, s.Nodes[0].Y
		for _, node := range s.Nodes[1:] {
			minX = minF32(minX, node.X)
			maxX = maxF32(maxX, node.X)
			minY = minF32(minY, node.Y)
			maxY = maxF32(maxY, node.Y)
		}
		if maxX == minX {
			maxX = minX + 1
		}
		if maxY == minY {
			maxY = minY + 1
		}
		// ECharts' view coordinate is centered with an 80% extent.
		view := Rect{X: bounds.X + bounds.W*.1, Y: bounds.Y + bounds.H*.1, W: bounds.W * .8, H: bounds.H * .8}
		points := make([]Point, len(s.Nodes))
		for i, node := range s.Nodes {
			points[i] = Point{
				X: view.X + (node.X-minX)/(maxX-minX)*view.W,
				Y: view.Y + (node.Y-minY)/(maxY-minY)*view.H,
			}
		}
		return points
	}

	// Graph view defaults to an 80% box centered in the chart.
	viewW, viewH := bounds.W*.8, bounds.H*.8
	cx, cy := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	radius := minF32(viewW, viewH) / 2
	points := make([]Point, len(s.Nodes))
	halfAngles := make([]float32, len(s.Nodes))
	var used float32
	for i, node := range s.Nodes {
		ratio := s.nodeSize(node) / 2 / radius
		if ratio > 1 {
			ratio = 1
		}
		halfAngles[i] = float32(math.Asin(float64(ratio)))
		used += halfAngles[i] * 2
	}
	halfRemain := (2*float32(math.Pi) - used) / float32(len(s.Nodes)) / 2
	angle := float32(0)
	for i := range s.Nodes {
		half := halfRemain + halfAngles[i]
		angle += half
		points[i] = Point{X: cx + radius*float32(math.Cos(float64(angle))), Y: cy + radius*float32(math.Sin(float64(angle)))}
		angle += half
	}
	return points
}

func (s *GraphSeries) endpointIndex(endpoint GraphEndpoint) int {
	key := string(endpoint)
	for i, node := range s.Nodes {
		if node.Name == key {
			return i
		}
	}
	if index, err := strconv.Atoi(key); err == nil && index >= 0 && index < len(s.Nodes) {
		return index
	}
	return -1
}

func (s *GraphSeries) Draw(canvas Canvas, plot Rect, _, _ Axis, hot HotSpot) {
	points := s.positions(plot)
	if len(points) == 0 {
		return
	}
	for _, link := range s.Links {
		a := s.endpointIndex(link.Source)
		b := s.endpointIndex(link.Target)
		if a < 0 || b < 0 {
			continue
		}
		canvas.DrawLine(points[a], points[b], s.LineColor, 1)
	}
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	for i, node := range s.Nodes {
		size := s.nodeSize(node)
		color := s.color
		if i < len(s.NodeColors) && !isZeroColor(s.NodeColors[i]) {
			color = s.NodeColors[i]
		}
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			size *= 1.12
		}
		canvas.FillRoundedRect(Rect{X: points[i].X - size/2, Y: points[i].Y - size/2, W: size, H: size}, size/2, color)
		if s.ShowLabels {
			w, _ := TextMetrics(node.Name, font)
			if w <= 0 {
				w = float32(len(node.Name)) * font.Size * .55
			}
			canvas.DrawText(node.Name, Rect{X: points[i].X - w/2, Y: points[i].Y + size/2 + 3, W: w, H: font.Size + 2}, s.LabelColor, font)
		}
	}
}

func (s *GraphSeries) HitTest(p Point, plot Rect, _, _ Axis) int {
	points := s.positions(plot)
	for i := len(points) - 1; i >= 0; i-- {
		size := s.nodeSize(s.Nodes[i])
		dx, dy := p.X-points[i].X, p.Y-points[i].Y
		if dx*dx+dy*dy <= size*size/4 {
			return i
		}
	}
	return -1
}

func (s *GraphSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, swatch.H/2, s.color)
}
