package diagram

import (
	"math"

	"github.com/qizhanchan/qui"
)

func drawVertexShape(canvas qui.Canvas, kind ShapeKind, rect Rect, fill, stroke qui.Color, strokeW, radius, shapeSize float32, dashed bool, opacity float32) {
	if kind == ShapeText {
		return
	}
	fillPaint := qui.Paint{Color: withOpacity(fill, opacity), AntiAlias: true}
	strokePaint := qui.Paint{
		Color: withOpacity(stroke, opacity), Style: qui.PaintStroke,
		StrokeWidth: strokeW, Cap: qui.CapButt, Join: qui.JoinMiter, AntiAlias: true,
	}
	qrect := qui.Rect{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H}
	switch kind {
	case ShapeRoundedRect:
		if radius <= 0 {
			r := min32(rect.W, rect.H) * .15
			radius = r
		}
		r := roundedRadius(rect, radius)
		if fill.A > 0 {
			canvas.DrawShape(qui.ShapeRRect{Rect: qrect, Radius: r}, fillPaint)
		}
		canvas.DrawShape(qui.ShapeRRect{Rect: qrect, Radius: r}, strokePaint)
	case ShapeEllipse:
		p := qui.NewPath().AddOval(qrect)
		if fill.A > 0 {
			canvas.DrawShape(qui.ShapePath{Path: p}, fillPaint)
		}
		canvas.DrawShape(qui.ShapePath{Path: p}, strokePaint)
	case ShapeDiamond:
		p := diamondPath(rect)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
	case ShapeParallelogram:
		p := parallelogramPath(rect)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
	case ShapeHexagon:
		p := hexagonPath(rect)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
	case ShapeCylinder:
		drawCylinder(canvas, rect, fillPaint, strokePaint, fill.A > 0, opacity, shapeSize)
	case ShapeDocument:
		p := documentPath(rect, shapeSize)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
	case ShapeCloud:
		p := cloudPath(rect)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
	case ShapeNote:
		p, fold := notePaths(rect, shapeSize)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
		canvas.DrawShape(qui.ShapePath{Path: fold}, strokePaint)
	case ShapeActor:
		p := actorPath(rect)
		drawFillStrokePath(canvas, p, fillPaint, strokePaint, fill.A > 0)
	default:
		if fill.A > 0 {
			canvas.DrawShape(qui.ShapeRect(qrect), fillPaint)
		}
		if dashed {
			drawDashedRect(canvas, rect, strokePaint)
		} else {
			canvas.DrawShape(qui.ShapeRect(qrect), strokePaint)
		}
	}
}

func drawFillStrokePath(canvas qui.Canvas, p *qui.Path, fill, stroke qui.Paint, hasFill bool) {
	if p == nil {
		return
	}
	if hasFill {
		canvas.DrawShape(qui.ShapePath{Path: p}, fill)
	}
	canvas.DrawShape(qui.ShapePath{Path: p}, stroke)
}

func roundedRadius(r Rect, want float32) float32 {
	maxR := min32(r.W, r.H) / 2
	if want > maxR {
		return maxR
	}
	return want
}

func diamondPath(r Rect) *qui.Path {
	return qui.NewPath().
		MoveTo(r.X+r.W/2, r.Y).
		LineTo(r.X+r.W, r.Y+r.H/2).
		LineTo(r.X+r.W/2, r.Y+r.H).
		LineTo(r.X, r.Y+r.H/2).
		Close()
}

func parallelogramPath(r Rect) *qui.Path {
	shear := float32(20)
	return qui.NewPath().
		MoveTo(r.X+shear, r.Y).
		LineTo(r.X+r.W, r.Y).
		LineTo(r.X+r.W-shear, r.Y+r.H).
		LineTo(r.X, r.Y+r.H).
		Close()
}

func hexagonPath(r Rect) *qui.Path {
	inset := float32(20)
	cy := r.Y + r.H/2
	return qui.NewPath().
		MoveTo(r.X+inset, r.Y).
		LineTo(r.X+r.W-inset, r.Y).
		LineTo(r.X+r.W, cy).
		LineTo(r.X+r.W-inset, r.Y+r.H).
		LineTo(r.X+inset, r.Y+r.H).
		LineTo(r.X, cy).
		Close()
}

