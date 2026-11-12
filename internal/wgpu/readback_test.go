//go:build cgo && !js

package wgpu

import (
	"bytes"
	"testing"
	"time"
)

// A buffer copied into a MapRead buffer maps without blocking: the map
// is pending until the device reports it, then ReadMapped returns the
// copy and the buffer is unmapped again (GPUBuffer.mapAsync semantics).
func TestAsyncReadback(t *testing.T) {
	d, err := Shared()
	if err != nil {
		t.Skip("no WebGPU device:", err)
	}
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i * 7)
	}
	src := d.CreateBufferInit("src", BufferStorage|BufferCopySrc, data)
	defer src.Release()
	dst := d.CreateBuffer("dst", BufferMapRead|BufferCopyDst, 256)
	defer dst.Release()
	enc := d.CreateCommandEncoder()
	if err := enc.CopyBuffer(src, 0, dst, 0, 2); err == nil {
		t.Fatal("a copy of 2 bytes must be rejected")
	}
	if err := enc.CopyBuffer(src, 0, dst, 128, 256); err == nil {
		t.Fatal("a copy past the end must be rejected")
	}
	if err := enc.CopyBuffer(src, 64, dst, 0, 192); err != nil {
		t.Fatal(err)
	}
	d.Submit(enc.Finish())
	if dst.MapState() != MapUnmapped {
		t.Fatal("a fresh buffer is unmapped")
	}
	if _, err := dst.ReadMapped(); err == nil {
		t.Fatal("reading an unmapped buffer must fail")
	}
	if err := dst.MapReadAsync(0, 192); err != nil {
		t.Fatal(err)
	}
	if err := dst.MapReadAsync(0, 192); err == nil {
		t.Fatal("a second map while one is pending must fail")
	}
	deadline := time.Now().Add(5 * time.Second)
	for dst.MapState() == MapPending {
		if time.Now().After(deadline) {
			t.Fatal("map never completed")
		}
		time.Sleep(time.Millisecond)
	}
	got, err := dst.ReadMapped()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data[64:]) {
		t.Fatal("readback differs from the copied range")
	}
	if dst.MapState() != MapUnmapped {
		t.Fatal("ReadMapped unmaps")
	}
	// The buffer can be written and mapped again.
	enc = d.CreateCommandEncoder()
	if err := enc.CopyBuffer(src, 0, dst, 0, 8); err != nil {
		t.Fatal(err)
	}
	d.Submit(enc.Finish())
	if err := dst.MapReadAsync(0, 8); err != nil {
		t.Fatal(err)
	}
	for dst.MapState() == MapPending {
		time.Sleep(time.Millisecond)
	}
	if got, err := dst.ReadMapped(); err != nil || !bytes.Equal(got, data[:8]) {
		t.Fatalf("second readback %v %v", got, err)
	}
	// Releasing a buffer with a map in flight is safe.
	other := d.CreateBuffer("other", BufferMapRead|BufferCopyDst, 16)
	if err := other.MapReadAsync(0, 16); err != nil {
		t.Fatal(err)
	}
	other.Release()
	d.Tick()
}
