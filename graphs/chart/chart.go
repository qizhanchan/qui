package chart

import (
	"math"
	"time"

	. "github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs"
)

// Chart is the top-level graph widget. It owns a set of series, up to
// two axes (X + Y), an optional title/subtitle, and an optional legend.
// Chart subclasses Container so the Legend participates in the widget
// tree and dispatch naturally.
//
// Usage:
//
//	c := chart.NewChart()
//	c.Title = "Revenue"
//	c.AddSeries(chart.NewLineSeries("Q1", points))
//	// c is a normal qui.Widget; add it to any container/layout.
type Chart struct {
	Container

	Title           string
	SubTitle        string
	TitleLeft       float32
	TitleTop        float32
	TitleFontSize   float32
	TitleFontWeight FontWeight
	TitleColor      Color
	SubTitleColor   Color
	TextColor       Color
	AxisColor       Color
	AxisLabelColor  Color
	FontFamily      string
	// Background, when non-nil, replaces the default theme surface fill.
	// The declarative API sets this from backgroundColor.
	Background *Color
	XAxis      Axis // nil → auto ValueAxis on first Layout from series bounds
	YAxis      Axis // nil → auto ValueAxis; defaults Inverted so larger = up
	Series     []Series
	Legend     *Legend // nil disables; use SetLegend to attach
	Palette    []Color
	ShowGrid   bool
	// GridColor, when set (A > 0), paints the gridlines instead of the
	// theme's subdued text colour.
	GridColor Color
	// Gutters overrides individual plot-area gutters. Zero sides use the
	// theme-derived defaults; this is populated by the declarative Option API.
	Gutters *PlotGutters
	// ShowTooltip gates hover tooltips independently of OnHover callbacks.
	ShowTooltip bool

	// EnableZoom gates scroll-wheel zoom (and double-click-to-reset).
	// EnablePan gates left-button drag-to-pan. EnableSelect gates
	// click-to-select (OnSelect firing). All default false — the chart
	// is a passive viewer unless the host opts in.
	EnableZoom   bool
	EnablePan    bool
	EnableSelect bool

	OnSelect func(series Series, index int)
	OnHover  func(series Series, index int) // index=-1 on leave

	// TooltipBuilder customizes item-trigger tooltip text for a
	// (series, index) pair. nil uses DefaultTooltipText.
	TooltipBuilder func(series Series, index int) string
	// TooltipTrigger is "item" (default) or "axis". Axis mode aggregates
	// all cartesian series at the hovered data index.
	TooltipTrigger string
	// AxisTooltipBuilder customizes axis-trigger tooltip text.
	AxisTooltipBuilder func(index int) string

	// autoPalette is true when Palette was not set explicitly; we then
	// derive it from the active theme and re-derive on theme change.
	autoPalette bool
	// fixedGutters means the declarative Grid specified the plot area
	// directly; title/legend must not grow those gutters.
	fixedGutters bool
	// cached layout rects.
	plotRect    Rect
	titleRect   Rect
	hoverSeries int
	hoverIndex  int
	tooltip     tooltipState
	interact    *interaction
	// lastBoundsX/Y cache the auto-range derived from series the last
	// time we resolved axes, so Layout can detect when series changed.
	lastBoundsX [2]float32
	lastBoundsY [2]float32
	// lastOption is retained for incremental MergeOption updates.
	lastOption Option
}

// NewChart constructs a Chart with grid enabled by default and a
// theme-subscribed palette.
func NewChart() *Chart {
	c := &Chart{
		Container:   *NewContainer(nil),
		ShowGrid:    true,
		ShowTooltip: true,
		TitleLeft:   10,
		TitleTop:    10,
		autoPalette: true,
		hoverSeries: -1,
		hoverIndex:  -1,
	}
	c.SetSelf(c)
	c.interact = newInteraction(c)
	SubscribeTheme(func() {
		if c.autoPalette {
			c.Palette = nil
		}
		c.InvalidateLayout()
	})
	return c
}

