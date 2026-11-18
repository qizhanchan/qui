package canvas2d

import (
	"math"

	"github.com/qizhanchan/qui/svg"
)

// NewPath2DFromSVG is new Path2D(d): SVG path data. Like a browser, it
// keeps the commands parsed before the first error.
func NewPath2DFromSVG(d string) *Path2D {
	p := NewPath2D()
	sp, err := svg.ParsePath(d)
	if err != nil || sp == nil {
		return p
	}
	var cx, cy float64
	for _, c := range sp.Commands {
		a := c.Args
		f := func(i int) float64 { return float64(a[i]) }
		switch c.Op {
		case svg.OpMoveTo:
			p.MoveTo(f(0), f(1))
			cx, cy = f(0), f(1)
		case svg.OpLineTo:
			p.LineTo(f(0), f(1))
			cx, cy = f(0), f(1)
		case svg.OpCurveTo:
			p.BezierCurveTo(f(0), f(1), f(2), f(3), f(4), f(5))
			cx, cy = f(4), f(5)
		case svg.OpQuadTo:
			p.QuadraticCurveTo(f(0), f(1), f(2), f(3))
			cx, cy = f(2), f(3)
		case svg.OpArcTo:
			svgArc(p, cx, cy, f(0), f(1), f(2), a[3] != 0, a[4] != 0, f(5), f(6))
			cx, cy = f(5), f(6)
		case svg.OpClose:
			p.ClosePath()
			if p.d.has {
				cx, cy = float64(p.d.cur.X), float64(p.d.cur.Y)
			}
		}
	}
	return p
}

// svgArc converts an SVG endpoint arc to an ellipse() call (SVG 1.1 F.6.5).
func svgArc(p *Path2D, x1, y1, rx, ry, deg float64, large, sweep bool, x2, y2 float64) {
	if x1 == x2 && y1 == y2 {
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.LineTo(x2, y2)
		return
	}
	phi := deg * math.Pi / 180
	sp, cp := math.Sincos(phi)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cp*dx + sp*dy
	y1p := -sp*dx + cp*dy
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	k := math.Sqrt(math.Max(0, num/den))
	if large == sweep {
		k = -k
	}
	cxp, cyp := k*rx*y1p/ry, -k*ry*x1p/rx
	cx := cp*cxp - sp*cyp + (x1+x2)/2
	cy := sp*cxp + cp*cyp + (y1+y2)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	t1 := ang(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dt := ang((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	p.Ellipse(cx, cy, rx, ry, phi, t1, t1+dt, !sweep)
}
