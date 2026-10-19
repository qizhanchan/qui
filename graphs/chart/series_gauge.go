package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// GaugeSeries renders an ECharts-style circular gauge. Its geometry and
// defaults mirror the common ECharts gauge: a 270° neutral track, major
// split lines, minor ticks, numeric labels, a tapered pointer, the data
// item name above the center and a bold detail value below it.
type GaugeSeries struct {
	seriesBase
	Value, Min, Max      float32
	StartAngle, EndAngle float32
	Clockwise            bool
	RadiusPercent        float32
	CenterXPercent       float32
	CenterYPercent       float32
	SplitCount           int
	Title                string
	FontFamily           string
}

func NewGaugeSeries(name string, value float32) *GaugeSeries {
	return &GaugeSeries{
		seriesBase:     newSeriesBase(name),
		Value:          value,
		Min:            0,
		Max:            100,
		StartAngle:     225,
		EndAngle:       -45,
		Clockwise:      true,
		RadiusPercent:  .75,
		CenterXPercent: .5,
		CenterYPercent: .5,
		SplitCount:     10,
	}
}

func (s *GaugeSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func (s *GaugeSeries) center(plot Rect) (float32, float32) {
	cxPct, cyPct := s.CenterXPercent, s.CenterYPercent
	if cxPct <= 0 {
		cxPct = .5
	}
	if cyPct <= 0 {
		cyPct = .5
	}
	return plot.X + plot.W*cxPct, plot.Y + plot.H*cyPct
}

func (s *GaugeSeries) radius(plot Rect) float32 {
	ratio := s.RadiusPercent
	if ratio <= 0 {
		ratio = .75
	}
	return minF32(plot.W, plot.H) * .5 * ratio
}

func normalizeDegrees(value float32) float32 {
	value = float32(math.Mod(float64(value), 360))
	if value < 0 {
		value += 360
	}
	return value
}

func (s *GaugeSeries) screenAngles() (float32, float32) {
	start := normalizeDegrees(-s.StartAngle)
	end := normalizeDegrees(-s.EndAngle)
	if s.Clockwise {
		span := normalizeDegrees(end - start)
		if span == 0 {
			span = 360
		}
		return start, start + span
	}
	span := normalizeDegrees(start - end)
	if span == 0 {
		span = 360
	}
	return start, start - span
}

func (s *GaugeSeries) valueRatio() float32 {
	if s.Max <= s.Min {
		return 0
	}
	return clampf((s.Value-s.Min)/(s.Max-s.Min), 0, 1)
}

func (s *GaugeSeries) Draw(canvas Canvas, plot Rect, _, _ Axis, _ HotSpot) {
	cx, cy := s.center(plot)
	radius := s.radius(plot)
	if radius <= 0 {
		return
	}
	start, end := s.screenAngles()
	span := end - start
	ratio := s.valueRatio()

	axisWidth := float32(10)
	track := Color{R: 0xe8 / 255.0, G: 0xeb / 255.0, B: 0xf0 / 255.0, A: 1}
	drawGaugeBand(canvas, cx, cy, radius-axisWidth, radius, start, end, track)

	major := Color{R: 0x54 / 255.0, G: 0x55 / 255.0, B: 0x5a / 255.0, A: 1}
	minor := Color{R: 0x6d / 255.0, G: 0x6e / 255.0, B: 0x73 / 255.0, A: 1}
	labelColor := major
	splits := s.SplitCount
	if splits <= 0 {
		splits = 10
	}
	const (
		axisGap           = float32(10)
		splitLength       = float32(10)
		labelDistance     = float32(15)
		minorLength       = float32(6)
		minorSubdivisions = 5
	)
	axisOuter := radius - axisWidth
	for i := 0; i <= splits; i++ {
		angle := start + span*float32(i)/float32(splits)
		rad := float64(angle) * math.Pi / 180
		ux, uy := float32(math.Cos(rad)), float32(math.Sin(rad))
		p1 := Point{X: cx + ux*(axisOuter-axisGap), Y: cy + uy*(axisOuter-axisGap)}
		p2 := Point{X: cx + ux*(axisOuter-axisGap-splitLength), Y: cy + uy*(axisOuter-axisGap-splitLength)}
		canvas.DrawLine(p1, p2, major, 3)

		value := s.Min + (s.Max-s.Min)*float32(i)/float32(splits)
		text := formatFloat(value)
		font := Font{Family: s.FontFamily, Size: 12}
		w, _ := TextMetrics(text, font)
		if w <= 0 {
			w = float32(len(text)) * 6.6
		}
		// ECharts places the labels at r - splitLength - splitDistance -
		// labelDistance; axisLineWidth is not part of this radius.
		lr := radius - splitLength - axisGap - labelDistance
		lx, ly := cx+ux*lr, cy+uy*lr
		textX := lx - w/2
		if ux < -.4 {
			textX = lx
		} else if ux > .4 {
			textX = lx - w
		}
		textY := ly - font.Size/2
		if uy < -.8 {
			textY = ly
		} else if uy > .8 {
			textY = ly - font.Size
		}
		canvas.DrawText(text, Rect{X: textX, Y: textY, W: w, H: font.Size + 2}, labelColor, font)

		if i == splits {
			continue
		}
		for j := 0; j < minorSubdivisions; j++ {
			subAngle := angle + span*float32(j)/float32(splits*minorSubdivisions)
			subRad := float64(subAngle) * math.Pi / 180
			sux, suy := float32(math.Cos(subRad)), float32(math.Sin(subRad))
			m1 := Point{X: cx + sux*(axisOuter-axisGap), Y: cy + suy*(axisOuter-axisGap)}
			m2 := Point{X: cx + sux*(axisOuter-axisGap-minorLength), Y: cy + suy*(axisOuter-axisGap-minorLength)}
			canvas.DrawLine(m1, m2, minor, 1)
		}
	}

	pointerColor := s.color
	if isZeroColor(pointerColor) {
		pointerColor = Color{R: 0x54 / 255.0, G: 0x70 / 255.0, B: 0xc6 / 255.0, A: 1}
	}
	drawGaugePointer(canvas, cx, cy, radius, start+span*ratio, 6, radius*.60, pointerColor)

	secondary := Color{R: 0x54 / 255.0, G: 0x55 / 255.0, B: 0x5a / 255.0, A: 1}
	primary := Color{R: 0x3c / 255.0, G: 0x3c / 255.0, B: 0x41 / 255.0, A: 1}
	if s.Title != "" {
		font := Font{Family: s.FontFamily, Size: 16}
		w, _ := TextMetrics(s.Title, font)
		if w <= 0 {
			w = float32(len(s.Title)) * 8.8
		}
		titleY := cy + radius*.20 - 4
		canvas.DrawText(s.Title, Rect{X: cx - w/2, Y: titleY - font.Size/2, W: w, H: font.Size + 2}, secondary, font)
	}
	detail := formatFloat(s.Value)
	detailFont := Font{Family: s.FontFamily, Size: 30, Weight: FontWeightBold}
	dw, _ := TextMetrics(detail, detailFont)
	if dw <= 0 {
		dw = float32(len(detail)) * 16.5
	}
	detailY := cy + radius*.40 - 5
	canvas.DrawText(detail, Rect{X: cx - dw/2, Y: detailY - detailFont.Size/2, W: dw, H: detailFont.Size + 4}, primary, detailFont)
}

// drawGaugeBand fills an annular sector. Building both arcs as a compound
// even-odd path gives butt ends like ECharts' non-round gauge axis.
func drawGaugeBand(canvas Canvas, cx, cy, inner, outer, start, end float32, color Color) {
	if outer <= inner || start == end {
		return
	}
	span := end - start
	steps := int(math.Abs(float64(span))*float64(outer)/2) + 1
	if steps < 8 {
		steps = 8
	}
	path := NewPath()
	for i := 0; i <= steps; i++ {
		a := start + span*float32(i)/float32(steps)
		rad := float64(a) * math.Pi / 180
		x := cx + outer*float32(math.Cos(rad))
		y := cy + outer*float32(math.Sin(rad))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	for i := steps; i >= 0; i-- {
		a := start + span*float32(i)/float32(steps)
		rad := float64(a) * math.Pi / 180
		path.LineTo(cx+inner*float32(math.Cos(rad)), cy+inner*float32(math.Sin(rad)))
	}
	path.Close()
	canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
}

func drawGaugePointer(canvas Canvas, cx, cy, radius, angle, width, length float32, color Color) {
	rad := float64(angle) * math.Pi / 180
	ux, uy := float32(math.Cos(rad)), float32(math.Sin(rad))
	px, py := -uy, ux
	tail := width * 2
	if width >= length/3 {
		tail = width
	}
	baseX, baseY := cx-ux*tail, cy-uy*tail
	tipX, tipY := cx+ux*length, cy+uy*length
	path := NewPath().
		MoveTo(baseX, baseY).
		LineTo(cx+px*width, cy+py*width).
		LineTo(tipX, tipY).
		LineTo(cx-px*width, cy-py*width).
		Close()
	canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
}

func (s *GaugeSeries) HitTest(p Point, plot Rect, _ Axis, _ Axis) int {
	cx, cy := s.center(plot)
	radius := s.radius(plot)
	dx, dy := p.X-cx, p.Y-cy
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if dist < radius*.52 || dist > radius+4 {
		return -1
	}
	angle := normalizeDegrees(float32(math.Atan2(float64(dy), float64(dx))) * 180 / math.Pi)
	start, end := s.screenAngles()
	inArc := false
	if end >= start {
		inArc = angle >= normalizeDegrees(start) || angle <= normalizeDegrees(end)
	} else {
		inArc = angle <= normalizeDegrees(start) && angle >= normalizeDegrees(end)
	}
	if inArc {
		return 0
	}
	return -1
}

func (s *GaugeSeries) LegendMarker(canvas Canvas, swatch Rect) {
	color := s.color
	if isZeroColor(color) {
		color = Color{R: 0x54 / 255.0, G: 0x70 / 255.0, B: 0xc6 / 255.0, A: 1}
	}
	canvas.FillRoundedRect(swatch, 2, color)
}
