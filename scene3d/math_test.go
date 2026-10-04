package scene3d

import (
	"math"
	"testing"
)

func approxVec3(t *testing.T, got, want Vec3, eps float32, tag string) {
	t.Helper()
	if math.Abs(float64(got.X-want.X)) > float64(eps) ||
		math.Abs(float64(got.Y-want.Y)) > float64(eps) ||
		math.Abs(float64(got.Z-want.Z)) > float64(eps) {
		t.Errorf("%s: got %+v, want %+v", tag, got, want)
	}
}

func TestVec3Basics(t *testing.T) {
	a := Vec3{1, 2, 3}
	b := Vec3{4, 5, 6}
	approxVec3(t, a.Add(b), Vec3{5, 7, 9}, 1e-5, "add")
	approxVec3(t, a.Sub(b), Vec3{-3, -3, -3}, 1e-5, "sub")
	approxVec3(t, a.Mul(2), Vec3{2, 4, 6}, 1e-5, "mul")
	if got := a.Dot(b); math.Abs(float64(got-32)) > 1e-5 {
		t.Errorf("dot=%v, want 32", got)
	}
	approxVec3(t, a.Cross(b), Vec3{-3, 6, -3}, 1e-5, "cross")
	if got := (Vec3{3, 4, 0}).Length(); math.Abs(float64(got-5)) > 1e-5 {
		t.Errorf("length=%v, want 5", got)
	}
	approxVec3(t, (Vec3{0, 0, 5}).Normalize(), Vec3{0, 0, 1}, 1e-5, "normalize")
}

func TestQuatIdentityIsUnitMatrix(t *testing.T) {
	q := QuatIdentity()
	m := q.Matrix()
	expected := Mat4Identity()
	for i := range m {
		if math.Abs(float64(m[i]-expected[i])) > 1e-5 {
			t.Fatalf("identity quat matrix[%d]=%v, want %v", i, m[i], expected[i])
		}
	}
}

func TestQuatAxisAngleRotatesVector(t *testing.T) {
	// 90° around Y should map (1,0,0) → (0,0,-1)  (right-handed).
	q := QuatFromAxisAngle(Vec3{0, 1, 0}, float32(math.Pi/2))
	m := q.Matrix()
	x := Vec4{1, 0, 0, 1}
	rx := applyMat4(m, x)
	approxVec3(t, Vec3{rx.X, rx.Y, rx.Z}, Vec3{0, 0, -1}, 1e-4, "rotate x 90° Y")
}

// applyMat4 applies a column-major Mat4 to a Vec4.
func applyMat4(m Mat4, v Vec4) Vec4 {
	return Vec4{
		m[0]*v.X + m[4]*v.Y + m[8]*v.Z + m[12]*v.W,
		m[1]*v.X + m[5]*v.Y + m[9]*v.Z + m[13]*v.W,
		m[2]*v.X + m[6]*v.Y + m[10]*v.Z + m[14]*v.W,
		m[3]*v.X + m[7]*v.Y + m[11]*v.Z + m[15]*v.W,
	}
}

func TestMat4TranslateMovesPoint(t *testing.T) {
	m := Mat4Translate(Vec3{10, 20, 30})
	r := applyMat4(m, Vec4{1, 2, 3, 1})
	approxVec3(t, Vec3{r.X, r.Y, r.Z}, Vec3{11, 22, 33}, 1e-5, "translate")
}

func TestMat4LookAtPlacesCameraZ(t *testing.T) {
	// Camera at (0,0,5), looking toward origin, up=+Y. A point at
	// (0,0,0) maps to (0,0,-5) in view space.
	v := Mat4LookAt(Vec3{0, 0, 5}, Vec3{0, 0, 0}, Vec3{0, 1, 0})
	r := applyMat4(v, Vec4{0, 0, 0, 1})
	approxVec3(t, Vec3{r.X, r.Y, r.Z}, Vec3{0, 0, -5}, 1e-4, "lookAt origin")
}

func TestCameraProjectionAspectRatio(t *testing.T) {
	c := NewPerspectiveCamera(60, 0.1, 100)
	p := c.ProjectionMatrix(1)
	// Perspective projection has m[0] = f/aspect and m[5] = f. At aspect=1 they match.
	if math.Abs(float64(p[0]-p[5])) > 1e-5 {
		t.Errorf("square-aspect perspective should have m[0]==m[5], got %v vs %v", p[0], p[5])
	}
	p2 := c.ProjectionMatrix(2)
	// Wider aspect → smaller m[0], same m[5].
	if p2[0] >= p2[5] {
		t.Errorf("wide aspect should reduce m[0] below m[5]")
	}
}
