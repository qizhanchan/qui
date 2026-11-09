//go:build !darwin || !cgo || qui_nometal

package render

// MetalRendererAvailable reports whether this build contains the Metal
// renderer. It does not: MetalRenderer is CPURenderer here, so code that
// names it still compiles.
const MetalRendererAvailable = false

// MetalRenderer is CPURenderer in builds without Metal.
type MetalRenderer = CPURenderer

// NewMetalRenderer returns a CPURenderer in builds without Metal.
func NewMetalRenderer() *MetalRenderer { return NewCPURenderer() }
