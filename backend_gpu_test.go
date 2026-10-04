package qui

import (
	"os"
	"testing"
)

// TestGPURasterFlagFromEnv checks that NewGLRenderer honors the
// QUI_GPU_RASTER environment variable. The rest of the GPU raster
// path can only be exercised with a live GL context, which no test
// here provides — this is the one env-var contract we can pin down.
func TestGPURasterFlagFromEnv(t *testing.T) {
	prev := os.Getenv("QUI_GPU_RASTER")
	defer func() {
		if prev == "" {
			os.Unsetenv("QUI_GPU_RASTER")
		} else {
			os.Setenv("QUI_GPU_RASTER", prev)
		}
	}()

	os.Setenv("QUI_GPU_RASTER", "1")
	if r := NewGLRenderer(); !r.GPURaster() {
		t.Fatal("QUI_GPU_RASTER=1 should enable GPU raster on new GLRenderers")
	}
	os.Setenv("QUI_GPU_RASTER", "0")
	if r := NewGLRenderer(); r.GPURaster() {
		t.Fatal("QUI_GPU_RASTER=0 should leave GPU raster off")
	}
	os.Unsetenv("QUI_GPU_RASTER")
	if r := NewGLRenderer(); r.GPURaster() {
		t.Fatal("no env var should default to CPU raster")
	}
}

// TestGPURasterToggle checks the SetGPURaster/GPURaster contract on
// GLRenderer directly. Doesn't touch GL.
func TestGPURasterToggle(t *testing.T) {
	r := &GLRenderer{}
	if r.GPURaster() {
		t.Fatal("fresh GLRenderer should have gpuRaster=false")
	}
	r.SetGPURaster(true)
	if !r.GPURaster() {
		t.Fatal("SetGPURaster(true) should flip the flag on")
	}
	r.SetGPURaster(false)
	if r.GPURaster() {
		t.Fatal("SetGPURaster(false) should flip the flag off")
	}
}
