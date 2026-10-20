package chart

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// OptionPatch is a presence-aware partial update for MergeOption. Pointer
// fields distinguish "not supplied" from an explicit zero value, so a patch
// can turn a boolean off, reset a number to zero, or clear a string.
//
// Nullable fields that need to be restored to their automatic/default state
// have an adjacent Clear flag (for example AxisPatch.ClearMin).
type OptionPatch struct {
	Title             *TitlePatch        `json:"title,omitempty"`
	Tooltip           *TooltipPatch      `json:"tooltip,omitempty"`
	AxisPointer       *AxisPointerOption `json:"axisPointer,omitempty"`
	TextStyle         *TextStylePatch    `json:"textStyle,omitempty"`
	Legend            *LegendPatch       `json:"legend,omitempty"`
	Grid              *GridPatch         `json:"grid,omitempty"`
	Grids             *[]GridPatch       `json:"grids,omitempty"`
	Radar             *RadarPatch        `json:"radar,omitempty"`
	VisualMap         *VisualMapPatch    `json:"visualMap,omitempty"`
	ParallelAxis      *[]AxisOption      `json:"parallelAxis,omitempty"`
	SingleAxis        *AxisPatch         `json:"singleAxis,omitempty"`
	XAxis             *AxisPatch         `json:"xAxis,omitempty"`
	YAxis             *AxisPatch         `json:"yAxis,omitempty"`
	XAxes             *[]AxisPatch       `json:"xAxes,omitempty"`
	YAxes             *[]AxisPatch       `json:"yAxes,omitempty"`
	Series            []SeriesPatch      `json:"series,omitempty"`
	ClearSeries       bool               `json:"clearSeries,omitempty"`
	Dataset           *DatasetList       `json:"dataset,omitempty"`
	DataZoom          *[]DataZoomOption  `json:"dataZoom,omitempty"`
	Palette           *[]CSSColor        `json:"color,omitempty"`
	BackgroundColor   *CSSColor          `json:"backgroundColor,omitempty"`
	EnableZoom        *bool              `json:"-"`
	EnablePan         *bool              `json:"-"`
	EnableSelect      *bool              `json:"-"`
	Animation         *bool              `json:"animation,omitempty"`
	AnimationDuration *int               `json:"animationDuration,omitempty"`
	AnimationEasing   *string            `json:"animationEasing,omitempty"`
}

// TitlePatch updates chart title fields.
type TitlePatch struct {
	Text    *string  `json:"text,omitempty"`
	SubText *string  `json:"subtext,omitempty"`
	Left    *float32 `json:"left,omitempty"`
	Top     *float32 `json:"top,omitempty"`
	Show    *bool    `json:"show,omitempty"`
}

// TooltipPatch updates tooltip fields. ClearFormatter removes a custom Go
// formatter while leaving the rest of the tooltip configuration intact.
type TooltipPatch struct {
	Show           *bool                                 `json:"show,omitempty"`
	Trigger        *string                               `json:"trigger,omitempty"`
	Formatter      *func(SeriesOption, DataPoint) string `json:"-"`
	AxisFormatter  *func(int, []TooltipEntry) string     `json:"-"`
	ClearFormatter bool                                  `json:"-"`
	ClearAxis      bool                                  `json:"-"`
}

// TextStylePatch updates chart-wide typography.
type TextStylePatch struct {
	FontFamily *string   `json:"fontFamily,omitempty"`
	Color      *CSSColor `json:"color,omitempty"`
}

// LegendPatch updates legend configuration.
type LegendPatch struct {
	Show     *bool   `json:"show,omitempty"`
	Position *string `json:"position,omitempty"`
}

// GridPatch updates individual plot-area gutters.
type GridPatch struct {
	Left   *float32 `json:"left,omitempty"`
	Right  *float32 `json:"right,omitempty"`
	Top    *float32 `json:"top,omitempty"`
	Bottom *float32 `json:"bottom,omitempty"`
}

// RadarPatch updates the radar coordinate configuration.
type RadarPatch struct {
	Indicators *[]RadarIndicator `json:"indicator,omitempty"`
}

// VisualMapPatch updates the continuous visual map. ClearMin/ClearMax
// restore automatic data extents.
type VisualMapPatch struct {
	Min      *float64          `json:"min,omitempty"`
	Max      *float64          `json:"max,omitempty"`
	Show     *bool             `json:"show,omitempty"`
	InRange  *VisualMapInRange `json:"inRange,omitempty"`
	ClearMin bool              `json:"-"`
	ClearMax bool              `json:"-"`
}

