// FrameView demo: a producer goroutine pushes synthetic 60 fps frames
// into a media.FrameView, the way a remote-desktop viewer pushes decoded
// VideoToolbox output.
//
//	go run ./examples/frameview
//
// "Source" switches between the portable RGBA upload path and (macOS)
// IOSurface-backed CVPixelBuffers bound zero-copy through
// CVOpenGLTextureCache. "Fit" cycles contain / cover / stretch. Hover the
// picture to see MapToFrame translate the pointer into frame pixels; the
// status line shows push / present / drop counters and the
// push→present latency of the last frame.
package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/media"
	"github.com/qizhanchan/qui/widgets"
)

const frameW, frameH = 1280, 720

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("FrameView", 1000, 680)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	fv := media.NewFrameView()
	fv.SetFlex(1)
	fv.SetID("frame")
	fv.SetAccessibleName("frame view")

	var useCV atomic.Bool
	cv := newCVSource(frameW, frameH)
	useCV.Store(cv != nil && os.Getenv("FRAMEVIEW_RGBA") == "")

	status := widgets.NewLabel("")
	pointer := widgets.NewLabel("pointer: -")

	fits := []media.FitMode{media.FitContain, media.FitCover, media.FitStretch}
	fitNames := []string{"contain", "cover", "stretch"}
	fitIdx := 0
	fitBtn := widgets.NewButton("Fit: contain", nil)
	fitBtn.OnClick = func() {
		fitIdx = (fitIdx + 1) % len(fits)
		fv.SetFitMode(fits[fitIdx])
		setText(fitBtn, "Fit: "+fitNames[fitIdx])
	}
	srcName := func() string {
		if useCV.Load() {
			return "Source: CVPixelBuffer (zero-copy)"
		}
		return "Source: RGBA upload"
	}
	srcBtn := widgets.NewButton(srcName(), nil)
	srcBtn.OnClick = func() {
		if cv == nil {
			return
		}
		useCV.Store(!useCV.Load())
		setText(srcBtn, srcName())
	}
	var paused atomic.Bool
	pauseBtn := widgets.NewButton("Pause", nil)
	pauseBtn.OnClick = func() {
		paused.Store(!paused.Load())
		if paused.Load() {
			setText(pauseBtn, "Resume")
		} else {
			setText(pauseBtn, "Pause")
		}
	}

	fv.OnEvent(func(e qui.Event) bool {
		if me, ok := e.(qui.MouseEvent); ok && me.Type() == qui.EventMouseMove {
			x, y, inside := fv.MapToFrame(qui.Point{X: me.X, Y: me.Y})
			if inside {
				pointer.SetText(fmt.Sprintf("pointer: frame (%.0f, %.0f)", x, y))
			} else {
				pointer.SetText("pointer: outside picture")
			}
		}
		return false
	})

	var lastLatency atomic.Int64
	fv.OnPresent(func(p media.PresentInfo) { lastLatency.Store(int64(p.Latency())) })

	controls := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8, AlignItems: qui.AlignCenter},
		srcBtn, fitBtn, pauseBtn, pointer)
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 8},
		controls, fv, status)
	root.Style().Padding = qui.Insets{Top: 10, Right: 10, Bottom: 10, Left: 10}
	window.SetRoot(root)

	setupDump(window, pointer, &paused)

	if srv, err := agent.BindEnv(window); err == nil && srv != nil {
		window.OnClose(func() { _ = srv.Stop() })
	}

	// Producer: 60 fps, off the UI thread, never waiting for it.
	go func() {
		img := [2]*image.RGBA{}
		tick := time.NewTicker(time.Second / 60)
		defer tick.Stop()
		for n := 0; ; {
			<-tick.C
			if paused.Load() {
				continue
			}
			n++
			if useCV.Load() && cv != nil {
				cv.push(fv, n)
				continue
			}
			// PushRGBA takes ownership, so each frame gets its own image.
			img[n%2] = image.NewRGBA(image.Rect(0, 0, frameW, frameH))
			paint(img[n%2].Pix, img[n%2].Stride, frameW, frameH, n, false)
			fv.PushRGBA(img[n%2])
		}
	}()
	// Stats readout, twice a second, through the UI thread.
	go func() {
		for range time.Tick(500 * time.Millisecond) {
			st := fv.Stats()
			lat := time.Duration(lastLatency.Load())
			window.PostJob(func() {
				status.SetText(fmt.Sprintf("pushed %d · presented %d · dropped %d · last latency %v",
					st.Pushed, st.Presented, st.Dropped, lat.Round(10*time.Microsecond)))
			})
		}
	}()

	app.Run()
	fv.Destroy()
}

// paint renders frame n: a scrolling gradient with a bouncing square and
// a 1-px grid every 64 px (to judge scaling). bgra selects channel order.
func paint(pix []byte, stride, w, h, n int, bgra bool) {
	bx := (n * 7) % (w - 120)
	by := (n * 5) % (h - 120)
	for y := 0; y < h; y++ {
		row := pix[y*stride:]
		for x := 0; x < w; x++ {
			r := byte((x + n*4) * 255 / (w + 1))
			g := byte(y * 255 / h)
			b := byte(128 + 127*((n/30)%2))
			if x%64 == 0 || y%64 == 0 {
				r, g, b = 255, 255, 255
			}
			if x >= bx && x < bx+120 && y >= by && y < by+120 {
				r, g, b = 240, 60, 40
			}
			o := x * 4
			if bgra {
				row[o], row[o+1], row[o+2] = b, g, r
			} else {
				row[o], row[o+1], row[o+2] = r, g, b
			}
			row[o+3] = 255
		}
	}
}

func setText(b *widgets.Button, s string) {
	b.Text = s
	b.InvalidateLayout()
}
