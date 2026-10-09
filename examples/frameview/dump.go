package main

// FRAMEVIEW_DUMP_DIR=<dir> makes the demo read back the real composited
// framebuffer (GL + 2D layer, which agent screenshots cannot see) at a few
// moments and write PNGs, so the zero-copy path can be checked headlessly:
//
//	running.png  — frames streaming
//	partial.png  — paused, then a partial repaint elsewhere in the window:
//	               the picture must survive (retained GL draw)
//	glonly.png   — paused, then a GL-only frame (RequestGLRedraw)

import (
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func setupDump(window *qui.Window, label *widgets.Label, paused *atomic.Bool) {
	dir := os.Getenv("FRAMEVIEW_DUMP_DIR")
	if dir == "" {
		return
	}
	var want atomic.Pointer[string]
	window.SetPostRender(func(s qui.GLState) {
		name := want.Swap(nil)
		if name == nil {
			return
		}
		w, h := int(s.FramebufferSize.W), int(s.FramebufferSize.H)
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		gl.ReadPixels(0, 0, int32(w), int32(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
		// GL rows are bottom-up.
		flipped := image.NewRGBA(img.Rect)
		for y := 0; y < h; y++ {
			copy(flipped.Pix[y*flipped.Stride:(y+1)*flipped.Stride], img.Pix[(h-1-y)*img.Stride:(h-y)*img.Stride])
		}
		f, err := os.Create(filepath.Join(dir, *name))
		if err != nil {
			log.Print(err)
			return
		}
		_ = png.Encode(f, flipped)
		_ = f.Close()
		log.Printf("dumped %s", *name)
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
		ask("glonly.png")
		window.RequestGLRedraw()
	}()
}
