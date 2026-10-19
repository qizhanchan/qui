package chart

import (
	"sort"

	. "github.com/qizhanchan/qui"
)

const (
	FunnelSortDescending = "descending"
	FunnelSortAscending  = "ascending"
	FunnelSortNone       = "none"
)

// FunnelSeries renders descending values as adjacent trapezoids. The
// layout follows ECharts' default vertical funnel: a 80/60/80/65 inset,
// value-proportional widths, no gaps, data-item palette colors, white
// borders and outside labels connected by 20px leader lines.
type FunnelSeries struct {
	seriesBase
	Slices []PieSlice

	Min, Max *float64
	MinSize  float32 // fraction of the funnel width
	MaxSize  float32 // fraction of the funnel width
	Gap      float32
	Sort     string
	Orient   string // "vertical" (default) or "horizontal"
	Align    string // "center" (default), "left"/"right" or "top"/"bottom"

	Left, Top, Right, Bottom float32
	LabelPosition            string // "outer" (default), "inside" or "left"
	LabelLineLength          float32
	FontFamily               string
}

func NewFunnelSeries(name string, slices []PieSlice) *FunnelSeries {
	return &FunnelSeries{
		seriesBase:      newSeriesBase(name),
		Slices:          slices,
		MaxSize:         1,
		Sort:            FunnelSortDescending,
		Orient:          "vertical",
		Align:           "center",
		Left:            80,
		Top:             60,
		Right:           80,
		Bottom:          65,
		LabelPosition:   "outer",
		LabelLineLength: 20,
	}
}

func (s *FunnelSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

type funnelSegment struct {
	points [4]Point // top-left, top-right, bottom-right, bottom-left
	index  int
	value  float32
}

func (s *FunnelSeries) orderedIndices() []int {
	indices := make([]int, len(s.Slices))
	for i := range indices {
		indices[i] = i
	}
	switch s.Sort {
	case FunnelSortNone:
	case FunnelSortAscending:
		sort.SliceStable(indices, func(i, j int) bool {
			return s.Slices[indices[i]].Value < s.Slices[indices[j]].Value
		})
	default:
		sort.SliceStable(indices, func(i, j int) bool {
			return s.Slices[indices[i]].Value > s.Slices[indices[j]].Value
		})
	}
	return indices
}

func (s *FunnelSeries) layoutArea(plot Rect) Rect {
	left, top, right, bottom := s.Left, s.Top, s.Right, s.Bottom
	if left == 0 && top == 0 && right == 0 && bottom == 0 {
		left, top, right, bottom = 80, 60, 80, 65
	}
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if right < 0 {
		right = 0
	}
	if bottom < 0 {
		bottom = 0
	}
	area := Rect{
		X: plot.X + left,
		Y: plot.Y + top,
		W: plot.W - left - right,
		H: plot.H - top - bottom,
	}
	if area.W < 0 {
		area.W = 0
	}
	if area.H < 0 {
		area.H = 0
	}
	return area
}

func (s *FunnelSeries) valueExtent() (float32, float32) {
	if len(s.Slices) == 0 {
		return 0, 0
	}
	lo, hi := s.Slices[0].Value, s.Slices[0].Value
	for _, item := range s.Slices[1:] {
		if item.Value < lo {
			lo = item.Value
		}
		if item.Value > hi {
			hi = item.Value
		}
	}
	if lo > 0 {
		lo = 0
	}
	if s.Min != nil {
		lo = float32(*s.Min)
	}
	if s.Max != nil {
		hi = float32(*s.Max)
	}
	return lo, hi
}

func (s *FunnelSeries) sizeFor(value, min, max float32) float32 {
	minSize, maxSize := s.MinSize, s.MaxSize
	if minSize < 0 {
		minSize = 0
	}
	if maxSize <= 0 && minSize <= 0 {
		maxSize = 1
	}
	if max <= min {
		return (minSize + maxSize) / 2
	}
	ratio := clampf((value-min)/(max-min), 0, 1)
	return minSize + (maxSize-minSize)*ratio
}

func (s *FunnelSeries) segments(plot Rect) []funnelSegment {
	ordered := s.orderedIndices()
	if len(ordered) == 0 {
		return nil
	}
	area := s.layoutArea(plot)
	orient := s.Orient
	if orient == "" {
		orient = "vertical"
	}
	gap := s.Gap
	if gap < 0 {
		gap = 0
	}
	lo, hi := s.valueExtent()
	out := make([]funnelSegment, 0, len(ordered))

	if orient == "horizontal" {
		itemSize := (area.W - gap*float32(len(ordered)-1)) / float32(len(ordered))
		if itemSize <= 0 {
			return nil
		}
		edgeY := func(size float32) (float32, float32) {
			h := area.H * size
			switch s.Align {
			case "top":
				return area.Y, area.Y + h
			case "bottom":
				return area.Y + area.H - h, area.Y + area.H
			default:
				y := area.Y + (area.H-h)/2
				return y, y + h
			}
		}
		x := area.X
		for pos, idx := range ordered {
			nextSize := float32(0)
			if pos+1 < len(ordered) {
				nextSize = s.sizeFor(s.Slices[ordered[pos+1]].Value, lo, hi)
			}
			size := s.sizeFor(s.Slices[idx].Value, lo, hi)
			y0, y1 := edgeY(size)
			ny0, ny1 := edgeY(nextSize)
			out = append(out, funnelSegment{
				points: [4]Point{{X: x, Y: y0}, {X: x + itemSize, Y: ny0}, {X: x + itemSize, Y: ny1}, {X: x, Y: y1}},
				index:  idx,
				value:  s.Slices[idx].Value,
			})
			x += itemSize + gap
		}
		return out
	}

	itemSize := (area.H - gap*float32(len(ordered)-1)) / float32(len(ordered))
	if itemSize <= 0 {
		return nil
	}
	edgeX := func(size float32) (float32, float32) {
		w := area.W * size
		switch s.Align {
		case "left":
			return area.X, area.X + w
		case "right":
			return area.X + area.W - w, area.X + area.W
		default:
			x := area.X + (area.W-w)/2
			return x, x + w
		}
	}
	y := area.Y
	for pos, idx := range ordered {
		nextSize := float32(0)
		if pos+1 < len(ordered) {
			nextSize = s.sizeFor(s.Slices[ordered[pos+1]].Value, lo, hi)
		}
		size := s.sizeFor(s.Slices[idx].Value, lo, hi)
		x0, x1 := edgeX(size)
		nx0, nx1 := edgeX(nextSize)
		out = append(out, funnelSegment{
			points: [4]Point{{X: x0, Y: y}, {X: x1, Y: y}, {X: nx1, Y: y + itemSize}, {X: nx0, Y: y + itemSize}},
			index:  idx,
			value:  s.Slices[idx].Value,
		})
		y += itemSize + gap
	}
	return out
}

func (s *FunnelSeries) Draw(canvas Canvas, plot Rect, _, _ Axis, hot HotSpot) {
	segments := s.segments(plot)
	if len(segments) == 0 {
		return
	}
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	border := Color{R: 1, G: 1, B: 1, A: 1}
	textColor := Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
	for _, segment := range segments {
		item := s.Slices[segment.index]
		color := item.Color
		if isZeroColor(color) {
			color = s.color
		}
		if hot.PointIdx == segment.index && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, 0.12)
		}
		path := NewPath()
		for i, point := range segment.points {
			if i == 0 {
				path.MoveTo(point.X, point.Y)
			} else {
				path.LineTo(point.X, point.Y)
			}
		}
		path.Close()
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: border, Style: PaintStroke, StrokeWidth: 1, AntiAlias: true})
		s.drawLabel(canvas, segment, font, textColor, color)
	}
}

