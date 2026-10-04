package graphs

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// interaction is the pan / zoom / hover FSM driving Chart.Handle.
// State is tied to the chart instance; each Chart has one.
type interaction struct {
	chart *Chart

	dragging  bool
	dragStart Point
	// origXMin/Max snapshot the axis ranges at drag start so MouseMove
	// can compute absolute deltas instead of integrating per-frame drift.
	origXMin, origXMax float32
	origYMin, origYMax float32

	// lastClickAt lets us detect double-click for "reset axes".
	lastClickAt    time.Time
	clickThreshold time.Duration
}

func newInteraction(c *Chart) *interaction {
	return &interaction{chart: c, clickThreshold: 350 * time.Millisecond}
}

// handleMouse is the Chart.Handle entry point. Returns whether the
// event was consumed.
func (i *interaction) handleMouse(e MouseEvent) bool {
	c := i.chart
	p := Point{X: e.X, Y: e.Y}
	switch e.Type() {
	case EventMouseDown:
		if e.Button != MouseButtonLeft {
			return false
		}
		if !c.plotRect.Contains(p) {
			return false
		}
		// Double-click reset only makes sense when the user can actually
		// pan or zoom the axes away from the auto-range.
		if c.EnableZoom || c.EnablePan {
			now := e.Timestamp()
			if !i.lastClickAt.IsZero() && now.Sub(i.lastClickAt) < i.clickThreshold {
				c.ResetAxes()
				i.lastClickAt = time.Time{}
				return true
			}
			i.lastClickAt = now
		}
		if c.EnablePan {
			i.dragging = true
			i.dragStart = p
			i.origXMin, i.origXMax = c.XAxis.Range()
			i.origYMin, i.origYMax = c.YAxis.Range()
			return true
		}
		if c.EnableSelect {
			// Track the press location so MouseUp can distinguish a
			// click-in-place from a slop drag even without pan enabled.
			i.dragStart = p
			return true
		}
		return false
	case EventMouseMove:
		if i.dragging {
			i.pan(p)
			return true
		}
		c.HoverAt(p)
		return false
	case EventMouseUp:
		wasDrag := i.dragging
		i.dragging = false
		if wasDrag {
			// Only fire OnSelect when the mouse didn't travel far — otherwise
			// the up was ending a pan, not clicking a point.
			dx := p.X - i.dragStart.X
			dy := p.Y - i.dragStart.Y
			if dx*dx+dy*dy < 16 && c.EnableSelect { // 4px threshold
				if c.hoverSeries >= 0 && c.OnSelect != nil {
					c.OnSelect(c.Series[c.hoverSeries], c.hoverIndex)
				}
			}
			return true
		}
		if c.EnableSelect && c.hoverSeries >= 0 && c.OnSelect != nil {
			c.OnSelect(c.Series[c.hoverSeries], c.hoverIndex)
			return true
		}
		return false
	case EventScroll:
		if !c.EnableZoom {
			return false
		}
		if !c.plotRect.Contains(p) {
			return false
		}
		i.zoom(p, e.DeltaY, e.Mods)
		return true
	case EventMouseLeave:
		c.clearHover()
		return false
	}
	return false
}

// pan shifts both axes so the cursor's data-value stays locked to the
// drag-start point. Category axes don't pan (their range is fixed).
func (i *interaction) pan(p Point) {
	c := i.chart
	plot := c.plotRect
	if plot.W <= 0 || plot.H <= 0 {
		return
	}
	if !c.XAxis.IsCategorical() {
		dx := p.X - i.dragStart.X
		xSpan := i.origXMax - i.origXMin
		shiftX := -dx / plot.W * xSpan
		c.XAxis.SetRange(i.origXMin+shiftX, i.origXMax+shiftX)
	}
	if !c.YAxis.IsCategorical() {
		dy := p.Y - i.dragStart.Y
		ySpan := i.origYMax - i.origYMin
		// Y axes are typically inverted (up=larger). Adjust sign so a
		// downward drag pans toward smaller values, matching intuition.
		var shiftY float32
		if c.YAxis.Inverted() {
			shiftY = dy / plot.H * ySpan
		} else {
			shiftY = -dy / plot.H * ySpan
		}
		c.YAxis.SetRange(i.origYMin+shiftY, i.origYMax+shiftY)
	}
	c.InvalidateLayout()
}

// zoom narrows / widens the axis ranges around the cursor so the data
// value under the cursor stays put while the rest of the chart scales.
// Shift modifier targets the Y axis; default is X.
func (i *interaction) zoom(p Point, deltaY float32, mods Modifiers) {
	c := i.chart
	plot := c.plotRect
	if plot.W <= 0 || plot.H <= 0 {
		return
	}
	factor := float32(1) + deltaY*0.1
	if factor < 0.1 {
		factor = 0.1
	}
	if factor > 10 {
		factor = 10
	}

	if mods&ModShift != 0 {
		if c.YAxis.IsCategorical() {
			return
		}
		ymin, ymax := c.YAxis.Range()
		anchor := c.YAxis.PixelToValue(p.Y-plot.Y, plot.H)
		newMin := anchor - (anchor-ymin)*factor
		newMax := anchor + (ymax-anchor)*factor
		c.YAxis.SetRange(newMin, newMax)
	} else {
		if c.XAxis.IsCategorical() {
			return
		}
		xmin, xmax := c.XAxis.Range()
		anchor := c.XAxis.PixelToValue(p.X-plot.X, plot.W)
		newMin := anchor - (anchor-xmin)*factor
		newMax := anchor + (xmax-anchor)*factor
		c.XAxis.SetRange(newMin, newMax)
	}
	c.InvalidateLayout()
}