// Handle routes mouse / scroll events through the interaction FSM.
// Keyboard shortcuts are reserved for wave 2.
func (c *Chart) Handle(event Event) bool {
	if me, ok := event.(MouseEvent); ok {
		return c.interact.handleMouse(me)
	}
	return false
}

// ResetAxes clears explicit axis ranges so the next Layout re-derives
// them from series bounds. Called from the double-click reset path.
func (c *Chart) ResetAxes() {
	if va, ok := c.XAxis.(*ValueAxis); ok {
		va.Min = 0
		va.Max = 0
	}
	if va, ok := c.YAxis.(*ValueAxis); ok {
		va.Min = 0
		va.Max = 0
	}
	c.InvalidateLayout()
}

func chartUsesFullBounds(series Series) bool {
	switch series.(type) {
	case *PieSeries, *RadarSeries, *FunnelSeries, *GaugeSeries, *GraphSeries, *TreeSeries, *TreemapSeries, *SunburstSeries, *SankeySeries, *ParallelSeries, *ThemeRiverSeries:
		return true
	default:
		return false
	}
}

// SetLegend installs the given legend as a child widget and wires its
// back-pointer to this chart. Passing nil removes any existing legend.
func (c *Chart) SetLegend(l *Legend) *Chart {
	// Remove any previously-attached legend.
	if c.Legend != nil {
		c.RemoveChild(c.Legend)
		c.Legend.chart = nil
	}
	c.Legend = l
	if l != nil {
		l.chart = c
		c.AddChild(l)
	}
	c.InvalidateLayout()
	return c
}

// AddSeries appends a series and assigns it a palette color when one
// isn't already set. Returns the chart for chaining.
func (c *Chart) AddSeries(s Series) *Chart {
	if s == nil {
		return c
	}
	if isZeroColor(s.Color()) {
		pal := c.resolvePalette()
		if len(pal) > 0 {
			s.SetColor(pal[len(c.Series)%len(pal)])
		}
	}
	c.Series = append(c.Series, s)
	c.InvalidateLayout()
	return c
}

// resolvePalette returns the effective palette, recomputing from theme
// when we're in auto mode and the cache is empty.
func (c *Chart) resolvePalette() []Color {
	if len(c.Palette) > 0 && !c.autoPalette {
		return c.Palette
	}
	if c.autoPalette && len(c.Palette) == 0 {
		c.Palette = PaletteFor(CurrentTheme())
	}
	return c.Palette
}

// Measure suggests a modest default chart footprint. The actual size
// is determined by the parent layout; we just report intrinsic min.
func (c *Chart) Measure(_ Size) Size {
	return Size{W: 280, H: 180}
}

