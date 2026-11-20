// Command pagehost shows an HTML document on a page thread of its own
// (RFC 0005): the document is styled, laid out and painted off the window's
// UI thread, so the window keeps responding while the page works.
//
// Click "Block the page for 3 s": the page thread sleeps, yet the window
// still scrolls the page (UI-thread scrolling of its committed tiles), the
// counter in the toolbar keeps ticking and the button stays clickable.
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/widgets"
)

func page() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><style>
body { margin: 0; font: 16px/1.5 system-ui, sans-serif; color: #1f2328 }
header { position: sticky; top: 0; background: #0969da; color: white; padding: 12px 24px; font-weight: 600 }
section { margin: 16px 24px; padding: 16px; border-radius: 8px }
@keyframes slide { from { transform: translateX(0) } to { transform: translateX(-50%) } }
.clip { overflow-x: clip; margin: 16px 0 }
.row { display: flex; gap: 12px; animation: slide 8s linear infinite }
.card { min-width: 160px; height: 60px; border-radius: 8px }
</style></head><body><header>A document on a page thread</header>
<div class=clip><div class=row>`)
	for i := 0; i < 16; i++ {
		fmt.Fprintf(&b, `<div class=card style="background:hsl(%d 70%% 60%%)"></div>`, i*45%360)
	}
	b.WriteString(`</div></div>`)
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, `<section style="background:hsl(%d 60%% 92%%)"><h3>Section %d</h3><p>The page renders on its own thread; the window shows its committed frames.</p></section>`, i*23%360, i+1)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	win, err := app.NewWindow("qui — PageHost", 900, 700)
	if err != nil {
		log.Fatal(err)
	}
	qui.SetTheme(qui.LightTheme)
	htmlcss.SetWebCompat(true)

	// One page thread can drive several pages; this demo has one.
	thread := qui.NewPageThread("demo")
	host := thread.NewPageHost(qui.PageHostConfig{Width: 900, Height: 640, Scale: win.DevicePixelRatio(),
		Setup: func(w *qui.Window) { // on the page thread
			view := widgets.NewScrollView()
			view.PixelScroll, view.FixedViewport = true, true // composited: the UI thread can scroll it
			doc := htmlcss.RenderDoc(page(), "", htmlcss.Options{Window: w, DocumentRoot: true, ViewportWidth: 900})
			view.SetContentAuto(doc.Root)
			w.SetRoot(view)
		}})

	status := widgets.NewLabel("")
	block := widgets.NewButton("Block the page for 3 s", func() {
		host.Do(func(*qui.Window) { time.Sleep(3 * time.Second) }) // a long task on the page thread
	})
	start := time.Now()
	go func() {
		for range time.Tick(100 * time.Millisecond) {
			win.PostJob(func() {
				busy, since := thread.Busy()
				s := fmt.Sprintf("UI thread alive: %.1fs", time.Since(start).Seconds())
				if busy && time.Since(since) > 200*time.Millisecond {
					s += fmt.Sprintf("  —  page busy for %.1fs", time.Since(since).Seconds())
				}
				status.SetText(s)
			})
		}
	}()
	bar := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 16, AlignItems: qui.AlignCenter}, block, status)
	pv := qui.NewPageView(host)
	pv.Style().Grow = 1
	win.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, bar, pv))
	win.OnClose(thread.Close)
	app.Run()
}
