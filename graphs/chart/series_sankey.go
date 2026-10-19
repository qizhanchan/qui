package chart

import (
	"math"
	"sort"
	"strconv"

	. "github.com/qizhanchan/qui"
)

// SankeyNode is one flow node. Value may be omitted; it is inferred from
// the larger of incoming and outgoing link totals.
type SankeyNode struct {
	Name  string
	Value float32
	Color Color
}

// SankeySeries renders a horizontal flow diagram. It implements
// ECharts-compatible node widths/gaps, longest-path depth assignment,
// column collision resolution, barycentric relaxation and cubic ribbon
// links.
type SankeySeries struct {
	seriesBase
	Nodes      []SankeyNode
	Links      []GraphLinkOption
	NodeWidth  float32
	NodeGap    float32
	ShowLabels bool
	FontFamily string
	LineColor  Color
}

func NewSankeySeries(name string, nodes []SankeyNode, links []GraphLinkOption) *SankeySeries {
	return &SankeySeries{
		seriesBase: newSeriesBase(name),
		Nodes:      nodes,
		Links:      links,
		NodeWidth:  20,
		NodeGap:    8,
		ShowLabels: true,
		LineColor:  Color{R: 0x86 / 255.0, G: 0x87 / 255.0, B: 0x8c / 255.0, A: .2},
	}
}

func (s *SankeySeries) DataBounds() SeriesBounds { return SeriesBounds{} }

type sankeyLink struct {
	source, target int
	value          float32
}

type sankeyLayoutNode struct {
	x, y, width, height float32
	value               float32
	depth               int
}

