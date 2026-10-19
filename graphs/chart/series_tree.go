package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// TreeSeriesNode is one hierarchical node.
type TreeSeriesNode struct {
	Name     string
	Value    float32
	Children []TreeSeriesNode
}

// TreeSeries renders an orthogonal tree with ECharts defaults: a 12%
// inset, left-to-right depth, cubic curved edges, hollow steel-blue
// symbols and right-side labels.
type TreeSeries struct {
	seriesBase
	Roots      []TreeSeriesNode
	Layout     string // "orthogonal" (default) or "radial"
	Orient     string // LR (default), RL, TB, BT
	SymbolSize float32
	ShowLabels bool
	FontFamily string
	LineColor  Color
	NodeColor  Color
}

func NewTreeSeries(name string, roots []TreeSeriesNode) *TreeSeries {
	return &TreeSeries{
		seriesBase: newSeriesBase(name),
		Roots:      roots,
		Layout:     "orthogonal",
		Orient:     "LR",
		SymbolSize: 7,
		ShowLabels: true,
		LineColor:  Color{R: 0xcf / 255.0, G: 0xd2 / 255.0, B: 0xd7 / 255.0, A: 1},
		NodeColor:  Color{R: 0xb0 / 255.0, G: 0xc4 / 255.0, B: 0xde / 255.0, A: 1},
	}
}

