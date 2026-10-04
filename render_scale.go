package qui

// Geometry helpers shared by every Canvas backend's state-stack
// projection. The math is needed by imageCanvas's public-method entry
// points and by scaleRoundedShape's callers (e.g. shadow / pill
// projection).
//
// These are the axis-aligned fast paths that don't need rotation. They
// are called only when the matrix's off-diagonal terms are known to be
// 0 (the rasterizers gate on Matrix.IsAxisAligned() first).

func scalePoint(p Point, sx, sy float32) Point {
	return Point{X: p.X * sx, Y: p.Y * sy}
}

func scaleRect(rect Rect, sx, sy float32) Rect {
	return Rect{
		X: rect.X * sx,
		Y: rect.Y * sy,
		W: rect.W * sx,
		H: rect.H * sy,
	}
}

// translateRect shifts the rect by (tx, ty). Pairs with scaleRect for
// axis-aligned matrix projection: physR = translateRect(scaleRect(r, sx, sy), tx, ty).
func translateRect(rect Rect, tx, ty float32) Rect {
	rect.X += tx
	rect.Y += ty
	return rect
}

// projectAxisAlignedRect applies an axis-aligned matrix to a rect.
// Asserts (in debug-builds) IsAxisAligned via callers — when rotation
// is present the rasterizer falls back to a Path conversion.
func projectAxisAlignedRect(m Matrix, r Rect) Rect {
	return Rect{
		X: m.A*r.X + m.TX,
		Y: m.D*r.Y + m.TY,
		W: m.A * r.W,
		H: m.D * r.H,
	}
}

// projectAxisAlignedPoint is the same projection for points. Used by
// the line rasterizer's endpoint mapping.
func projectAxisAlignedPoint(m Matrix, p Point) Point {
	return Point{
		X: m.A*p.X + m.TX,
		Y: m.D*p.Y + m.TY,
	}
}

func avgScale(sx, sy float32) float32 {
	return (sx + sy) * 0.5
}

// projectAARoundedShape is the matrix-aware version of scaleRoundedShape.
// Asserts (via the IsAxisAligned check) that m has no rotation; rotated
// rounded rects must be converted to a Path before rasterization. Used
// by FillRoundedRect / StrokeRoundedRect / DrawShadow which all hit the
// AA corner rasterizer that's tied to an axis-aligned rect.
func projectAARoundedShape(m Matrix, rect Rect, radius float32) (Rect, float32) {
	// Scale + translate. scaleRoundedShape's pill-centering logic still
	// applies because it operates on the rect's WIDTH/HEIGHT magnitude;
	// after the geometric scale we just add the translation.
	scaled, scaledRadius := scaleRoundedShape(rect, radius, m.A, m.D)
	scaled.X += m.TX
	scaled.Y += m.TY
	return scaled, scaledRadius
}

// isCircularRoundedShape reports whether the (rect, radius) pair
// renders as a pill, capsule, or circle — i.e. the radius covers at
// least half of the shorter side. The 0.5 px slack guards against
// float-math drift when callers compute `radius := rect.H / 2` and
// the rect dimensions came from layout arithmetic that has its own
// rounding.
func isCircularRoundedShape(rect Rect, radius float32) bool {
	minSide := rect.W
	if rect.H < minSide {
		minSide = rect.H
	}
	if minSide <= 0 {
		return false
	}
	return radius >= minSide/2-0.5
}

// scaleRoundedShape returns the (rect, radius) pair the backend should
// rasterize for a rounded shape that started as logical coordinates.
//
// For non-circular rounded rects (radius < min(W,H)/2) it keeps the
// existing per-axis behavior — W scales by sx, H by sy, radius by
// avgScale — so the corner curve resolves at full physical-pixel
// precision on the canvas's actual pixel grid.
//
// For pills / capsules / circles it instead uses uniform scaling
// (avg) on both axes and centers the result within the per-axis
// scaled rect. When sx == sy the two paths are identical; when they
// differ (macOS scaled HiDPI modes, mixed-DPI multi-display configs)
// the uniform path keeps the shape geometrically a pill — without
// this clamp a 40×40 logical button on a 2.05×2.00 canvas renders as
// an 82×80 stadium with a 2 px straight midband, which reads visually
// as "the icon button is not a perfect circle."
func scaleRoundedShape(rect Rect, radius, sx, sy float32) (Rect, float32) {
	if sx == sy || !isCircularRoundedShape(rect, radius) {
		return scaleRect(rect, sx, sy), radius * avgScale(sx, sy)
	}
	perAxis := scaleRect(rect, sx, sy)
	s := avgScale(sx, sy)
	uniformW := rect.W * s
	uniformH := rect.H * s
	return Rect{
		X: perAxis.X + (perAxis.W-uniformW)/2,
		Y: perAxis.Y + (perAxis.H-uniformH)/2,
		W: uniformW,
		H: uniformH,
	}, radius * s
}
