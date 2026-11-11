//go:build darwin && cgo

package scene3d

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/gpu"
)

// The scene renders with WebGPU: a lit cube in front of the camera shows
// up in the middle of the viewport, the clear color around it.
func TestViewportRendersCube(t *testing.T) {
	if _, err := gpu.Shared(); err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	gpu.SetErrorHandler(func(kind int, msg string) { t.Errorf("WebGPU error %d: %s", kind, msg) })
	defer gpu.SetErrorHandler(nil)

	scene := NewScene()
	scene.Camera.Position = Vec3{X: 0, Y: 0, Z: 3}
	scene.Camera.Target = Vec3{}
	cube := NewNode("cube")
	cube.Mesh = NewCubeMesh()
	cube.Material = NewUnlitMaterial(qui.Color{R: 1, G: 0.5, A: 1})
	cube.Lines = NewLineMesh([]Vec3{{X: -2, Y: -0.9, Z: 0.6}, {X: 2, Y: -0.9, Z: 0.6}})
	scene.Root.AddChild(cube)
	lit := NewNode("lit")
	lit.Mesh = NewPlaneMesh()
	lit.Material = NewLitMaterial(qui.Color{G: 1, A: 1})
	lit.Transform.Position = Vec3{Y: -3}
	scene.Root.AddChild(lit)
	vp := NewViewport(scene)
	vp.ClearColor = qui.Color{B: 1, A: 1}

	img, err := qui.RenderWithWebGPUForTest(vp, qui.Size{W: 64, H: 64}, 2)
	if err != nil {
		t.Skip(err)
	}
	px := func(x, y int) [4]uint8 {
		c := img.RGBAAt(x, y)
		return [4]uint8{c.R, c.G, c.B, c.A}
	}
	if p := px(32, 32); p[0] < 250 || p[1] < 120 || p[1] > 135 || p[2] > 5 {
		t.Errorf("center = %v, want the cube's orange", p)
	}
	if p := px(2, 2); p[2] < 250 || p[0] > 5 {
		t.Errorf("corner = %v, want the clear blue", p)
	}
}

// Meshes keep OpenGL's convention: counter-clockwise triangles (seen from
// the camera) are front faces, and back faces are culled.
func TestViewportCullsBackFaces(t *testing.T) {
	if _, err := gpu.Shared(); err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	quad := func(ccw bool) *Mesh {
		pos := []Vec3{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}
		idx := []uint32{0, 1, 2, 0, 2, 3}
		if !ccw {
			idx = []uint32{0, 2, 1, 0, 3, 2}
		}
		n := []Vec3{{Z: 1}, {Z: 1}, {Z: 1}, {Z: 1}}
		return NewMesh(pos, n, nil, idx)
	}
	for _, ccw := range []bool{true, false} {
		scene := NewScene()
		scene.Camera.Position = Vec3{Z: 3}
		node := NewNode("quad")
		node.Mesh = quad(ccw)
		node.Material = NewUnlitMaterial(qui.Color{R: 1, A: 1})
		scene.Root.AddChild(node)
		vp := NewViewport(scene)
		vp.ClearColor = qui.Color{B: 1, A: 1}
		img, err := qui.RenderWithWebGPUForTest(vp, qui.Size{W: 32, H: 32}, 1)
		if err != nil {
			t.Skip(err)
		}
		c := img.RGBAAt(16, 16)
		if visible := c.R > 200; visible != ccw {
			t.Errorf("ccw=%v: center %v, visible=%v", ccw, c, visible)
		}
	}
}

// Translucent Unlit color blends source-over (the shader premultiplies),
// and a Lit material draws lines (normal / uv read as zero).
func TestViewportTranslucentUnlitAndLitLines(t *testing.T) {
	if _, err := gpu.Shared(); err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	gpu.SetErrorHandler(func(kind int, msg string) { t.Errorf("WebGPU error %d: %s", kind, msg) })
	defer gpu.SetErrorHandler(nil)

	scene := NewScene()
	scene.Camera.Position = Vec3{Z: 3}
	node := NewNode("quad")
	node.Mesh = NewMesh([]Vec3{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}},
		[]Vec3{{Z: 1}, {Z: 1}, {Z: 1}, {Z: 1}}, nil, []uint32{0, 1, 2, 0, 2, 3})
	node.Material = NewUnlitMaterial(qui.Color{R: 1, A: 0.5})
	node.Lines = NewLineMesh([]Vec3{{X: -2, Y: -1.2}, {X: 2, Y: -1.2}})
	node.LineMaterial = NewLitMaterial(qui.Color{G: 1, A: 1})
	scene.Root.AddChild(node)
	vp := NewViewport(scene)
	vp.ClearColor = qui.Color{B: 1, A: 1}
	img, err := qui.RenderWithWebGPUForTest(vp, qui.Size{W: 32, H: 32}, 1)
	if err != nil {
		t.Skip(err)
	}
	// 50% red over opaque blue: about (128, 0, 128), not (255, 0, 128).
	c := img.RGBAAt(16, 16)
	if c.R < 118 || c.R > 138 || c.B < 118 || c.B > 138 {
		t.Errorf("center = %v, want ~50%% red over blue", c)
	}
}

