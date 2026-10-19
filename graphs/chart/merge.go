package chart

// MergeOption applies an ECharts-style incremental update on top of the
// last full Option. Series are matched by name first and by index second;
// omitted fields retain their previous values.
//
// Boolean fields are one-way in merge mode: true enables an option, while
// false means "unspecified". Use SetOption for full replacement when an
// explicit false must clear a previously enabled boolean.
func (c *Chart) MergeOption(update Option) {
	if c == nil {
		return
	}
	c.SetOption(mergeOption(c.lastOption, update))
}

func mergeOption(base, update Option) Option {
	out := base
	out.Title = mergeTitle(base.Title, update.Title)
	if update.Tooltip != nil {
		out.Tooltip = update.Tooltip
	}
	if update.TextStyle != nil {
		out.TextStyle = update.TextStyle
	}
	if update.Legend != nil {
		target := LegendOption{}
		if out.Legend != nil {
			target = *out.Legend
		}
		if update.Legend.Show != nil {
			target.Show = update.Legend.Show
		}
		if update.Legend.Position != "" {
			target.Position = update.Legend.Position
		}
		out.Legend = &target
	}
	if update.Grid != nil {
		target := GridOption{}
		if out.Grid != nil {
			target = *out.Grid
		}
		if update.Grid.Left != 0 {
			target.Left = update.Grid.Left
		}
		if update.Grid.Right != 0 {
			target.Right = update.Grid.Right
		}
		if update.Grid.Top != 0 {
			target.Top = update.Grid.Top
		}
		if update.Grid.Bottom != 0 {
			target.Bottom = update.Grid.Bottom
		}
		out.Grid = &target
	}
	if update.VisualMap != nil {
		out.VisualMap = update.VisualMap
	}
	if update.Radar != nil {
		out.Radar = update.Radar
	}
	if len(update.ParallelAxis) > 0 {
		out.ParallelAxis = append([]AxisOption(nil), update.ParallelAxis...)
	}
	out.SingleAxis = mergeAxisOption(base.SingleAxis, update.SingleAxis)
	out.XAxis = mergeAxisOption(base.XAxis, update.XAxis)
	out.YAxis = mergeAxisOption(base.YAxis, update.YAxis)
	if len(update.Palette) > 0 {
		out.Palette = append([]CSSColor(nil), update.Palette...)
	} else {
		out.Palette = append([]CSSColor(nil), base.Palette...)
	}
	if update.BackgroundColor != "" {
		out.BackgroundColor = update.BackgroundColor
	}
	if update.EnableZoom {
		out.EnableZoom = true
	}
	if update.EnablePan {
		out.EnablePan = true
	}
	if update.EnableSelect {
		out.EnableSelect = true
	}

	out.Series = append([]SeriesOption(nil), base.Series...)
	for i, next := range update.Series {
		target := -1
		if next.Name != "" {
			for j := range out.Series {
				if out.Series[j].Name == next.Name {
					target = j
					break
				}
			}
		}
		if target < 0 && i < len(out.Series) {
			target = i
		}
		if target < 0 {
			out.Series = append(out.Series, next)
			continue
		}
		out.Series[target] = mergeSeriesOption(out.Series[target], next)
	}
	return out
}

func mergeTitle(base, update *TitleOption) *TitleOption {
	if update == nil {
		return base
	}
	if base == nil {
		copy := *update
		return &copy
	}
	out := *base
	if update.Text != "" {
		out.Text = update.Text
	}
	if update.SubText != "" {
		out.SubText = update.SubText
	}
	if update.Left != 0 {
		out.Left = update.Left
	}
	if update.Top != 0 {
		out.Top = update.Top
	}
	if update.Show != nil {
		out.Show = update.Show
	}
	return &out
}

func mergeAxisOption(base, update AxisOption) AxisOption {
	out := base
	if update.Type != "" {
		out.Type = update.Type
	}
	if update.Name != "" {
		out.Name = update.Name
	}
	if update.Data != nil {
		out.Data = append([]string(nil), update.Data...)
	}
	if update.Min != nil {
		out.Min = update.Min
	}
	if update.Max != nil {
		out.Max = update.Max
	}
	if update.Inverse != nil {
		out.Inverse = update.Inverse
	}
	if update.Scale != nil {
		out.Scale = update.Scale
	}
	if update.SplitNumber != 0 {
		out.SplitNumber = update.SplitNumber
	}
	if update.GroupPadding != 0 {
		out.GroupPadding = update.GroupPadding
	}
	if update.Formatter != nil {
		out.Formatter = update.Formatter
	}
	return out
}

func mergeSeriesOption(base, update SeriesOption) SeriesOption {
	out := base
	if update.Type != "" {
		out.Type = update.Type
	}
	if update.Name != "" {
		out.Name = update.Name
	}
	if update.Data != nil {
		out.Data = append([]DataPoint(nil), update.Data...)
	}
	if update.Color != "" {
		out.Color = update.Color
	}
	if update.Stack != "" {
		out.Stack = update.Stack
	}
	if update.Smooth {
		out.Smooth = true
	}
	if update.ShowSymbol != nil {
		out.ShowSymbol = update.ShowSymbol
	}
	if update.SymbolSize != 0 {
		out.SymbolSize = update.SymbolSize
	}
	if update.SymbolHeight != 0 {
		out.SymbolHeight = update.SymbolHeight
	}
	if update.Symbol != "" {
		out.Symbol = update.Symbol
	}
	if update.SymbolRepeat != nil {
		out.SymbolRepeat = update.SymbolRepeat
	}
	if update.SymbolMargin != 0 {
		out.SymbolMargin = update.SymbolMargin
	}
	if update.SymbolClip != nil {
		out.SymbolClip = update.SymbolClip
	}
	if update.FillOpacity != 0 {
		out.FillOpacity = update.FillOpacity
	}
	if update.BarGap != 0 {
		out.BarGap = update.BarGap
	}
	if update.Radius != "" {
		out.Radius = update.Radius
	}
	if update.Opacity != nil {
		out.Opacity = update.Opacity
	}
	if update.Label != nil {
		out.Label = update.Label
	}
	if update.Layout != "" {
		out.Layout = update.Layout
	}
	if update.Links != nil {
		out.Links = append([]GraphLinkOption(nil), update.Links...)
	}
	if update.Edges != nil {
		out.Edges = append([]GraphLinkOption(nil), update.Edges...)
	}
	if update.NodeWidth != 0 {
		out.NodeWidth = update.NodeWidth
	}
	if update.NodeGap != 0 {
		out.NodeGap = update.NodeGap
	}
	if update.Sort != "" {
		out.Sort = update.Sort
	}
	if update.Gap != 0 {
		out.Gap = update.Gap
	}
	if update.MinSize != "" {
		out.MinSize = update.MinSize
	}
	if update.MaxSize != "" {
		out.MaxSize = update.MaxSize
	}
	if update.Min != nil {
		out.Min = update.Min
	}
	if update.Max != nil {
		out.Max = update.Max
	}
	if update.StartAngle != 0 {
		out.StartAngle = update.StartAngle
	}
	if update.EndAngle != 0 {
		out.EndAngle = update.EndAngle
	}
	if update.Clockwise != nil {
		out.Clockwise = update.Clockwise
	}
	return out
}
