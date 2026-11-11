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

const (
	maxTimestampQueries = 4096
	timestampBytes      = 8
	bufferQueryResolve  = 0x200 // WGPUBufferUsage_QueryResolve
)

// SupportsTimestampQueries reports whether the enabled device can write
// timestamps at the beginning and end of a render pass.
func (d *Device) SupportsTimestampQueries() bool {
	return d != nil && d.c != nil && d.timestamps && !Lost()
}

// TimestampQuery holds a query set and its readback buffers. At most one
// resolve may be pending at a time. Release the query to abandon a sample.
type TimestampQuery struct {
	device          *Device
	set             C.WGPUQuerySet
	resolve, mapped *Buffer
	capacity        int
	pending         *CommandEncoder
	pendingCount    int
}

// PassTimestampWrites selects two distinct indices in a timestamp query set.
// Begin and End write at the respective boundaries of the render pass.
type PassTimestampWrites struct {
	Query      *TimestampQuery
	Begin, End int
}

func (w *PassTimestampWrites) validFor(encoder *CommandEncoder) bool {
	q := w.Query
	return encoder != nil && encoder.c != nil && q != nil && q.set != nil &&
		q.device == encoder.device && q.device.SupportsTimestampQueries() && q.pending == nil &&
		w.Begin >= 0 && w.Begin < q.capacity && w.End >= 0 && w.End < q.capacity && w.Begin != w.End
}

// NewTimestampQuery allocates up to 4096 timestamp slots with readback space.
func (d *Device) NewTimestampQuery(count int) (*TimestampQuery, error) {
	if !d.SupportsTimestampQueries() {
		return nil, ErrUnavailable
	}
	if count < 1 || count > maxTimestampQueries {
		return nil, errors.New("wgpu: timestamp query count must be in [1, 4096]")
	}
	q := &TimestampQuery{device: d, capacity: count}
	desc := C.WGPUQuerySetDescriptor{_type: C.WGPUQueryType_Timestamp, count: C.uint32_t(count)}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	C.wgpuDevicePushErrorScope(d.c, C.WGPUErrorFilter_OutOfMemory)
	C.wgpuDevicePushErrorScope(d.c, C.WGPUErrorFilter_Validation)
	q.set = C.wgpuDeviceCreateQuerySet(d.c, &desc)
	size := uint64(count) * timestampBytes
	q.resolve = d.CreateBuffer("qui timestamp resolve", bufferQueryResolve|BufferCopySrc, size)
	q.mapped = d.CreateBuffer("qui timestamp readback", BufferCopyDst|BufferMapRead, size)
	validation := d.popScope("timestamp query")
	allocation := d.popScope("timestamp query allocation")
	if err := errors.Join(validation, allocation); err != nil {
		q.Release()
		return nil, err
	}
	if q.set == nil || q.resolve == nil || q.mapped == nil {
		q.Release()
		return nil, errors.New("wgpu: cannot allocate timestamp query")
	}
	return q, nil
}

// Resolve records the copy of the first count query slots into readback
// memory. End all passes on encoder before calling Resolve.
func (q *TimestampQuery) Resolve(encoder *CommandEncoder, count int) error {
	if q == nil || q.set == nil || !q.device.SupportsTimestampQueries() ||
		encoder == nil || encoder.c == nil || q.device != encoder.device ||
		q.pending != nil || count < 1 || count > q.capacity {
		return errors.New("wgpu: invalid timestamp resolve")
	}
	C.wgpuCommandEncoderResolveQuerySet(encoder.c, q.set, 0, C.uint32_t(count), q.resolve.c, 0)
	C.wgpuCommandEncoderCopyBufferToBuffer(encoder.c, q.resolve.c, 0, q.mapped.c, 0, C.uint64_t(count)*timestampBytes)
	q.pending, q.pendingCount = encoder, count
	return nil
}

// Read blocks until the submitted resolve completes and copies the first
// count timestamps in nanoseconds. count must match the preceding Resolve.
func (q *TimestampQuery) Read(count int) ([]uint64, error) {
	if q == nil || q.set == nil || !q.device.SupportsTimestampQueries() ||
		q.pending == nil || q.pending.finished == nil || !q.pending.finished.submitted || count != q.pendingCount {
		return nil, errors.New("wgpu: timestamp read requires a matching submitted resolve")
	}
	size := C.size_t(count) * timestampBytes
	if C.quiWGPUMapRead(q.device.inst, q.mapped.c, 0, size) == 0 {
		return nil, errors.New("wgpu: timestamp readback map failed")
	}
	defer C.wgpuBufferUnmap(q.mapped.c)
	p := C.wgpuBufferGetConstMappedRange(q.mapped.c, 0, size)
	if p == nil {
		return nil, errors.New("wgpu: timestamp readback range unavailable")
	}
	values := make([]uint64, count)
	copy(values, unsafe.Slice((*uint64)(p), count))
	q.pending, q.pendingCount = nil, 0
	return values, nil
}

// Release drops the query and its buffer references.
func (q *TimestampQuery) Release() {
	if q == nil {
		return
	}
	if q.set != nil {
		C.wgpuQuerySetRelease(q.set)
	}
	q.resolve.Release()
	q.mapped.Release()
	*q = TimestampQuery{}
}
