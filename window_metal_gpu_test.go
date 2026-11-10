//go:build cgo && !js

package qui

import "testing"

// requestWebGPU makes canvas see a GPU widget's draw.
func requestWebGPU(t *testing.T, canvas Canvas) {
	t.Helper()
	g, ok := canvas.(GPUCanvas)
	if !ok {
		t.Fatalf("Metal canvas must answer GPUCanvas to detect WebGPU widgets")
	}
	g.QueueGPUDraw(func(*GPUFrame) {})
}
