// multiwindow-sessions — an iTerm-flavored demo of qui's real multi-window
// support: every "session" is its own top-level OS window with independent
// state, driven by reactive + htmlcss.
//
// Run: go run ./examples/multiwindow-sessions
//
// What it shows:
//   - h.OpenWindow bundles the per-window boilerplate (shared-context
//     window + GL renderer + min-size + position + Mount). Opening one from
//     ANY window spawns another real OS window.
//   - New windows CASCADE via WindowSpec.Position from the spawner's own
//     Position; the first window is centered with Window.CenterOn on the
//     primary monitor (qui.PrimaryMonitor / qui.Monitors).
//   - Each window mounts its own reactive Runtime + StyleEngine: the
//     command log in one window is fully independent of the others.
//   - Window.SetTitle updates the native title bar live as you type,
//     reflecting per-window state (the line count). Fullscreen toggles via
//     Window.SetFullscreen / IsFullscreen.
//   - Closing a window disposes just that session; the app exits when the
//     last window is gone.
//   - CROSS-WINDOW SHARED STATE with zero new engine machinery: a plain
//     reactive.Signal created at app scope is bound (.BindText) in every
//     window. Setting it from ANY window updates ALL windows live, because
//     BindWidget routes each update to the bound widget's own window via
//     PostJob. The "N open" counter and the broadcast banner in every
//     toolbar are the same two shared signals, seen from every window.
package main

import (
	_ "embed"
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// styleCSS is shared by every window. Each window compiles its own copy
// into its own StyleEngine — styling state is never shared across windows.
//
//go:embed style.css
var styleCSS string

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	hub := newHub(app)
	first, err := hub.open(nil) // first window has no spawner
	if err != nil {
		log.Fatal(err)
	}
	// AI-native operability: QUI_AGENT=1 lets you drive the first window
	// (e.g. click "New Window") from cmd/qui-agent.
	if srv, err := agent.BindEnv(first); err == nil && srv != nil {
		first.OnClose(func() { _ = srv.Stop() })
	}
	app.Run()
}

// sessionHub is the app-level shared state that multi-window apps need but
// reactive's per-window Runtimes don't provide.
//
//   - count is a monotonic session number for titling/cascading. It's only
//     touched on the main goroutine (window creation happens inside
//     main-thread event callbacks), so a plain int is safe — no locking.
//   - live and banner are SHARED reactive signals. They are NOT owned by any
//     window's Runtime: they're plain reactive.NewSignal values created once,
//     here, and bound (.BindText) inside every window. A Set from one window
//     fans out to all windows because BindWidget marshals each update to the
//     bound widget's own window. liveText is a derived signal (Map) so the
//     count renders as text.
type sessionHub struct {
	app   *qui.App
	count int

	live     *reactive.Signal[int]    // number of windows currently open
	liveText *reactive.Signal[string] // live, formatted for display
	banner   *reactive.Signal[string] // last broadcast message, seen everywhere
}

func newHub(app *qui.App) *sessionHub {
	live := reactive.NewSignal(0)
	return &sessionHub{
		app:      app,
		live:     live,
		liveText: reactive.Map(live, func(n int) string { return fmt.Sprintf("%d open", n) }),
		banner:   reactive.NewSignal("(no broadcasts yet)"),
	}
}

