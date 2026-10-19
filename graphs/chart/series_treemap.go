package chart

import (
	"sort"

	. "github.com/qizhanchan/qui"
)

// TreemapNode is one hierarchy node. Parent values are the sum of their
// children when Value is omitted.
type TreemapNode struct {
	Name     string
	Value    float32
	Children []TreemapNode
}

// TreemapSeries renders a hierarchy as area-proportional rectangles
// using a squarified tiling. Default geometry mirrors ECharts' 20px
// horizontal and 50px vertical inset.
type TreemapSeries struct {
	seriesBase
	Roots      []TreemapNode
	Colors     []Color
	ShowLabels bool
	FontFamily string
}

func NewTreemapSeries(name string, roots []TreemapNode) *TreemapSeries {
	return &TreemapSeries{seriesBase: newSeriesBase(name), Roots: roots, ShowLabels: true}
}

func (s *TreemapSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func treemapNodeFromDataPoint(point DataPoint) TreemapNode {
	node := TreemapNode{Name: point.Name, Value: float32(point.Y)}
	for _, child := range point.Children {
		node.Children = append(node.Children, treemapNodeFromDataPoint(child))
	}
	if node.Value == 0 {
		for _, child := range node.Children {
			node.Value += treemapChildValue(child)
		}
	}
	return node
}

func treemapChildValue(node TreemapNode) float32 {
	if node.Value > 0 {
		return node.Value
	}
	var sum float32
	for _, child := range node.Children {
		sum += treemapChildValue(child)
	}
	return sum
}

type treemapCell struct {
	node  TreemapNode
	rect  Rect
	depth int
	color Color
}

type treemapItem struct {
	node  TreemapNode
	area  float32
	color Color
	depth int
}

func (s *TreemapSeries) layout(bounds Rect) []treemapCell {
	area := Rect{X: bounds.X + 20, Y: bounds.Y + 50, W: bounds.W - 40, H: bounds.H - 100}
	if area.W <= 0 || area.H <= 0 {
		return nil
	}
	items := make([]treemapItem, 0, len(s.Roots))
	var sum float32
	for _, root := range s.Roots {
		sum += treemapChildValue(root)
	}
	rectArea := area.W * area.H
	for i, root := range s.Roots {
		value := treemapChildValue(root)
		if value <= 0 || sum <= 0 {
			continue
		}
		color := s.color
		if i < len(s.Colors) && !isZeroColor(s.Colors[i]) {
			color = s.Colors[i]
		}
		items = append(items, treemapItem{node: root, area: value / sum * rectArea, color: color})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].area > items[j].area })
	var cells []treemapCell
	s.squarify(items, area, 0, &cells)
	return cells
}

func (s *TreemapSeries) squarify(items []treemapItem, rect Rect, depth int, out *[]treemapCell) {
	if len(items) == 0 || rect.W <= 0 || rect.H <= 0 {
		return
	}
	var total float32
	for _, item := range items {
		total += item.area
	}
	if total <= 0 {
		return
	}
	short := minF32(rect.W, rect.H)
	row := make([]treemapItem, 0, len(items))
	var rowArea float32
	best := float32(1e30)
	i := 0
	for i < len(items) {
		candidate := append(row, items[i])
		candidateArea := rowArea + items[i].area
		score := worstAspect(candidate, candidateArea, short, total, rect)
		if len(row) == 0 || score <= best {
			row = candidate
			rowArea = candidateArea
			best = score
			i++
			continue
		}
		rect = s.placeRow(row, rowArea, total, rect, depth, out)
		short = minF32(rect.W, rect.H)
		row = row[:0]
		rowArea = 0
		best = 1e30
	}
	if len(row) > 0 {
		s.placeRow(row, rowArea, total, rect, depth, out)
	}
}