func drawCylinder(canvas qui.Canvas, r Rect, fill, stroke qui.Paint, hasFill bool, opacity, shapeSize float32) {
	dy := shapeSize
	if dy <= 0 {
		dy = float32(math.Round(float64(r.H / 5)))
	}
	if dy > 40 {
		dy = 40
	}
	if dy > r.H/2 {
		dy = r.H / 2
	}
	const kappa float32 = .5522847498
	cx := r.X + r.W/2
	left, right := r.X, r.X+r.W
	top, bottom := r.Y, r.Y+r.H
	topY, bottomY := top+dy, bottom-dy
	body := qui.NewPath().
		MoveTo(left, topY).
		CubicTo(left, topY-dy*kappa, cx-r.W/2*kappa, top, cx, top).
		CubicTo(cx+r.W/2*kappa, top, right, topY-dy*kappa, right, topY).
		LineTo(right, bottomY).
		CubicTo(right, bottomY+dy*kappa, cx+r.W/2*kappa, bottom, cx, bottom).
		CubicTo(cx-r.W/2*kappa, bottom, left, bottomY+dy*kappa, left, bottomY).
		Close()
	drawFillStrokePath(canvas, body, fill, stroke, hasFill)
	topArc := qui.NewPath().
		MoveTo(right, topY).
		CubicTo(right, topY+dy*kappa, cx+r.W/2*kappa, top+2*dy, cx, top+2*dy).
		CubicTo(cx-r.W/2*kappa, top+2*dy, left, topY+dy*kappa, left, topY)
	stroke.Color.A *= opacity
	canvas.DrawShape(qui.ShapePath{Path: topArc}, stroke)
}

func documentPath(r Rect, shapeSize float32) *qui.Path {
	w, h := r.W, r.H
	size := shapeSize
	if size <= 0 {
		size = .3
	}
	if size <= 1 {
		size *= h
	}
	size = clamp32(size, 0, h)
	return qui.NewPath().
		MoveTo(r.X, r.Y).
		LineTo(r.X+w, r.Y).
		LineTo(r.X+w, r.Y+h-size/2).
		QuadTo(r.X+w*.75, r.Y+h-size*1.4, r.X+w*.5, r.Y+h-size/2).
		QuadTo(r.X+w*.25, r.Y+h-size*(1-1.4), r.X, r.Y+h-size/2).
		LineTo(r.X, r.Y+size/2).
		Close()
}

func cloudPath(r Rect) *qui.Path {
	x, y, w, h := r.X, r.Y, r.W, r.H
	p := func(fx, fy float32) Point { return Point{X: x + fx*w, Y: y + fy*h} }
	a := p(.25, .25)
	b := p(.05, .25)
	c := p(0, .5)
	d := p(.16, .55)
	e := p(0, .66)
	f := p(.18, .9)
	g := p(.31, .8)
	hh := p(.4, 1)
	i := p(.7, 1)
	j := p(.8, .8)
	k := p(1, .8)
	l := p(1, .6)
	m := p(.875, .5)
	n := p(1, .3)
	o := p(.8, .1)
	q := p(.625, .2)
	rr := p(.5, .05)
	s := p(.3, .05)
	return qui.NewPath().MoveTo(a.X, a.Y).
		CubicTo(b.X, b.Y, c.X, c.Y, d.X, d.Y).
		CubicTo(e.X, e.Y, f.X, f.Y, g.X, g.Y).
		CubicTo(hh.X, hh.Y, i.X, i.Y, j.X, j.Y).
		CubicTo(k.X, k.Y, l.X, l.Y, m.X, m.Y).
		CubicTo(n.X, n.Y, o.X, o.Y, q.X, q.Y).
		CubicTo(rr.X, rr.Y, s.X, s.Y, a.X, a.Y).
		Close()
}

func notePaths(r Rect, shapeSize float32) (*qui.Path, *qui.Path) {
	fold := shapeSize
	if fold <= 0 {
		fold = 30
	}
	fold = clamp32(fold, 0, min32(r.W, r.H))
	body := qui.NewPath().
		MoveTo(r.X, r.Y).
		LineTo(r.X+r.W-fold, r.Y).
		LineTo(r.X+r.W, r.Y+fold).
		LineTo(r.X+r.W, r.Y+r.H).
		LineTo(r.X, r.Y+r.H).
		Close()
	crease := qui.NewPath().
		MoveTo(r.X+r.W-fold, r.Y).
		LineTo(r.X+r.W-fold, r.Y+fold).
		LineTo(r.X+r.W, r.Y+fold)
	return body, crease
}

func actorPath(r Rect) *qui.Path {
	x, y, w, h := r.X, r.Y, r.W, r.H
	third := w / 3
	return qui.NewPath().
		MoveTo(x, y+h).
		CubicTo(x, y+3*h/5, x, y+2*h/5, x+w/2, y+2*h/5).
		CubicTo(x+w/2-third, y+2*h/5, x+w/2-third, y, x+w/2, y).
		CubicTo(x+w/2+third, y, x+w/2+third, y+2*h/5, x+w/2, y+2*h/5).
		CubicTo(x+w, y+2*h/5, x+w, y+3*h/5, x+w, y+h).
		Close()
}

