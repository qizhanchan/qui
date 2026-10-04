package qui

import (
	"image"
	"image/color"
	"testing"
)

// Snapshot under the GPU raster backend used to return a blank frame: the
// pixels live in an FBO, and gpuBackend.End zeroes the CPU fallback buffer
// after uploading it, so the buffer Snapshot read was genuinely empty. It
// encoded as an all-white PNG, which reads as "the capture worked and the UI
// is blank" rather than as a failure.
//
// The GL half can't run without a context, so these tests pin the two pieces
// that can be wrong independently of GL: the row flip, and Snapshot's choice
// of source.

func TestFlipRowsVerticallyReversesRowOrder(t *testing.T) {
	// Distinct value per row so a wrong order is unambiguous.
	img := image.NewRGBA(image.Rect(0, 0, 3, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 3; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(10 * (y + 1)), G: uint8(x), B: 0, A: 0xff})
		}
	}
	flipRowsVertically(img)

	for y := 0; y < 4; y++ {
		wantRow := uint8(10 * (4 - y))
		for x := 0; x < 3; x++ {
			got := img.RGBAAt(x, y)
			if got.R != wantRow {
				t.Errorf("row %d col %d: R=%d, want %d", y, x, got.R, wantRow)
			}
			// Columns must NOT be mirrored — only rows.
			if got.G != uint8(x) {
				t.Errorf("row %d col %d: G=%d, want %d (columns must not flip)", y, x, got.G, x)
			}
		}
	}
}

func TestFlipRowsVerticallyHandlesOddHeightAndEdgeCases(t *testing.T) {
	// Odd height: the middle row must stay put and nothing may be dropped.
	img := image.NewRGBA(image.Rect(0, 0, 1, 5))
	for y := 0; y < 5; y++ {
		img.SetRGBA(0, y, color.RGBA{R: uint8(y), A: 0xff})
	}
	flipRowsVertically(img)
	for y := 0; y < 5; y++ {
		if got := img.RGBAAt(0, y).R; got != uint8(4-y) {
			t.Errorf("odd height: row %d = %d, want %d", y, got, 4-y)
		}
	}

	// A single row is already in order; flipping must not corrupt it.
	single := image.NewRGBA(image.Rect(0, 0, 2, 1))
	single.SetRGBA(0, 0, color.RGBA{R: 7, A: 0xff})
	single.SetRGBA(1, 0, color.RGBA{R: 9, A: 0xff})
	flipRowsVertically(single)
	if single.RGBAAt(0, 0).R != 7 || single.RGBAAt(1, 0).R != 9 {
		t.Error("single-row image was altered by the flip")
	}

	// Must not panic on degenerate inputs — a readback of a zero-sized
	// framebuffer is reachable during window teardown.
	flipRowsVertically(nil)
	flipRowsVertically(image.NewRGBA(image.Rect(0, 0, 0, 0)))
}

// readbackBackend is a RasterBackend that also offers a readback, standing
// in for gpuBackend. It embeds a real cpuBackend so it satisfies the whole
// interface — a method added to RasterBackend shouldn't break this test.
type readbackBackend struct {
	*cpuBackend
	readback *image.RGBA
	calls    int
}

func (b *readbackBackend) ReadbackCPU() *image.RGBA {
	b.calls++
	return b.readback
}

// CPUImage mirrors gpuBackend: the pixels are not in system memory, so the
// free accessor must report nothing.
func (b *readbackBackend) CPUImage() *image.RGBA { return nil }

func TestSnapshotPrefersTheBackendReadback(t *testing.T) {
	// The retained buffer holds the zeroes the GPU backend leaves behind;
	// the readback holds the real frame. Snapshot must return the latter.
	retained := image.NewRGBA(image.Rect(0, 0, 4, 4))
	frame := image.NewRGBA(image.Rect(0, 0, 4, 4))
	frame.SetRGBA(1, 2, color.RGBA{R: 0xAB, G: 0xCD, B: 0xEF, A: 0xff})

	backend := &readbackBackend{cpuBackend: newCPUBackend(retained), readback: frame}
	r := &GLRenderer{img: retained, backend: backend}

	got := r.Snapshot()
	if got == nil {
		t.Fatal("Snapshot returned nil despite an available readback")
	}
	if backend.calls != 1 {
		t.Errorf("ReadbackCPU called %d times, want 1", backend.calls)
	}
	if px := got.RGBAAt(1, 2); px.R != 0xAB || px.G != 0xCD || px.B != 0xEF {
		t.Errorf("Snapshot returned %v — it read the retained (empty) buffer, not the readback", px)
	}
}

