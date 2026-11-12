//go:build cgo && !js

package wgpu

import (
	"encoding/binary"
	"math"
	"testing"
)

// A compute shader doubles a storage buffer in place and the result reads back.
func TestComputeDoublesBuffer(t *testing.T) {
	d, err := Shared()
	if err != nil {
		t.Skip("no WebGPU device:", err)
	}
	const n = 1000
	data := make([]byte, 4*n)
	for i := range n {
		binary.LittleEndian.PutUint32(data[4*i:], math.Float32bits(float32(i)))
	}
	buf := d.CreateBufferInit("values", BufferStorage|BufferCopySrc, data)
	defer buf.Release()
	mod, err := d.CreateShaderModule("double", `
@group(0) @binding(0) var<storage, read_write> values: array<f32>;
@compute @workgroup_size(64) fn main(@builtin(global_invocation_id) id: vec3u) {
    if id.x < arrayLength(&values) { values[id.x] = values[id.x] * 2.0; }
}`)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	layout := d.CreateBindGroupLayout("values", BindGroupLayoutEntry{Binding: 0, Visibility: StageCompute, Type: BindStorageBuffer})
	defer layout.Release()
	pl := d.CreatePipelineLayout("double", layout)
	defer pl.Release()
	pipe, err := d.CreateComputePipeline(ComputePipelineDescriptor{Label: "double", Layout: pl, Module: mod})
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Release()
	group := d.CreateBindGroup(layout, BindGroupEntry{Binding: 0, Buffer: buf})
	defer group.Release()
	enc := d.CreateCommandEncoder()
	pass := enc.BeginComputePass("double")
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, group)
	pass.Dispatch((n+63)/64, 1, 1)
	pass.End()
	d.Submit(enc.Finish())
	out, err := d.ReadBuffer(buf, 0, 4*n)
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if v := math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:])); v != float32(2*i) {
			t.Fatalf("values[%d] = %v, want %v", i, v, 2*i)
		}
	}
	// A shader error comes back as an error, not a crash.
	bad, _ := d.CreateShaderModule("bad", `@compute @workgroup_size(1) fn main() {}`)
	defer bad.Release()
	if _, err := d.CreateComputePipeline(ComputePipelineDescriptor{Module: bad, Entry: "missing"}); err == nil {
		t.Fatal("a missing entry point was accepted")
	}
}

// A timed compute pass writes ordered timestamps; invalid writes are refused.
func TestComputePassTimestamps(t *testing.T) {
	d := sharedOrSkip(t)
	if !d.SupportsTimestampQueries() {
		t.Skipf("timestamp queries unavailable on %s", d.AdapterName())
	}
	q, err := d.NewTimestampQuery(2)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Release()
	buf := d.CreateBufferInit("values", BufferStorage, make([]byte, 4*4096))
	defer buf.Release()
	mod, err := d.CreateShaderModule("increment", `
@group(0) @binding(0) var<storage, read_write> values: array<u32>;
@compute @workgroup_size(64) fn main(@builtin(global_invocation_id) id: vec3u) {
    if id.x < arrayLength(&values) { values[id.x] = values[id.x] + 1u; }
}`)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Release()
	pipe, err := d.CreateComputePipeline(ComputePipelineDescriptor{Module: mod, Entry: "main"})
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Release()
	group := d.CreateBindGroup(pipe.BindGroupLayout(0), BindGroupEntry{Binding: 0, Buffer: buf})
	defer group.Release()
	enc := d.CreateCommandEncoder()
	defer enc.Release()
	if enc.BeginTimedComputePass("bad", &PassTimestampWrites{Query: q, Begin: 0, End: 2}) != nil {
		t.Fatal("out-of-bounds timestamp write accepted")
	}
	pass := enc.BeginTimedComputePass("increment", &PassTimestampWrites{Query: q, Begin: 0, End: 1})
	if pass == nil {
		t.Fatal("valid timestamp write rejected")
	}
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, group)
	pass.Dispatch(64, 1, 1)
	pass.End()
	if err := q.Resolve(enc, 2); err != nil {
		t.Fatal(err)
	}
	d.Submit(enc.Finish())
	got, err := q.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] == 0 || got[1] < got[0] {
		t.Fatalf("invalid compute timestamps: %v", got)
	}
}
