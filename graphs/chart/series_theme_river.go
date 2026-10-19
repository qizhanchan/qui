package chart

import (
	"math"
	"sort"

	. "github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs"
)

// ThemeRiverPoint is one time/value observation in a named stream layer.
type ThemeRiverPoint struct {
	Time  float32
	Value float32
	Name  string
}

// ThemeRiverSeries renders a stacked streamgraph. It uses a symmetric
// baseline and clipped total-height normalization, following ECharts'
// themeRiver layout.
type ThemeRiverSeries struct {
	seriesBase
	Points     []ThemeRiverPoint
	Axis       AxisOption
	Colors     []Color
	FontFamily string
}

func NewThemeRiverSeries(name string, points []ThemeRiverPoint, axis AxisOption) *ThemeRiverSeries {
	return &ThemeRiverSeries{seriesBase: newSeriesBase(name), Points: points, Axis: axis}
}

func (s *ThemeRiverSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func (s *ThemeRiverSeries) layerNames() []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, point := range s.Points {
		if !seen[point.Name] {
			seen[point.Name] = true
			out = append(out, point.Name)
		}
	}
	return out
}

type themeRiverLayer struct {
	name   string
	points []ThemeRiverPoint
	color  Color
}

type themeRiverFrame struct {
	points   []Point // top boundary, ordered by layer stack from low to high
	bottom   []Point
	original []ThemeRiverPoint
	color    Color
}

