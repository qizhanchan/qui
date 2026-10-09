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
	"image/color"
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

	// Cursor over the picture: arrow, hidden (the remote draws its own),
	// or a custom image cursor like a mirrored remote pointer — a 2x
	// image, so it is crisp on Retina.
	remoteCursor, err := qui.NewImageCursorScaled(ringCursor(48), 2, 12, 12)
	if err != nil {
		log.Fatal(err)
	}
	cursorNames := []string{"arrow", "hidden", "image"}
	cursorShapes := []qui.CursorShape{qui.CursorDefault, qui.CursorHidden, remoteCursor}
	cursorIdx := 0
	cursorBtn := widgets.NewButton("Cursor: arrow", nil)
	cursorBtn.OnClick = func() {
		cursorIdx = (cursorIdx + 1) % len(cursorShapes)
		fv.SetCursorShape(cursorShapes[cursorIdx])
		setText(cursorBtn, "Cursor: "+cursorNames[cursorIdx])
	}

	// Raw keyboard: while on and the picture is focused, every key —
	// Cmd+Q included — arrives here with its native code, as a viewer
	// would forward it. Ctrl+Alt+Escape (the default escape chord) turns it
	// back off.
	keyLog := widgets.NewLabel("keys: -")
	rawBtn := widgets.NewButton("Raw keys: off", nil)
	setRaw := func(on bool) {
		fv.SetRawKeyboard(on)
		if on {
			setText(rawBtn, "Raw keys: on")
			window.SetFocus(fv)
		} else {
			setText(rawBtn, "Raw keys: off")
		}
	}
	rawBtn.OnClick = func() { setRaw(!fv.WantsRawKeyboard()) }
	// System shortcuts (Cmd+Tab, Cmd+Space, Ctrl+arrows) too — needs the
	// Accessibility permission; only active while raw mode is.
	captureOn := false
	captureBtn := widgets.NewButton("System keys: off", nil)
	captureBtn.OnClick = func() {
		if err := window.SetSystemKeyCapture(!captureOn); err != nil {
			keyLog.SetText("keys: " + err.Error())
			logKey("capture: " + err.Error())
			return
		}
		captureOn = !captureOn
		if captureOn {
			setText(captureBtn, "System keys: on")
		} else {
			setText(captureBtn, "System keys: off")
		}
	}
	window.OnRawKeyboardEscape(func() {
		setRaw(false)
		keyLog.SetText("keys: escaped raw mode")
		logKey("escape chord")
	})

	fv.OnEvent(func(e qui.Event) bool {
		switch ev := e.(type) {
		case qui.KeyEvent:
			dir := "down"
			if ev.Type() == qui.EventKeyUp {
				dir = "up"
			}
			msg := fmt.Sprintf("key %s code=0x%02X mods=%d", dir, ev.ScanCode, ev.Mods)
			keyLog.SetText("keys: " + msg)
			logKey(msg)
			return fv.WantsRawKeyboard()
		case qui.CharEvent:
			logKey(fmt.Sprintf("char %q raw=%v", ev.Rune, ev.Raw))
			return fv.WantsRawKeyboard()
		}
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
		srcBtn, fitBtn, pauseBtn, cursorBtn, rawBtn, captureBtn, pointer)
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 8},
		controls, fv, keyLog, status)
	root.Style().Padding = qui.Insets{Top: 10, Right: 10, Bottom: 10, Left: 10}
	window.SetRoot(root)

	setupDump(window, pointer, &paused)
	setupKeyTest(app, window, func() { setRaw(true) }, func() { window.SetFocus(fv) })

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

// ringCursor draws an n×n ring with a dot — a stand-in for a remote
// pointer image.
func ringCursor(n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	c := float64(n-1) / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			d := dx*dx + dy*dy
			r := float64(n) / 2
			switch {
			case d < (r*0.15)*(r*0.15):
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			case d < (r*0.95)*(r*0.95) && d > (r*0.7)*(r*0.7):
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 200, B: 0, A: 255})
			}
		}
	}
	return img
}

// keyTrace (FRAMEVIEW_KEYLOG=1, implied by FRAMEVIEW_KEYTEST) logs every
// key event the view receives to stderr.
var keyTrace = os.Getenv("FRAMEVIEW_KEYTEST") != "" || os.Getenv("FRAMEVIEW_KEYLOG") != ""

func logKey(s string) {
	if keyTrace {
		log.Print(s)
	}
}

// setupKeyTest (FRAMEVIEW_KEYTEST=1, macOS) turns raw mode on, posts real
// AppKit key events — Cmd+Q, 'a', the escape chord, then 'b' — and quits:
// the log shows which reached the view, and the app surviving Cmd+Q shows
// the menu never saw it.
func setupKeyTest(app *qui.App, window *qui.Window, rawOn, focus func()) {
	if os.Getenv("FRAMEVIEW_KEYTEST") == "" {
		return
	}
	go func() {
		time.Sleep(time.Second)
		window.PostJob(focus)
		window.PostJob(rawOn)
		window.PostJob(func() {
			err := window.SetSystemKeyCapture(true)
			log.Printf("SetSystemKeyCapture: err=%v active=%v", err, window.SystemKeyCaptureActive())
		})
		time.Sleep(300 * time.Millisecond)
		keys := []struct {
			code  int
			chars string
			flags uint
		}{
			{0x0C, "q", nsCmd},             // Cmd+Q
			{0x00, "a", 0},                 // a
			{0x35, "\x1b", nsCtrl | nsAlt}, // escape chord
			{0x0B, "b", 0},                 // b, raw mode now off
		}
		for _, k := range keys {
			postSyntheticKey(k.code, k.chars, k.flags, true)
			postSyntheticKey(k.code, k.chars, k.flags, false)
			time.Sleep(200 * time.Millisecond)
		}
		time.Sleep(500 * time.Millisecond)
		log.Print("keytest done")
		window.PostJob(app.Quit)
	}()
}
