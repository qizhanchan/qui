//go:build cgo && !js

package wgpu

import "testing"

func TestFlagConstantsMatchHeader(t *testing.T) {
	if name := flagConstMismatch(); name != "" {
		t.Fatalf("%s differs from webgpu.h", name)
	}
}
