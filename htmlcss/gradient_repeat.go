package htmlcss

import (
	"github.com/qizhanchan/qui"
	"math"
)

type repeatingLinearShader struct {
	gradient    qui.LinearGradient
	first, last float32
}

func (s repeatingLinearShader) ColorAt(x, y float32) qui.Color {
	g := s.gradient
	dx, dy := g.End.X-g.Start.X, g.End.Y-g.Start.Y
	denominator := dx*dx + dy*dy
	period := s.last - s.first
	if denominator <= 0 || period <= 0 {
		return g.Stops[len(g.Stops)-1].Color
	}
	t := ((x-g.Start.X)*dx + (y-g.Start.Y)*dy) / denominator
	t = s.first + float32(math.Mod(float64(t-s.first), float64(period)))
	if t < s.first {
		t += period
	}
	return g.ColorAt(g.Start.X+dx*t, g.Start.Y+dy*t)
}