// AxisPatch updates one cartesian or polar axis. ClearMin/ClearMax restore
// automatic ranging; ClearFormatter removes a custom label formatter.
type AxisPatch struct {
	Type           *AxisType             `json:"type,omitempty"`
	Name           *string               `json:"name,omitempty"`
	Data           *[]string             `json:"data,omitempty"`
	Min            *float64              `json:"min,omitempty"`
	Max            *float64              `json:"max,omitempty"`
	Inverse        *bool                 `json:"inverse,omitempty"`
	Scale          *bool                 `json:"scale,omitempty"`
	SplitNumber    *int                  `json:"splitNumber,omitempty"`
	GroupPadding   *float32              `json:"groupPadding,omitempty"`
	Formatter      *func(float64) string `json:"-"`
	ClearMin       bool                  `json:"-"`
	ClearMax       bool                  `json:"-"`
	ClearFormatter bool                  `json:"-"`
}

// LabelPatch updates one series label configuration.
type LabelPatch struct {
	Show     *bool     `json:"show,omitempty"`
	Position *string   `json:"position,omitempty"`
	Color    *CSSColor `json:"color,omitempty"`
	FontSize *float64  `json:"fontSize,omitempty"`
}

// SeriesPatch is a presence-aware partial update for one series. Series are
// matched by Name first and by index second, retaining all unpatched fields.
// ClearMin/ClearMax and ClearLabel restore automatic/default state.
type SeriesPatch struct {
	Type           *SeriesType         `json:"type,omitempty"`
	Name           *string             `json:"name,omitempty"`
	Data           *[]DataPoint        `json:"data,omitempty"`
	Color          *CSSColor           `json:"color,omitempty"`
	Stack          *string             `json:"stack,omitempty"`
	Smooth         *bool               `json:"smooth,omitempty"`
	ShowSymbol     *bool               `json:"showSymbol,omitempty"`
	SymbolSize     *float64            `json:"symbolSize,omitempty"`
	SymbolHeight   *float64            `json:"-"`
	Symbol         *string             `json:"symbol,omitempty"`
	SymbolRepeat   *bool               `json:"symbolRepeat,omitempty"`
	SymbolMargin   *float64            `json:"symbolMargin,omitempty"`
	SymbolClip     *bool               `json:"symbolClip,omitempty"`
	FillOpacity    *float64            `json:"fillOpacity,omitempty"`
	BarGap         *float64            `json:"barGap,omitempty"`
	Radius         *string             `json:"radius,omitempty"`
	Opacity        *float64            `json:"opacity,omitempty"`
	Label          *LabelPatch         `json:"label,omitempty"`
	LabelLayout    *LabelLayoutOption  `json:"labelLayout,omitempty"`
	Layout         *string             `json:"layout,omitempty"`
	Links          *[]GraphLinkOption  `json:"links,omitempty"`
	Edges          *[]GraphLinkOption  `json:"edges,omitempty"`
	NodeWidth      *float64            `json:"nodeWidth,omitempty"`
	NodeGap        *float64            `json:"nodeGap,omitempty"`
	Sort           *string             `json:"sort,omitempty"`
	Gap            *float64            `json:"gap,omitempty"`
	MinSize        *string             `json:"minSize,omitempty"`
	MaxSize        *string             `json:"maxSize,omitempty"`
	Min            *float64            `json:"min,omitempty"`
	Max            *float64            `json:"max,omitempty"`
	StartAngle     *float64            `json:"startAngle,omitempty"`
	EndAngle       *float64            `json:"endAngle,omitempty"`
	Clockwise      *bool               `json:"clockwise,omitempty"`
	Sampling       *string             `json:"sampling,omitempty"`
	Large          *bool               `json:"large,omitempty"`
	LargeThreshold *int                `json:"largeThreshold,omitempty"`
	Candles        *[]CandlestickPoint `json:"-"`
	DatasetIndex   *int                `json:"datasetIndex,omitempty"`
	Encode         *EncodeOption       `json:"encode,omitempty"`
	ClearEncode    bool                `json:"-"`
	XAxisIndex     *int                `json:"xAxisIndex,omitempty"`
	YAxisIndex     *int                `json:"yAxisIndex,omitempty"`
	GridIndex      *int                `json:"gridIndex,omitempty"`
	ClearMin       bool                `json:"-"`
	ClearMax       bool                `json:"-"`
	ClearLabel     bool                `json:"-"`
}

