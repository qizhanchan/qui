package gfx

// Size represents width and height in logical pixels.
type Size struct {
	W, H float32
}

// Point represents an X/Y coordinate.
type Point struct {
	X, Y float32
}

// Rect represents a rectangle in window coordinates.
type Rect struct {
	X, Y float32
	W, H float32
}

func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H
}

func (r Rect) Inset(inset Insets) Rect {
	return Rect{
		X: r.X + inset.Left,
		Y: r.Y + inset.Top,
		W: r.W - inset.Left - inset.Right,
		H: r.H - inset.Top - inset.Bottom,
	}
}

// IsEmpty reports whether the rectangle has zero or negative area.
// The zero-value Rect is empty; this is the canonical "no region" sentinel.
func (r Rect) IsEmpty() bool {
	return r.W <= 0 || r.H <= 0
}

// Union returns the smallest rectangle enclosing both r and other.
// An empty rectangle is absorbed into the other (empty + r = r).
func (r Rect) Union(other Rect) Rect {
	if r.IsEmpty() {
		return other
	}
	if other.IsEmpty() {
		return r
	}
	x0 := MinF(r.X, other.X)
	y0 := MinF(r.Y, other.Y)
	x1 := MaxF(r.X+r.W, other.X+other.W)
	y1 := MaxF(r.Y+r.H, other.Y+other.H)
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Intersect returns the overlap of r and other. Returns an empty Rect if
// they do not overlap.
func (r Rect) Intersect(other Rect) Rect {
	x0 := MaxF(r.X, other.X)
	y0 := MaxF(r.Y, other.Y)
	x1 := MinF(r.X+r.W, other.X+other.W)
	y1 := MinF(r.Y+r.H, other.Y+other.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Intersects reports whether r and other have any overlap.
func (r Rect) Intersects(other Rect) bool {
	return !r.Intersect(other).IsEmpty()
}

func MinF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func MaxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// Insets represents margins/padding.
type Insets struct {
	Top, Right, Bottom, Left float32
}

func (i Insets) Horizontal() float32 { return i.Left + i.Right }
func (i Insets) Vertical() float32   { return i.Top + i.Bottom }
