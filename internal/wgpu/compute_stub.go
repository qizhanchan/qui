//go:build !cgo || js

package wgpu

// Without cgo (or in the browser) there is no Dawn: compute is unavailable.

type ComputePipeline struct{}

type ComputePipelineDescriptor struct {
	Label  string
	Layout *PipelineLayout
	Module *ShaderModule
	Entry  string
}

func (_ *Device) CreateComputePipeline(_ ComputePipelineDescriptor) (*ComputePipeline, error) {
	return nil, ErrUnavailable
}
func (_ *ComputePipeline) BindGroupLayout(_ uint32) *BindGroupLayout { return nil }
func (_ *ComputePipeline) Release()                                  {}

type ComputePass struct{}

func (_ *CommandEncoder) BeginComputePass(_ string) *ComputePass { return &ComputePass{} }
func (_ *CommandEncoder) BeginTimedComputePass(_ string, _ *PassTimestampWrites) *ComputePass {
	return &ComputePass{}
}
func (_ *ComputePass) SetPipeline(_ *ComputePipeline)      {}
func (_ *ComputePass) SetBindGroup(_ uint32, _ *BindGroup) {}
func (_ *ComputePass) Dispatch(_, _, _ uint32)             {}
func (_ *ComputePass) End()                                {}

func (_ *Device) ReadBuffer(_ *Buffer, _, _ uint64) ([]byte, error) { return nil, ErrUnavailable }