// UnmarshalJSON accepts scalar and [width,height] ECharts symbolSize values,
// matching SeriesOption's declarative JSON surface.
func (s *SeriesPatch) UnmarshalJSON(data []byte) error {
	type plain SeriesPatch
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
		s.SymbolSize = float64Pointer(scalar)
		s.SymbolHeight = float64Pointer(scalar)
		return nil
	}
	var pair []float64
	if err := json.Unmarshal(aux.RawSymbolSize, &pair); err != nil || len(pair) == 0 {
		return fmt.Errorf("chart: invalid symbolSize %s", aux.RawSymbolSize)
	}
	height := pair[0]
	if len(pair) > 1 {
		height = pair[1]
	}
	s.SymbolSize = float64Pointer(pair[0])
	s.SymbolHeight = float64Pointer(height)
	return nil
}

// MergeOption applies a partial update on top of the last full Option.
// Series are matched by name first and by index second. Unpatched fields are
// retained, while pointer fields explicitly carrying zero values are applied.
func (c *Chart) MergeOption(update OptionPatch) {
	if c == nil {
		return
	}
	c.SetOption(mergeOptionPatch(c.lastOption, update))
}

func mergeOptionPatch(base Option, update OptionPatch) Option {
	out := base
	if update.Title != nil {
		out.Title = patchTitleOption(base.Title, update.Title)
	}
	if update.Tooltip != nil {
		out.Tooltip = patchTooltipOption(base.Tooltip, update.Tooltip)
	}
	if update.AxisPointer != nil {
		axisPointer := *update.AxisPointer
		out.AxisPointer = &axisPointer
	}
	if update.TextStyle != nil {
		out.TextStyle = patchTextStyleOption(base.TextStyle, update.TextStyle)
	}
	if update.Legend != nil {
		out.Legend = patchLegendOption(base.Legend, update.Legend)
	}
	if update.Grid != nil {
		out.Grid = patchGridOption(base.Grid, update.Grid)
		if out.Grid != nil {
			if len(out.Grids) == 0 {
				out.Grids = []GridOption{*out.Grid}
			} else {
				out.Grids[0] = *out.Grid
			}
		}
	}
	if update.Grids != nil {
		out.Grids = make([]GridOption, len(*update.Grids))
		baseOptions := gridOptionsOf(base)
		for i, patch := range *update.Grids {
			baseGrid := GridOption{}
			if i < len(baseOptions) {
				baseGrid = baseOptions[i]
			}
			out.Grids[i] = *patchGridOption(&baseGrid, &patch)
		}
		if len(out.Grids) > 0 {
			one := out.Grids[0]
			out.Grid = &one
		}
	}
	if update.Radar != nil {
		out.Radar = patchRadarOption(base.Radar, update.Radar)
	}
	if update.VisualMap != nil {
		out.VisualMap = patchVisualMapOption(base.VisualMap, update.VisualMap)
	}
	if update.ParallelAxis != nil {
		out.ParallelAxis = append([]AxisOption(nil), (*update.ParallelAxis)...)
	}
	if update.SingleAxis != nil {
		out.SingleAxis = patchAxisOption(base.SingleAxis, update.SingleAxis)
	}
	if update.XAxis != nil {
		out.XAxis = patchAxisOption(base.XAxis, update.XAxis)
		if len(out.XAxes) == 0 {
			out.XAxes = []AxisOption{out.XAxis}
		} else {
			out.XAxes[0] = out.XAxis
		}
	}
	if update.YAxis != nil {
		out.YAxis = patchAxisOption(base.YAxis, update.YAxis)
		if len(out.YAxes) == 0 {
			out.YAxes = []AxisOption{out.YAxis}
		} else {
			out.YAxes[0] = out.YAxis
		}
	}
	if update.XAxes != nil {
		out.XAxes = make([]AxisOption, len(*update.XAxes))
		for i, patch := range *update.XAxes {
			baseAxis := AxisOption{}
			if i < len(base.XAxes) {
				baseAxis = base.XAxes[i]
			} else if i == 0 {
				baseAxis = base.XAxis
			}
			out.XAxes[i] = patchAxisOption(baseAxis, &patch)
		}
		if len(out.XAxes) > 0 {
			out.XAxis = out.XAxes[0]
		}
	}
	if update.YAxes != nil {
		out.YAxes = make([]AxisOption, len(*update.YAxes))
		for i, patch := range *update.YAxes {
			baseAxis := AxisOption{}
			if i < len(base.YAxes) {
				baseAxis = base.YAxes[i]
			} else if i == 0 {
				baseAxis = base.YAxis
			}
			out.YAxes[i] = patchAxisOption(baseAxis, &patch)
		}
		if len(out.YAxes) > 0 {
			out.YAxis = out.YAxes[0]
		}
	}
	if update.ClearSeries {
		out.Series = nil
	} else if len(update.Series) > 0 {
		out.Series = patchSeriesOptions(base.Series, update.Series)
	}
	if update.Dataset != nil {
		out.Dataset = append(DatasetList(nil), (*update.Dataset)...)
	}
	if update.DataZoom != nil {
		out.DataZoom = append([]DataZoomOption(nil), (*update.DataZoom)...)
	}
	if update.Palette != nil {
		out.Palette = append([]CSSColor(nil), (*update.Palette)...)
	}
	if update.BackgroundColor != nil {
		out.BackgroundColor = *update.BackgroundColor
	}
	if update.EnableZoom != nil {
		out.EnableZoom = *update.EnableZoom
	}
	if update.EnablePan != nil {
		out.EnablePan = *update.EnablePan
	}
	if update.EnableSelect != nil {
		out.EnableSelect = *update.EnableSelect
	}
	if update.Animation != nil {
		out.Animation = boolPointer(*update.Animation)
	}
	if update.AnimationDuration != nil {
		out.AnimationDuration = *update.AnimationDuration
	}
	if update.AnimationEasing != nil {
		out.AnimationEasing = *update.AnimationEasing
	}
	return out
}

