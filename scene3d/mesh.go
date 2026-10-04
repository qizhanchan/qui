package scene3d

import "github.com/go-gl/gl/v3.3-core/gl"

// Mesh is a GPU-resident indexed triangle mesh. Vertex layout is
// fixed: position (vec3) + normal (vec3) + uv (vec2), interleaved —
// 8 floats per vertex. Custom layouts are out of scope for the
// lightweight scene graph; users who need them implement their own
// material with a custom shader reading a custom VBO.
//
// Create via helpers (NewCubeMesh, NewPlaneMesh) or NewMesh with
// raw data. Upload is lazy — the first DrawCall uploads to GPU;
// calling Destroy releases the VBO/VAO/IBO.
type Mesh struct {
	Positions []Vec3
	Normals   []Vec3
	UVs       []Vec2
	Indices   []uint32

	vao        uint32
	vbo        uint32
	ibo        uint32
	indexCount int32
	uploaded   bool
}

// NewMesh builds a mesh from arrays. len(Positions) must equal
// len(Normals) and len(UVs) when they're non-nil.
func NewMesh(positions, normals []Vec3, uvs []Vec2, indices []uint32) *Mesh {
	return &Mesh{
		Positions: positions,
		Normals:   normals,
		UVs:       uvs,
		Indices:   indices,
	}
}

// upload interleaves the vertex arrays into a single VBO and
// initializes VAO pointers. Safe to call repeatedly — idempotent
// after first upload.
func (m *Mesh) upload() {
	if m.uploaded {
		return
	}
	n := len(m.Positions)
	if n == 0 {
		return
	}
	interleaved := make([]float32, 0, n*8)
	for i := 0; i < n; i++ {
		p := m.Positions[i]
		var nrm Vec3
		if i < len(m.Normals) {
			nrm = m.Normals[i]
		}
		var uv Vec2
		if i < len(m.UVs) {
			uv = m.UVs[i]
		}
		interleaved = append(interleaved,
			p.X, p.Y, p.Z,
			nrm.X, nrm.Y, nrm.Z,
			uv.X, uv.Y,
		)
	}

	gl.GenVertexArrays(1, &m.vao)
	gl.BindVertexArray(m.vao)
	gl.GenBuffers(1, &m.vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, m.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(interleaved)*4, gl.Ptr(interleaved), gl.STATIC_DRAW)

	// aPos (location 0): 3 floats
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(0))
	// aNormal (location 1): 3 floats
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(1, 3, gl.FLOAT, false, 8*4, gl.PtrOffset(3*4))
	// aUV (location 2): 2 floats
	gl.EnableVertexAttribArray(2)
	gl.VertexAttribPointer(2, 2, gl.FLOAT, false, 8*4, gl.PtrOffset(6*4))

	if len(m.Indices) > 0 {
		gl.GenBuffers(1, &m.ibo)
		gl.BindBuffer(gl.ELEMENT_ARRAY_BUFFER, m.ibo)
		gl.BufferData(gl.ELEMENT_ARRAY_BUFFER, len(m.Indices)*4, gl.Ptr(m.Indices), gl.STATIC_DRAW)
		m.indexCount = int32(len(m.Indices))
	}
	gl.BindVertexArray(0)
	m.uploaded = true
}

// Draw issues the GL draw call. Assumes a shader is already bound
// and the necessary uniforms have been set.
func (m *Mesh) Draw() {
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
	if m.indexCount > 0 {
		gl.DrawElements(gl.TRIANGLES, m.indexCount, gl.UNSIGNED_INT, gl.PtrOffset(0))
	} else {
		gl.DrawArrays(gl.TRIANGLES, 0, int32(len(m.Positions)))
	}
	gl.BindVertexArray(0)
}

// Destroy releases GL resources.
func (m *Mesh) Destroy() {
	if m == nil {
		return
	}
	if m.ibo != 0 {
		gl.DeleteBuffers(1, &m.ibo)
	}
	if m.vbo != 0 {
		gl.DeleteBuffers(1, &m.vbo)
	}
	if m.vao != 0 {
		gl.DeleteVertexArrays(1, &m.vao)
	}
	m.vao, m.vbo, m.ibo = 0, 0, 0
	m.uploaded = false
}

// NewCubeMesh builds a unit cube centered at origin (extent ±0.5)
// with per-face normals (so each face is flat-shaded).
func NewCubeMesh() *Mesh {
	// 6 faces × 4 verts = 24 unique vertices so face normals are
	// exact (shared-vertex cubes smear normals across faces).
	positions := []Vec3{
		// +X (right)
		{0.5, -0.5, -0.5}, {0.5, 0.5, -0.5}, {0.5, 0.5, 0.5}, {0.5, -0.5, 0.5},
		// -X (left)
		{-0.5, -0.5, 0.5}, {-0.5, 0.5, 0.5}, {-0.5, 0.5, -0.5}, {-0.5, -0.5, -0.5},
		// +Y (top)
		{-0.5, 0.5, -0.5}, {-0.5, 0.5, 0.5}, {0.5, 0.5, 0.5}, {0.5, 0.5, -0.5},
		// -Y (bottom)
		{-0.5, -0.5, 0.5}, {-0.5, -0.5, -0.5}, {0.5, -0.5, -0.5}, {0.5, -0.5, 0.5},
		// +Z (front)
		{-0.5, -0.5, 0.5}, {0.5, -0.5, 0.5}, {0.5, 0.5, 0.5}, {-0.5, 0.5, 0.5},
		// -Z (back)
		{0.5, -0.5, -0.5}, {-0.5, -0.5, -0.5}, {-0.5, 0.5, -0.5}, {0.5, 0.5, -0.5},
	}
	normals := []Vec3{
		{1, 0, 0}, {1, 0, 0}, {1, 0, 0}, {1, 0, 0},
		{-1, 0, 0}, {-1, 0, 0}, {-1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {0, 1, 0},
		{0, -1, 0}, {0, -1, 0}, {0, -1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, 1}, {0, 0, 1}, {0, 0, 1},
		{0, 0, -1}, {0, 0, -1}, {0, 0, -1}, {0, 0, -1},
	}
	uvs := []Vec2{
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
		{0, 0}, {1, 0}, {1, 1}, {0, 1},
	}
	var indices []uint32
	for face := uint32(0); face < 6; face++ {
		base := face * 4
		indices = append(indices,
			base+0, base+1, base+2,
			base+0, base+2, base+3,
		)
	}
	return NewMesh(positions, normals, uvs, indices)
}

// NewPlaneMesh builds a unit quad in the XZ plane (Y=0), facing +Y.
// Width/depth 1, centered at origin. Two triangles.
func NewPlaneMesh() *Mesh {
	positions := []Vec3{
		{-0.5, 0, -0.5},
		{0.5, 0, -0.5},
		{0.5, 0, 0.5},
		{-0.5, 0, 0.5},
	}
	normals := []Vec3{{0, 1, 0}, {0, 1, 0}, {0, 1, 0}, {0, 1, 0}}
	uvs := []Vec2{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	// CCW when viewed from +Y (the normal side): (v2-v0)×(v1-v0) = +Y.
	// The previous 0,1,2 / 0,2,3 order faced -Y, so the plane was
	// back-face culled whenever seen from above.
	indices := []uint32{0, 2, 1, 0, 3, 2}
	return NewMesh(positions, normals, uvs, indices)
}
