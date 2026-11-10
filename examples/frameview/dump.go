//go:build cgo && !js

package main

// FRAMEVIEW_DUMP_DIR=<dir> makes the demo read back the real presented
// frame (GPU-composited picture + 2D layer, which agent screenshots cannot
// see) at a few moments and write PNGs, so the zero-copy path can be
// checked headlessly:
//
//	running.png  — frames streaming
//	partial.png  — paused, then a partial repaint elsewhere in the window:
//	               the picture must survive (retained draw)
//	redraw.png   — paused, then a redraw-only frame (RequestGPURedraw)
//
// The capture uses a WebGPU post-render hook, so the window presents with
// WebGPU while dumping.

import (
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func setupDump(window *qui.Window, label *widgets.Label, paused *atomic.Bool) {
	dir := os.Getenv("FRAMEVIEW_DUMP_DIR")
	if dir == "" {
		return
	}
	var want atomic.Pointer[string]
	window.SetPostRender(func(f *qui.GPUFrame) {
		name := want.Swap(nil)
		if name == nil {
			return
		}
		f.Capture(func(img *image.RGBA) {
			out, err := os.Create(filepath.Join(dir, *name))
			if err != nil {
				log.Print(err)
				return
			}
			_ = png.Encode(out, img)
			_ = out.Close()
			log.Printf("dumped %s", *name)
		})
	})
	ask := func(name string) { want.Store(&name) }
	go func() {
		time.Sleep(2 * time.Second)
		ask("running.png")
		time.Sleep(time.Second)
		paused.Store(true)
		time.Sleep(time.Second)
		window.PostJob(func() {
			ask("partial.png")
			label.SetText("partial repaint")
		})
		time.Sleep(time.Second)
		ask("redraw.png")
		window.RequestGPURedraw()
	}()
}
