package scene3d

import "github.com/qizhanchan/qui"

// defaultLineMat is the shared unlit material used for Node.Lines when
// the node sets no LineMaterial.
var defaultLineMat = NewUnlitMaterial(qui.Color{R: 0.08, G: 0.09, B: 0.11, A: 1})

func defaultLineMaterial() Material { return defaultLineMat }

// LineMesh is a GPU-resident set of line segments — the counterpart to
// Mesh for edge / wireframe / grid / axis rendering. Vertices are
// position-only (location 0); every consecutive pair forms one segment
// (a line list), so a cube's 12 edges are 24 vertices. It reuses the
// UnlitMaterial shader (which reads only the position), so no dedicated
// line shader is needed.
//
// CAD apps lean on this heavily: B-Rep edges drawn over shaded faces
// are what makes a model read as a solid rather than a blob.
type LineMesh struct {
	Positions []Vec3

	gpu meshGPU
}

// NewLineMesh builds a line mesh from segment-endpoint pairs.
func NewLineMesh(positions []Vec3) *LineMesh {
	return &LineMesh{Positions: positions}
}

// LinesFromSegments builds a LineMesh from a flat xyz array where every
// two consecutive vertices (6 floats) form one segment — the shape
// occt.Shape.Edges returns.
func LinesFromSegments(flat []float32) *LineMesh {
	n := len(flat) / 3
	pts := make([]Vec3, n)
	for i := 0; i < n; i++ {
		pts[i] = Vec3{X: flat[i*3], Y: flat[i*3+1], Z: flat[i*3+2]}
	}
	return NewLineMesh(pts)
}

// Invalidate makes the next frame upload the positions again.
func (m *LineMesh) Invalidate() {
	if m != nil {
		m.gpu.release()
	}
}

// Destroy releases the line mesh's GPU buffer (UI thread).
func (m *LineMesh) Destroy() {
	if m != nil {
		m.gpu.release()
	}
}
