//go:build !cgo || js

package wgpu

// Load always fails without cgo: there is no way to call the WebGPU C API.
func Load() error { return ErrUnavailable }