func gridOptionsOf(option Option) []GridOption {
	if len(option.Grids) > 0 {
		return append([]GridOption(nil), option.Grids...)
	}
	if option.Grid != nil {
		return []GridOption{*option.Grid}
	}
	return nil
}

func patchTitleOption(base *TitleOption, patch *TitlePatch) *TitleOption {
	var out TitleOption
	if base != nil {
		out = *base
	}
	if patch.Text != nil {
		out.Text = *patch.Text
	}
	if patch.SubText != nil {
		out.SubText = *patch.SubText
	}
	if patch.Left != nil {
		out.Left = *patch.Left
	}
	if patch.Top != nil {
		out.Top = *patch.Top
	}
	if patch.Show != nil {
		out.Show = boolPointer(*patch.Show)
	}
	return &out
}

func patchTooltipOption(base *TooltipOption, patch *TooltipPatch) *TooltipOption {
	var out TooltipOption
	if base != nil {
		out = *base
	}
	if patch.Show != nil {
		out.Show = boolPointer(*patch.Show)
	}
	if patch.Trigger != nil {
		out.Trigger = *patch.Trigger
	}
	if patch.Formatter != nil {
		out.Formatter = *patch.Formatter
	}
	if patch.AxisFormatter != nil {
		out.AxisFormatter = *patch.AxisFormatter
	}
	if patch.ClearFormatter {
		out.Formatter = nil
	}
	if patch.ClearAxis {
		out.AxisFormatter = nil
	}
	return &out
}

func patchTextStyleOption(base *TextStyleOption, patch *TextStylePatch) *TextStyleOption {
	var out TextStyleOption
	if base != nil {
		out = *base
	}
	if patch.FontFamily != nil {
		out.FontFamily = *patch.FontFamily
	}
	if patch.Color != nil {
		out.Color = *patch.Color
	}
	return &out
}

func patchLegendOption(base *LegendOption, patch *LegendPatch) *LegendOption {
	var out LegendOption
	if base != nil {
		out = *base
	}
	if patch.Show != nil {
		out.Show = boolPointer(*patch.Show)
	}
	if patch.Position != nil {
		out.Position = *patch.Position
	}
	return &out
}

