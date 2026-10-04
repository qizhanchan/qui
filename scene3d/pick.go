package scene3d

import "math"

// Ray is a world-space ray for picking. Dir is unit length.
type Ray struct {
	Origin Vec3
	Dir    Vec3
}

// ScreenRay builds a world-space ray from normalized device coords
// (ndcX, ndcY in [-1,1], with +Y up). It unprojects the near and far
// clip points through the inverse view-projection and connects them —
// works for both perspective and orthographic cameras.
func ScreenRay(cam *Camera, aspect, ndcX, ndcY float32) Ray {
	proj := cam.ProjectionMatrix(aspect)
	view := cam.ViewMatrix()
	inv, ok := Mat4Inverse(Mat4Mul(proj, view))
	if !ok {
		// Degenerate camera: shoot forward from the eye.
		return Ray{Origin: cam.Position, Dir: cam.Target.Sub(cam.Position).Normalize()}
	}
	near := unproject(inv, ndcX, ndcY, -1)
	far := unproject(inv, ndcX, ndcY, 1)
	return Ray{Origin: near, Dir: far.Sub(near).Normalize()}
}

func unproject(inv Mat4, x, y, z float32) Vec3 {
	p := inv.MulVec4(Vec4{X: x, Y: y, Z: z, W: 1})
	if p.W != 0 {
		return Vec3{X: p.X / p.W, Y: p.Y / p.W, Z: p.Z / p.W}
	}
	return Vec3{X: p.X, Y: p.Y, Z: p.Z}
}

// IntersectTriangle returns the ray parameter t (>0) at which the ray
// hits triangle abc, via Möller–Trumbore. ok is false on a miss or a
// back-of-origin hit.
func (r Ray) IntersectTriangle(a, b, c Vec3) (t float32, ok bool) {
	const eps = 1e-7
	e1 := b.Sub(a)
	e2 := c.Sub(a)
	p := r.Dir.Cross(e2)
	det := e1.Dot(p)
	if det > -eps && det < eps {
		return 0, false // parallel
	}
	inv := 1 / det
	tv := r.Origin.Sub(a)
	u := tv.Dot(p) * inv
	if u < 0 || u > 1 {
		return 0, false
	}
	q := tv.Cross(e1)
	v := r.Dir.Dot(q) * inv
	if v < 0 || u+v > 1 {
		return 0, false
	}
	t = e2.Dot(q) * inv
	if t <= eps {
		return 0, false
	}
	return t, true
}

// IntersectMesh returns the nearest ray hit against a mesh's triangles,
// transformed by the given model matrix. ok is false when nothing is
// hit. Meshes with an index buffer are traversed indexed; others as a
// triangle soup.
func (r Ray) IntersectMesh(m *Mesh, model Mat4) (t float32, ok bool) {
	if m == nil || len(m.Positions) == 0 {
		return 0, false
	}
	best := float32(math.MaxFloat32)
	hit := false
	tri := func(ia, ib, ic uint32) {
		if int(ia) >= len(m.Positions) || int(ib) >= len(m.Positions) || int(ic) >= len(m.Positions) {
			return
		}
		a := mulPoint(model, m.Positions[ia])
		b := mulPoint(model, m.Positions[ib])
		c := mulPoint(model, m.Positions[ic])
		if tt, okT := r.IntersectTriangle(a, b, c); okT && tt < best {
			best = tt
			hit = true
		}
	}
	if len(m.Indices) >= 3 {
		for i := 0; i+2 < len(m.Indices); i += 3 {
			tri(m.Indices[i], m.Indices[i+1], m.Indices[i+2])
		}
	} else {
		for i := 0; i+2 < len(m.Positions); i += 3 {
			tri(uint32(i), uint32(i+1), uint32(i+2))
		}
	}
	return best, hit
}

// PointAt returns the world point at ray parameter t.
func (r Ray) PointAt(t float32) Vec3 { return r.Origin.Add(r.Dir.Mul(t)) }

// IntersectPlaneY returns where the ray crosses the horizontal plane
// Y = y. ok is false when the ray is (near) parallel to the plane.
func (r Ray) IntersectPlaneY(y float32) (Vec3, bool) {
	if r.Dir.Y > -1e-6 && r.Dir.Y < 1e-6 {
		return Vec3{}, false
	}
	t := (y - r.Origin.Y) / r.Dir.Y
	if t <= 0 {
		return Vec3{}, false
	}
	return r.PointAt(t), true
}

// mulPoint transforms a point (w=1) by a column-major matrix.
func mulPoint(m Mat4, p Vec3) Vec3 {
	v := m.MulVec4(Vec4{X: p.X, Y: p.Y, Z: p.Z, W: 1})
	return Vec3{X: v.X, Y: v.Y, Z: v.Z}
}