// Layout resolves axes (auto-range if unset), reserves space for title
// and legend, then computes plotRect and lays out the legend.
func (c *Chart) Layout(r Rect) {
	c.BaseWidget.Layout(r)
	theme := CurrentTheme()
	var g PlotGutters
	if c.usesAxes() {
		g = defaultGutters(theme)
	}
	if c.Gutters != nil {
		if c.Gutters.Left > 0 {
			g.Left = c.Gutters.Left
		}
		if c.Gutters.Right > 0 {
			g.Right = c.Gutters.Right
		}
		if c.Gutters.Top > 0 {
			g.Top = c.Gutters.Top
		}
		if c.Gutters.Bottom > 0 {
			g.Bottom = c.Gutters.Bottom
		}
	}

	// Title reserves space at the top of the chart unless Grid supplied an
	// explicit plot rectangle, in which case ECharts semantics leave the
	// grid untouched.
	titleH := float32(0)
	titleSize := c.TitleFontSize
	if titleSize <= 0 {
		titleSize = theme.FontLarge
		if titleSize <= 0 {
			titleSize = 18
		}
	}
	if c.Title != "" {
		titleH = titleSize + 4
	}
	if c.SubTitle != "" {
		titleH += theme.FontSmall + 9
	}
	if titleH > 0 {
		c.titleRect = Rect{X: r.X + c.TitleLeft + 5, Y: r.Y + c.TitleTop + 2, W: r.W - c.TitleLeft - 5 - g.Right, H: titleH}
		if !c.fixedGutters {
			g.Top += titleH + 4
		}
	} else {
		c.titleRect = Rect{}
	}

	// Legend reserves space at bottom / right when attached.
	var legendSize Size
	if c.Legend != nil {
		legendSize = MeasureChild(c.Legend, Size{W: r.W, H: r.H})
		if !c.fixedGutters {
			switch c.Legend.Position {
			case LegendBottom:
				g.Bottom += legendSize.H + 4
			case LegendTop:
				g.Top += legendSize.H + 8
			case LegendRight:
				g.Right += legendSize.W + 8
			case LegendLeft:
				g.Left += legendSize.W + 8
			}
		}
	}

	// An end-positioned X axis name occupies space at the right edge of
	// the grid in ECharts. Reserve it explicitly so data coordinates and
	// tick positions match the browser renderer.
	if c.XAxis != nil && c.XAxis.Title() != "" {
		nameFont := Font{Family: c.FontFamily, Size: theme.FontSmall}
		if nameFont.Size <= 0 {
			nameFont.Size = 12
		}
		if nameW, _ := TextMetrics(c.XAxis.Title(), nameFont); nameW > 12 {
			g.Right += nameW - 12
		}
	}

	c.plotRect = Rect{
		X: r.X + g.Left,
		Y: r.Y + g.Top,
		W: r.W - g.Left - g.Right,
		H: r.H - g.Top - g.Bottom,
	}
	if c.plotRect.W < 0 {
		c.plotRect.W = 0
	}
	if c.plotRect.H < 0 {
		c.plotRect.H = 0
	}

	if c.Legend != nil {
		var legendRect Rect
		switch c.Legend.Position {
		case LegendBottom:
			y := r.Y + r.H - 10 - legendSize.H
			legendRect = Rect{X: r.X, Y: y, W: r.W, H: legendSize.H}
		case LegendRight:
			legendRect = Rect{
				X: c.plotRect.X + c.plotRect.W + 8,
				Y: c.plotRect.Y,
				W: legendSize.W,
				H: c.plotRect.H,
			}
		case LegendTop:
			legendRect = Rect{
				X: r.X,
				Y: c.plotRect.Y - legendSize.H - 8,
				W: r.W,
				H: legendSize.H,
			}
		case LegendLeft:
			legendRect = Rect{
				X: r.X + 4,
				Y: c.plotRect.Y,
				W: legendSize.W,
				H: c.plotRect.H,
			}
		default:
			legendRect = Rect{X: c.plotRect.X, Y: c.plotRect.Y + c.plotRect.H + 20, W: c.plotRect.W, H: legendSize.H}
		}
		c.Legend.Layout(legendRect)
	}

	if c.usesAxes() {
		c.ensureAxes()
	}
	c.resolveBarGrouping()
	c.resolveBoxplotGrouping()
	c.resolvePieSliceColors()
	c.resolveGraphColors()
	c.resolveTreemapColors()
	c.resolveSunburstColors()
	c.resolveSankeyColors()
	c.resolveThemeRiverColors()
}

// usesAxes reports whether any current series needs cartesian axes +
// grid. PieSeries renders radially and doesn't; everything else does.
// An empty chart is treated as axes-using so a blank frame still shows.
func (c *Chart) usesAxes() bool {
	if len(c.Series) == 0 {
		return true
	}
	for _, s := range c.Series {
		switch s.(type) {
		case *PieSeries, *RadarSeries, *FunnelSeries, *GaugeSeries, *GraphSeries, *TreeSeries, *TreemapSeries, *SunburstSeries, *SankeySeries, *ParallelSeries, *ThemeRiverSeries:
		default:
			return true
		}
	}
	return false
}