func (s *TreeSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func treeNodeFromDataPoint(point DataPoint) TreeSeriesNode {
	node := TreeSeriesNode{Name: point.Name, Value: float32(point.Y)}
	for _, child := range point.Children {
		node.Children = append(node.Children, treeNodeFromDataPoint(child))
	}
	return node
}

type treePlacedNode struct {
	node     *TreeSeriesNode
	parent   int
	children []int
	depth    int
	point    Point
}

func (s *TreeSeries) flatten() ([]treePlacedNode, int) {
	var nodes []treePlacedNode
	maxDepth := 0
	var walk func(node *TreeSeriesNode, parent, depth int) int
	walk = func(node *TreeSeriesNode, parent, depth int) int {
		index := len(nodes)
		if depth > maxDepth {
			maxDepth = depth
		}
		nodes = append(nodes, treePlacedNode{node: node, parent: parent, depth: depth})
		for i := range node.Children {
			child := walk(&node.Children[i], index, depth+1)
			nodes[index].children = append(nodes[index].children, child)
		}
		return index
	}
	for i := range s.Roots {
		walk(&s.Roots[i], -1, 0)
	}
	return nodes, maxDepth
}

func (s *TreeSeries) layout(bounds Rect) []treePlacedNode {
	nodes, maxDepth := s.flatten()
	if len(nodes) == 0 {
		return nil
	}
	insetX, insetY := bounds.W*.12, bounds.H*.12
	area := Rect{X: bounds.X + insetX, Y: bounds.Y + insetY, W: bounds.W - 2*insetX, H: bounds.H - 2*insetY}
	orient := s.Orient
	if orient == "" {
		orient = "LR"
	}
	if s.Layout == "radial" {
		s.layoutRadial(nodes, maxDepth, area)
		return nodes
	}
	horizontal := orient == "LR" || orient == "RL"
	if horizontal {
		var leaf int
		var place func(index int)
		place = func(index int) {
			if len(nodes[index].children) == 0 {
				nodes[index].point.Y = area.Y + area.H*(float32(leaf)+.5)/float32(max(1, countLeaves(nodes)))
				leaf++
				return
			}
			var sum float32
			for _, child := range nodes[index].children {
				place(child)
				sum += nodes[child].point.Y
			}
			nodes[index].point.Y = sum / float32(len(nodes[index].children))
		}
		rootCount := len(s.Roots)
		for i := range nodes {
			if nodes[i].parent < 0 {
				place(i)
			}
		}
		for i := range nodes {
			t := float32(0)
			if maxDepth > 0 {
				t = float32(nodes[i].depth) / float32(maxDepth)
			}
			if orient == "RL" {
				t = 1 - t
			}
			nodes[i].point.X = area.X + t*area.W
		}
		_ = rootCount
		return nodes
	}
	var leaf int
	var place func(index int)
	place = func(index int) {
		if len(nodes[index].children) == 0 {
			nodes[index].point.X = area.X + area.W*(float32(leaf)+.5)/float32(max(1, countLeaves(nodes)))
			leaf++
			return
		}
		var sum float32
		for _, child := range nodes[index].children {
			place(child)
			sum += nodes[child].point.X
		}
		nodes[index].point.X = sum / float32(len(nodes[index].children))
	}
	for i := range nodes {
		if nodes[i].parent < 0 {
			place(i)
		}
	}
	for i := range nodes {
		t := float32(0)
		if maxDepth > 0 {
			t = float32(nodes[i].depth) / float32(maxDepth)
		}
		if orient == "BT" {
			t = 1 - t
		}
		nodes[i].point.Y = area.Y + t*area.H
	}
	return nodes
}

func countLeaves(nodes []treePlacedNode) int {
	n := 0
	for _, node := range nodes {
		if len(node.children) == 0 {
			n++
		}
	}
	return n
}

func (s *TreeSeries) layoutRadial(nodes []treePlacedNode, maxDepth int, area Rect) {
	cx, cy := area.X+area.W/2, area.Y+area.H/2
	radius := minF32(area.W, area.H) / 2
	var rootCount, leaf int
	for _, node := range nodes {
		if node.parent < 0 {
			rootCount++
		}
		if len(node.children) == 0 {
			leaf++
		}
	}
	rootIndex := 0
	var angleFor func(index int) float32
	angleFor = func(index int) float32 {
		if len(nodes[index].children) == 0 {
			angle := float32(leaf) * 2 * float32(math.Pi) / float32(max(1, leaf))
			leaf++
			return angle
		}
		var sum float32
		for _, child := range nodes[index].children {
			sum += angleFor(child)
		}
		return sum / float32(len(nodes[index].children))
	}
	for i := range nodes {
		if nodes[i].parent < 0 {
			angle := angleFor(i)
			nodes[i].point = Point{X: cx, Y: cy}
			_ = angle
			rootIndex++
		}
	}
	for i := range nodes {
		if nodes[i].parent < 0 {
			continue
		}
		t := float32(0)
		if maxDepth > 0 {
			t = float32(nodes[i].depth) / float32(maxDepth)
		}
		angle := float32(i) * 2 * float32(math.Pi) / float32(len(nodes))
		nodes[i].point = Point{X: cx + radius*t*float32(math.Cos(float64(angle))), Y: cy + radius*t*float32(math.Sin(float64(angle)))}
	}
}

func (s *TreeSeries) Draw(canvas Canvas, bounds Rect, _, _ Axis, hot HotSpot) {
	nodes := s.layout(bounds)
	if len(nodes) == 0 {
		return
	}
	size := s.SymbolSize
	if size <= 0 {
		size = 7
	}
	for i := range nodes {
		if nodes[i].parent < 0 {
			continue
		}
		parent := nodes[nodes[i].parent].point
		child := nodes[i].point
		path := NewPath().MoveTo(parent.X, parent.Y)
		if s.Layout == "radial" {
			mid := (parent.X + child.X) / 2
			path.CubicTo(mid, parent.Y, mid, child.Y, child.X, child.Y)
		} else if s.Orient == "TB" || s.Orient == "BT" {
			mid := (parent.Y + child.Y) / 2
			path.CubicTo(parent.X, mid, child.X, mid, child.X, child.Y)
		} else {
			mid := (parent.X + child.X) / 2
			path.CubicTo(mid, parent.Y, mid, child.Y, child.X, child.Y)
		}
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: s.LineColor, Style: PaintStroke, StrokeWidth: 1.5, AntiAlias: true})
	}
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	labelColor := Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
	for i := range nodes {
		point := nodes[i].point
		radius := size / 2
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			radius *= 1.15
		}
		canvas.FillRoundedRect(Rect{X: point.X - radius, Y: point.Y - radius, W: radius * 2, H: radius * 2}, radius, Color{R: 1, G: 1, B: 1, A: 1})
		canvas.StrokeRoundedRect(Rect{X: point.X - radius, Y: point.Y - radius, W: radius * 2, H: radius * 2}, radius, s.NodeColor, 1.5)
		if s.ShowLabels {
			label := nodes[i].node.Name
			w, _ := TextMetrics(label, font)
			if w <= 0 {
				w = float32(len(label)) * font.Size * .55
			}
			canvas.DrawText(label, Rect{X: point.X + radius + 5, Y: point.Y - font.Size/2, W: w, H: font.Size + 2}, labelColor, font)
		}
	}
}

func (s *TreeSeries) HitTest(p Point, bounds Rect, _, _ Axis) int {
	nodes := s.layout(bounds)
	size := s.SymbolSize
	if size <= 0 {
		size = 7
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		dx, dy := p.X-nodes[i].point.X, p.Y-nodes[i].point.Y
		if dx*dx+dy*dy <= size*size/4 {
			return i
		}
	}
	return -1
}

func (s *TreeSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, swatch.H/2, s.NodeColor)
}
