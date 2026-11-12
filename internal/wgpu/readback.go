//go:build cgo && !js

package wgpu

/*
#include <stdlib.h>
#include "helpers.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Asynchronous readback, as in WebGPU: copy into a MapRead buffer with
// CommandEncoder.CopyBuffer, submit, then MapReadAsync and poll MapState
// until the map lands (frames later, without blocking the CPU on the
// GPU), and ReadMapped.

// MapState is GPUBuffer.mapState.
type MapState int

const (
	MapUnmapped MapState = iota // not mapped; a failed map returns here
	MapPending                  // mapAsync in flight
	MapMapped                   // ready for ReadMapped
)

// CopyBuffer records GPUCommandEncoder.copyBufferToBuffer: size bytes from
// src at srcOff to dst at dstOff. Offsets and size are multiples of 4 and
// both ranges lie inside their buffers; src needs BufferCopySrc and dst
// BufferCopyDst usage (validated by the implementation).
func (e *CommandEncoder) CopyBuffer(src *Buffer, srcOff uint64, dst *Buffer, dstOff, size uint64) error {
	if e == nil || e.c == nil || e.finished != nil || src == nil || src.c == nil || dst == nil || dst.c == nil || src == dst ||
		srcOff%4 != 0 || dstOff%4 != 0 || size%4 != 0 || srcOff+size > src.size || dstOff+size > dst.size ||
		srcOff+size < srcOff || dstOff+size < dstOff {
		return errors.New("wgpu: invalid buffer copy")
	}
	C.wgpuCommandEncoderCopyBufferToBuffer(e.c, src.c, C.uint64_t(srcOff), dst.c, C.uint64_t(dstOff), C.uint64_t(size))
	return nil
}

// MapReadAsync starts GPUBuffer.mapAsync(READ, off, size) and returns at
// once. Call it after the command buffers that write the buffer are
// submitted (a buffer with a pending map cannot be used by a submit). b
// needs BufferMapRead usage; off is a multiple of 8, size of 4.
func (b *Buffer) MapReadAsync(off, size uint64) error {
	if b == nil || b.c == nil || b.dev == nil || off%8 != 0 || size%4 != 0 || size == 0 || off+size > b.size || off+size < off {
		return errors.New("wgpu: invalid buffer map range")
	}
	if b.mapStatus != nil {
		return errors.New("wgpu: buffer map already pending or mapped")
	}
	// The callback writes the status into C memory: it may run after b
	// is garbage, so the cell outlives a buffer released mid-map.
	b.mapStatus = (*C.int)(C.calloc(1, C.size_t(unsafe.Sizeof(C.int(0)))))
	b.mapOff, b.mapSize = off, size
	C.quiWGPUMapReadAsync(b.c, C.size_t(off), C.size_t(size), b.mapStatus)
	return nil
}

// MapState processes the device's pending callbacks and reports the
// buffer's map state.
func (b *Buffer) MapState() MapState {
	if b == nil || b.c == nil || b.mapStatus == nil {
		return MapUnmapped
	}
	if *b.mapStatus == 0 {
		C.wgpuInstanceProcessEvents(b.dev.inst)
	}
	switch *b.mapStatus {
	case 0:
		return MapPending
	case 1:
		return MapMapped
	}
	C.free(unsafe.Pointer(b.mapStatus)) // the map failed (or was aborted)
	b.mapStatus = nil
	return MapUnmapped
}

// ReadMapped copies the mapped range and unmaps the buffer. It fails
// unless MapState is MapMapped.
func (b *Buffer) ReadMapped() ([]byte, error) {
	if b.MapState() != MapMapped {
		return nil, errors.New("wgpu: buffer is not mapped")
	}
	defer func() {
		C.wgpuBufferUnmap(b.c)
		C.free(unsafe.Pointer(b.mapStatus))
		b.mapStatus = nil
	}()
	p := C.wgpuBufferGetConstMappedRange(b.c, C.size_t(b.mapOff), C.size_t(b.mapSize))
	if p == nil {
		return nil, errors.New("wgpu: mapped range")
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), b.mapSize)...), nil
}