// resolvePieSliceColors fills in palette colors for any pie slice that
// the caller left at the zero value. Each series' slices cycle through
// the palette independently so two pies in the same chart don't share
// a shifted color ordering. Slices with an explicit color are left
// alone so user overrides win.
func (c *Chart) resolvePieSliceColors() {
	var palette []Color
	assign := func(slices []PieSlice) {
		if palette == nil {
			palette = c.resolvePalette()
		}
		if len(palette) == 0 {
			return
		}
		for i := range slices {
			if isZeroColor(slices[i].Color) {
				slices[i].Color = palette[i%len(palette)]
			}
		}
	}
	for _, s := range c.Series {
		switch series := s.(type) {
		case *PieSeries:
			assign(series.Slices)
		case *FunnelSeries:
			assign(series.Slices)
		}
	}
}

// ensureAxes creates default ValueAxes when missing and auto-ranges
// them from series data. If axes are supplied but have Min==Max==0
// (zero-value), we treat that as "auto" and compute from series too.
func (c *Chart) ensureAxes() {
	bx, by := c.dataBounds()
	if c.XAxis == nil {
		c.XAxis = &ValueAxis{}
	}
	if c.YAxis == nil {
		c.YAxis = &ValueAxis{InvertedAxis: true}
	}
	xmin, xmax := c.XAxis.Range()
	if xmin == 0 && xmax == 0 && bx.HasData {
		xLo := bx.Xmin
		xHi := bx.Xmax
		if !c.XAxis.IsCategorical() && !valueAxisScale(c.XAxis) {
			if bx.Xmin >= 0 {
				xLo = 0
			}
			if bx.Xmax <= 0 {
				xHi = 0
			}
		}
		c.XAxis.SetRange(xLo, xHi)
	}
	ymin, ymax := c.YAxis.Range()
	if va, ok := c.YAxis.(*ValueAxis); ok && (va.PinMin || va.PinMax) {
		// A pinned end stays; the other still follows the data.
		lo, hi := va.Min, va.Max
		if by.HasData {
			if !va.PinMin {
				lo = padLow(min(by.Ymin, hi), max(by.Ymax, hi))
			}
			if !va.PinMax {
				hi = padHigh(min(by.Ymin, lo), max(by.Ymax, lo))
			}
		}
		if hi > lo {
			va.SetRange(lo, hi)
		}
	} else if ymin == 0 && ymax == 0 && by.HasData {
		yLo := by.Ymin
		yHi := by.Ymax
		// ECharts value axes include zero by default when all values are on
		// one side of it. Bars do not add percentage padding; their visible
		// extent is already meaningful and is expanded only to nice ticks.
		if !valueAxisScale(c.YAxis) {
			if by.Ymin >= 0 {
				yLo = 0
			}
			if by.Ymax <= 0 {
				yHi = 0
			}
		}
		if c.hasBarSeries() {
			if by.Ymin < 0 {
				yLo = by.Ymin
			}
			if by.Ymax > 0 {
				yHi = by.Ymax
			}
		}
		c.YAxis.SetRange(yLo, yHi)
	}
	extendValueAxisToNiceTicks(c.XAxis)
	extendValueAxisToNiceTicks(c.YAxis)
	c.lastBoundsX = [2]float32{bx.Xmin, bx.Xmax}
	c.lastBoundsY = [2]float32{by.Ymin, by.Ymax}
}

func (c *Chart) hasBarSeries() bool {
	for _, s := range c.Series {
		switch s.(type) {
		case *BarSeries, *PictorialBarSeries:
			return true
		}
	}
	return false
}

func valueAxisScale(axis Axis) bool {
	if value, ok := axis.(*ValueAxis); ok {
		return value.Scale
	}
	return false
}

