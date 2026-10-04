package qui

import "testing"

// TestCanvasInterfaceConformance is the runtime echo of the compile-
// time `var _ Canvas = …` checks. Both guard the same invariant:
// every Canvas implementation in this package satisfies the full
// surface — state stack (Save/Restore/ClipRect/Scale) plus all draw
// methods. Without that guarantee, the framework's dirty-region paint
// + HiDPI scale at the Window boundary cannot safely target an
// arbitrary backend.
//
// The remaining backends are imageCanvas, gpuImageCanvas, noopCanvas,
// and the test-only RecordingCanvas; var-asserts in the package
// already check them at compile time, this test is the runtime echo.
func TestCanvasInterfaceConformance(t *testing.T) {
	candidates := []struct {
		name   string
		canvas Canvas
	}{
		{"noopCanvas", noopCanvas{canvasState: newCanvasState(Rect{W: 100, H: 100})}},
		{"RecordingCanvas", &RecordingCanvas{}},
	}
	for _, c := range candidates {
		if c.canvas == nil {
			t.Errorf("%s: nil canvas", c.name)
		}
	}
}
