package scene3d

// Mesh is a GPU-resident indexed triangle mesh. Vertex layout is
// fixed: position (vec3) + normal (vec3) + uv (vec2), interleaved —
// 8 floats per vertex. Custom layouts are out of scope for the
// lightweight scene graph; users who need them implement their own
// material with a custom shader reading a custom VBO.
//
// Create via helpers (NewCubeMesh, NewPlaneMesh) or NewMesh with
// raw data. Upload is lazy — the first frame that draws the mesh uploads
// it to GPU buffers; Destroy releases them. After changing the arrays,
// call Invalidate to upload again.
type Mesh struct {
	Positions []Vec3
	Normals   []Vec3
	UVs       []Vec2
	Indices   []uint32

	gpu meshGPU // vertex / index buffers (gpu_render.go)
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

// Invalidate makes the next frame upload the mesh's arrays again.
func (m *Mesh) Invalidate() {
	if m != nil {
		m.gpu.release()
	}
}

// Destroy releases the mesh's GPU buffers (UI thread).
func (m *Mesh) Destroy() {
	if m != nil {
		m.gpu.release()
	}
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