// extendValueAxisToNiceTicks expands auto-range endpoints to the next
// nice tick boundary. Pinned endpoints are preserved, matching ECharts'
// explicit min/max semantics.
func extendValueAxisToNiceTicks(axis Axis) {
	a, ok := axis.(*ValueAxis)
	if !ok || a.Max <= a.Min || a.Max-a.Min < 1e-12 {
		return
	}
	target := a.TickCount
	if target <= 0 {
		target = 7
	}
	ticks := graphs.NiceLinearTicks(float64(a.Min), float64(a.Max), target)
	if len(ticks) < 2 {
		return
	}
	step := ticks[1].Value - ticks[0].Value
	if step <= 0 {
		return
	}
	lo := float64(a.Min)
	hi := float64(a.Max)
	if !a.PinMin {
		lo = math.Floor(lo/step) * step
	}
	if !a.PinMax {
		hi = math.Ceil(hi/step) * step
	}
	if hi > lo {
		a.SetRange(float32(lo), float32(hi))
	}
}

// resolveBarGrouping walks the BarSeries list and populates
// GroupOffset / GroupTotal (for grouped bars) and StackBase (for
// stacked bars). Stacked series share a per-category cumulative sum;
// grouped series get sequential offsets.
func (c *Chart) resolveBarGrouping() {
	type stackKey struct {
		id     string
		series int
	}
	keys := make([]stackKey, 0)
	keyIndex := make(map[stackKey]int)
	bars := make([]*BarSeries, 0)
	for i, s := range c.Series {
		bs, ok := s.(*BarSeries)
		if !ok {
			continue
		}
		bars = append(bars, bs)
		key := stackKey{series: i}
		if bs.Stacked {
			key = stackKey{id: bs.StackID}
		}
		if _, exists := keyIndex[key]; !exists {
			keyIndex[key] = len(keys)
			keys = append(keys, key)
		}
	}
	groupTotal := len(keys)
	categoryGap := maxF32(35-4*float32(groupTotal), 15) / 100
	for _, bs := range bars {
		key := stackKey{series: -1}
		for i, s := range c.Series {
			if s == bs {
				key.series = i
				break
			}
		}
		if bs.Stacked {
			key = stackKey{id: bs.StackID}
		}
		bs.GroupOffset = keyIndex[key]
		bs.GroupTotal = groupTotal
		bs.CategoryGap = categoryGap
	}

	runningByStack := make(map[string][]float32)
	for _, bs := range bars {
		if !bs.Stacked {
			continue
		}
		running := runningByStack[bs.StackID]
		if len(running) < len(bs.Values) {
			running = append(running, make([]float32, len(bs.Values)-len(running))...)
		}
		base := make([]float32, len(bs.Values))
		copy(base, running)
		bs.StackBase = base
		for i, v := range bs.Values {
			running[i] += v
		}
		runningByStack[bs.StackID] = running
	}
}

// resolveThemeRiverColors assigns one palette color per stream layer in
// first-appearance order.
func (c *Chart) resolveThemeRiverColors() {
	palette := c.resolvePalette()
	if len(palette) == 0 {
		return
	}
	for _, series := range c.Series {
		river, ok := series.(*ThemeRiverSeries)
		if !ok {
			continue
		}
		names := river.layerNames()
		if len(river.Colors) != len(names) {
			river.Colors = make([]Color, len(names))
		}
		for i := range names {
			if isZeroColor(river.Colors[i]) {
				river.Colors[i] = palette[i%len(palette)]
			}
		}
	}
}

