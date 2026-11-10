//go:build !cgo || js

package qui

// applyGPUHooks: there are no GPU hooks without WebGPU.
func (w *Window) applyGPUHooks() {}