// Instancing must read a different model matrix per copy and keep the
// composite at full size even when the offscreen targets are smaller.
func TestViewportScaledInstanceBatch(t *testing.T) {
	if _, err := gpu.Shared(); err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	gpu.SetErrorHandler(func(kind int, msg string) { t.Errorf("WebGPU error %d: %s", kind, msg) })
	defer gpu.SetErrorHandler(nil)

	scene := NewScene()
	scene.Camera = &Camera{
		Position: Vec3{Z: 4}, Up: Vec3{Y: 1},
		Near: 0.1, Far: 20, Ortho: true, OrthoHeight: 4,
	}
	b := &InstanceBatch{Mesh: NewCubeMesh(), Material: NewUnlitMaterial(qui.Color{R: 1, A: 1})}
	defer b.Mesh.Destroy()
	b.SetTransforms([]Transform{{Position: Vec3{X: -1.5}}, {Position: Vec3{X: 1.5}}})
	scene.Batches = []*InstanceBatch{b}
	vp := NewViewport(scene)
	defer vp.Destroy()
	vp.ClearColor = qui.Color{B: 1, A: 1}
	vp.SetRenderScale(0.5)
	img, err := qui.RenderWithWebGPUForTest(vp, qui.Size{W: 96, H: 64}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if vp.gpu.w != 48 || vp.gpu.h != 32 {
		t.Fatalf("target = %dx%d, want 48x32", vp.gpu.w, vp.gpu.h)
	}
	for _, x := range []int{24, 72} {
		if c := img.RGBAAt(x, 32); c.R < 245 || c.B > 10 {
			t.Errorf("instance pixel at %d = %v, want red", x, c)
		}
	}
	if c := img.RGBAAt(48, 32); c.B < 245 || c.R > 10 {
		t.Errorf("between instances = %v, want blue", c)
	}
}

// An Overlay node draws over nearer geometry: a small green cube behind a
// big red one still shows, and its nested children follow it.
func TestViewportOverlayIgnoresDepth(t *testing.T) {
	if _, err := gpu.Shared(); err != nil {
		t.Skipf("WebGPU unavailable: %v", err)
	}
	scene := NewScene()
	scene.Camera.Position = Vec3{Z: 4}
	scene.Camera.Target = Vec3{}
	front := NewNode("front")
	front.Mesh = NewCubeMesh()
	front.Material = NewUnlitMaterial(qui.Color{R: 1, A: 1})
	front.Transform.Scale = Vec3{X: 3, Y: 3, Z: 0.2}
	front.Transform.Position = Vec3{Z: 1}
	scene.Root.AddChild(front)
	gizmo := NewNode("gizmo")
	gizmo.Overlay = true
	marker := NewNode("marker")
	marker.Mesh = NewCubeMesh()
	marker.Material = NewUnlitMaterial(qui.Color{G: 1, A: 1})
	marker.Transform.Scale = Vec3{X: 0.4, Y: 0.4, Z: 0.4}
	gizmo.AddChild(marker)
	scene.Root.AddChild(gizmo)
	vp := NewViewport(scene)
	img, err := qui.RenderWithWebGPUForTest(vp, qui.Size{W: 64, H: 64}, 1)
	if err != nil {
		t.Skip(err)
	}
	if c := img.RGBAAt(32, 32); c.G < 250 || c.R > 5 {
		t.Errorf("center = %v, want the overlay's green over the red occluder", c)
	}
	if c := img.RGBAAt(12, 32); c.R < 250 || c.G > 5 {
		t.Errorf("side = %v, want the red occluder", c)
	}
	gizmo.Overlay = false
	vp.InvalidateScene()
	img, err = qui.RenderWithWebGPUForTest(vp, qui.Size{W: 64, H: 64}, 1)
	if err != nil {
		t.Skip(err)
	}
	if c := img.RGBAAt(32, 32); c.R < 250 {
		t.Errorf("without Overlay center = %v, want the occluder", c)
	}
}
