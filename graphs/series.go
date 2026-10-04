package graphs

import . "github.com/qizhanchan/qui"

// Point2D is a (x, y) data point.
type Point2D struct {
	X, Y float32
}

// SeriesBounds reports the data-space extent of a series. Chart
// auto-range combines every series' bounds to derive ValueAxis ranges
// when no explicit axis has been supplied.
type SeriesBounds struct {
	Xmin, Xmax, Ymin, Ymax float32
	HasData                bool
}

// HotSpot identifies the currently-hovered or -selected (series,index)
// pair. Chart passes it into Series.Draw so each series can highlight
// the relevant point without reading back the chart's state.
type HotSpot struct {
	SeriesIdx int
	PointIdx  int
}

// NoHotSpot is the sentinel for "nothing hot".
var NoHotSpot = HotSpot{SeriesIdx: -1, PointIdx: -1}

// Series is one rendered data layer inside a Chart. Each series owns
// its data + visual defaults; Chart coordinates layout and dispatches
// Draw / HitTest / LegendMarker.
type Series interface {
	Name() string
	Color() Color
	SetColor(c Color)
	Visible() bool
	SetVisible(bool)
	DataBounds() SeriesBounds
	Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot)
	HitTest(p Point, plot Rect, x, y Axis) int
	LegendMarker(canvas Canvas, swatch Rect)
}

// seriesBase provides the color/name/visibility bookkeeping every
// concrete series needs. Concrete types embed it and can override
// individual accessors for custom behavior.
type seriesBase struct {
	name    string
	color   Color
	visible bool
}

func (s *seriesBase) Name() string      { return s.name }
func (s *seriesBase) Color() Color      { return s.color }
func (s *seriesBase) SetColor(c Color)  { s.color = c }
func (s *seriesBase) Visible() bool     { return s.visible }
func (s *seriesBase) SetVisible(v bool) { s.visible = v }
func (s *seriesBase) SetName(n string)  { s.name = n }

// newSeriesBase initializes with default visibility = true.
func newSeriesBase(name string) seriesBase {
	return seriesBase{name: name, visible: true}
}

// boundsFromPoints is a small helper used by point-based series.
func boundsFromPoints(pts []Point2D) SeriesBounds {
	if len(pts) == 0 {
		return SeriesBounds{}
	}
	b := SeriesBounds{
		Xmin: pts[0].X, Xmax: pts[0].X,
		Ymin: pts[0].Y, Ymax: pts[0].Y,
		HasData: true,
	}
	for _, p := range pts[1:] {
		if p.X < b.Xmin {
			b.Xmin = p.X
		}
		if p.X > b.Xmax {
			b.Xmax = p.X
		}
		if p.Y < b.Ymin {
			b.Ymin = p.Y
		}
		if p.Y > b.Ymax {
			b.Ymax = p.Y
		}
	}
	return b
}

// isZeroColor reports whether the color is the zero-value (used by
// Chart.AddSeries to decide whether to auto-assign from the palette).
func isZeroColor(c Color) bool {
	return c.R == 0 && c.G == 0 && c.B == 0 && c.A == 0
}