func worstAspect(row []treemapItem, rowArea, short, total float32, rect Rect) float32 {
	if rowArea <= 0 || short <= 0 {
		return 1e30
	}
	areaMin, areaMax := float32(1e30), float32(0)
	for _, item := range row {
		if item.area <= 0 {
			continue
		}
		areaMin = minF32(areaMin, item.area)
		areaMax = maxF32(areaMax, item.area)
	}
	if areaMin == 1e30 || areaMin <= 0 {
		return 1e30
	}
	// ECharts defaults squareRatio to the golden ratio, deliberately
	// allowing moderately elongated rectangles to reduce the number of
	// strips. This is the same row score used by its squarified layout.
	const ratio = float32(1.61803398875)
	squareArea := rowArea * rowArea
	f := short * short * ratio
	return maxF32(f*areaMax/squareArea, squareArea/(f*areaMin))
}

func (s *TreemapSeries) placeRow(row []treemapItem, rowArea, total float32, rect Rect, depth int, out *[]treemapCell) Rect {
	if rowArea <= 0 || rect.W <= 0 || rect.H <= 0 {
		return rect
	}
	horizontal := rect.W >= rect.H
	thickness := rowArea / minF32(rect.W, rect.H)
	if horizontal {
		if thickness > rect.W {
			thickness = rect.W
		}
		y := rect.Y
		for _, item := range row {
			h := item.area / rowArea * rect.H
			cell := Rect{X: rect.X, Y: y, W: thickness, H: h}
			s.emit(item, cell, depth, out)
			y += h
		}
		return Rect{X: rect.X + thickness, Y: rect.Y, W: rect.W - thickness, H: rect.H}
	}
	if thickness > rect.H {
		thickness = rect.H
	}
	x := rect.X
	for _, item := range row {
		w := item.area / rowArea * rect.W
		cell := Rect{X: x, Y: rect.Y, W: w, H: thickness}
		s.emit(item, cell, depth, out)
		x += w
	}
	return Rect{X: rect.X, Y: rect.Y + thickness, W: rect.W, H: rect.H - thickness}
}

func (s *TreemapSeries) emit(item treemapItem, rect Rect, depth int, out *[]treemapCell) {
	if len(item.node.Children) == 0 {
		*out = append(*out, treemapCell{node: item.node, rect: rect, depth: depth, color: item.color})
		return
	}
	children := make([]treemapItem, 0, len(item.node.Children))
	var sum float32
	for _, child := range item.node.Children {
		sum += treemapChildValue(child)
	}
	rectArea := rect.W * rect.H
	for i, child := range item.node.Children {
		value := treemapChildValue(child)
		if value <= 0 || sum <= 0 {
			continue
		}
		color := item.color
		if depth == 0 && i > 0 {
			// A subtle tint keeps nested branches distinguishable while
			// their common branch color remains obvious.
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, float32(i)*.04)
		}
		children = append(children, treemapItem{node: child, area: value / sum * rectArea, color: color, depth: depth + 1})
	}
	sort.SliceStable(children, func(i, j int) bool { return children[i].area > children[j].area })
	s.squarify(children, rect, depth+1, out)
}

func (s *TreemapSeries) Draw(canvas Canvas, bounds Rect, _, _ Axis, hot HotSpot) {
	cells := s.layout(bounds)
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	for i, cell := range cells {
		color := cell.color
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, .12)
		}
		canvas.FillRect(cell.rect, color)
		if s.ShowLabels && cell.rect.W > font.Size*2 && cell.rect.H > font.Size+8 {
			w, _ := TextMetrics(cell.node.Name, font)
			if w <= 0 {
				w = float32(len(cell.node.Name)) * font.Size * .55
			}
			textW := minF32(w, cell.rect.W-10)
			canvas.DrawText(cell.node.Name, Rect{X: cell.rect.X + (cell.rect.W-textW)/2, Y: cell.rect.Y + (cell.rect.H-font.Size)/2, W: textW, H: font.Size + 2}, Color{R: 1, G: 1, B: 1, A: 1}, font)
		}
	}
}

func (s *TreemapSeries) HitTest(p Point, bounds Rect, _, _ Axis) int {
	for i, cell := range s.layout(bounds) {
		if cell.rect.Contains(p) {
			return i
		}
	}
	return -1
}

func (s *TreemapSeries) LegendMarker(canvas Canvas, swatch Rect) {
	color := s.color
	if len(s.Colors) > 0 && !isZeroColor(s.Colors[0]) {
		color = s.Colors[0]
	}
	canvas.FillRoundedRect(swatch, 2, color)
}
