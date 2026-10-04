package graphs

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// Chart is the top-level graph widget. It owns a set of series, up to
// two axes (X + Y), an optional title/subtitle, and an optional legend.
// Chart subclasses Container so the Legend participates in the widget
// tree and dispatch naturally.
//
// Usage:
//
//	c := graphs.NewChart()
//	c.Title = "Revenue"
//	c.AddSeries(graphs.NewLineSeries("Q1", points))
//	// c is a normal qui.Widget; add it to any container/layout.
type Chart struct {
	Container

	Title    string
	SubTitle string
	XAxis    Axis // nil → auto ValueAxis on first Layout from series bounds
	YAxis    Axis // nil → auto ValueAxis; defaults Inverted so larger = up
	Series   []Series
	Legend   *Legend // nil disables; use SetLegend to attach
	Palette  []Color
	ShowGrid bool

	// EnableZoom gates scroll-wheel zoom (and double-click-to-reset).
	// EnablePan gates left-button drag-to-pan. EnableSelect gates
	// click-to-select (OnSelect firing). All default false — the chart
	// is a passive viewer unless the host opts in.
	EnableZoom   bool
	EnablePan    bool
	EnableSelect bool

	OnSelect func(series Series, index int)
	OnHover  func(series Series, index int) // index=-1 on leave

	// TooltipBuilder customizes tooltip text for a (series, index) pair.
	// nil uses DefaultTooltipText, which renders "name: (x, y)" etc.
	TooltipBuilder func(series Series, index int) string

	// autoPalette is true when Palette was not set explicitly; we then
	// derive it from the active theme and re-derive on theme change.
	autoPalette bool
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
}