// resolveSankeyColors assigns a palette color per sankey node unless the
// data item supplied an explicit color.
func (c *Chart) resolveSankeyColors() {
	palette := c.resolvePalette()
	if len(palette) == 0 {
		return
	}
	for _, series := range c.Series {
		sankey, ok := series.(*SankeySeries)
		if !ok {
			continue
		}
		values := sankey.nodeValues()
		lo, hi := float32(math.Inf(1)), float32(-math.Inf(1))
		for _, value := range values {
			lo = minF32(lo, value)
			hi = maxF32(hi, value)
		}
		for i := range sankey.Nodes {
			if !isZeroColor(sankey.Nodes[i].Color) {
				continue
			}
			t := float32(0)
			if hi > lo {
				t = (values[i] - lo) / (hi - lo)
			}
			pos := t * float32(len(palette)-1)
			index := int(pos)
			if index >= len(palette)-1 {
				sankey.Nodes[i].Color = palette[len(palette)-1]
			} else {
				sankey.Nodes[i].Color = mixColor(palette[index], palette[index+1], pos-float32(index))
			}
		}
	}
}

// resolveSunburstColors assigns palette colors to top-level branches.
func (c *Chart) resolveSunburstColors() {
	palette := c.resolvePalette()
	if len(palette) == 0 {
		return
	}
	for _, series := range c.Series {
		sun, ok := series.(*SunburstSeries)
		if !ok {
			continue
		}
		if len(sun.Colors) != len(sun.Roots) {
			sun.Colors = make([]Color, len(sun.Roots))
		}
		for i := range sun.Roots {
			if isZeroColor(sun.Colors[i]) {
				sun.Colors[i] = palette[i%len(palette)]
			}
		}
	}
}

// resolveTreemapColors assigns one palette color per top-level treemap
// branch, matching ECharts' default level-0 color mapping.
func (c *Chart) resolveTreemapColors() {
	palette := c.resolvePalette()
	if len(palette) == 0 {
		return
	}
	for _, series := range c.Series {
		tree, ok := series.(*TreemapSeries)
		if !ok {
			continue
		}
		if len(tree.Colors) != len(tree.Roots) {
			tree.Colors = make([]Color, len(tree.Roots))
		}
		for i := range tree.Roots {
			if isZeroColor(tree.Colors[i]) {
				tree.Colors[i] = palette[i%len(palette)]
			}
		}
	}
}

// resolveGraphColors assigns a palette color to every graph node. The
// graph renderer additionally stores per-node colors because ECharts'
// default graph color encoding varies by data item.
func (c *Chart) resolveGraphColors() {
	palette := c.resolvePalette()
	if len(palette) == 0 {
		return
	}
	for _, series := range c.Series {
		graph, ok := series.(*GraphSeries)
		if !ok {
			continue
		}
		if len(graph.NodeColors) != len(graph.Nodes) {
			graph.NodeColors = make([]Color, len(graph.Nodes))
		}
		// ECharts graph defaults to one series color for all nodes. Node
		// categories can override this once category palettes are enabled.
		for i := range graph.Nodes {
			if isZeroColor(graph.NodeColors[i]) {
				graph.NodeColors[i] = graph.Color()
			}
		}
	}
}

// resolveBoxplotGrouping assigns each boxplot series a slot in its
// category band, using the same 80%-minus-2px available width and 30%
// inter-series gap as ECharts.
func (c *Chart) resolveBoxplotGrouping() {
	total := 0
	for _, series := range c.Series {
		if _, ok := series.(*BoxplotSeries); ok {
			total++
		}
	}
	offset := 0
	for _, series := range c.Series {
		box, ok := series.(*BoxplotSeries)
		if !ok {
			continue
		}
		box.GroupOffset = offset
		box.GroupTotal = total
		offset++
	}
}

