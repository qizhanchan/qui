package main

import "github.com/qizhanchan/qui"

func fillForShape(s nodeShape) qui.Color {
	switch s {
	case shapeEllipse:
		return nodeOval
	case shapeDiamond:
		return nodeAccent
	default:
		return nodeFill
	}
}

// drawShapeSilhouette is the unified shape painter — used by nodes,
// palette previews, and the drag-from-palette ghost. Every shape funnels
// through DrawShape with a Path so the AA winding-rule fill handles
// every silhouette without per-shape coverage code.
func drawShapeSilhouette(canvas qui.Canvas, s nodeShape, rect qui.Rect, fill, stroke qui.Color, strokeWidth float32) {
	switch s {
	case shapeRect:
		canvas.DrawShape(qui.ShapeRect(rect),
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(qui.ShapeRect(rect),
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth,
				Cap: qui.CapButt, Join: qui.JoinMiter, AntiAlias: true})
	case shapeRoundedRect:
		canvas.DrawShape(qui.ShapeRRect{Rect: rect, Radius: 10},
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(qui.ShapeRRect{Rect: rect, Radius: 10},
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth, AntiAlias: true})
	case shapeEllipse:
		p := qui.NewPath().AddOval(rect)
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth, AntiAlias: true})
	case shapeDiamond:
		p := diamondPath(rect)
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth,
				Join: qui.JoinMiter, AntiAlias: true})
	case shapeParallelogram:
		p := parallelogramPath(rect)
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth,
				Join: qui.JoinMiter, AntiAlias: true})
	case shapeHexagon:
		p := hexagonPath(rect)
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(qui.ShapePath{Path: p},
			qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth,
				Join: qui.JoinMiter, AntiAlias: true})
	case shapeCylinder:
		drawCylinder(canvas, rect, fill, stroke, strokeWidth)
	}
}

func diamondPath(rect qui.Rect) *qui.Path {
	cx := rect.X + rect.W/2
	cy := rect.Y + rect.H/2
	return qui.NewPath().
		MoveTo(cx, rect.Y).
		LineTo(rect.X+rect.W, cy).
		LineTo(cx, rect.Y+rect.H).
		LineTo(rect.X, cy).
		Close()
}

func parallelogramPath(rect qui.Rect) *qui.Path {
	shear := rect.W * 0.18
	return qui.NewPath().
		MoveTo(rect.X+shear, rect.Y).
		LineTo(rect.X+rect.W, rect.Y).
		LineTo(rect.X+rect.W-shear, rect.Y+rect.H).
		LineTo(rect.X, rect.Y+rect.H).
		Close()
}

func hexagonPath(rect qui.Rect) *qui.Path {
	inset := rect.W * 0.18
	cy := rect.Y + rect.H/2
	return qui.NewPath().
		MoveTo(rect.X+inset, rect.Y).
		LineTo(rect.X+rect.W-inset, rect.Y).
		LineTo(rect.X+rect.W, cy).
		LineTo(rect.X+rect.W-inset, rect.Y+rect.H).
		LineTo(rect.X+inset, rect.Y+rect.H).
		LineTo(rect.X, cy).
		Close()
}

func drawCylinder(canvas qui.Canvas, rect qui.Rect, fill, stroke qui.Color, strokeWidth float32) {
	const kappa float32 = 0.5522847498
	earH := rect.H * 0.18
	rx := rect.W / 2
	ry := earH / 2
	kx := rx * kappa
	ky := ry * kappa
	cx := rect.X + rect.W/2
	topY := rect.Y + ry
	botY := rect.Y + rect.H - ry

	body := qui.NewPath().
		MoveTo(rect.X, topY).
		CubicTo(rect.X, topY-ky, cx-kx, rect.Y, cx, rect.Y).
		CubicTo(cx+kx, rect.Y, rect.X+rect.W, topY-ky, rect.X+rect.W, topY).
		LineTo(rect.X+rect.W, botY).
		CubicTo(rect.X+rect.W, botY+ky, cx+kx, rect.Y+rect.H, cx, rect.Y+rect.H).
		CubicTo(cx-kx, rect.Y+rect.H, rect.X, botY+ky, rect.X, botY).
		Close()
	canvas.DrawShape(qui.ShapePath{Path: body},
		qui.Paint{Color: fill, AntiAlias: true})
	canvas.DrawShape(qui.ShapePath{Path: body},
		qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth, AntiAlias: true})

	topCurve := qui.NewPath().
		MoveTo(rect.X, topY).
		CubicTo(rect.X, topY+ky, cx-kx, rect.Y+earH, cx, rect.Y+earH).
		CubicTo(cx+kx, rect.Y+earH, rect.X+rect.W, topY+ky, rect.X+rect.W, topY)
	canvas.DrawShape(qui.ShapePath{Path: topCurve},
		qui.Paint{Color: stroke, Style: qui.PaintStroke, StrokeWidth: strokeWidth, AntiAlias: true})
}
