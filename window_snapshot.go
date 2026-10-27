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
// GL framebuffer (see internal/render/renderer_gl.go End). The buffer persists between
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

// Snapshot returns the latest rendered frame for this window, or nil
// if the window's renderer keeps no frame (it has no Snapshot method, as
// GLRenderer does) or no frame exists yet. Convenience wrapper that lets
// callers reach the snapshot without holding a renderer reference.
func (w *Window) Snapshot() *image.RGBA {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.Snapshot")
	if s, ok := w.renderer.(interface{ Snapshot() *image.RGBA }); ok {
		return s.Snapshot()
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