func (s *FunnelSeries) drawLabel(canvas Canvas, segment funnelSegment, font Font, textColor, lineColor Color) {
	label := s.Slices[segment.index].Label
	if label == "" {
		return
	}
	w, _ := TextMetrics(label, font)
	if w <= 0 {
		w = float32(len(label)) * font.Size * 0.55
	}
	position := s.LabelPosition
	if position == "" {
		position = "outer"
	}
	if position == "inside" || position == "center" {
		cx := (segment.points[0].X + segment.points[1].X + segment.points[2].X + segment.points[3].X) / 4
		cy := (segment.points[0].Y + segment.points[1].Y + segment.points[2].Y + segment.points[3].Y) / 4
		canvas.DrawText(label, Rect{X: cx - w/2, Y: cy - font.Size/2, W: w, H: font.Size + 2}, Color{R: 1, G: 1, B: 1, A: 1}, font)
		return
	}
	left := position == "left"
	var x1, y1 float32
	if left {
		x1 = (segment.points[3].X + segment.points[0].X) / 2
		y1 = (segment.points[3].Y + segment.points[0].Y) / 2
	} else {
		x1 = (segment.points[1].X + segment.points[2].X) / 2
		y1 = (segment.points[1].Y + segment.points[2].Y) / 2
	}
	length := s.LabelLineLength
	if length <= 0 {
		length = 20
	}
	x2 := x1 + length
	if left {
		x2 = x1 - length
	}
	canvas.DrawLine(Point{X: x1, Y: y1}, Point{X: x2, Y: y1}, lineColor, 1)
	textX := x2 + 5
	if left {
		textX = x2 - 5 - w
	}
	canvas.DrawText(label, Rect{X: textX, Y: y1 - font.Size/2, W: w, H: font.Size + 2}, textColor, font)
}

func (s *FunnelSeries) HitTest(p Point, plot Rect, _ Axis, _ Axis) int {
	for _, segment := range s.segments(plot) {
		if pointInQuad(p, segment.points) {
			return segment.index
		}
	}
	return -1
}

func pointInQuad(p Point, quad [4]Point) bool {
	inside := false
	j := len(quad) - 1
	for i := 0; i < len(quad); i++ {
		pi, pj := quad[i], quad[j]
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			x := (pj.X-pi.X)*(p.Y-pi.Y)/(pj.Y-pi.Y) + pi.X
			if p.X < x {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

func (s *FunnelSeries) LegendMarker(canvas Canvas, swatch Rect) {
	color := s.color
	if len(s.Slices) > 0 && !isZeroColor(s.Slices[0].Color) {
		color = s.Slices[0].Color
	}
	canvas.FillRoundedRect(swatch, 2, color)
}

func mixColor(a, b Color, t float32) Color {
	t = clampf(t, 0, 1)
	return Color{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A + (b.A-a.A)*t,
	}
}
