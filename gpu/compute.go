package gpu

import "github.com/qizhanchan/qui/internal/wgpu"

// Compute: pipelines built from a WGSL @compute entry point, and passes
// that dispatch workgroups. Bind storage buffers with BindStorageBuffer
// (read_write) or BindReadOnlyStorageBuffer layouts.
type (
	ComputePipeline           = wgpu.ComputePipeline
	ComputePipelineDescriptor = wgpu.ComputePipelineDescriptor
	ComputePass               = wgpu.ComputePass
)
