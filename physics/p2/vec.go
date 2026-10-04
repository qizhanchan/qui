// Package p2 is a 2D rigid-body engine for tile-based games: swept
// per-axis AABB collision against a static spatial hash, gravity
// integration, one-way platforms, sensor overlap events and layer
// filtering. See the parent physics package doc for the cross-dimension
// contract this implementation obeys.
package p2

// Vec2 is a 2D vector in world pixels. +Y points down (screen space).
type Vec2 struct {
	X, Y float32
}

func (v Vec2) Add(o Vec2) Vec2    { return Vec2{v.X + o.X, v.Y + o.Y} }
func (v Vec2) Sub(o Vec2) Vec2    { return Vec2{v.X - o.X, v.Y - o.Y} }
func (v Vec2) Mul(s float32) Vec2 { return Vec2{v.X * s, v.Y * s} }

// AABB is an axis-aligned box in Min/Max corner form.
type AABB struct {
	Min, Max Vec2
}

// Overlaps reports strict overlap — boxes that merely touch edges do
// not overlap. Resting flush on a surface therefore never triggers
// sensor events.
func (a AABB) Overlaps(b AABB) bool {
	return a.Min.X < b.Max.X && a.Max.X > b.Min.X &&
		a.Min.Y < b.Max.Y && a.Max.Y > b.Min.Y
}

// Union returns the smallest AABB enclosing both boxes.
func (a AABB) Union(b AABB) AABB {
	return AABB{
		Min: Vec2{minf(a.Min.X, b.Min.X), minf(a.Min.Y, b.Min.Y)},
		Max: Vec2{maxf(a.Max.X, b.Max.X), maxf(a.Max.Y, b.Max.Y)},
	}
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
