// Package p3 is the 3D rigid-body engine, an API mirror of physics/p2
// with a Z axis: swept per-axis AABB collision against a static spatial
// hash, gravity integration, one-way platforms (Y axis), sensor overlap
// events and layer filtering. Axis sweep order is X → Z → Y (both
// horizontals before the vertical). See the parent physics package doc
// for the cross-dimension contract this implementation obeys.
//
// Units are world units (a game may use meters); +Y points DOWN to
// match p2's screen-space convention, so gravity is +Y and "on ground"
// means blocked while moving toward +Y.
package p3

// Vec3 is a 3D vector in world units. +Y points down.
type Vec3 struct {
	X, Y, Z float32
}

func (v Vec3) Add(o Vec3) Vec3    { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3    { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Mul(s float32) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }

// AABB is an axis-aligned box in Min/Max corner form.
type AABB struct {
	Min, Max Vec3
}

// Overlaps reports strict overlap — boxes that merely touch faces do
// not overlap, so resting flush never triggers sensor events.
func (a AABB) Overlaps(b AABB) bool {
	return a.Min.X < b.Max.X && a.Max.X > b.Min.X &&
		a.Min.Y < b.Max.Y && a.Max.Y > b.Min.Y &&
		a.Min.Z < b.Max.Z && a.Max.Z > b.Min.Z
}

// Union returns the smallest AABB enclosing both boxes.
func (a AABB) Union(b AABB) AABB {
	return AABB{
		Min: Vec3{minf(a.Min.X, b.Min.X), minf(a.Min.Y, b.Min.Y), minf(a.Min.Z, b.Min.Z)},
		Max: Vec3{maxf(a.Max.X, b.Max.X), maxf(a.Max.Y, b.Max.Y), maxf(a.Max.Z, b.Max.Z)},
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
