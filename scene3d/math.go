package scene3d

import "math"

// Minimal 3D math. Vec3/Vec4 are plain structs passed by value;
// Mat4 is a [16]float32 in column-major order (OpenGL convention —
// glUniformMatrix4fv does not transpose). Quat is a unit quaternion
// for rotations. All operations return new values rather than
// mutating in place so callers can freely chain.

type Vec2 struct{ X, Y float32 }
type Vec3 struct{ X, Y, Z float32 }
type Vec4 struct{ X, Y, Z, W float32 }

// Quat is a quaternion in (x, y, z, w) order where w is the scalar.
type Quat struct{ X, Y, Z, W float32 }

// Mat4 is column-major, so m[0..3] is column 0, m[4..7] is column 1, etc.
type Mat4 [16]float32

// ---- Vec3 ----

func (v Vec3) Add(o Vec3) Vec3 { return Vec3{v.X + o.X, v.Y + o.Y, v.Z + o.Z} }
func (v Vec3) Sub(o Vec3) Vec3 { return Vec3{v.X - o.X, v.Y - o.Y, v.Z - o.Z} }
func (v Vec3) Mul(s float32) Vec3 {
	return Vec3{v.X * s, v.Y * s, v.Z * s}
}
func (v Vec3) Dot(o Vec3) float32 { return v.X*o.X + v.Y*o.Y + v.Z*o.Z }
func (v Vec3) Cross(o Vec3) Vec3 {
	return Vec3{
		v.Y*o.Z - v.Z*o.Y,
		v.Z*o.X - v.X*o.Z,
		v.X*o.Y - v.Y*o.X,
	}
}
func (v Vec3) Length() float32 { return float32(math.Sqrt(float64(v.Dot(v)))) }
func (v Vec3) Normalize() Vec3 {
	l := v.Length()
	if l == 0 {
		return Vec3{}
	}
	return v.Mul(1 / l)
}

// ---- Quat ----

// QuatFromAxisAngle builds a rotation quaternion from an axis (unit
// vector recommended) and angle in radians.
func QuatFromAxisAngle(axis Vec3, angle float32) Quat {
	s := float32(math.Sin(float64(angle) / 2))
	c := float32(math.Cos(float64(angle) / 2))
	a := axis.Normalize()
	return Quat{a.X * s, a.Y * s, a.Z * s, c}
}

// QuatIdentity is the no-rotation quaternion.
func QuatIdentity() Quat { return Quat{0, 0, 0, 1} }

// Mul composes two quaternions: q*o applies o first, then q.
func (q Quat) Mul(o Quat) Quat {
	return Quat{
		q.W*o.X + q.X*o.W + q.Y*o.Z - q.Z*o.Y,
		q.W*o.Y - q.X*o.Z + q.Y*o.W + q.Z*o.X,
		q.W*o.Z + q.X*o.Y - q.Y*o.X + q.Z*o.W,
		q.W*o.W - q.X*o.X - q.Y*o.Y - q.Z*o.Z,
	}
}

// Matrix converts the quaternion to a 4x4 rotation matrix.
func (q Quat) Matrix() Mat4 {
	x, y, z, w := q.X, q.Y, q.Z, q.W
	return Mat4{
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w), 0,
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w), 0,
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y), 0,
		0, 0, 0, 1,
	}
}

// ---- Mat4 ----

// Mat4Identity returns the 4x4 identity matrix.
func Mat4Identity() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// Mat4Translate returns a translation matrix.
func Mat4Translate(t Vec3) Mat4 {
	m := Mat4Identity()
	m[12] = t.X
	m[13] = t.Y
	m[14] = t.Z
	return m
}

// Mat4Scale returns a scaling matrix.
func Mat4Scale(s Vec3) Mat4 {
	m := Mat4Identity()
	m[0] = s.X
	m[5] = s.Y
	m[10] = s.Z
	return m
}

// Mat4Mul multiplies two matrices (column-major): result = a * b.
// Geometrically, b's transform is applied first, then a.
func Mat4Mul(a, b Mat4) Mat4 {
	var r Mat4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var s float32
			for k := 0; k < 4; k++ {
				s += a[k*4+j] * b[i*4+k]
			}
			r[i*4+j] = s
		}
	}
	return r
}

// Mat4Perspective builds a standard GL perspective projection.
// fovY is vertical field of view in radians; aspect = width/height.
func Mat4Perspective(fovY, aspect, near, far float32) Mat4 {
	f := 1 / float32(math.Tan(float64(fovY)/2))
	nf := 1 / (near - far)
	return Mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, (far + near) * nf, -1,
		0, 0, 2 * far * near * nf, 0,
	}
}

// Mat4Ortho builds an orthographic projection.
func Mat4Ortho(left, right, bottom, top, near, far float32) Mat4 {
	lr := 1 / (left - right)
	bt := 1 / (bottom - top)
	nf := 1 / (near - far)
	return Mat4{
		-2 * lr, 0, 0, 0,
		0, -2 * bt, 0, 0,
		0, 0, 2 * nf, 0,
		(left + right) * lr, (top + bottom) * bt, (far + near) * nf, 1,
	}
}

// Mat4LookAt builds a view matrix looking from eye toward target.
func Mat4LookAt(eye, target, up Vec3) Mat4 {
	f := target.Sub(eye).Normalize() // forward
	s := f.Cross(up).Normalize()     // side (right)
	u := s.Cross(f)                  // true up
	return Mat4{
		s.X, u.X, -f.X, 0,
		s.Y, u.Y, -f.Y, 0,
		s.Z, u.Z, -f.Z, 0,
		-s.Dot(eye), -u.Dot(eye), f.Dot(eye), 1,
	}
}

