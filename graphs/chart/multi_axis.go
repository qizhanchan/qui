package chart

import . "github.com/qizhanchan/qui"

func normalizedXAxisOptions(opt Option) []AxisOption {
	if len(opt.XAxes) > 0 {
		return append([]AxisOption(nil), opt.XAxes...)
	}
	return []AxisOption{opt.XAxis}
}

func normalizedYAxisOptions(opt Option) []AxisOption {
	if len(opt.YAxes) > 0 {
		return append([]AxisOption(nil), opt.YAxes...)
	}
	return []AxisOption{opt.YAxis}
}

func normalizedGridOptions(opt Option) []GridOption {
	if len(opt.Grids) > 0 {
		return append([]GridOption(nil), opt.Grids...)
	}
	if opt.Grid != nil {
		return []GridOption{*opt.Grid}
	}
	return nil
}

func seriesAxisRefFor(option SeriesOption, xCount, yCount, gridCount int) seriesAxisRef {
	ref := seriesAxisRef{xAxis: option.XAxisIndex, yAxis: option.YAxisIndex, grid: option.GridIndex}
	if ref.xAxis < 0 || ref.xAxis >= xCount {
		ref.xAxis = 0
	}
	if ref.yAxis < 0 || ref.yAxis >= yCount {
		ref.yAxis = 0
	}
	if ref.grid < 0 || ref.grid >= gridCount {
		ref.grid = 0
	}
	return ref
}

func (c *Chart) seriesAxesAt(index int) (Rect, Axis, Axis) {
	if c == nil || index < 0 || index >= len(c.Series) {
		return Rect{}, nil, nil
	}
	ref := c.seriesRefAt(index)
	if ref.xAxis < 0 || ref.xAxis >= len(c.XAxes) {
		ref.xAxis = 0
	}
	if ref.yAxis < 0 || ref.yAxis >= len(c.YAxes) {
		ref.yAxis = 0
	}
	var rect Rect
	if ref.grid >= 0 && ref.grid < len(c.GridRects) {
		rect = c.GridRects[ref.grid]
	}
	if rect.IsEmpty() {
		rect = c.plotRect
	}
	var xAxis, yAxis Axis
	if len(c.XAxes) > 0 {
		xAxis = c.XAxes[ref.xAxis]
	}
	if len(c.YAxes) > 0 {
		yAxis = c.YAxes[ref.yAxis]
	}
	return rect, xAxis, yAxis
}

func (c *Chart) seriesRefAt(index int) seriesAxisRef {
	ref := seriesAxisRef{}
	if c != nil && index >= 0 && index < len(c.seriesAxes) {
		ref = c.seriesAxes[index]
	}
	return ref
}

func (c *Chart) gridAxesAt(index int) (Axis, Axis) {
	if c == nil {
		return nil, nil
	}
	xIndex, yIndex := index, index
	if xIndex < 0 || xIndex >= len(c.XAxes) {
		xIndex = 0
	}
	if yIndex < 0 || yIndex >= len(c.YAxes) {
		yIndex = 0
	}
	var xAxis, yAxis Axis
	if len(c.XAxes) > 0 {
		xAxis = c.XAxes[xIndex]
	}
	if len(c.YAxes) > 0 {
		yAxis = c.YAxes[yIndex]
	}
	return xAxis, yAxis
}

func (c *Chart) layoutGridRects(r Rect, base PlotGutters, theme *Theme) {
	if len(c.GridOptions) <= 1 {
		c.GridRects = []Rect{c.plotRect}
		return
	}
	c.GridRects = make([]Rect, len(c.GridOptions))
	for i, option := range c.GridOptions {
		g := base
		if option.Left > 0 {
			g.Left = option.Left
		}
		if option.Right > 0 {
			g.Right = option.Right
		}
		if option.Top > 0 {
			g.Top = option.Top
		}
		if option.Bottom > 0 {
			g.Bottom = option.Bottom
		}
		rect := Rect{X: r.X + g.Left, Y: r.Y + g.Top, W: r.W - g.Left - g.Right, H: r.H - g.Top - g.Bottom}
		if rect.W < 0 {
			rect.W = 0
		}
		if rect.H < 0 {
			rect.H = 0
		}
		c.GridRects[i] = rect
	}
	c.plotRect = c.GridRects[0]
}

func (c *Chart) boundsForAxes(xIndex, yIndex int) (SeriesBounds, SeriesBounds) {
	var bx, by SeriesBounds
	for i, series := range c.Series {
		if !series.Visible() {
			continue
		}
		ref := seriesAxisRef{}
		if i < len(c.seriesAxes) {
			ref = c.seriesAxes[i]
		}
		bounds := series.DataBounds()
		if bounds.HasData && ref.xAxis == xIndex {
			bx = mergeSeriesBounds(bx, SeriesBounds{Xmin: bounds.Xmin, Xmax: bounds.Xmax, HasData: true})
		}
		if bounds.HasData && ref.yAxis == yIndex {
			by = mergeSeriesBounds(by, SeriesBounds{Ymin: bounds.Ymin, Ymax: bounds.Ymax, HasData: true})
		}
	}
	runningByStack := make(map[string][]float32)
	for i, series := range c.Series {
		bar, ok := series.(*BarSeries)
		if !ok || !bar.Visible() || !bar.Stacked {
			continue
		}
		ref := seriesAxisRef{}
		if i < len(c.seriesAxes) {
			ref = c.seriesAxes[i]
		}
		if ref.yAxis != yIndex {
			continue
		}
		running := runningByStack[bar.StackID]
		if len(running) < len(bar.Values) {
			running = append(running, make([]float32, len(bar.Values)-len(running))...)
		}
		for j, value := range bar.Values {
			running[j] += value
			if !by.HasData || running[j] < by.Ymin {
				by.Ymin = running[j]
			}
			if !by.HasData || running[j] > by.Ymax {
				by.Ymax = running[j]
			}
			by.HasData = true
		}
		runningByStack[bar.StackID] = running
	}
	return bx, by
}

func mergeSeriesBounds(base, next SeriesBounds) SeriesBounds {
	if !next.HasData {
		return base
	}
	if !base.HasData {
		return next
	}
	if next.Xmin < base.Xmin {
		base.Xmin = next.Xmin
	}
	if next.Xmax > base.Xmax {
		base.Xmax = next.Xmax
	}
	if next.Ymin < base.Ymin {
		base.Ymin = next.Ymin
	}
	if next.Ymax > base.Ymax {
		base.Ymax = next.Ymax
	}
	return base
}

func (c *Chart) hasBarSeriesForY(yIndex int) bool {
	for i, series := range c.Series {
		ref := seriesAxisRef{}
		if i < len(c.seriesAxes) {
			ref = c.seriesAxes[i]
		}
		if ref.yAxis != yIndex {
			continue
		}
		switch series.(type) {
		case *BarSeries, *PictorialBarSeries:
			return true
		}
	}
	return false
}
