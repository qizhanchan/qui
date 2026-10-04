// examples/webview — smoke test for qui's webview/ subpackage.
//
// Layout: a navigation toolbar (Back / Forward / Reload + URL field +
// Go button) above a WebView that fills the remaining space. The URL
// field commits via Enter; Reload / Back / Forward map to the
// equivalent Page operations.
//
// Run with the CEF backend enabled (requires webview/scripts/fetch-cef.sh
// to have populated webview/lib/darwin/cef/ first):
//
//	go build -tags webview_cef -o webview-demo ./examples/webview
//	webview/scripts/package-app.sh ./webview-demo ./build/WebViewDemo.app
//	open ./build/WebViewDemo.app
//
// Without the tag (or on non-darwin), the binary still compiles and
// runs — opening the WebView fails with ErrNotSupported, which the
// status label surfaces so you can see the error path from the GUI.
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/webview"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	var initialURL string
	flag.StringVar(&initialURL, "url", "https://example.com", "initial URL to load")
	flag.Parse()

	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	if err := webview.PreInitialize(); err != nil {
		log.Printf("webview.PreInitialize: %v", err)
	}
	window, err := app.NewWindow("webview-demo", 1024, 720)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	view := webview.NewWebView(webview.Config{
		InitialURL: initialURL,
		UserAgent:  "qui/webview-demo (Macintosh; CEF)",
	})
	view.SetFlex(1) // fill remaining vertical space
	// Tear down CEF cleanly on window close. `defer view.Destroy()`
	// would fire only after app.Run returns, when the window event loop is
	// no longer pumping and CEF's async CloseBrowser tasks can be stranded.
	// OnClose runs while the platform window is still alive, so the
	// CefDoMessageLoopWork pump inside webview.Shutdown actually
	// makes progress and we get a clean exit.
	window.OnClose(func() {
		view.Destroy()
		if err := webview.Shutdown(); err != nil {
			log.Printf("webview.Shutdown: %v", err)
		}
	})

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

	backBtn := widgets.NewButton("‹", func() {
		_ = view.GoBack()
	})
	fwdBtn := widgets.NewButton("›", func() {
		_ = view.GoForward()
	})
	reloadBtn := widgets.NewButton("⟳", func() {
		_ = view.Reload()
	})
	goBtn := widgets.NewButton("Go", func() {
		loadCurrent()
	})
	// "JS" button loads the bridge demo page. Inside the page, the
	// "call" button exercises the JS→Go direction (window.qui.echo).
	jsBtn := widgets.NewButton("JS", func() {
		if err := view.LoadHTML(bridgeDemoHTML, "about:blank"); err != nil {
			status.SetText("load HTML error: " + err.Error())
		}
	})
	// "Eval" button exercises the Go→JS direction. Runs a small
	// expression to prove the DevTools-Protocol round-trip, then reads
	// window.__quiResult — which the bridge demo page populates after
	// the user clicks its "call" button. So the full round trip is:
	//   JS → call → Go echo handler → reply → window.__quiResult →
	//   Eval → EvaluateJS → status bar.
	evalBtn := widgets.NewButton("Eval", func() {
		sum, err := view.EvaluateJS("1 + 2")
		if err != nil {
			status.SetText("EvaluateJS error: " + err.Error())
			return
		}
		stored, err := view.EvaluateJS("window.__quiResult || '(nothing yet — click JS then call first)'")
		if err != nil {
			status.SetText("EvaluateJS __quiResult error: " + err.Error())
			return
		}
		status.SetText(fmt.Sprintf("EvaluateJS: 1+2 = %s ; __quiResult = %s",
			formatJSValue(sum), formatJSValue(stored)))
	})

	urlField.OnSubmit = func(_ string) {
		loadCurrent()
	}

	view.OnLoadStart(func(u string) {
		status.SetText("loading " + u + "…")
		urlField.Text = u
		window.InvalidateRect(urlField.Bounds())
	})
	view.OnLoadFinish(func(u string) {
		status.SetText("loaded " + u)
		urlField.Text = u
		window.InvalidateRect(urlField.Bounds())
	})
	view.OnTitleChanged(func(title string) {
		// Window has no SetTitle yet; surface the title via the
		// status bar so the demo still demonstrates the callback.
		status.SetText(fmt.Sprintf("title: %s", title))
	})

	// Demonstrate the JS↔Go bridge. The page can call
	// `await window.qui.echo(JSON.stringify({text: "hi"}))` and get
	// back the round-tripped payload tagged from Go.
	_ = view.RegisterHandler("echo", func(payload []byte) ([]byte, error) {
		// Just echo back so the demo HTML can verify the round trip.
		reply := fmt.Sprintf(`{"from":"go","echoed":%s}`, string(payload))
		return []byte(reply), nil
	})

	if err := view.Open(); err != nil {
		// Surface the error inline rather than fatal — the rest of
		// the UI is useful for showing where users will see it.
		status.SetText("open error: " + err.Error())
		log.Printf("webview.Open: %v", err)
	}

	toolbar := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 6, AlignItems: qui.AlignCenter},
		backBtn, fwdBtn, reloadBtn, urlField, goBtn, jsBtn, evalBtn,
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
	window.SetRoot(root)
	app.Run()
}

// formatJSValue renders a JSValue for the status bar. Just enough to
// prove the right kind came back — apps that want richer rendering
// should switch on JSValueKind and format per-type.
func formatJSValue(v webview.JSValue) string {
	switch v.Kind {
	case webview.JSKindNull:
		return "null"
	case webview.JSKindBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case webview.JSKindNumber:
		return fmt.Sprintf("%g", v.Number)
	case webview.JSKindString:
		return fmt.Sprintf("%q", v.String)
	case webview.JSKindJSON:
		return string(v.JSON)
	default:
		return fmt.Sprintf("<kind=%d>", v.Kind)
	}
}

// bridgeDemoHTML is loaded by the "JS" toolbar button. The page exercises
// the JS→Go side by calling window.qui.echo and rendering the reply;
// when the user hits the page button it calls back through echo and the
// host can also use EvaluateJS to read window.__quiResult.
const bridgeDemoHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>qui bridge demo</title>
<style>
body { font-family: -apple-system, sans-serif; padding: 24px; color: #222; }
button { padding: 8px 14px; font-size: 14px; }
pre { background: #f4f4f4; padding: 8px; border-radius: 4px; max-width: 600px; }
</style></head>
<body>
<h1>qui webview bridge</h1>
<p>Click the button below to call <code>window.qui.echo</code> with a JSON payload.
The Go-side handler should reply with the round-tripped payload tagged with
<code>from: "go"</code>.</p>
<button id="b">call window.qui.echo</button>
<pre id="out">(no result yet)</pre>
<script>
window.__quiResult = "";
document.getElementById('b').addEventListener('click', async () => {
  try {
    const reply = await window.qui.echo(JSON.stringify({hello: "world", n: 1}));
    window.__quiResult = reply;
    document.getElementById('out').textContent = reply;
  } catch (e) {
    document.getElementById('out').textContent = "error: " + e;
  }
});
</script>
</body></html>`
