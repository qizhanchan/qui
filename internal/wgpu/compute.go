//go:build cgo && !js

package wgpu

/*
#include "helpers.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"unsafe"
)

// ComputePipeline is a compiled compute shader entry point.
type ComputePipeline struct{ c C.WGPUComputePipeline }

// ComputePipelineDescriptor names the shader entry and its layout. A nil
// Layout lets the implementation derive one from the shader ("auto").
type ComputePipelineDescriptor struct {
	Label  string
	Layout *PipelineLayout
	Module *ShaderModule
	Entry  string // default "main"
}

// CreateComputePipeline builds a compute pipeline, returning the
// validation message on error.
func (d *Device) CreateComputePipeline(desc ComputePipelineDescriptor) (*ComputePipeline, error) {
	if d == nil || d.c == nil || desc.Module == nil || desc.Module.c == nil {
		return nil, errors.New("compute pipeline needs a device and a shader module")
	}
	var a arena
	defer a.free()
	pd := (*C.WGPUComputePipelineDescriptor)(a.alloc(C.sizeof_WGPUComputePipelineDescriptor))
	pd.label = a.str(desc.Label)
	if desc.Layout != nil {
		pd.layout = desc.Layout.c
	}
	entry := desc.Entry
	if entry == "" {
		entry = "main"
	}
	pd.compute.module = desc.Module.c
	pd.compute.entryPoint = a.str(entry)
	// Dawn keeps error scopes on the calling OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	C.wgpuDevicePushErrorScope(d.c, C.WGPUErrorFilter_Validation)
	p := C.wgpuDeviceCreateComputePipeline(d.c, pd)
	if err := d.popScope(desc.Label); err != nil {
		if p != nil {
			C.wgpuComputePipelineRelease(p)
		}
		return nil, err
	}
	if p == nil {
		return nil, errors.New("compute pipeline creation failed")
	}
	return &ComputePipeline{c: p}, nil
}

// BindGroupLayout returns the layout of group index (useful with an
// auto-derived pipeline layout). The caller releases it.
func (p *ComputePipeline) BindGroupLayout(index uint32) *BindGroupLayout {
	if p == nil || p.c == nil {
		return nil
	}
	return &BindGroupLayout{c: C.wgpuComputePipelineGetBindGroupLayout(p.c, C.uint32_t(index))}
}

// Release frees the pipeline; nil-safe and idempotent.
func (p *ComputePipeline) Release() {
	if p != nil && p.c != nil {
		C.wgpuComputePipelineRelease(p.c)
		p.c = nil
	}
}

// ComputePass records dispatches into a command encoder. End it before
// the encoder records anything else.
type ComputePass struct{ c C.WGPUComputePassEncoder }

// BeginComputePass starts a compute pass.
func (e *CommandEncoder) BeginComputePass(label string) *ComputePass {
	var a arena
	defer a.free()
	pd := (*C.WGPUComputePassDescriptor)(a.alloc(C.sizeof_WGPUComputePassDescriptor))
	pd.label = a.str(label)
	return &ComputePass{c: C.wgpuCommandEncoderBeginComputePass(e.c, pd)}
}

// SetPipeline selects the pipeline for following dispatches.
func (p *ComputePass) SetPipeline(pl *ComputePipeline) {
	C.wgpuComputePassEncoderSetPipeline(p.c, pl.c)
}

// SetBindGroup binds group g at index.
func (p *ComputePass) SetBindGroup(index uint32, g *BindGroup) {
	C.wgpuComputePassEncoderSetBindGroup(p.c, C.uint32_t(index), g.c, 0, nil)
}

// Dispatch runs x·y·z workgroups.
func (p *ComputePass) Dispatch(x, y, z uint32) {
	C.wgpuComputePassEncoderDispatchWorkgroups(p.c, C.uint32_t(x), C.uint32_t(y), C.uint32_t(z))
}

// End finishes the pass and releases it.
func (p *ComputePass) End() {
	if p.c != nil {
		C.wgpuComputePassEncoderEnd(p.c)
		C.wgpuComputePassEncoderRelease(p.c)
		p.c = nil
	}
}

// ReadBuffer copies size bytes at off from b (which needs BufferCopySrc)
// and blocks until they are readable. Offsets and sizes are multiples of 4.
func (d *Device) ReadBuffer(b *Buffer, off, size uint64) ([]byte, error) {
	if b == nil || b.c == nil || size == 0 || off%4 != 0 || size%4 != 0 || off+size > b.size {
		return nil, errors.New("wgpu: invalid buffer read range")
	}
	dst := d.CreateBuffer("qui buffer readback", BufferMapRead|BufferCopyDst, size)
	if dst == nil {
		return nil, errors.New("wgpu: readback buffer")
	}
	defer dst.Release()
	enc := d.CreateCommandEncoder()
	C.wgpuCommandEncoderCopyBufferToBuffer(enc.c, b.c, C.uint64_t(off), dst.c, 0, C.uint64_t(size))
	d.Submit(enc.Finish())
	if C.quiWGPUMapRead(d.inst, dst.c, 0, C.size_t(size)) == 0 {
		return nil, errors.New("wgpu: map failed")
	}
	defer C.wgpuBufferUnmap(dst.c)
	p := C.wgpuBufferGetConstMappedRange(dst.c, 0, C.size_t(size))
	if p == nil {
		return nil, errors.New("wgpu: mapped range")
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), size)...), nil
}
