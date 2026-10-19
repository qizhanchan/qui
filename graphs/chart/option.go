package chart

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/qizhanchan/qui"
)

// AxisType is the declarative axis kind.
type AxisType string

const (
	AxisCategory AxisType = "category"
	AxisValue    AxisType = "value"
)

// SeriesType is the declarative series kind.
type SeriesType string

const (
	SeriesLine          SeriesType = "line"
	SeriesBar           SeriesType = "bar"
	SeriesArea          SeriesType = "area"
	SeriesScatter       SeriesType = "scatter"
	SeriesEffectScatter SeriesType = "effectScatter"
	SeriesPictorialBar  SeriesType = "pictorialBar"
	SeriesPie           SeriesType = "pie"
	SeriesCandlestick   SeriesType = "candlestick"
	SeriesRadar         SeriesType = "radar"
	SeriesFunnel        SeriesType = "funnel"
	SeriesGauge         SeriesType = "gauge"
	SeriesHeatmap       SeriesType = "heatmap"
	SeriesBoxplot       SeriesType = "boxplot"
	SeriesGraph         SeriesType = "graph"
	SeriesTree          SeriesType = "tree"
	SeriesTreemap       SeriesType = "treemap"
	SeriesSunburst      SeriesType = "sunburst"
	SeriesSankey        SeriesType = "sankey"
	SeriesParallel      SeriesType = "parallel"
	SeriesThemeRiver    SeriesType = "themeRiver"
)

// DataPoint is one declarative data item. For cartesian series X/Y are
// used; bars and pies primarily use Y. Name doubles as a category or pie
// slice label. Missing represents a null value in JSON input.
type DataPoint struct {
	X, Y     float64
	Name     string
	Values   []float64
	Missing  bool
	Color    CSSColor
	Children []DataPoint
}

// Number returns a Y-only data point, convenient for category bar/line data.
func Number(value float64) DataPoint { return DataPoint{Y: value} }

// XYPoint returns a cartesian data point.
func XYPoint(x, y float64) DataPoint { return DataPoint{X: x, Y: y} }

// NamedPoint returns a labelled data point, used by pie slices and
// category series that want labels carried with the data.
func NamedPoint(name string, value float64) DataPoint {
	return DataPoint{Y: value, Name: name}
}

// Numbers converts values into Y-only data points.
func Numbers(values ...float64) []DataPoint {
	out := make([]DataPoint, len(values))
	for i, value := range values {
		out[i] = Number(value)
	}
	return out
}

// UnmarshalJSON accepts ECharts-style scalar, [x,y], [name,value] and
// object data items.
func (p *DataPoint) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*p = DataPoint{Missing: true}
		return nil
	}
	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*p = DataPoint{Y: number}
		return nil
	}
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err == nil {
		if len(pair) < 2 {
			return fmt.Errorf("chart: invalid data point %s", data)
		}
		if name, ok := rawString(pair[0]); ok {
			value, ok := rawFloat(pair[1])
			if !ok {
				return fmt.Errorf("chart: invalid data point %s", data)
			}
			*p = DataPoint{Name: name, Y: value}
			return nil
		}
		x, okX := rawFloat(pair[0])
		y, okY := rawFloat(pair[1])
		if !okX || !okY {
			return fmt.Errorf("chart: invalid data point %s", data)
		}
		values := make([]float64, 0, len(pair))
		for _, raw := range pair {
			if value, ok := rawFloat(raw); ok {
				values = append(values, value)
			}
		}
		name := ""
		if len(pair) > 2 {
			name, _ = rawString(pair[2])
		}
		*p = DataPoint{X: x, Y: y, Values: values, Name: name}
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var out DataPoint
	if raw, ok := object["name"]; ok {
		out.Name, _ = rawString(raw)
	}
	if raw, ok := object["x"]; ok {
		out.X, _ = rawFloat(raw)
	}
	if raw, ok := object["y"]; ok {
		out.Y, _ = rawFloat(raw)
	} else if raw, ok := object["value"]; ok {
		if value, ok := rawFloat(raw); ok {
			out.Y = value
		} else {
			var values []float64
			if json.Unmarshal(raw, &values) == nil {
				out.Values = values
				if len(values) > 0 {
					out.Y = values[0]
				}
			}
		}
	}
	if raw, ok := object["missing"]; ok {
		_ = json.Unmarshal(raw, &out.Missing)
	}
	if raw, ok := object["children"]; ok {
		_ = json.Unmarshal(raw, &out.Children)
	}
	if raw, ok := object["itemStyle"]; ok {
		var itemStyle struct {
			Color CSSColor `json:"color,omitempty"`
		}
		if json.Unmarshal(raw, &itemStyle) == nil {
			out.Color = itemStyle.Color
		}
	}
	*p = out
	return nil
}

