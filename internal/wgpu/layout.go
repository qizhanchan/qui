//go:build cgo && !js

package wgpu

/*
#include <stdlib.h>
#include "helpers.h"
*/
import "C"

// ShaderStage is a set of shader stages a binding is visible to.
type ShaderStage uint64

const (
	StageVertex   ShaderStage = 1
	StageFragment ShaderStage = 2
	StageCompute  ShaderStage = 4
)

// BindingType is what a bind group layout entry holds.
type BindingType int

const (
	BindUniformBuffer BindingType = iota
	BindStorageBuffer
	BindReadOnlyStorageBuffer
	BindTexture // texture_2d<f32>, filterable
	BindSampler // filtering sampler
	BindUnfilterableTexture
)

type BindGroupLayoutEntry struct {
	Binding    uint32
	Visibility ShaderStage
	Type       BindingType
}

// CreateBindGroupLayout makes an explicit layout, for pipelines that must
// share bind groups (automatic layouts belong to one pipeline each).
func (d *Device) CreateBindGroupLayout(label string, entries ...BindGroupLayoutEntry) *BindGroupLayout {
	var a arena
	defer a.free()
	es := allocN[C.WGPUBindGroupLayoutEntry](&a, len(entries))
	for i, e := range entries {
		es[i].binding = C.uint32_t(e.Binding)
		es[i].visibility = C.WGPUShaderStage(e.Visibility)
		switch e.Type {
		case BindUniformBuffer:
			es[i].buffer._type = C.WGPUBufferBindingType_Uniform
		case BindStorageBuffer:
			es[i].buffer._type = C.WGPUBufferBindingType_Storage
		case BindReadOnlyStorageBuffer:
			es[i].buffer._type = C.WGPUBufferBindingType_ReadOnlyStorage
		case BindTexture, BindUnfilterableTexture:
			es[i].texture.sampleType = C.WGPUTextureSampleType_Float
			if e.Type == BindUnfilterableTexture {
				es[i].texture.sampleType = C.WGPUTextureSampleType_UnfilterableFloat
			}
			es[i].texture.viewDimension = C.WGPUTextureViewDimension_2D
		case BindSampler:
			es[i].sampler._type = C.WGPUSamplerBindingType_Filtering
		}
	}
	ld := C.WGPUBindGroupLayoutDescriptor{label: a.str(label), entryCount: C.size_t(len(es))}
	if len(es) > 0 {
		ld.entries = &es[0]
	}
	return &BindGroupLayout{c: C.wgpuDeviceCreateBindGroupLayout(d.c, &ld)}
}

// PipelineLayout is the bind group layouts of a pipeline, group by group.
type PipelineLayout struct{ c C.WGPUPipelineLayout }

func (d *Device) CreatePipelineLayout(label string, groups ...*BindGroupLayout) *PipelineLayout {
	var a arena
	defer a.free()
	ls := allocN[C.WGPUBindGroupLayout](&a, len(groups))
	for i, g := range groups {
		ls[i] = g.c
	}
	pd := C.WGPUPipelineLayoutDescriptor{label: a.str(label), bindGroupLayoutCount: C.size_t(len(ls))}
	if len(ls) > 0 {
		pd.bindGroupLayouts = &ls[0]
	}
	return &PipelineLayout{c: C.wgpuDeviceCreatePipelineLayout(d.c, &pd)}
}

func (l *PipelineLayout) Release() {
	if l != nil && l.c != nil {
		C.wgpuPipelineLayoutRelease(l.c)
		l.c = nil
	}
}
