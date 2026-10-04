package scene3d

import "github.com/qizhanchan/qui"

// 3D scene graph. Data-oriented: Node holds optional Mesh + Material
// plus a Transform; Scene holds a root Node, a Camera, and a slice
// of Lights. No heap allocations happen per frame during traversal —
// world matrices are cached on each Node and only recomputed when
// their Transform is mutated (via MarkDirty; see Transform.Invalidate).

// Transform is pos/rot/scale. Rotations are quaternions. Zero scale
// is interpreted as {1,1,1} so zero-valued structs work.
type Transform struct {
	Position Vec3
	Rotation Quat
	Scale    Vec3
}

// LocalMatrix returns the combined TRS matrix. Order: T * R * S.
func (t Transform) LocalMatrix() Mat4 {
	s := t.Scale
	if s == (Vec3{}) {
		s = Vec3{1, 1, 1}
	}
	rot := t.Rotation
	if rot == (Quat{}) {
		rot = QuatIdentity()
	}
	return Mat4Mul(Mat4Mul(Mat4Translate(t.Position), rot.Matrix()), Mat4Scale(s))
}

// Node is a scene graph element. Mesh and Material are optional — a
// node without them is a pure transform group (useful for grouping
// children).
type Node struct {
	Name      string
	Transform Transform
	Mesh      *Mesh
	Material  Material
	// Lines is an optional edge/wireframe overlay drawn after Mesh with
	// LineMaterial (defaults to a thin dark unlit line). Used for B-Rep
	// edges, grids, and axes.
	Lines        *LineMesh
	LineMaterial Material
	Children     []*Node
	Visible      bool
}

// NewNode creates a visible node with identity transform.
func NewNode(name string) *Node {
	return &Node{
		Name:      name,
		Transform: Transform{Scale: Vec3{1, 1, 1}, Rotation: QuatIdentity()},
		Visible:   true,
	}
}

// AddChild appends a child node.
func (n *Node) AddChild(c *Node) *Node {
	if n == nil || c == nil {
		return n
	}
	n.Children = append(n.Children, c)
	return n
}

// Camera represents a 3D viewpoint. Ortho toggles orthographic vs
// perspective; OrthoHeight is used in ortho mode (width derives from
// viewport aspect ratio).
type Camera struct {
	Position    Vec3
	Target      Vec3
	Up          Vec3
	FOV         float32 // radians, perspective only
	Near, Far   float32
	Ortho       bool
	OrthoHeight float32
}

// NewPerspectiveCamera returns a camera with common defaults.
// fovDeg is vertical FOV in degrees.
func NewPerspectiveCamera(fovDeg, near, far float32) *Camera {
	return &Camera{
		Position: Vec3{0, 0, 5},
		Target:   Vec3{0, 0, 0},
		Up:       Vec3{0, 1, 0},
		FOV:      fovDeg * (3.14159265 / 180),
		Near:     near,
		Far:      far,
	}
}

// ViewMatrix returns the camera's view transform.
func (c *Camera) ViewMatrix() Mat4 {
	return Mat4LookAt(c.Position, c.Target, c.Up)
}

// ProjectionMatrix returns the projection for the given aspect.
func (c *Camera) ProjectionMatrix(aspect float32) Mat4 {
	if c.Ortho {
		h := c.OrthoHeight
		if h == 0 {
			h = 2
		}
		w := h * aspect
		return Mat4Ortho(-w/2, w/2, -h/2, h/2, c.Near, c.Far)
	}
	return Mat4Perspective(c.FOV, aspect, c.Near, c.Far)
}

// Light is a marker interface for scene lights. Concrete types
// expose their data; Material.Bind reads them to set shader
// uniforms.
type Light interface {
	isLight()
}

// DirectionalLight is infinitely far, fully parallel — sun-like.
// Direction points FROM the light TOWARD the scene (so negating it
// gives the light-to-fragment direction used in shaders).
type DirectionalLight struct {
	Direction Vec3
	Color     qui.Color
	Intensity float32
}

func (DirectionalLight) isLight() {}

// PointLight emits in all directions. Range is the falloff radius
// beyond which contribution is clamped to zero.
type PointLight struct {
	Position  Vec3
	Color     qui.Color
	Intensity float32
	Range     float32
}

func (PointLight) isLight() {}

// Scene is a self-contained renderable: a root node tree, camera,
// ambient color, and zero or more lights.
type Scene struct {
	Root    *Node
	Camera  *Camera
	Lights  []Light
	Ambient qui.Color
}

// NewScene creates a scene with a default camera and ambient fill.
func NewScene() *Scene {
	s := &Scene{
		Root:    NewNode("root"),
		Camera:  NewPerspectiveCamera(60, 0.1, 100),
		Ambient: qui.Color{R: 0.15, G: 0.15, B: 0.2, A: 1},
	}
	return s
}

// firstDirectional returns the first DirectionalLight in the scene,
// or a default if none exists. Materials that only support one
// directional light (the baseline LitMaterial) use this.
func (s *Scene) firstDirectional() DirectionalLight {
	for _, l := range s.Lights {
		if d, ok := l.(DirectionalLight); ok {
			return d
		}
	}
	return DirectionalLight{
		Direction: Vec3{-0.3, -1, -0.3}.Normalize(),
		Color:     qui.Color{R: 1, G: 1, B: 1, A: 1},
		Intensity: 1,
	}
}
