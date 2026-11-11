package scene3d

import "github.com/qizhanchan/qui/gpu"

// InstanceMaterial is a scene material with built-in instanced vertex support.
type InstanceMaterial interface {
	Material
	instanceWGSL() string
}

func (*LitMaterial) instanceWGSL() string   { return litInstanceWGSL }
func (*UnlitMaterial) instanceWGSL() string { return unlitInstanceWGSL }

// InstanceBatch draws copies of one mesh and material in a single WebGPU
// draw. Transforms are world-space. Treat the mesh and material as opaque
// and use ordinary nodes when translucent draw order matters.
type InstanceBatch struct {
	Mesh     *Mesh
	Material InstanceMaterial

	matrices []float32
	revision uint64
}

// SetTransforms snapshots transforms for the next render. Call this on the
// viewport's UI thread and invalidate a non-continuous viewport afterward.
func (b *InstanceBatch) SetTransforms(transforms []Transform) {
	b.matrices = b.matrices[:0]
	for _, t := range transforms {
		matrix := t.LocalMatrix()
		b.matrices = append(b.matrices, matrix[:]...)
	}
	b.revision++
}

const instancePreamble = `
@group(0) @binding(2) var<storage, read> instance_models: array<mat4x4f>;
fn mesh_model(index: u32) -> mat4x4f {
    return instance_models[index];
}
`

type batchGPU struct {
	revision   uint64
	used       uint64
	models     *gpu.Buffer
	object     *gpu.Buffer
	group      *gpu.BindGroup
	params     MaterialParams
	haveParams bool
}

func (b *batchGPU) release() {
	b.group.Release()
	b.models.Release()
	b.object.Release()
}

func (v *Viewport) drawBatch(dev *gpu.Device, pass *gpu.RenderPass, batch *InstanceBatch) {
	if batch == nil || batch.Mesh == nil || batch.Material == nil || len(batch.matrices) == 0 {
		return
	}
	mesh := batch.Mesh.buffers(dev)
	if mesh.vb == nil {
		return
	}
	pipeline := pipelineFor(dev, batch.Material.instanceWGSL(), false, true, false)
	if pipeline == nil {
		return
	}
	g := &v.gpu
	if g.batches == nil {
		g.batches = make(map[*InstanceBatch]*batchGPU)
	}
	state := g.batches[batch]
	if state == nil || state.revision != batch.revision {
		models := dev.CreateBufferInit("scene3d instance transforms", gpu.BufferStorage, floatBytes(batch.matrices))
		object := dev.CreateBuffer("scene3d instance material", gpu.BufferUniform|gpu.BufferCopyDst, 160)
		if state != nil {
			state.release()
		}
		state = &batchGPU{
			revision: batch.revision,
			models:   models, object: object,
			group: dev.CreateBindGroup(instanceGroup,
				gpu.BindGroupEntry{Binding: 0, Buffer: g.frameBuf},
				gpu.BindGroupEntry{Binding: 1, Buffer: object},
				gpu.BindGroupEntry{Binding: 2, Buffer: models}),
		}
		g.batches[batch] = state
	}
	params := batch.Material.Params(v.Scene)
	if !state.haveParams || state.params != params {
		dev.WriteBuffer(state.object, 0, objectBytes(Mat4Identity(), params))
		state.params, state.haveParams = params, true
	}
	state.used = g.frame
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, state.group)
	drawMesh(pass, mesh, len(batch.matrices)/16)
}
