package scene3d

import (
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
)

// defaultLineMat is the shared unlit material used for Node.Lines when
// the node sets no LineMaterial. Lazily compiled on first Bind.
var defaultLineMat = NewUnlitMaterial(qui.Color{R: 0.08, G: 0.09, B: 0.11, A: 1})

func defaultLineMaterial() Material { return defaultLineMat }

// LineMesh is a GPU-resident set of line segments — the counterpart to
// Mesh for edge / wireframe / grid / axis rendering. Vertices are
// position-only (location 0); every consecutive pair forms one segment
// (GL_LINES), so a cube's 12 edges are 24 vertices. It reuses the
// UnlitMaterial shader (which reads only aPos), so no dedicated line
// shader is needed.
//
// CAD apps lean on this heavily: B-Rep edges drawn over shaded faces
// are what makes a model read as a solid rather than a blob.
type LineMesh struct {
	Positions []Vec3

	vao      uint32
	vbo      uint32
	count    int32
	uploaded bool
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

func (m *LineMesh) upload() {
	if m.uploaded {
		return
	}
	n := len(m.Positions)
	if n == 0 {
		return
	}
	flat := make([]float32, 0, n*3)
	for _, p := range m.Positions {
		flat = append(flat, p.X, p.Y, p.Z)
	}
	gl.GenVertexArrays(1, &m.vao)
	gl.BindVertexArray(m.vao)
	gl.GenBuffers(1, &m.vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, m.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(flat)*4, gl.Ptr(flat), gl.STATIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 3*4, gl.PtrOffset(0))
	gl.BindVertexArray(0)
	m.count = int32(n)
	m.uploaded = true
}

// Draw issues the GL_LINES draw call. Assumes a shader (e.g. an
// UnlitMaterial) is already bound with the MVP + color uniforms set.
func (m *LineMesh) Draw() {
	if m == nil {
		return
	}
	if !m.uploaded {
		m.upload()
	}
	if m.vao == 0 {
		return
	}
	gl.BindVertexArray(m.vao)
	gl.DrawArrays(gl.LINES, 0, m.count)
	gl.BindVertexArray(0)
}

// Destroy releases GL resources.
func (m *LineMesh) Destroy() {
	if m == nil {
		return
	}
	if m.vbo != 0 {
		gl.DeleteBuffers(1, &m.vbo)
	}
	if m.vao != 0 {
		gl.DeleteVertexArrays(1, &m.vao)
	}
	m.vao, m.vbo = 0, 0
	m.uploaded = false
}