func patchGridOption(base *GridOption, patch *GridPatch) *GridOption {
	var out GridOption
	if base != nil {
		out = *base
	}
	if patch.Left != nil {
		out.Left = *patch.Left
	}
	if patch.Right != nil {
		out.Right = *patch.Right
	}
	if patch.Top != nil {
		out.Top = *patch.Top
	}
	if patch.Bottom != nil {
		out.Bottom = *patch.Bottom
	}
	return &out
}

func patchRadarOption(base *RadarOption, patch *RadarPatch) *RadarOption {
	var out RadarOption
	if base != nil {
		out = *base
	}
	if patch.Indicators != nil {
		out.Indicators = append([]RadarIndicator(nil), (*patch.Indicators)...)
	}
	return &out
}

func patchVisualMapOption(base *VisualMapOption, patch *VisualMapPatch) *VisualMapOption {
	var out VisualMapOption
	if base != nil {
		out = *base
	}
	if patch.Min != nil {
		out.Min = float64Pointer(*patch.Min)
	}
	if patch.Max != nil {
		out.Max = float64Pointer(*patch.Max)
	}
	if patch.ClearMin {
		out.Min = nil
	}
	if patch.ClearMax {
		out.Max = nil
	}
	if patch.Show != nil {
		out.Show = boolPointer(*patch.Show)
	}
	if patch.InRange != nil {
		inRange := *patch.InRange
		inRange.Color = append([]CSSColor(nil), patch.InRange.Color...)
		out.InRange = &inRange
	}
	return &out
}

func patchAxisOption(base AxisOption, patch *AxisPatch) AxisOption {
	out := base
	if patch.Type != nil {
		out.Type = *patch.Type
	}
	if patch.Name != nil {
		out.Name = *patch.Name
	}
	if patch.Data != nil {
		out.Data = append([]string(nil), (*patch.Data)...)
	}
	if patch.Min != nil {
		out.Min = float64Pointer(*patch.Min)
	}
	if patch.Max != nil {
		out.Max = float64Pointer(*patch.Max)
	}
	if patch.ClearMin {
		out.Min = nil
	}
	if patch.ClearMax {
		out.Max = nil
	}
	if patch.Inverse != nil {
		out.Inverse = boolPointer(*patch.Inverse)
	}
	if patch.Scale != nil {
		out.Scale = boolPointer(*patch.Scale)
	}
	if patch.SplitNumber != nil {
		out.SplitNumber = *patch.SplitNumber
	}
	if patch.GroupPadding != nil {
		out.GroupPadding = *patch.GroupPadding
	}
	if patch.Formatter != nil {
		out.Formatter = *patch.Formatter
	}
	if patch.ClearFormatter {
		out.Formatter = nil
	}
	return out
}

func patchSeriesOptions(base []SeriesOption, patches []SeriesPatch) []SeriesOption {
	out := append([]SeriesOption(nil), base...)
	for i, patch := range patches {
		target := -1
		if patch.Name != nil && *patch.Name != "" {
			for j := range out {
				if out[j].Name == *patch.Name {
					target = j
					break
				}
			}
		}
		if target < 0 && i < len(out) {
			target = i
		}
		if target < 0 {
			out = append(out, patchSeriesOption(SeriesOption{}, patch))
			continue
		}
		out[target] = patchSeriesOption(out[target], patch)
	}
	return out
}

