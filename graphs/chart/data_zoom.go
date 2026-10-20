package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// DataZoomOption configures an initial visible window and, for slider/inside
// modes, the interaction affordance. XAxisIndex is reserved for the
// multi-axis extension; the current chart applies dataZoom to its primary X
// axis. Start/End are percentages in [0,100].
type DataZoomOption struct {
	Type       string   `json:"type,omitempty"` // slider (default) or inside
	XAxisIndex int      `json:"xAxisIndex,omitempty"`
	Start      *float64 `json:"start,omitempty"`
	End        *float64 `json:"end,omitempty"`
	MinValue   *float64 `json:"minValue,omitempty"`
	MaxValue   *float64 `json:"maxValue,omitempty"`
	Show       *bool    `json:"show,omitempty"`
	Height     float32  `json:"height,omitempty"`
	Bottom     float32  `json:"bottom,omitempty"`
}

func (c *Chart) hasDataZoomSlider() bool {
	for _, zoom := range c.DataZoom {
		if dataZoomType(zoom) == "slider" && boolDefault(zoom.Show, true) {
			return true
		}
	}
	return false
}

func (c *Chart) hasInsideDataZoom() bool {
	for _, zoom := range c.DataZoom {
		if dataZoomType(zoom) == "inside" {
			return true
		}
	}
	return false
}

func dataZoomType(option DataZoomOption) string {
	if option.Type == "" {
		return "slider"
	}
	return option.Type
}

// DataZoomRect exposes the slider hit/paint rect. The returned rect is the
// visible track; interaction code uses a padded hit area around it.
func (c *Chart) DataZoomRect() Rect { return c.dataZoomRect }

func (c *Chart) layoutDataZoom(r Rect) {
	c.dataZoomRect = Rect{}
	if !c.hasDataZoomSlider() || c.plotRect.IsEmpty() {
		return
	}
	height := float32(8)
	for _, zoom := range c.DataZoom {
		if dataZoomType(zoom) == "slider" && boolDefault(zoom.Show, true) && zoom.Height > 0 {
			height = zoom.Height
			break
		}
	}
	bottom := float32(10)
	for _, zoom := range c.DataZoom {
		if dataZoomType(zoom) == "slider" && boolDefault(zoom.Show, true) && zoom.Bottom > 0 {
			bottom = zoom.Bottom
			break
		}
	}
	c.dataZoomRect = Rect{
		X: c.plotRect.X,
		Y: r.Y + r.H - bottom - height,
		W: c.plotRect.W,
		H: height,
	}
}

func (c *Chart) applyPendingDataZoom() {
	if !c.pendingDataZoom {
		return
	}
	c.pendingDataZoom = false
	if c.XAxis == nil || c.XAxis.IsCategorical() {
		return
	}
	baseLo, baseHi := c.XAxis.Range()
	if baseHi <= baseLo {
		return
	}
	c.dataZoomBaseMin, c.dataZoomBaseMax = baseLo, baseHi
	for _, zoom := range c.DataZoom {
		lo, hi := baseLo, baseHi
		if zoom.MinValue != nil {
			lo = float32(*zoom.MinValue)
		} else if zoom.Start != nil {
			lo = baseLo + (baseHi-baseLo)*clampf(float32(*zoom.Start)/100, 0, 1)
		}
		if zoom.MaxValue != nil {
			hi = float32(*zoom.MaxValue)
		} else if zoom.End != nil {
			hi = baseLo + (baseHi-baseLo)*clampf(float32(*zoom.End)/100, 0, 1)
		}
		if hi < lo {
			lo, hi = hi, lo
		}
		if hi > lo {
			c.XAxis.SetRange(lo, hi)
		}
	}
}

func (c *Chart) drawDataZoomSlider(canvas Canvas, theme *Theme) {
	rect := c.dataZoomRect
	if rect.IsEmpty() || c.XAxis == nil || c.XAxis.IsCategorical() {
		return
	}
	baseLo, baseHi := c.dataZoomBaseMin, c.dataZoomBaseMax
	if baseHi <= baseLo {
		return
	}
	lo, hi := c.XAxis.Range()
	start := clampf((lo-baseLo)/(baseHi-baseLo), 0, 1)
	end := clampf((hi-baseLo)/(baseHi-baseLo), 0, 1)
	if end < start {
		start, end = end, start
	}
	track := Rect{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H}
	trackColor := theme.TextMuted
	trackColor.A *= .25
	canvas.FillRoundedRect(track, rect.H/2, trackColor)
	selected := Rect{
		X: rect.X + rect.W*start,
		Y: rect.Y,
		W: maxF32(rect.W*(end-start), rect.H),
		H: rect.H,
	}
	accent := theme.Accent
	if accent.A == 0 {
		accent = c.AxisColor
	}
	accent.A = .55
	canvas.FillRoundedRect(selected, rect.H/2, accent)
	handleW := maxF32(rect.H*.65, 5)
	handleColor := theme.Surface
	handleBorder := theme.TextMuted
	for _, x := range []float32{rect.X + rect.W*start, rect.X + rect.W*end} {
		handle := Rect{X: x - handleW/2, Y: rect.Y - 3, W: handleW, H: rect.H + 6}
		canvas.FillRoundedRect(handle, 3, handleColor)
		canvas.StrokeRoundedRect(handle, 3, handleBorder, 1)
	}
}

func (c *Chart) dataZoomSliderHit(p Point) bool {
	rect := c.dataZoomRect
	if rect.IsEmpty() {
		return false
	}
	return p.X >= rect.X-4 && p.X <= rect.X+rect.W+4 && p.Y >= rect.Y-8 && p.Y <= rect.Y+rect.H+8
}

func (c *Chart) setDataZoomFromSliderPixels(x0, x1 float32) {
	rect := c.dataZoomRect
	if rect.IsEmpty() || rect.W <= 0 || c.XAxis == nil || c.XAxis.IsCategorical() {
		return
	}
	baseLo, baseHi := c.dataZoomBaseMin, c.dataZoomBaseMax
	if baseHi <= baseLo {
		return
	}
	t0 := clampf((x0-rect.X)/rect.W, 0, 1)
	t1 := clampf((x1-rect.X)/rect.W, 0, 1)
	if t1 < t0 {
		t0, t1 = t1, t0
	}
	if t1-t0 < .01 {
		return
	}
	lo := baseLo + (baseHi-baseLo)*t0
	hi := baseLo + (baseHi-baseLo)*t1
	if math.Abs(float64(hi-lo)) < 1e-6 {
		return
	}
	c.XAxis.SetRange(lo, hi)
	c.Invalidate()
}