func (s *ThemeRiverSeries) layout(bounds Rect) ([]themeRiverFrame, Rect, Axis) {
	if len(s.Points) == 0 {
		return nil, Rect{}, nil
	}
	area := Rect{X: bounds.X + bounds.W*.05, Y: bounds.Y + bounds.H*.05, W: bounds.W * .90, H: bounds.H * .90}
	axisOption := s.Axis
	if axisOption.Type == "" {
		axisOption.Type = AxisValue
	}
	axis := axisFromOption(axisOption, false)
	need := 5
	if axisOption.SplitNumber > 0 {
		need = axisOption.SplitNumber
	}
	lo, hi := s.Points[0].Time, s.Points[0].Time
	for _, point := range s.Points[1:] {
		lo = minF32(lo, point.Time)
		hi = maxF32(hi, point.Time)
	}
	if axisOption.Min != nil {
		lo = float32(*axisOption.Min)
	}
	if axisOption.Max != nil {
		hi = float32(*axisOption.Max)
	}
	if hi <= lo {
		hi = lo + 1
	}
	ticks := graphs.NiceLinearTicks(float64(lo), float64(hi), need)
	if len(ticks) > 1 {
		lo = float32(ticks[0].Value)
		hi = float32(ticks[len(ticks)-1].Value)
	}
	axis.SetRange(lo, hi)
	names := s.layerNames()
	layers := make([]themeRiverLayer, len(names))
	indexByName := make(map[string]int)
	for i, name := range names {
		indexByName[name] = i
		color := s.color
		if i < len(s.Colors) && !isZeroColor(s.Colors[i]) {
			color = s.Colors[i]
		}
		layers[i] = themeRiverLayer{name: name, color: color}
	}
	for _, point := range s.Points {
		index := indexByName[point.Name]
		layers[index].points = append(layers[index].points, point)
	}
	for i := range layers {
		sort.SliceStable(layers[i].points, func(a, b int) bool { return layers[i].points[a].Time < layers[i].points[b].Time })
	}
	frames := make([]themeRiverFrame, len(layers))
	for i := range layers {
		frames[i].color = layers[i].color
	}
	for _, layer := range layers {
		for _, point := range layer.points {
			frames[indexByName[layer.name]].original = append(frames[indexByName[layer.name]].original, point)
		}
	}
	// All layers are assumed to share timestamps. Build totals by time.
	timeIndex := make(map[float32]int)
	var times []float32
	for _, point := range s.Points {
		if _, ok := timeIndex[point.Time]; !ok {
			timeIndex[point.Time] = len(times)
			times = append(times, point.Time)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	sums := make([]float32, len(times))
	values := make([][]float32, len(layers))
	for i := range values {
		values[i] = make([]float32, len(times))
	}
	for _, point := range s.Points {
		t := timeIndex[point.Time]
		i := indexByName[point.Name]
		values[i][t] = point.Value
		sums[t] += point.Value
	}
	var maxSum float32
	for _, sum := range sums {
		maxSum = maxF32(maxSum, sum)
	}
	if maxSum <= 0 {
		return nil, area, axis
	}
	// ECharts reserves 10% top and bottom boundary gaps then recenters
	// each timestamp by (max-total)/2.
	streamHeight := area.H * .8
	scale := streamHeight / maxSum
	topY := area.Y + area.H*.1
	for layerIndex := range frames {
		frames[layerIndex].points = make([]Point, len(times))
		frames[layerIndex].bottom = make([]Point, len(times))
	}
	for ti, time := range times {
		stacked := float32(0)
		base := (maxSum - sums[ti]) / 2
		for li := range frames {
			value := values[li][ti]
			x := area.X + axis.ValueToPixel(time, area.W)
			top := topY + (base+stacked)*scale
			bottom := top + value*scale
			frames[li].points[ti] = Point{X: x, Y: top}
			frames[li].bottom[ti] = Point{X: x, Y: bottom}
			stacked += value
		}
	}
	return frames, area, axis
}

func (s *ThemeRiverSeries) Draw(canvas Canvas, bounds Rect, _, _ Axis, hot HotSpot) {
	frames, area, axis := s.layout(bounds)
	if len(frames) == 0 || axis == nil {
		return
	}
	for i, frame := range frames {
		color := frame.color
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, .12)
		}
		path := NewPath()
		for j, point := range frame.points {
			if j == 0 {
				path.MoveTo(point.X, point.Y)
			} else {
				path.LineTo(point.X, point.Y)
			}
		}
		for j := len(frame.bottom) - 1; j >= 0; j-- {
			path.LineTo(frame.bottom[j].X, frame.bottom[j].Y)
		}
		path.Close()
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
	}
	axisColor := Color{R: 0x6e / 255.0, G: 0x70 / 255.0, B: 0x79 / 255.0, A: 1}
	bottom := area.Y + area.H
	canvas.DrawLine(Point{X: area.X, Y: bottom}, Point{X: area.X + area.W, Y: bottom}, axisColor, 1)
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	for _, tick := range axis.TickPositions() {
		x := area.X + axis.ValueToPixel(tick.Value, area.W)
		canvas.DrawLine(Point{X: x, Y: bottom}, Point{X: x, Y: bottom + 5}, axisColor, 1)
		w, _ := TextMetrics(tick.Label, font)
		if w <= 0 {
			w = float32(len(tick.Label)) * font.Size * .55
		}
		canvas.DrawText(tick.Label, Rect{X: x - w/2, Y: bottom + 3, W: w, H: font.Size + 2}, axisColor, font)
	}
}

func (s *ThemeRiverSeries) HitTest(p Point, bounds Rect, _, _ Axis) int {
	frames, _, _ := s.layout(bounds)
	for i := len(frames) - 1; i >= 0; i-- {
		for j := 1; j < len(frames[i].points); j++ {
			quad := [4]Point{frames[i].points[j-1], frames[i].points[j], frames[i].bottom[j], frames[i].bottom[j-1]}
			if pointInQuad(p, quad) {
				return i
			}
		}
	}
	return -1
}

func (s *ThemeRiverSeries) LegendMarker(canvas Canvas, swatch Rect) {
	color := s.color
	if len(s.Colors) > 0 && !isZeroColor(s.Colors[0]) {
		color = s.Colors[0]
	}
	canvas.FillRoundedRect(swatch, 2, color)
}

func absTheme(v float32) float32 {
	return float32(math.Abs(float64(v)))
}