func patchSeriesOption(base SeriesOption, patch SeriesPatch) SeriesOption {
	out := base
	if patch.Type != nil {
		out.Type = *patch.Type
	}
	if patch.Name != nil {
		out.Name = *patch.Name
	}
	if patch.Data != nil {
		out.Data = cloneDataPoints(*patch.Data)
	}
	if patch.Color != nil {
		out.Color = *patch.Color
	}
	if patch.Stack != nil {
		out.Stack = *patch.Stack
	}
	if patch.Smooth != nil {
		out.Smooth = *patch.Smooth
	}
	if patch.ShowSymbol != nil {
		out.ShowSymbol = boolPointer(*patch.ShowSymbol)
	}
	if patch.SymbolSize != nil {
		out.SymbolSize = *patch.SymbolSize
	}
	if patch.SymbolHeight != nil {
		out.SymbolHeight = *patch.SymbolHeight
	}
	if patch.Symbol != nil {
		out.Symbol = *patch.Symbol
	}
	if patch.SymbolRepeat != nil {
		out.SymbolRepeat = boolPointer(*patch.SymbolRepeat)
	}
	if patch.SymbolMargin != nil {
		out.SymbolMargin = *patch.SymbolMargin
	}
	if patch.SymbolClip != nil {
		out.SymbolClip = boolPointer(*patch.SymbolClip)
	}
	if patch.FillOpacity != nil {
		out.FillOpacity = *patch.FillOpacity
	}
	if patch.BarGap != nil {
		out.BarGap = *patch.BarGap
	}
	if patch.Radius != nil {
		out.Radius = *patch.Radius
	}
	if patch.Opacity != nil {
		out.Opacity = float64Pointer(*patch.Opacity)
	}
	if patch.Label != nil {
		out.Label = patchLabelOption(out.Label, patch.Label)
	}
	if patch.LabelLayout != nil {
		layout := *patch.LabelLayout
		out.LabelLayout = &layout
	}
	if patch.ClearLabel {
		out.Label = nil
	}
	if patch.Layout != nil {
		out.Layout = *patch.Layout
	}
	if patch.Links != nil {
		out.Links = append([]GraphLinkOption(nil), (*patch.Links)...)
	}
	if patch.Edges != nil {
		out.Edges = append([]GraphLinkOption(nil), (*patch.Edges)...)
	}
	if patch.NodeWidth != nil {
		out.NodeWidth = *patch.NodeWidth
	}
	if patch.NodeGap != nil {
		out.NodeGap = *patch.NodeGap
	}
	if patch.Sort != nil {
		out.Sort = *patch.Sort
	}
	if patch.Gap != nil {
		out.Gap = *patch.Gap
	}
	if patch.MinSize != nil {
		out.MinSize = *patch.MinSize
	}
	if patch.MaxSize != nil {
		out.MaxSize = *patch.MaxSize
	}
	if patch.Min != nil {
		out.Min = float64Pointer(*patch.Min)
	}
	if patch.Max != nil {
		out.Max = float64Pointer(*patch.Max)
	}
	if patch.ClearMin {
		out.Min = nil
	}
	if patch.ClearMax {
		out.Max = nil
	}
	if patch.StartAngle != nil {
		out.StartAngle = *patch.StartAngle
	}
	if patch.EndAngle != nil {
		out.EndAngle = *patch.EndAngle
	}
	if patch.Clockwise != nil {
		out.Clockwise = boolPointer(*patch.Clockwise)
	}
	if patch.Sampling != nil {
		out.Sampling = *patch.Sampling
	}
	if patch.Large != nil {
		out.Large = boolPointer(*patch.Large)
	}
	if patch.LargeThreshold != nil {
		out.LargeThreshold = *patch.LargeThreshold
	}
	if patch.Candles != nil {
		out.Candles = append([]CandlestickPoint(nil), (*patch.Candles)...)
	}
	if patch.DatasetIndex != nil {
		out.DatasetIndex = *patch.DatasetIndex
	}
	if patch.Encode != nil {
		encode := *patch.Encode
		out.Encode = &encode
	}
	if patch.ClearEncode {
		out.Encode = nil
	}
	if patch.XAxisIndex != nil {
		out.XAxisIndex = *patch.XAxisIndex
	}
	if patch.YAxisIndex != nil {
		out.YAxisIndex = *patch.YAxisIndex
	}
	if patch.GridIndex != nil {
		out.GridIndex = *patch.GridIndex
	}
	return out
}

func patchLabelOption(base *LabelOption, patch *LabelPatch) *LabelOption {
	var out LabelOption
	if base != nil {
		out = *base
	}
	if patch.Show != nil {
		out.Show = boolPointer(*patch.Show)
	}
	if patch.Position != nil {
		out.Position = *patch.Position
	}
	if patch.Color != nil {
		out.Color = *patch.Color
	}
	if patch.FontSize != nil {
		out.FontSize = *patch.FontSize
	}
	return &out
}

func cloneDataPoints(points []DataPoint) []DataPoint {
	if points == nil {
		return nil
	}
	out := make([]DataPoint, len(points))
	for i, point := range points {
		out[i] = point
		out[i].Values = append([]float64(nil), point.Values...)
		if point.Children != nil {
			out[i].Children = cloneDataPoints(point.Children)
		}
	}
	return out
}

func boolPointer(value bool) *bool { return &value }

func float64Pointer(value float64) *float64 { return &value }