// NewChart constructs a Chart with grid enabled by default and a
// theme-subscribed palette.
func NewChart() *Chart {
	c := &Chart{
		Container:   *NewContainer(nil),
		ShowGrid:    true,
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

	// Title reserves space at the top of the chart.
	titleH := float32(0)
	if c.Title != "" {
		titleH = theme.FontLarge + 4
		if titleH <= 0 {
			titleH = 22
		}
	}
	if c.SubTitle != "" {
		titleH += theme.FontSmall + 2
	}
	if titleH > 0 {
		c.titleRect = Rect{X: r.X + g.Left, Y: r.Y + 4, W: r.W - g.Left - g.Right, H: titleH}
		g.Top += titleH + 4
	} else {
		c.titleRect = Rect{}
	}

	// Legend reserves space at bottom / right when attached.
	var legendSize Size
	if c.Legend != nil {
		legendSize = c.Legend.Measure(Size{W: r.W, H: r.H})
		switch c.Legend.Position {
		case LegendBottom, LegendTop:
			g.Bottom += legendSize.H + 4
		case LegendRight, LegendLeft:
			g.Right += legendSize.W + 8
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
			legendRect = Rect{
				X: c.plotRect.X,
				Y: c.plotRect.Y + c.plotRect.H + 20,
				W: c.plotRect.W,
				H: legendSize.H,
			}
		case LegendRight:
			legendRect = Rect{
				X: c.plotRect.X + c.plotRect.W + 8,
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
	c.resolvePieSliceColors()
}

// usesAxes reports whether any current series needs cartesian axes +
// grid. PieSeries renders radially and doesn't; everything else does.
// An empty chart is treated as axes-using so a blank frame still shows.
func (c *Chart) usesAxes() bool {
	if len(c.Series) == 0 {
		return true
	}
	for _, s := range c.Series {
		if _, isPie := s.(*PieSeries); !isPie {
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
	for _, s := range c.Series {
		ps, ok := s.(*PieSeries)
		if !ok {
			continue
		}
		if palette == nil {
			palette = c.resolvePalette()
		}
		if len(palette) == 0 {
			return
		}
		for i := range ps.Slices {
			if isZeroColor(ps.Slices[i].Color) {
				ps.Slices[i].Color = palette[i%len(palette)]
			}
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
		c.XAxis.SetRange(padLow(bx.Xmin, bx.Xmax), padHigh(bx.Xmin, bx.Xmax))
	}
	ymin, ymax := c.YAxis.Range()
	if ymin == 0 && ymax == 0 && by.HasData {
		c.YAxis.SetRange(padLow(by.Ymin, by.Ymax), padHigh(by.Ymin, by.Ymax))
	}
	c.lastBoundsX = [2]float32{bx.Xmin, bx.Xmax}
	c.lastBoundsY = [2]float32{by.Ymin, by.Ymax}
}

// resolveBarGrouping walks the BarSeries list and populates
// GroupOffset / GroupTotal (for grouped bars) and StackBase (for
// stacked bars). Stacked series share a per-category cumulative sum;
// grouped series get sequential offsets.
func (c *Chart) resolveBarGrouping() {
	var grouped []*BarSeries
	var stacked []*BarSeries
	for _, s := range c.Series {
		bs, ok := s.(*BarSeries)
		if !ok {
			continue
		}
		if bs.Stacked {
			stacked = append(stacked, bs)
		} else {
			grouped = append(grouped, bs)
		}
	}
	for i, bs := range grouped {
		bs.GroupOffset = i
		bs.GroupTotal = len(grouped)
	}
	if len(stacked) == 0 {
		return
	}
	// Per-category cumulative base for stacked bars.
	n := 0
	for _, bs := range stacked {
		if len(bs.Values) > n {
			n = len(bs.Values)
		}
	}
	running := make([]float32, n)
	for _, bs := range stacked {
		base := make([]float32, len(bs.Values))
		for i := range bs.Values {
			base[i] = running[i]
		}
		bs.StackBase = base
		for i, v := range bs.Values {
			running[i] += v
		}
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
	if theme.Surface.A > 0 {
		canvas.FillRoundedRect(bounds, theme.RadiusMedium, theme.Surface)
	}

	if c.Title != "" {
		titleFont := Font{Size: theme.FontLarge}
		titleRow := c.titleRect
		titleRow.H = theme.FontLarge + 4
		canvas.DrawText(c.Title, titleRow, theme.Text, titleFont)
	}
	if c.SubTitle != "" {
		subFont := Font{Size: theme.FontSmall}
		sub := c.titleRect
		sub.Y += theme.FontLarge + 4
		sub.H = subFont.Size + 2
		canvas.DrawText(c.SubTitle, sub, theme.TextMuted, subFont)
	}

	if c.plotRect.W <= 0 || c.plotRect.H <= 0 {
		return
	}

	if c.ShowGrid && c.XAxis != nil && c.YAxis != nil && c.usesAxes() {
		drawGrid(canvas, c.plotRect, c.XAxis, c.YAxis, theme)
	}

	// Clip series to the plot rectangle.
	plotID := canvas.Save()
	canvas.ClipRect(c.plotRect)
	for i, s := range c.Series {
		if !s.Visible() {
			continue
		}
		hot := NoHotSpot
		if c.hoverSeries == i {
			hot = HotSpot{SeriesIdx: i, PointIdx: c.hoverIndex}
		}
		s.Draw(canvas, c.plotRect, c.XAxis, c.YAxis, hot)
	}
	canvas.RestoreTo(plotID)

	if c.XAxis != nil && c.YAxis != nil && c.usesAxes() {
		drawAxes(canvas, c.plotRect, c.XAxis, c.YAxis, theme)
	}

	// Paint children (legend, etc.) on top via the Container path.
	for _, child := range c.Container.ChildList() {
		child.Draw(canvas)
	}

	// Tooltip goes last so it sits above everything.
	drawTooltipInline(canvas, c.tooltip, c.plotRect, theme)
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
		if idx := s.HitTest(p, c.plotRect, c.XAxis, c.YAxis); idx >= 0 {
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
	if c.TooltipBuilder != nil {
		text = c.TooltipBuilder(c.Series[seriesIdx], pointIdx)
	} else {
		text = DefaultTooltipText(c.Series[seriesIdx], pointIdx)
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