func TestSnapshotFallsBackWhenReadbackDeclines(t *testing.T) {
	// A GPU backend whose FBO isn't allocated yet returns nil. That must
	// not become a blank frame reported as success: fall through to the
	// retained buffer, which is either the real thing or nil.
	retained := image.NewRGBA(image.Rect(0, 0, 2, 2))
	retained.SetRGBA(0, 0, color.RGBA{R: 0x11, A: 0xff})

	backend := &readbackBackend{cpuBackend: newCPUBackend(retained), readback: nil}
	r := &GLRenderer{img: retained, backend: backend}

	got := r.Snapshot()
	if got == nil {
		t.Fatal("Snapshot should fall back to the retained buffer")
	}
	if px := got.RGBAAt(0, 0); px.R != 0x11 {
		t.Errorf("fallback returned %v, want the retained buffer's pixel", px)
	}
	if got == retained {
		t.Error("Snapshot must return a copy, not the live buffer")
	}
}

func TestSnapshotUsesRetainedBufferForCPUBackend(t *testing.T) {
	// The CPU backend deliberately does NOT implement CPUReadback: its
	// pixels are already in memory, and a readback would be pure cost.
	retained := image.NewRGBA(image.Rect(0, 0, 2, 2))
	retained.SetRGBA(1, 1, color.RGBA{G: 0x77, A: 0xff})
	backend := newCPUBackend(retained)

	if _, ok := interface{}(backend).(CPUReadback); ok {
		t.Error("cpuBackend should not implement CPUReadback — CPUImage already exposes its pixels")
	}

	r := &GLRenderer{img: retained, backend: backend}
	got := r.Snapshot()
	if got == nil {
		t.Fatal("Snapshot returned nil for the CPU backend")
	}
	if px := got.RGBAAt(1, 1); px.G != 0x77 {
		t.Errorf("Snapshot returned %v, want the retained buffer's pixel", px)
	}
}

func TestSnapshotIsNilBeforeTheFirstFrame(t *testing.T) {
	// No backend and no buffer yet: Begin has never run.
	if img := (&GLRenderer{}).Snapshot(); img != nil {
		t.Errorf("Snapshot before the first frame returned %v, want nil", img.Bounds())
	}
	var nilRenderer *GLRenderer
	if img := nilRenderer.Snapshot(); img != nil {
		t.Error("Snapshot on a nil renderer must return nil")
	}
}

// The GPU backend must keep reporting "no CPU image" so the per-frame paths
// (End's blit, presentation via CPUFrameSource) never trigger a readback.
// That separation is the whole reason CPUReadback is a distinct interface.
func TestGPUBackendKeepsCPUImageNilSoPresentationNeverReadsBack(t *testing.T) {
	backend := &gpuBackend{}
	if img := backend.CPUImage(); img != nil {
		t.Error("gpuBackend.CPUImage must stay nil; a readback here would cost a GPU stall every frame")
	}
	// Not fboReady, so the readback declines rather than returning a
	// partly-filled image. This runs without a GL context precisely because
	// the guard comes first.
	if img := backend.ReadbackCPU(); img != nil {
		t.Error("ReadbackCPU with no FBO must return nil, not an empty image")
	}
	var nilBackend *gpuBackend
	if img := nilBackend.ReadbackCPU(); img != nil {
		t.Error("ReadbackCPU on a nil backend must return nil")
	}

	// And it must satisfy the interface Snapshot looks for.
	var _ CPUReadback = (*gpuBackend)(nil)
}