func shapeContains(kind ShapeKind, rect Rect, p Point) bool {
	if !rect.Contains(p) {
		return false
	}
	if kind == ShapeText || kind == ShapeRect || kind == ShapeRoundedRect || kind == ShapeCylinder ||
		kind == ShapeDocument || kind == ShapeCloud || kind == ShapeNote || kind == ShapeActor {
		return true
	}
	cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
	switch kind {
	case ShapeEllipse:
		dx, dy := (p.X-cx)/(rect.W/2), (p.Y-cy)/(rect.H/2)
		return dx*dx+dy*dy <= 1
	case ShapeDiamond:
		return abs32(p.X-cx)/(rect.W/2)+abs32(p.Y-cy)/(rect.H/2) <= 1
	case ShapeParallelogram:
		shear := rect.W * .18
		y := (p.Y - rect.Y) / rect.H
		x0 := rect.X + shear*(1-y)
		x1 := rect.X + rect.W - shear*y
		return p.X >= x0 && p.X <= x1
	case ShapeHexagon:
		inset := rect.W * .18
		y := (p.Y - rect.Y) / rect.H
		var x0, x1 float32
		if y < .5 {
			x0 = rect.X + inset*(1-2*y)
			x1 = rect.X + rect.W - inset*(1-2*y)
		} else {
			x0 = rect.X + inset*(2*y-1)
			x1 = rect.X + rect.W - inset*(2*y-1)
		}
		return p.X >= x0 && p.X <= x1
	}
	return true
}

func portPoint(rect Rect, port Port) Point {
	switch port {
	case PortNorth:
		return Point{X: rect.X + rect.W/2, Y: rect.Y}
	case PortEast:
		return Point{X: rect.X + rect.W, Y: rect.Y + rect.H/2}
	case PortSouth:
		return Point{X: rect.X + rect.W/2, Y: rect.Y + rect.H}
	case PortWest:
		return Point{X: rect.X, Y: rect.Y + rect.H/2}
	default:
		return rect.Center()
	}
}

func portTangent(port Port) Point {
	switch port {
	case PortNorth:
		return Point{Y: -1}
	case PortEast:
		return Point{X: 1}
	case PortSouth:
		return Point{Y: 1}
	case PortWest:
		return Point{X: -1}
	default:
		return Point{}
	}
}

func nearestPort(rect Rect, toward Point, fallback Port) Port {
	c := rect.Center()
	dx, dy := toward.X-c.X, toward.Y-c.Y
	if abs32(dx) > abs32(dy) {
		if dx >= 0 {
			return PortEast
		}
		return PortWest
	}
	if dy >= 0 {
		return PortSouth
	}
	if dy < 0 {
		return PortNorth
	}
	if fallback != "" {
		return fallback
	}
	return PortAuto
}

func perimeterPoint(kind ShapeKind, rect Rect, toward Point) Point {
	c := rect.Center()
	dx, dy := toward.X-c.X, toward.Y-c.Y
	if dx == 0 && dy == 0 {
		return c
	}
	if kind == ShapeEllipse {
		rx, ry := rect.W/2, rect.H/2
		t := 1 / float32(math.Sqrt(float64((dx*dx)/(rx*rx)+(dy*dy)/(ry*ry))))
		return Point{X: c.X + dx*t, Y: c.Y + dy*t}
	}
	if kind == ShapeDiamond {
		t := 1 / (abs32(dx)/(rect.W/2) + abs32(dy)/(rect.H/2))
		return Point{X: c.X + dx*t, Y: c.Y + dy*t}
	}
	tx := float32(math.Inf(1))
	ty := float32(math.Inf(1))
	if dx != 0 {
		tx = (rect.W / 2) / abs32(dx)
	}
	if dy != 0 {
		ty = (rect.H / 2) / abs32(dy)
	}
	t := min32(tx, ty)
	return Point{X: c.X + dx*t, Y: c.Y + dy*t}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func drawDashedRect(canvas qui.Canvas, r Rect, paint qui.Paint) {
	const dash, gap float32 = 7, 5
	drawLine := func(a, b Point) {
		dx, dy := b.X-a.X, b.Y-a.Y
		length := float32(math.Hypot(float64(dx), float64(dy)))
		if length == 0 {
			return
		}
		ux, uy := dx/length, dy/length
		for pos := float32(0); pos < length; {
			n := min32(dash, length-pos)
			s := Point{X: a.X + ux*pos, Y: a.Y + uy*pos}
			e := Point{X: s.X + ux*n, Y: s.Y + uy*n}
			canvas.DrawLine(qui.Point{X: s.X, Y: s.Y}, qui.Point{X: e.X, Y: e.Y}, paint.Color, paint.StrokeWidth)
			pos += n + gap
		}
	}
	drawLine(Point{X: r.X, Y: r.Y}, Point{X: r.X + r.W, Y: r.Y})
	drawLine(Point{X: r.X + r.W, Y: r.Y}, Point{X: r.X + r.W, Y: r.Y + r.H})
	drawLine(Point{X: r.X + r.W, Y: r.Y + r.H}, Point{X: r.X, Y: r.Y + r.H})
	drawLine(Point{X: r.X, Y: r.Y + r.H}, Point{X: r.X, Y: r.Y})
}