// dataBounds unions bounds across visible series. XBounds uses Xmin/Xmax,
// YBounds uses Ymin/Ymax — the return-type trick here is just two copies
// of SeriesBounds because the fields are the same and the caller filters.
func (c *Chart) dataBounds() (SeriesBounds, SeriesBounds) {
	var bx, by SeriesBounds
	for _, s := range c.Series {
		if !s.Visible() {
			continue
		}
		b := s.DataBounds()
		if !b.HasData {
			continue
		}
		if !bx.HasData {
			bx = b
			by = b
			continue
		}
		if b.Xmin < bx.Xmin {
			bx.Xmin = b.Xmin
		}
		if b.Xmax > bx.Xmax {
			bx.Xmax = b.Xmax
		}
		if b.Ymin < by.Ymin {
			by.Ymin = b.Ymin
		}
		if b.Ymax > by.Ymax {
			by.Ymax = b.Ymax
		}
	}

	// Stacked bars overlap, so their individual value bounds are not
	// enough: the Y domain must include each category's cumulative sum.
	runningByStack := make(map[string][]float32)
	for _, s := range c.Series {
		bs, ok := s.(*BarSeries)
		if !ok || !bs.Visible() || !bs.Stacked {
			continue
		}
		running := runningByStack[bs.StackID]
		if len(running) < len(bs.Values) {
			running = append(running, make([]float32, len(bs.Values)-len(running))...)
		}
		for i, v := range bs.Values {
			running[i] += v
			if !by.HasData || running[i] < by.Ymin {
				by.Ymin = running[i]
			}
			if !by.HasData || running[i] > by.Ymax {
				by.Ymax = running[i]
			}
			by.HasData = true
		}
		runningByStack[bs.StackID] = running
	}
	return bx, by
}

// padLow / padHigh expand an auto-range by ~5% so data doesn't hug the
// plot edges. Constant-value series (min==max) get a synthesized ±1
// window so the axis still has a visible span.
func padLow(min, max float32) float32 {
	if max <= min {
		return min - 1
	}
	return min - (max-min)*0.05
}

func padHigh(min, max float32) float32 {
	if max <= min {
		return max + 1
	}
	return max + (max-min)*0.05
}

// Draw paints the chart: background → title → grid → series (clipped)
// → axes → children (legend, etc.). Call order matters: grid under
// series, axes over series.
func (c *Chart) Draw(canvas Canvas) {
	theme := CurrentTheme()
	bounds := c.Bounds()
	if c.Background != nil {
		if c.Background.A > 0 {
			canvas.FillRect(bounds, *c.Background)
		}
	} else if theme.Surface.A > 0 {
		canvas.FillRoundedRect(bounds, theme.RadiusMedium, theme.Surface)
	}

	titleSize := c.TitleFontSize
	if titleSize <= 0 {
		titleSize = theme.FontLarge
		if titleSize <= 0 {
			titleSize = 18
		}
	}
	titleColor := c.TitleColor
	if titleColor.A == 0 {
		titleColor = theme.Text
	}
	subColor := c.SubTitleColor
	if subColor.A == 0 {
		subColor = theme.TextMuted
	}
	if c.Title != "" {
		titleFont := Font{Family: c.FontFamily, Size: titleSize, Weight: c.TitleFontWeight}
		titleRow := c.titleRect
		titleRow.H = titleSize + 4
		canvas.DrawText(c.Title, titleRow, titleColor, titleFont)
	}
	if c.SubTitle != "" {
		subFont := Font{Family: c.FontFamily, Size: theme.FontSmall}
		sub := c.titleRect
		sub.X -= 1
		sub.Y += titleSize + 11
		sub.H = subFont.Size + 2
		canvas.DrawText(c.SubTitle, sub, subColor, subFont)
	}

	if c.plotRect.W <= 0 || c.plotRect.H <= 0 {
		return
	}

	if c.ShowGrid && c.XAxis != nil && c.YAxis != nil && c.usesAxes() {
		drawGrid(canvas, c.plotRect, c.XAxis, c.YAxis, theme, c.GridColor)
	}

	// Clip cartesian series to the plot rectangle. Radial, hierarchical
	// and graph series use the whole chart bounds and must not be clipped
	// by title/legend gutters.
	for i, s := range c.Series {
		if !s.Visible() {
			continue
		}
		hot := NoHotSpot
		if c.hoverSeries == i {
			hot = HotSpot{SeriesIdx: i, PointIdx: c.hoverIndex}
		}
		seriesRect := c.plotRect
		clipRect := c.plotRect
		if chartUsesFullBounds(s) {
			seriesRect = c.Bounds()
			clipRect = c.Bounds()
		}
		depth := canvas.Save()
		canvas.ClipRect(clipRect)
		s.Draw(canvas, seriesRect, c.XAxis, c.YAxis, hot)
		canvas.RestoreTo(depth)
	}

	if c.XAxis != nil && c.YAxis != nil && c.usesAxes() {
		drawAxes(canvas, c.plotRect, c.XAxis, c.YAxis, theme, c.FontFamily, c.AxisColor, c.AxisLabelColor, c.TextColor)
	}

	// Paint children (legend, etc.) on top via the Container path.
	for _, child := range c.Container.ChildList() {
		child.Draw(canvas)
	}

	// Tooltip goes last so it sits above everything.
	drawTooltipInline(canvas, c.tooltip, c.plotRect, theme, c.FontFamily)
}

