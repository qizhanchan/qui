package qui

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
)

// Snapshot APIs — a debugging aid for developing widgets in headless
// environments or for capturing UI regressions in CI.
//
// Under the default CPU raster backend, GLRenderer composites every
// frame into a CPU-side *image.RGBA buffer before blitting it to the
// GL framebuffer (see renderer_gl.go End). The buffer persists between
// frames, so any code path that holds a reference to the renderer can
// read the last-rendered frame at any time — including from a one-shot
// Animator that fires N seconds after launch. Under the GPU raster
// backend the frame is read back from the GPU on demand instead
// (RasterBackend / CPUReadback); the API is the same either way.
//
// Typical use:
//
//	app, _ := qui.NewApp()
//	w, _ := app.NewWindow("demo", 800, 600)
//	w.SetRenderer(qui.NewGLRenderer())
//	w.SetRoot(myRoot)
//	w.RegisterAnimator(qui.NewOneShotTimer(5*time.Second, func() {
//	    img := w.Snapshot()
//	    qui.SaveSnapshotPNG("debug.png", img)
//	    os.Exit(0)
//	}))
//	app.Run()
//
// The buffer is a *copy* — callers may save, mutate, or pass it to
// downstream image processing without affecting subsequent frames.

// Snapshot returns a deep copy of the last fully-rendered frame, or
// nil if no frame has been rendered yet (the window's first Step has
// not completed). The returned image's bounds match the framebuffer
// size (physical pixels on HiDPI displays).
//
// Where the pixels come from depends on the active raster backend, and the
// caller should not have to care:
//
//   - CPU raster (the default): the retained frame buffer, copied.
//   - GPU raster (QUI_GPU_RASTER=1): read back from the backend's FBO. The
//     pixels were never in system memory — and the CPU-fallback buffer is
//     zeroed once uploaded — so without the readback this returned an empty
//     image, which encodes as a blank white PNG.
//
// Because the GPU path issues GL calls, call this from the goroutine that
// owns the window's GL context: the main goroutine, inside Step (animators,
// event handlers, or a PostJob job — Step makes the context current before
// draining jobs). Off-main-goroutine callers should use SnapshotScaled and
// friends, which serialize through that queue for exactly this reason.
func (r *GLRenderer) Snapshot() *image.RGBA {
	if r == nil {
		return nil
	}
	// A backend whose pixels live on the GPU offers a readback; one whose
	// pixels are already in memory does not, and falls through to the copy
	// below. Readback failure (no FBO allocated yet) also falls through
	// rather than reporting a blank frame as success.
	if rb, ok := r.backend.(CPUReadback); ok {
		if img := rb.ReadbackCPU(); img != nil {
			return img
		}
	}
	if r.img == nil {
		return nil
	}
	src := r.img
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}

// Snapshot returns the latest rendered frame for this window, or nil
// if the window's renderer is not a GLRenderer or no frame exists
// yet. Convenience wrapper that lets callers reach the snapshot
// without holding a renderer reference.
func (w *Window) Snapshot() *image.RGBA {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.Snapshot")
	if gl, ok := w.renderer.(*GLRenderer); ok {
		return gl.Snapshot()
	}
	return nil
}

// SaveSnapshotPNG writes img to path as a PNG file. Returns an error
// if img is nil, the file cannot be created, or the PNG encoder
// fails. Intended for use with Window.Snapshot — both functions are
// debugging utilities and panic-free.
func SaveSnapshotPNG(path string, img image.Image) error {
	if img == nil {
		return errors.New("qui: SaveSnapshotPNG: nil image")
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("qui: SaveSnapshotPNG: %w", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("qui: SaveSnapshotPNG: encode: %w", err)
	}
	return nil
}
