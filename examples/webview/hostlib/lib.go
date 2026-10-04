// hostlib is the Go side of the cefsimple-style host approach: a
// c-shared library loaded by webview/host/main.mm, which owns the
// C++ main() and calls CefInitialize from real C++ code (the path
// that works — initializing CEF from a cgo entry frame fails
// silently with exit_code=-1 on macOS for reasons that look related
// to Go-runtime signal / thread setup).
//
// The host drives a tick loop:
//
//	QuiStart()                  // build the qui app + widget tree
//	while QuiTick() != 0 {      // one frame of qui's event loop
//	    CefDoMessageLoopWork()  // one iteration of CEF's pump
//	}
//	QuiStop()                   // tear down before CefShutdown
//
// Build: use the standard CEF fetch + package flow from webview docs,
// then compile this package as c-shared if you need the hostlib path.
package main

import "C"

import (
	"fmt"
	"sync/atomic"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/webview"
	"github.com/qizhanchan/qui/widgets"
)

var (
	hostApp   *qui.App
	hostWin   *qui.Window
	hostView  *webview.WebView
	hostAlive atomic.Bool
)

const initialURL = "https://example.com"

//export QuiStart
func QuiStart() C.int {
	app, err := qui.NewApp()
	if err != nil {
		fmt.Println("[hostlib] qui.NewApp:", err)
		return -1
	}
	hostApp = app

	win, err := app.NewWindow("webview-host-demo", 1024, 720)
	if err != nil {
		fmt.Println("[hostlib] NewWindow:", err)
		return -2
	}
	hostWin = win
	win.SetRenderer(qui.NewGLRenderer())

	view := webview.NewWebView(webview.Config{
		InitialURL: initialURL,
		UserAgent:  "qui/webview-host-demo (Macintosh; CEF)",
	})
	view.SetFlex(1) // fill remaining vertical space
	hostView = view

	status := widgets.NewLabel("opening…")
	status.Style().Foreground = qui.Color{R: 0.7, G: 0.85, B: 1, A: 1}
	status.Style().Font.Size = 13

	urlField := widgets.NewInput("")
	urlField.Text = initialURL
	urlField.SetFlex(1)
	urlField.Style().Font.Size = 14

	loadCurrent := func() {
		if err := view.LoadURL(urlField.Text); err != nil {
			status.SetText("load error: " + err.Error())
		}
	}

	backBtn := widgets.NewButton("‹", func() { _ = view.GoBack() })
	fwdBtn := widgets.NewButton("›", func() { _ = view.GoForward() })
	reloadBtn := widgets.NewButton("⟳", func() { _ = view.Reload() })
	goBtn := widgets.NewButton("Go", func() { loadCurrent() })

	urlField.OnSubmit = func(_ string) { loadCurrent() }

	view.OnLoadStart(func(u string) {
		status.SetText("loading " + u + "…")
		urlField.Text = u
		win.InvalidateRect(urlField.Bounds())
	})
	view.OnLoadFinish(func(u string) {
		status.SetText("loaded " + u)
		urlField.Text = u
		win.InvalidateRect(urlField.Bounds())
	})
	view.OnTitleChanged(func(title string) {
		status.SetText(fmt.Sprintf("title: %s", title))
	})

	// The host has already called CefInitialize. PreInitialize and Open
	// both see qui_webview_cef_is_initialized()=1 and skip re-init. Open
	// still returns ErrNotSupported until backend_darwin.go's NewPage
	// stops being a Phase-A stub, but the runtime is healthy.
	if err := view.Open(); err != nil {
		status.SetText("open error: " + err.Error())
		fmt.Println("[hostlib] view.Open:", err)
	}

	toolbar := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 6, AlignItems: qui.AlignCenter},
		backBtn, fwdBtn, reloadBtn, urlField, goBtn,
	)
	toolbar.Style().Padding = qui.Insets{Top: 6, Right: 8, Bottom: 6, Left: 8}

	statusBar := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 6, AlignItems: qui.AlignCenter},
		status,
	)
	statusBar.Style().Padding = qui.Insets{Top: 4, Right: 8, Bottom: 4, Left: 8}

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 0},
		toolbar, view, statusBar,
	)
	win.SetRoot(root)

	hostAlive.Store(true)
	return 0
}

//export QuiTick
func QuiTick() C.int {
	if !hostAlive.Load() || hostApp == nil {
		return 0
	}
	// waitTimeoutSeconds=0 means glfw.PollEvents — non-blocking, since the
	// host's outer loop already paces itself with a 16ms sleep.
	if !hostApp.RunStep(0) {
		hostAlive.Store(false)
		return 0
	}
	return 1
}

//export QuiStop
func QuiStop() {
	if hostView != nil {
		hostView.Destroy()
	}
	hostApp = nil
	hostWin = nil
	hostView = nil
}

// Required by cgo even for buildmode=c-shared; never called.
func main() {}