func (s *SankeySeries) endpointIndex(endpoint GraphEndpoint) int {
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

func (s *SankeySeries) nodeValues() []float32 {
	values := make([]float32, len(s.Nodes))
	incoming := make([]float32, len(s.Nodes))
	outgoing := make([]float32, len(s.Nodes))
	for _, node := range s.Nodes {
		_ = node
	}
	for _, link := range s.resolvedLinks() {
		outgoing[link.source] += link.value
		incoming[link.target] += link.value
	}
	for i, node := range s.Nodes {
		values[i] = maxF32(node.Value, maxF32(incoming[i], outgoing[i]))
	}
	return values
}

func (s *SankeySeries) resolvedLinks() []sankeyLink {
	out := make([]sankeyLink, 0, len(s.Links))
	for _, raw := range s.Links {
		a, b := s.endpointIndex(raw.Source), s.endpointIndex(raw.Target)
		if a < 0 || b < 0 || a == b {
			continue
		}
		value := float32(raw.Value)
		if value <= 0 {
			continue
		}
		out = append(out, sankeyLink{source: a, target: b, value: value})
	}
	return out
}

func (s *SankeySeries) layout(bounds Rect) ([]sankeyLayoutNode, []sankeyLink) {
	if len(s.Nodes) == 0 {
		return nil, nil
	}
	links := s.resolvedLinks()
	if len(links) == 0 {
		return nil, nil
	}
	area := Rect{X: bounds.X + bounds.W*.05, Y: bounds.Y + bounds.H*.05, W: bounds.W * .75, H: bounds.H * .90}
	nodeWidth := s.NodeWidth
	if nodeWidth <= 0 {
		nodeWidth = 20
	}
	nodeGap := s.NodeGap
	if nodeGap < 0 {
		nodeGap = 0
	}
	depth := make([]int, len(s.Nodes))
	// Longest-path layering. Repeating n times also tolerates cycles.
	for iter := 0; iter < len(s.Nodes); iter++ {
		changed := false
		for _, link := range links {
			if depth[link.target] < depth[link.source]+1 {
				depth[link.target] = depth[link.source] + 1
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	maxDepth := 0
	for _, d := range depth {
		if d > maxDepth {
			maxDepth = d
		}
	}
	hasOutgoing := make([]bool, len(s.Nodes))
	for _, link := range links {
		hasOutgoing[link.source] = true
	}
	for i := range depth {
		if !hasOutgoing[i] {
			depth[i] = maxDepth // nodeAlign: justify
		}
	}
	values := make([]float32, len(s.Nodes))
	for i, node := range s.Nodes {
		values[i] = node.Value
	}
	for _, link := range links {
		values[link.source] += 0
	}
	incoming := make([]float32, len(s.Nodes))
	outgoing := make([]float32, len(s.Nodes))
	for _, link := range links {
		outgoing[link.source] += link.value
		incoming[link.target] += link.value
	}
	for i := range values {
		values[i] = maxF32(values[i], maxF32(incoming[i], outgoing[i]))
		if values[i] <= 0 {
			values[i] = 1
		}
	}
	columns := make(map[int][]int)
	for i, d := range depth {
		columns[d] = append(columns[d], i)
	}
	minScale := float32(math.Inf(1))
	for _, indices := range columns {
		var sum float32
		for _, i := range indices {
			sum += values[i]
		}
		space := area.H - nodeGap*float32(len(indices)-1)
		scale := float32(0)
		if sum > 0 && space > 0 {
			scale = space / sum
		}
		if scale < minScale {
			minScale = scale
		}
	}
	if math.IsInf(float64(minScale), 1) || minScale <= 0 {
		minScale = 1
	}
	nodes := make([]sankeyLayoutNode, len(s.Nodes))
	for i, d := range depth {
		t := float32(0)
		if maxDepth > 0 {
			t = float32(d) / float32(maxDepth)
		}
		nodes[i] = sankeyLayoutNode{
			x: area.X + t*(area.W-nodeWidth), width: nodeWidth,
			height: values[i] * minScale, value: values[i], depth: d,
		}
	}
	// Initial stack and collision resolution per column.
	for _, indices := range columns {
		y := area.Y
		for _, i := range indices {
			nodes[i].y = y
			y += nodes[i].height + nodeGap
		}
	}
	// Barycentric relaxation. Link values weight the desired node center,
	// matching ECharts' weighted Gauss-Seidel passes.
	for iter := 0; iter < 32; iter++ {
		for d := maxDepth - 1; d >= 0; d-- {
			for _, i := range columns[d] {
				var sum, weight float32
				for _, link := range links {
					if link.source == i {
						sum += (nodes[link.target].y + nodes[link.target].height/2) * link.value
						weight += link.value
					}
				}
				if weight > 0 {
					nodes[i].y = sum/weight - nodes[i].height/2
				}
			}
			resolveSankeyCollisions(nodes, columns[d], nodeGap, area.Y, area.Y+area.H)
		}
		for d := 1; d <= maxDepth; d++ {
			for _, i := range columns[d] {
				var sum, weight float32
				for _, link := range links {
					if link.target == i {
						sum += (nodes[link.source].y + nodes[link.source].height/2) * link.value
						weight += link.value
					}
				}
				if weight > 0 {
					nodes[i].y = sum/weight - nodes[i].height/2
				}
			}
			resolveSankeyCollisions(nodes, columns[d], nodeGap, area.Y, area.Y+area.H)
		}
	}
	return nodes, links
}

func resolveSankeyCollisions(nodes []sankeyLayoutNode, indices []int, gap, top, bottom float32) {
	sort.SliceStable(indices, func(i, j int) bool { return nodes[indices[i]].y < nodes[indices[j]].y })
	y := top
	for _, i := range indices {
		if nodes[i].y < y {
			nodes[i].y = y
		}
		y = nodes[i].y + nodes[i].height + gap
	}
	overflow := y - gap - bottom
	if overflow > 0 {
		for i := len(indices) - 1; i >= 0; i-- {
			idx := indices[i]
			nodes[idx].y -= overflow
			if i > 0 && nodes[idx].y < nodes[indices[i-1]].y+nodes[indices[i-1]].height+gap {
				overflow = nodes[indices[i-1]].y + nodes[indices[i-1]].height + gap - nodes[idx].y
			} else {
				break
			}
		}
	}
}

func (s *SankeySeries) Draw(canvas Canvas, bounds Rect, _, _ Axis, hot HotSpot) {
	nodes, links := s.layout(bounds)
	if len(nodes) == 0 {
		return
	}
	// Compute incoming/outgoing vertical offsets.
	outOffset := make([]float32, len(nodes))
	inOffset := make([]float32, len(nodes))
	scale := float32(math.Inf(1))
	for i := range nodes {
		if nodes[i].value > 0 {
			ratio := nodes[i].height / nodes[i].value
			if ratio < scale {
				scale = ratio
			}
		}
	}
	if math.IsInf(float64(scale), 1) || scale <= 0 {
		scale = 1
	}
	for _, link := range links {
		source, target := nodes[link.source], nodes[link.target]
		thickness := link.value * scale
		sy := source.y + outOffset[link.source]
		ty := target.y + inOffset[link.target]
		outOffset[link.source] += thickness
		inOffset[link.target] += thickness
		sx := source.x + source.width
		tx := target.x
		path := NewPath()
		path.MoveTo(sx, sy)
		path.CubicTo((sx+tx)/2, sy, (sx+tx)/2, ty, tx, ty)
		path.LineTo(tx, ty+thickness)
		path.CubicTo((sx+tx)/2, ty+thickness, (sx+tx)/2, sy+thickness, sx, sy+thickness)
		path.Close()
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: s.LineColor, AntiAlias: true})
	}
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	labelColor := Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
	for i, node := range nodes {
		color := s.Nodes[i].Color
		if isZeroColor(color) {
			color = s.color
		}
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, .12)
		}
		canvas.FillRect(Rect{X: node.x, Y: node.y, W: node.width, H: node.height}, color)
		if s.ShowLabels {
			text := s.Nodes[i].Name
			w, _ := TextMetrics(text, font)
			if w <= 0 {
				w = float32(len(text)) * font.Size * .55
			}
			canvas.DrawText(text, Rect{X: node.x + node.width + 5, Y: node.y + node.height/2 - font.Size/2, W: w, H: font.Size + 2}, labelColor, font)
		}
	}
}

func (s *SankeySeries) HitTest(p Point, bounds Rect, _, _ Axis) int {
	nodes, _ := s.layout(bounds)
	for i := len(nodes) - 1; i >= 0; i-- {
		if (Rect{X: nodes[i].x, Y: nodes[i].y, W: nodes[i].width, H: nodes[i].height}).Contains(p) {
			return i
		}
	}
	return -1
}

func (s *SankeySeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 2, s.color)
}