// Ptr returns a pointer to the matrix's first element — convenient
// for glUniformMatrix4fv(loc, 1, GL_FALSE, m.Ptr()).
func (m *Mat4) Ptr() *float32 {
	return &m[0]
}

// MulVec4 applies the matrix to a homogeneous vector (column-major:
// result[r] = Σ_c m[c*4+r]*v[c]).
func (m Mat4) MulVec4(v Vec4) Vec4 {
	return Vec4{
		X: m[0]*v.X + m[4]*v.Y + m[8]*v.Z + m[12]*v.W,
		Y: m[1]*v.X + m[5]*v.Y + m[9]*v.Z + m[13]*v.W,
		Z: m[2]*v.X + m[6]*v.Y + m[10]*v.Z + m[14]*v.W,
		W: m[3]*v.X + m[7]*v.Y + m[11]*v.Z + m[15]*v.W,
	}
}

// Mat4Inverse returns the inverse of m and whether it is invertible.
// Uses the cofactor (adjugate/determinant) method — the same routine
// Mesa's gluInvertMatrix uses on column-major GL matrices. Returns the
// identity + false when m is singular.
func Mat4Inverse(m Mat4) (Mat4, bool) {
	var inv Mat4
	inv[0] = m[5]*m[10]*m[15] - m[5]*m[11]*m[14] - m[9]*m[6]*m[15] + m[9]*m[7]*m[14] + m[13]*m[6]*m[11] - m[13]*m[7]*m[10]
	inv[4] = -m[4]*m[10]*m[15] + m[4]*m[11]*m[14] + m[8]*m[6]*m[15] - m[8]*m[7]*m[14] - m[12]*m[6]*m[11] + m[12]*m[7]*m[10]
	inv[8] = m[4]*m[9]*m[15] - m[4]*m[11]*m[13] - m[8]*m[5]*m[15] + m[8]*m[7]*m[13] + m[12]*m[5]*m[11] - m[12]*m[7]*m[9]
	inv[12] = -m[4]*m[9]*m[14] + m[4]*m[10]*m[13] + m[8]*m[5]*m[14] - m[8]*m[6]*m[13] - m[12]*m[5]*m[10] + m[12]*m[6]*m[9]
	inv[1] = -m[1]*m[10]*m[15] + m[1]*m[11]*m[14] + m[9]*m[2]*m[15] - m[9]*m[3]*m[14] - m[13]*m[2]*m[11] + m[13]*m[3]*m[10]
	inv[5] = m[0]*m[10]*m[15] - m[0]*m[11]*m[14] - m[8]*m[2]*m[15] + m[8]*m[3]*m[14] + m[12]*m[2]*m[11] - m[12]*m[3]*m[10]
	inv[9] = -m[0]*m[9]*m[15] + m[0]*m[11]*m[13] + m[8]*m[1]*m[15] - m[8]*m[3]*m[13] - m[12]*m[1]*m[11] + m[12]*m[3]*m[9]
	inv[13] = m[0]*m[9]*m[14] - m[0]*m[10]*m[13] - m[8]*m[1]*m[14] + m[8]*m[2]*m[13] + m[12]*m[1]*m[10] - m[12]*m[2]*m[9]
	inv[2] = m[1]*m[6]*m[15] - m[1]*m[7]*m[14] - m[5]*m[2]*m[15] + m[5]*m[3]*m[14] + m[13]*m[2]*m[7] - m[13]*m[3]*m[6]
	inv[6] = -m[0]*m[6]*m[15] + m[0]*m[7]*m[14] + m[4]*m[2]*m[15] - m[4]*m[3]*m[14] - m[12]*m[2]*m[7] + m[12]*m[3]*m[6]
	inv[10] = m[0]*m[5]*m[15] - m[0]*m[7]*m[13] - m[4]*m[1]*m[15] + m[4]*m[3]*m[13] + m[12]*m[1]*m[7] - m[12]*m[3]*m[5]
	inv[14] = -m[0]*m[5]*m[14] + m[0]*m[6]*m[13] + m[4]*m[1]*m[14] - m[4]*m[2]*m[13] - m[12]*m[1]*m[6] + m[12]*m[2]*m[5]
	inv[3] = -m[1]*m[6]*m[11] + m[1]*m[7]*m[10] + m[5]*m[2]*m[11] - m[5]*m[3]*m[10] - m[9]*m[2]*m[7] + m[9]*m[3]*m[6]
	inv[7] = m[0]*m[6]*m[11] - m[0]*m[7]*m[10] - m[4]*m[2]*m[11] + m[4]*m[3]*m[10] + m[8]*m[2]*m[7] - m[8]*m[3]*m[6]
	inv[11] = -m[0]*m[5]*m[11] + m[0]*m[7]*m[9] + m[4]*m[1]*m[11] - m[4]*m[3]*m[9] - m[8]*m[1]*m[7] + m[8]*m[3]*m[5]
	inv[15] = m[0]*m[5]*m[10] - m[0]*m[6]*m[9] - m[4]*m[1]*m[10] + m[4]*m[2]*m[9] + m[8]*m[1]*m[6] - m[8]*m[2]*m[5]

	det := m[0]*inv[0] + m[1]*inv[4] + m[2]*inv[8] + m[3]*inv[12]
	if det == 0 {
		return Mat4Identity(), false
	}
	d := 1 / det
	for i := range inv {
		inv[i] *= d
	}
	return inv, true
}
