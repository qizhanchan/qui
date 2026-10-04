package scene3d

import (
	"math"
	"testing"
)

func TestScreenRayHitsCenteredCube(t *testing.T) {
	cam := NewPerspectiveCamera(60, 0.1, 100)
	cam.Position = Vec3{X: 0, Y: 0, Z: 5}
	cam.Target = Vec3{X: 0, Y: 0, Z: 0}
	cam.Up = Vec3{X: 0, Y: 1, Z: 0}

	// Center of screen (ndc 0,0) should shoot straight down -Z and hit
	// the unit cube at the origin near its +Z face (z=0.5, distance 4.5).
	ray := ScreenRay(cam, 1.0, 0, 0)
	cube := NewCubeMesh()
	dist, ok := ray.IntersectMesh(cube, Mat4Identity())
	if !ok {
		t.Fatal("center ray should hit the cube")
	}
	if math.Abs(float64(dist-4.5)) > 0.1 {
		t.Errorf("hit distance = %v, want ~4.5", dist)
	}
}

func TestScreenRayMissesOffCenter(t *testing.T) {
	cam := NewPerspectiveCamera(60, 0.1, 100)
	cam.Position = Vec3{X: 0, Y: 0, Z: 5}
	// A ray toward the far corner should miss the small cube.
	ray := ScreenRay(cam, 1.0, 0.95, 0.95)
	cube := NewCubeMesh()
	if _, ok := ray.IntersectMesh(cube, Mat4Identity()); ok {
		t.Error("corner ray should miss the centered unit cube")
	}
}

func TestIntersectPlaneY(t *testing.T) {
	r := Ray{Origin: Vec3{X: 0, Y: 10, Z: 0}, Dir: Vec3{X: 0, Y: -1, Z: 0}}
	p, ok := r.IntersectPlaneY(0)
	if !ok {
		t.Fatal("downward ray should hit Y=0 plane")
	}
	if math.Abs(float64(p.Y)) > 1e-5 {
		t.Errorf("plane hit Y = %v, want 0", p.Y)
	}
}
