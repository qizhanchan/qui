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