// open creates a new session window. spawner is the window the "New Window"
// click came from (nil for the very first window); it's used to cascade the
// new window's position next to its parent.
func (hub *sessionHub) open(spawner *qui.Window) (*qui.Window, error) {
	hub.count++
	n := hub.count

	// Where to place the new window: cascade down-right of its spawner, or
	// (for the first window) centered on the primary monitor's work area.
	var pos *qui.Point
	if spawner != nil {
		p := spawner.Position()
		pos = &qui.Point{X: p.X + 34, Y: p.Y + 34}
	}

	// Bump the shared live-window count BEFORE mounting so the new window
	// binds to the correct "N open" immediately, and every already-open
	// window updates via PostJob. (Undo it if window creation fails.)
	hub.live.Set(hub.live.Peek() + 1)

	// OpenWindow bundles NewSharedWindow + GL renderer + min-size + position
	// + Mount. Sharing the spawner's GL context reuses the font atlas across
	// sessions. The build closure gets the new window.
	win, err := h.OpenWindow(hub.app, h.WindowSpec{
		Title:     fmt.Sprintf("Session %d", n),
		Width:     720,
		Height:    420,
		MinWidth:  360,
		MinHeight: 240,
		Share:     spawner,
		CSS:       styleCSS,
		Position:  pos,
	}, func(win *qui.Window) h.Node { return session(hub, win, n) })
	if err != nil {
		hub.live.Set(hub.live.Peek() - 1)
		return nil, err
	}
	if spawner == nil {
		win.CenterOn(qui.PrimaryMonitor())
	}
	win.OnClose(func() { hub.live.Set(hub.live.Peek() - 1) })
	log.Printf("opened session %d at screen %v (scale %v)", n, win.Position(), win.ContentScale())
	return win, nil
}

// session is the per-window root component. Its state (the command log) is
// local to this window's reactive Runtime, so each window is independent.
func session(hub *sessionHub, win *qui.Window, n int) h.Node {
	lines := reactive.UseSignal([]string{
		fmt.Sprintf("session %d started — type a command and press Enter", n),
	})
	draft, setDraft := reactive.UseState("")

	run := func() {
		cmd := draft
		if cmd == "" {
			return
		}
		next := append(append([]string{}, lines.Peek()...), "$ "+cmd, fakeRun(cmd))
		lines.Set(next)
		setDraft("")
		// Live native-title update reflecting this window's own state.
		win.SetTitle(fmt.Sprintf("Session %d — %d lines", n, len(next)))
	}

	return h.Div(
		h.Div(
			h.Span(fmt.Sprintf("Session %d", n)).Class("title"),
			// Shared signals, bound in every window. BindText applies the
			// current value immediately (so a freshly-opened window is
			// correct) and on every future Set from any window.
			h.Span("").BindText(hub.liveText).Class("count"),
			h.Span("").BindText(hub.banner).Class("banner"),
			h.Button("Broadcast").Class("btn").
				OnClick(func() {
					hub.banner.Set(fmt.Sprintf("session %d says hi 👋", n))
				}),
			h.Button("New Window").Class("btn").
				OnClick(func() {
					if _, err := hub.open(win); err != nil {
						log.Println(err)
					}
				}),
			h.Button("Fullscreen").Class("btn").
				OnClick(func() {
					if win.IsFullscreen() {
						win.SetFullscreen(qui.Monitor{}) // back to windowed
					} else {
						win.SetFullscreen(qui.PrimaryMonitor())
					}
				}),
			h.Button("Close").Class("btn").
				OnClick(func() { hub.app.CloseWindow(win) }),
		).Class("toolbar"),

		h.For("log", lines, func(i int, ln string) h.Node {
			cls := "out"
			switch {
			case len(ln) >= 2 && ln[:2] == "$ ":
				cls = "prompt"
			case i == 0:
				cls = "hint"
			}
			return h.Div(h.Span(ln).Class(cls)).Class("row").Key(fmt.Sprintf("%d", i))
		}),

		h.Div(
			h.Span("›").Class("caret"),
			h.Input().Class("field").Placeholder("run a command…").
				Value(draft).OnInput(setDraft).OnSubmit(func(string) { run() }),
		).Class("composer"),
	).Class("app")
}

// fakeRun returns canned output so the demo has something to print without a
// real PTY — the point is the windowing, not the shell.
func fakeRun(cmd string) string {
	switch cmd {
	case "help":
		return "commands: help, date, whoami, echo <text>"
	case "date":
		return "Wed Jul  9 2026"
	case "whoami":
		return "qui"
	default:
		if len(cmd) > 5 && cmd[:5] == "echo " {
			return cmd[5:]
		}
		return fmt.Sprintf("%s: command not found", cmd)
	}
}
