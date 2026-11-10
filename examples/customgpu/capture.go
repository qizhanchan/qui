//go:build cgo && !js

package main

import (
	"image"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/qizhanchan/qui"
)

// CUSTOMGPU_CAPTURE=out.png writes the presented frame (shader background
// and widgets) after a second and quits — a headless check of the example.
func captureIfAsked(window *qui.Window, app *qui.App) {
	out := os.Getenv("CUSTOMGPU_CAPTURE")
	if out == "" {
		return
	}
	go func() {
		time.Sleep(time.Second)
		window.PostJob(func() {
			window.SetPostRender(func(f *qui.GPUFrame) {
				window.SetPostRender(nil)
				f.Capture(func(img *image.RGBA) {
					fh, err := os.Create(out)
					if err != nil {
						log.Fatal(err)
					}
					_ = png.Encode(fh, img)
					_ = fh.Close()
					app.Quit()
				})
			})
		})
	}()
}