// HoverAt is exposed for tests + for the interaction FSM that ships in
// Commit 4. It hit-tests every series top-to-bottom and, on a hit,
// updates tooltip state and hover tracking. index == -1 clears hover.
func (c *Chart) HoverAt(p Point) (seriesIdx, pointIdx int) {
	for i := len(c.Series) - 1; i >= 0; i-- {
		s := c.Series[i]
		if !s.Visible() {
			continue
		}
		seriesRect := c.plotRect
		if chartUsesFullBounds(s) {
			seriesRect = c.Bounds()
		}
		if idx := s.HitTest(p, seriesRect, c.XAxis, c.YAxis); idx >= 0 {
			c.setHover(i, idx, p)
			return i, idx
		}
	}
	c.clearHover()
	return -1, -1
}

func (c *Chart) setHover(seriesIdx, pointIdx int, anchor Point) {
	changed := c.hoverSeries != seriesIdx || c.hoverIndex != pointIdx
	c.hoverSeries = seriesIdx
	c.hoverIndex = pointIdx
	if changed && c.OnHover != nil {
		c.OnHover(c.Series[seriesIdx], pointIdx)
	}
	text := ""
	if c.ShowTooltip {
		switch {
		case c.TooltipTrigger == "axis" && c.AxisTooltipBuilder != nil:
			text = c.AxisTooltipBuilder(pointIdx)
		case c.TooltipBuilder != nil:
			text = c.TooltipBuilder(c.Series[seriesIdx], pointIdx)
		default:
			text = DefaultTooltipText(c.Series[seriesIdx], pointIdx)
		}
	}
	prev := c.tooltip
	c.tooltip = tooltipState{active: text != "", text: text, anchor: anchor}
	if changed || prev != c.tooltip {
		c.Invalidate()
	}
}

func (c *Chart) clearHover() {
	had := c.hoverSeries >= 0 || c.tooltip.active
	if c.hoverSeries >= 0 && c.OnHover != nil {
		c.OnHover(nil, -1)
	}
	c.hoverSeries = -1
	c.hoverIndex = -1
	c.tooltip = tooltipState{}
	if had {
		c.Invalidate()
	}
}

// PlotRect exposes the plot area for tests and interaction logic.
func (c *Chart) PlotRect() Rect { return c.plotRect }

// Tick is a no-op today; interaction-driven animations hook here later.
func (c *Chart) Tick(_ time.Time) Rect { return Rect{} }

// HitTest lets children (e.g. Legend) claim the pointer first, then
// falls back to returning the chart itself when the point is inside
// the plot rect (for pan/zoom capture) or the chart bounds.
func (c *Chart) HitTest(p Point) Widget {
	for i := c.ChildCount() - 1; i >= 0; i-- {
		if hit := c.ChildAt(i).HitTest(p); hit != nil {
			return hit
		}
	}
	if c.plotRect.Contains(p) || c.Bounds().Contains(p) {
		if s := c.Self(); s != nil {
			return s
		}
		return c
	}
	return nil
}