func rawFloat(raw json.RawMessage) (float64, bool) {
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func rawString(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// TitleOption configures the chart heading.
type TitleOption struct {
	Text    string  `json:"text,omitempty"`
	SubText string  `json:"subtext,omitempty"`
	Left    float32 `json:"left,omitempty"`
	Top     float32 `json:"top,omitempty"`
	Show    *bool   `json:"show,omitempty"`
}

// AxisOption configures one cartesian axis.
type AxisOption struct {
	Type         AxisType             `json:"type,omitempty"`
	Name         string               `json:"name,omitempty"`
	Data         []string             `json:"data,omitempty"`
	Min          *float64             `json:"min,omitempty"`
	Max          *float64             `json:"max,omitempty"`
	Inverse      *bool                `json:"inverse,omitempty"`
	Scale        *bool                `json:"scale,omitempty"`
	SplitNumber  int                  `json:"splitNumber,omitempty"`
	GroupPadding float32              `json:"groupPadding,omitempty"`
	Formatter    func(float64) string `json:"-"`
}

// LegendOption configures the chart legend.
type LegendOption struct {
	Show     *bool  `json:"show,omitempty"`
	Position string `json:"position,omitempty"` // bottom, top, left, right
}

// TooltipOption controls hover tooltips.
type TooltipOption struct {
	Show    *bool  `json:"show,omitempty"`
	Trigger string `json:"trigger,omitempty"` // item or axis

	// Formatter customizes item tooltips in Go.
	Formatter func(series SeriesOption, point DataPoint) string `json:"-"`
	// AxisFormatter customizes axis tooltips in Go.
	AxisFormatter func(index int, entries []TooltipEntry) string `json:"-"`
}

// TextStyleOption carries chart-wide typography inherited by labels.
type TextStyleOption struct {
	FontFamily string   `json:"fontFamily,omitempty"`
	Color      CSSColor `json:"color,omitempty"`
}

// VisualMapInRange configures the continuous color ramp used by chart
// types such as heatmap and line visual encodings.
type VisualMapInRange struct {
	Color []CSSColor `json:"color,omitempty"`
}

// VisualMapOption is the subset of ECharts visualMap needed for native
// data-to-color mapping. The legend itself is not drawn yet.
type VisualMapOption struct {
	Min     *float64          `json:"min,omitempty"`
	Max     *float64          `json:"max,omitempty"`
	Show    *bool             `json:"show,omitempty"`
	InRange *VisualMapInRange `json:"inRange,omitempty"`
}

// GridOption overrides plot-area gutters. Zero sides use theme defaults.
// RadarIndicator configures one radar dimension.
type RadarIndicator struct {
	Name string  `json:"name,omitempty"`
	Max  float64 `json:"max,omitempty"`
}

// RadarOption configures a radar coordinate system.
type RadarOption struct {
	Indicators []RadarIndicator `json:"indicator,omitempty"`
}

type GridOption struct {
	Left   float32 `json:"left,omitempty"`
	Right  float32 `json:"right,omitempty"`
	Top    float32 `json:"top,omitempty"`
	Bottom float32 `json:"bottom,omitempty"`
}

// LabelOption carries the common series-label switch used by graph,
// tree, pie and other non-cartesian series.
type LabelOption struct {
	Show     *bool    `json:"show,omitempty"`
	Position string   `json:"position,omitempty"`
	Color    CSSColor `json:"color,omitempty"`
	FontSize float64  `json:"fontSize,omitempty"`
}

// GraphEndpoint accepts either a node name or a numeric node index.
type GraphEndpoint string

func (e *GraphEndpoint) UnmarshalJSON(data []byte) error {
	if raw, ok := rawString(data); ok {
		*e = GraphEndpoint(raw)
		return nil
	}
	if raw, ok := rawFloat(data); ok {
		*e = GraphEndpoint(strconv.FormatFloat(raw, 'f', -1, 64))
		return nil
	}
	return fmt.Errorf("chart: invalid graph endpoint %s", data)
}

// GraphLinkOption is one edge in a graph/network series.
type GraphLinkOption struct {
	Source GraphEndpoint `json:"source"`
	Target GraphEndpoint `json:"target"`
	Value  float64       `json:"value,omitempty"`
}

// SeriesOption is one declarative data layer.
type SeriesOption struct {
	Type         SeriesType         `json:"type,omitempty"`
	Name         string             `json:"name,omitempty"`
	Data         []DataPoint        `json:"data,omitempty"`
	Color        CSSColor           `json:"color,omitempty"`
	Stack        string             `json:"stack,omitempty"`
	Smooth       bool               `json:"smooth,omitempty"`
	ShowSymbol   *bool              `json:"showSymbol,omitempty"`
	SymbolSize   float64            `json:"symbolSize,omitempty"`
	SymbolHeight float64            `json:"-"`
	Symbol       string             `json:"symbol,omitempty"`
	SymbolRepeat *bool              `json:"symbolRepeat,omitempty"`
	SymbolMargin float64            `json:"symbolMargin,omitempty"`
	SymbolClip   *bool              `json:"symbolClip,omitempty"`
	FillOpacity  float64            `json:"fillOpacity,omitempty"`
	BarGap       float64            `json:"barGap,omitempty"`
	Radius       string             `json:"radius,omitempty"`
	Opacity      *float64           `json:"opacity,omitempty"`
	Label        *LabelOption       `json:"label,omitempty"`
	Layout       string             `json:"layout,omitempty"`
	Links        []GraphLinkOption  `json:"links,omitempty"`
	Edges        []GraphLinkOption  `json:"edges,omitempty"`
	NodeWidth    float64            `json:"nodeWidth,omitempty"`
	NodeGap      float64            `json:"nodeGap,omitempty"`
	Sort         string             `json:"sort,omitempty"`
	Gap          float64            `json:"gap,omitempty"`
	MinSize      string             `json:"minSize,omitempty"`
	MaxSize      string             `json:"maxSize,omitempty"`
	Min          *float64           `json:"min,omitempty"`
	Max          *float64           `json:"max,omitempty"`
	StartAngle   float64            `json:"startAngle,omitempty"`
	EndAngle     float64            `json:"endAngle,omitempty"`
	Clockwise    *bool              `json:"clockwise,omitempty"`
	Candles      []CandlestickPoint `json:"-"`
}

// UnmarshalJSON accepts both scalar and [width,height] ECharts
// symbolSize values while retaining a simple scalar Go field.
func (s *SeriesOption) UnmarshalJSON(data []byte) error {
	type plain SeriesOption
	aux := struct {
		*plain
		RawSymbolSize json.RawMessage `json:"symbolSize"`
	}{plain: (*plain)(s)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.RawSymbolSize) == 0 || bytes.Equal(bytes.TrimSpace(aux.RawSymbolSize), []byte("null")) {
		return nil
	}
	var scalar float64
	if err := json.Unmarshal(aux.RawSymbolSize, &scalar); err == nil {
		s.SymbolSize = scalar
		s.SymbolHeight = scalar
		return nil
	}
	var pair []float64
	if err := json.Unmarshal(aux.RawSymbolSize, &pair); err != nil || len(pair) == 0 {
		return fmt.Errorf("chart: invalid symbolSize %s", aux.RawSymbolSize)
	}
	s.SymbolSize = pair[0]
	s.SymbolHeight = pair[0]
	if len(pair) > 1 {
		s.SymbolHeight = pair[1]
	}
	return nil
}

// Option is the declarative, ECharts-shaped chart description. It is a
// native Go value: no JavaScript runtime or document object model is used.
type Option struct {
	Title           *TitleOption     `json:"title,omitempty"`
	Tooltip         *TooltipOption   `json:"tooltip,omitempty"`
	TextStyle       *TextStyleOption `json:"textStyle,omitempty"`
	Legend          *LegendOption    `json:"legend,omitempty"`
	Grid            *GridOption      `json:"grid,omitempty"`
	Radar           *RadarOption     `json:"radar,omitempty"`
	VisualMap       *VisualMapOption `json:"visualMap,omitempty"`
	ParallelAxis    []AxisOption     `json:"parallelAxis,omitempty"`
	SingleAxis      AxisOption       `json:"singleAxis,omitempty"`
	XAxis           AxisOption       `json:"xAxis,omitempty"`
	YAxis           AxisOption       `json:"yAxis,omitempty"`
	Series          []SeriesOption   `json:"series,omitempty"`
	Palette         []CSSColor       `json:"color,omitempty"`
	BackgroundColor CSSColor         `json:"backgroundColor,omitempty"`

	EnableZoom   bool `json:"-"`
	EnablePan    bool `json:"-"`
	EnableSelect bool `json:"-"`
}

// New creates a chart from a declarative Option.
func New(opt Option) *Chart {
	c := NewChart()
	c.SetOption(opt)
	return c
}

// SetOption replaces the chart's declarative configuration. Existing
// event callbacks are retained. A future merge update API can add partial
// ECharts setOption semantics without changing this full-replacement call.
func (c *Chart) SetOption(opt Option) {
	if c == nil {
		return
	}
	c.lastOption = opt
	// ECharts-compatible title defaults for the declarative surface.
	c.TitleLeft = 10
	c.TitleTop = 10
	c.TitleFontSize = 18
	c.TitleFontWeight = qui.FontWeightBold
	c.TitleColor = qui.Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
	c.SubTitleColor = qui.Color{R: 0xaa / 255.0, G: 0xaa / 255.0, B: 0xaa / 255.0, A: 1}
	c.TextColor = qui.Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
	c.AxisColor = qui.Color{R: 0x6e / 255.0, G: 0x70 / 255.0, B: 0x79 / 255.0, A: 1}
	c.AxisLabelColor = c.AxisColor
	c.FontFamily = ""
	if opt.TextStyle != nil {
		c.FontFamily = opt.TextStyle.FontFamily
		if color, ok := ParseColor(string(opt.TextStyle.Color)); ok {
			c.TextColor = color
		}
	}
	if opt.Title != nil && boolDefault(opt.Title.Show, true) {
		c.Title = opt.Title.Text
		c.SubTitle = opt.Title.SubText
		if opt.Title.Left > 0 {
			c.TitleLeft = opt.Title.Left
		}
		if opt.Title.Top > 0 {
			c.TitleTop = opt.Title.Top
		}
	} else {
		c.Title = ""
		c.SubTitle = ""
	}

	c.ShowGrid = true
	c.GridColor = qui.Color{R: 0xe0 / 255.0, G: 0xe6 / 255.0, B: 0xf1 / 255.0, A: 1}
	c.fixedGutters = opt.Grid != nil
	if opt.Grid != nil {
		c.Gutters = &PlotGutters{
			Left: opt.Grid.Left, Right: opt.Grid.Right,
			Top: opt.Grid.Top, Bottom: opt.Grid.Bottom,
		}
	} else {
		c.Gutters = nil
	}

	c.Background = nil
	if color, ok := ParseColor(string(opt.BackgroundColor)); ok {
		c.Background = &color
	}

	c.EnableZoom = opt.EnableZoom
	c.EnablePan = opt.EnablePan
	c.EnableSelect = opt.EnableSelect

	if opt.Tooltip == nil {
		c.ShowTooltip = true
	} else {
		c.ShowTooltip = boolDefault(opt.Tooltip.Show, true)
	}

	c.autoPalette = true
	c.Palette = nil
	if len(opt.Palette) > 0 {
		for _, raw := range opt.Palette {
			if color, ok := ParseColor(string(raw)); ok {
				c.Palette = append(c.Palette, color)
			}
		}
		if len(c.Palette) > 0 {
			c.autoPalette = false
		}
	}

	hasCategorySeries := false
	for _, series := range opt.Series {
		switch series.effectiveType() {
		case SeriesBar, SeriesPictorialBar, SeriesCandlestick, SeriesHeatmap, SeriesBoxplot:
			hasCategorySeries = true
		}
	}
	xAxis := opt.XAxis
	if xAxis.Type == "" {
		if hasCategorySeries || len(xAxis.Data) > 0 {
			xAxis.Type = AxisCategory
		} else {
			xAxis.Type = AxisValue
		}
	}
	if xAxis.Type == AxisCategory && len(xAxis.Data) == 0 {
		xAxis.Data = deriveCategories(opt.Series)
	}
	c.XAxis = axisFromOption(xAxis, false)
	yAxis := opt.YAxis
	if yAxis.Type == "" {
		yAxis.Type = AxisValue
		for _, series := range opt.Series {
			if series.effectiveType() == SeriesHeatmap {
				yAxis.Type = AxisCategory
				break
			}
		}
	}
	c.YAxis = axisFromOption(yAxis, true)

	c.SetLegend(nil)
	if opt.Legend != nil && boolDefault(opt.Legend.Show, true) {
		c.SetLegend(NewLegend(parseLegendPosition(opt.Legend.Position)))
	}

	// Replace series only after axes and palette are configured so
	// color assignment and layout observe the new option immediately.
	c.Series = nil
	c.hoverSeries = -1
	c.hoverIndex = -1
	c.tooltip = tooltipState{}
	for _, seriesOpt := range opt.Series {
		if series := seriesFromOption(seriesOpt, opt); series != nil {
			if pie, ok := series.(*PieSeries); ok {
				pie.FontFamily = c.FontFamily
				pie.LabelColor = c.TextColor
				pie.LabelColorSet = true
			}
			if funnel, ok := series.(*FunnelSeries); ok {
				funnel.FontFamily = c.FontFamily
			}
			c.AddSeries(series)
		}
	}
	c.TooltipTrigger = "item"
	c.AxisTooltipBuilder = nil
	if opt.Tooltip != nil {
		if opt.Tooltip.Trigger != "" {
			c.TooltipTrigger = opt.Tooltip.Trigger
		}
		if opt.Tooltip.Formatter != nil {
			formatter := opt.Tooltip.Formatter
			c.TooltipBuilder = func(series Series, index int) string {
				for i, built := range c.Series {
					if built != series || i >= len(opt.Series) {
						continue
					}
					point := DataPoint{Y: 0}
					if index >= 0 && index < len(opt.Series[i].Data) {
						point = opt.Series[i].Data[index]
					} else if value, ok := seriesValueAt(series, index); ok {
						point.Y = float64(value)
					}
					return formatter(opt.Series[i], point)
				}
				return DefaultTooltipText(series, index)
			}
		} else {
			c.TooltipBuilder = nil
		}
		if c.TooltipTrigger == "axis" {
			if opt.Tooltip.AxisFormatter != nil {
				formatter := opt.Tooltip.AxisFormatter
				c.AxisTooltipBuilder = func(index int) string {
					entries := axisTooltipEntries(c, index)
					return formatter(index, entries)
				}
			} else {
				c.AxisTooltipBuilder = func(index int) string { return DefaultAxisTooltipText(c, index) }
			}
		} else {
			c.AxisTooltipBuilder = nil
		}
	} else {
		c.TooltipBuilder = nil
	}
	c.InvalidateLayout()
}

func (s SeriesOption) effectiveType() SeriesType {
	if s.Type == "" {
		return SeriesLine
	}
	return s.Type
}

func axisFromOption(opt AxisOption, yAxis bool) Axis {
	if opt.Type == AxisCategory {
		axis := &CategoryAxis{
			Categories:   append([]string(nil), opt.Data...),
			TitleText:    opt.Name,
			GroupPadding: opt.GroupPadding,
			InvertedAxis: yAxis,
		}
		if opt.Inverse != nil {
			axis.InvertedAxis = *opt.Inverse
		}
		return axis
	}
	tickCount := opt.SplitNumber
	if tickCount <= 0 {
		tickCount = 7
	}
	axis := &ValueAxis{
		TitleText:    opt.Name,
		TickCount:    tickCount,
		InvertedAxis: yAxis,
	}
	if opt.Inverse != nil {
		axis.InvertedAxis = *opt.Inverse
	}
	if opt.Scale != nil {
		axis.Scale = *opt.Scale
	}
	if opt.Min != nil {
		axis.Min = float32(*opt.Min)
		axis.PinMin = true
	}
	if opt.Max != nil {
		axis.Max = float32(*opt.Max)
		axis.PinMax = true
	}
	if opt.Formatter != nil {
		formatter := opt.Formatter
		axis.Format = func(value float32) string { return formatter(float64(value)) }
	}
	return axis
}

func seriesFromOption(opt SeriesOption, root Option) Series {
	radar := root.Radar
	visualMap := root.VisualMap
	fontFamily := ""
	if root.TextStyle != nil {
		fontFamily = root.TextStyle.FontFamily
	}
	name := opt.Name
	switch opt.effectiveType() {
	case SeriesLine:
		points := cartesianPoints(opt.Data, false)
		series := NewLineSeries(name, points)
		if opt.ShowSymbol != nil {
			series.ShowMarkers = *opt.ShowSymbol
		}
		series.Smooth = opt.Smooth
		if opt.SymbolSize > 0 {
			series.MarkerSize = float32(opt.SymbolSize)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesArea:
		points := cartesianPoints(opt.Data, false)
		series := NewAreaSeries(name, points)
		if opt.FillOpacity > 0 {
			series.FillAlpha = float32(opt.FillOpacity)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesBar:
		values := barValues(opt.Data)
		series := NewBarSeries(name, values)
		series.Stacked = opt.Stack != ""
		series.StackID = opt.Stack
		if opt.BarGap > 0 {
			series.BarGap = float32(opt.BarGap)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesPictorialBar:
		values := barValues(opt.Data)
		series := NewPictorialBarSeries(name, values)
		series.Symbol = opt.Symbol
		if opt.SymbolSize > 0 {
			series.SymbolWidth = float32(opt.SymbolSize)
		}
		if opt.SymbolHeight > 0 {
			series.SymbolHeight = float32(opt.SymbolHeight)
		}
		if opt.SymbolRepeat != nil {
			series.Repeat = *opt.SymbolRepeat
		}
		if opt.SymbolMargin > 0 {
			series.Margin = float32(opt.SymbolMargin)
		}
		if opt.SymbolClip != nil {
			series.Clip = *opt.SymbolClip
		}
		for _, point := range opt.Data {
			color, _ := ParseColor(string(point.Color))
			series.ItemColors = append(series.ItemColors, color)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesEffectScatter:
		points := cartesianPoints(opt.Data, true)
		series := NewEffectScatterSeries(name, points)
		if opt.SymbolSize > 0 {
			series.Size = float32(opt.SymbolSize)
		}
		if opt.Opacity != nil {
			series.Opacity = float32(*opt.Opacity)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesScatter:
		points := cartesianPoints(opt.Data, true)
		series := NewScatterSeries(name, points)
		series.Opacity = 0.8
		if opt.SymbolSize > 0 {
			series.Size = float32(opt.SymbolSize)
		}
		if opt.Opacity != nil {
			series.Opacity = float32(*opt.Opacity)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesCandlestick:
		candles := append([]CandlestickPoint(nil), opt.Candles...)
		if len(candles) == 0 {
			for _, point := range opt.Data {
				if len(point.Values) >= 4 {
					candles = append(candles, CandlestickPoint{Open: float32(point.Values[0]), Close: float32(point.Values[1]), Low: float32(point.Values[2]), High: float32(point.Values[3])})
				}
			}
		}
		series := NewCandlestickSeries(name, candles)
		applySeriesColor(series, opt.Color)
		return series
	case SeriesRadar:
		values := make([]float32, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing {
				continue
			}
			if len(point.Values) > 0 {
				for _, value := range point.Values {
					values = append(values, float32(value))
				}
			} else {
				values = append(values, float32(point.Y))
			}
		}
		var indicators []RadarIndicator
		if radar != nil {
			indicators = append(indicators, radar.Indicators...)
		}
		series := NewRadarSeries(name, indicators, values)
		if opt.FillOpacity > 0 {
			series.FillAlpha = float32(opt.FillOpacity)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesThemeRiver:
		points := make([]ThemeRiverPoint, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing || len(point.Values) < 2 || point.Name == "" {
				continue
			}
			points = append(points, ThemeRiverPoint{Time: float32(point.Values[0]), Value: float32(point.Values[1]), Name: point.Name})
		}
		series := NewThemeRiverSeries(name, points, root.SingleAxis)
		series.FontFamily = fontFamily
		applySeriesColor(series, opt.Color)
		return series
	case SeriesParallel:
		items := make([][]float32, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing {
				continue
			}
			values := point.Values
			if len(values) == 0 && point.Y != 0 {
				values = []float64{point.Y}
			}
			row := make([]float32, len(values))
			for i, value := range values {
				row[i] = float32(value)
			}
			items = append(items, row)
		}
		series := NewParallelSeries(name, items)
		series.FontFamily = fontFamily
		series.Axes = append([]AxisOption(nil), root.ParallelAxis...)
		series.Smooth = opt.Smooth
		applySeriesColor(series, opt.Color)
		return series
	case SeriesSankey:
		nodes := make([]SankeyNode, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing || point.Name == "" {
				continue
			}
			node := SankeyNode{Name: point.Name, Value: float32(point.Y)}
			if color, ok := ParseColor(string(point.Color)); ok {
				node.Color = color
			}
			nodes = append(nodes, node)
		}
		links := append([]GraphLinkOption(nil), opt.Links...)
		if len(links) == 0 {
			links = append(links, opt.Edges...)
		}
		series := NewSankeySeries(name, nodes, links)
		series.FontFamily = fontFamily
		if opt.NodeWidth > 0 {
			series.NodeWidth = float32(opt.NodeWidth)
		}
		if opt.NodeGap > 0 {
			series.NodeGap = float32(opt.NodeGap)
		}
		if opt.Label != nil && opt.Label.Show != nil {
			series.ShowLabels = *opt.Label.Show
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesSunburst:
		roots := make([]TreemapNode, 0, len(opt.Data))
		for _, point := range opt.Data {
			if !point.Missing {
				roots = append(roots, treemapNodeFromDataPoint(point))
			}
		}
		series := NewSunburstSeries(name, roots)
		series.FontFamily = fontFamily
		if opt.Label != nil && opt.Label.Show != nil {
			series.ShowLabels = *opt.Label.Show
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesTreemap:
		roots := make([]TreemapNode, 0, len(opt.Data))
		for _, point := range opt.Data {
			if !point.Missing {
				roots = append(roots, treemapNodeFromDataPoint(point))
			}
		}
		series := NewTreemapSeries(name, roots)
		series.FontFamily = fontFamily
		if opt.Label != nil && opt.Label.Show != nil {
			series.ShowLabels = *opt.Label.Show
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesTree:
		roots := make([]TreeSeriesNode, 0, len(opt.Data))
		for _, point := range opt.Data {
			if !point.Missing {
				roots = append(roots, treeNodeFromDataPoint(point))
			}
		}
		series := NewTreeSeries(name, roots)
		series.FontFamily = fontFamily
		if opt.Layout != "" {
			series.Layout = opt.Layout
		}
		if opt.SymbolSize > 0 {
			series.SymbolSize = float32(opt.SymbolSize)
		}
		if opt.Label != nil && opt.Label.Show != nil {
			series.ShowLabels = *opt.Label.Show
		}
		return series
	case SeriesGraph:
		nodes := make([]GraphNode, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing || point.Name == "" {
				continue
			}
			nodes = append(nodes, GraphNode{Name: point.Name, X: float32(point.X), Y: float32(point.Y), Value: float32(point.Y)})
		}
		links := append([]GraphLinkOption(nil), opt.Links...)
		if len(links) == 0 {
			links = append(links, opt.Edges...)
		}
		series := NewGraphSeries(name, nodes)
		series.FontFamily = fontFamily
		series.Links = links
		series.Layout = opt.Layout
		if opt.SymbolSize > 0 {
			series.SymbolSize = float32(opt.SymbolSize)
		}
		if opt.Label != nil && opt.Label.Show != nil {
			series.ShowLabels = *opt.Label.Show
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesBoxplot:
		boxes := make([]BoxplotPoint, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing || len(point.Values) < 5 {
				continue
			}
			boxes = append(boxes, BoxplotPoint{
				Min: float32(point.Values[0]), Q1: float32(point.Values[1]),
				Median: float32(point.Values[2]), Q3: float32(point.Values[3]),
				Max: float32(point.Values[4]),
			})
		}
		series := NewBoxplotSeries(name, boxes)
		applySeriesColor(series, opt.Color)
		return series
	case SeriesHeatmap:
		points := make([]HeatmapPoint, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing || len(point.Values) < 3 {
				continue
			}
			points = append(points, HeatmapPoint{
				X:     int(point.Values[0] + 0.5),
				Y:     int(point.Values[1] + 0.5),
				Value: float32(point.Values[2]),
			})
		}
		series := NewHeatmapSeries(name, points)
		if visualMap != nil {
			if visualMap.Min != nil {
				series.Min = float32(*visualMap.Min)
			}
			if visualMap.Max != nil {
				series.Max = float32(*visualMap.Max)
			}
			if visualMap.InRange != nil {
				for _, raw := range visualMap.InRange.Color {
					if color, ok := ParseColor(string(raw)); ok {
						series.Colors = append(series.Colors, color)
					}
				}
			}
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesFunnel:
		var slices []PieSlice
		for _, point := range opt.Data {
			if !point.Missing {
				slice := PieSlice{Label: point.Name, Value: float32(point.Y)}
				if color, ok := ParseColor(string(point.Color)); ok {
					slice.Color = color
				}
				slices = append(slices, slice)
			}
		}
		series := NewFunnelSeries(name, slices)
		series.Min, series.Max = opt.Min, opt.Max
		series.MinSize = parsePercentValue(opt.MinSize)
		series.MaxSize = parsePercentValue(opt.MaxSize)
		if opt.MaxSize == "" {
			series.MaxSize = 1
		}
		if opt.Sort != "" {
			series.Sort = opt.Sort
		}
		if opt.Gap >= 0 {
			series.Gap = float32(opt.Gap)
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesGauge:
		value := float64(0)
		if len(opt.Data) > 0 {
			value = opt.Data[0].Y
		}
		series := NewGaugeSeries(name, float32(value))
		if len(opt.Data) > 0 {
			series.Title = opt.Data[0].Name
		}
		if opt.Min != nil {
			series.Min = float32(*opt.Min)
		}
		if opt.Max != nil {
			series.Max = float32(*opt.Max)
		}
		if opt.StartAngle != 0 {
			series.StartAngle = float32(opt.StartAngle)
		}
		if opt.EndAngle != 0 {
			series.EndAngle = float32(opt.EndAngle)
		}
		if opt.Clockwise != nil {
			series.Clockwise = *opt.Clockwise
		}
		if pct := parsePercentValue(opt.Radius); pct > 0 {
			series.RadiusPercent = pct
		}
		applySeriesColor(series, opt.Color)
		return series
	case SeriesPie:
		slices := make([]PieSlice, 0, len(opt.Data))
		for _, point := range opt.Data {
			if point.Missing {
				continue
			}
			slice := PieSlice{Label: point.Name, Value: float32(point.Y)}
			if color, ok := ParseColor(string(point.Color)); ok {
				slice.Color = color
			}
			slices = append(slices, slice)
		}
		series := NewPieSeries(name, slices)
		if pct := parsePercentValue(opt.Radius); pct > 0 {
			series.RadiusPercent = pct
		}
		applySeriesColor(series, opt.Color)
		return series
	default:
		return nil
	}
}

func cartesianPoints(data []DataPoint, useExplicitX bool) []Point2D {
	points := make([]Point2D, 0, len(data))
	for i, point := range data {
		if point.Missing {
			continue
		}
		x := float64(i)
		if useExplicitX {
			x = point.X
		}
		points = append(points, Point2D{X: float32(x), Y: float32(point.Y)})
	}
	return points
}

func barValues(data []DataPoint) []float32 {
	values := make([]float32, 0, len(data))
	for _, point := range data {
		if point.Missing {
			values = append(values, 0)
			continue
		}
		values = append(values, float32(point.Y))
	}
	return values
}

func applySeriesColor(series Series, raw CSSColor) {
	if color, ok := ParseColor(string(raw)); ok {
		series.SetColor(color)
	}
}

func deriveCategories(series []SeriesOption) []string {
	maxLen := 0
	for _, s := range series {
		if len(s.Data) > maxLen {
			maxLen = len(s.Data)
		}
	}
	if maxLen == 0 {
		return nil
	}
	out := make([]string, maxLen)
	for i := 0; i < maxLen; i++ {
		label := ""
		for _, s := range series {
			if i < len(s.Data) && s.Data[i].Name != "" {
				label = s.Data[i].Name
				break
			}
		}
		if label == "" {
			label = strconv.Itoa(i + 1)
		}
		out[i] = label
	}
	return out
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func parseLegendPosition(value string) LegendPosition {
	switch value {
	case "top":
		return LegendTop
	case "left":
		return LegendLeft
	case "right":
		return LegendRight
	default:
		return LegendBottom
	}
}

// Ensure the declarative surface keeps returning a normal qui widget.
var _ qui.Widget = (*Chart)(nil)

func parsePercentValue(value string) float32 {
	if value == "" {
		return 0
	}
	trimmed := value[:len(value)-1]
	if len(value) == 0 || value[len(value)-1] != '%' {
		return 0
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return float32(parsed / 100)
}

func axisTooltipEntries(c *Chart, index int) []TooltipEntry {
	entries := make([]TooltipEntry, 0, len(c.Series))
	for _, series := range c.Series {
		if !series.Visible() {
			continue
		}
		if value, ok := seriesValueAt(series, index); ok {
			entries = append(entries, TooltipEntry{Name: series.Name(), Value: float64(value), Color: series.Color()})
		}
	}
	return entries
}
