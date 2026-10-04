// chrome-tabs example — a browser-style tab strip in the window's TITLE BAR.
//
// Run:            go run ./examples/chrome-tabs
// Drive/observe:  QUI_AGENT=1 go run ./examples/chrome-tabs
//
// The framework capability this demonstrates (qui/window_titlebar.go):
//
//   - window.SetTitlebarStyle(qui.TitlebarOverlay) makes the content area
//     span the full window height with a transparent system title bar, so
//     the widget tree paints the title area itself. Resize edges, traffic
//     lights, fullscreen and window snapping stay native.
//   - window.TitlebarInsets() reports how much room the system's own window
//     buttons need, so the strip leaves a gap for them instead of
//     hardcoding a per-OS pixel count.
//   - CSS `app-region: drag` (in style.css, on .tabstrip) makes pressing an
//     empty part of the strip move the window, via
//     window.BeginWindowDrag(); `app-region: no-drag` on the tabs and the
//     buttons carves them back out, exactly like Chrome.
//
// On a platform without overlay title bars SetTitlebarStyle returns false,
// insets are zero, and the same strip simply renders below the native title
// bar — the app still works, which is the point of checking rather than
// assuming.
//
// The tab strip itself is ordinary app code: clicking a tab shows that tab's
// pane, dragging reorders (El Draggable/OnDragOver, horizontal axis), ✕
// closes, + opens a new one. Every pane stays mounted and keeps its state
// (counter value, notes text, chosen swatch) while hidden, so switching tabs
// is instant and lossless — Chrome semantics.
package main

import (
	_ "embed"
	"fmt"
	"log"
	"strconv"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/icons"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	"github.com/qizhanchan/qui/svg"
)

//go:embed style.css
var styleCSS string

// paneKind is what a tab holds. A new tab cycles through them so every pane
// type is one click away.
type paneKind int

const (
	paneCounter paneKind = iota
	paneNotes
	panePalette
	paneAbout
)

func (k paneKind) title() string {
	switch k {
	case paneCounter:
		return "Counter"
	case paneNotes:
		return "Notes"
	case panePalette:
		return "Palette"
	}
	return "About"
}

func (k paneKind) icon() *svg.Document {
	switch k {
	case paneCounter:
		return icons.Timer
	case paneNotes:
		return icons.Description
	case panePalette:
		return icons.Palette
	}
	return icons.Info
}

// tab is one strip entry. id keys both the strip row and its pane, so a
// reorder moves the tab without remounting the pane behind it.
type tab struct {
	id   int
	kind paneKind
}

// paneData is one tab's pane state, kept alive while the pane is hidden.
type paneData struct {
	count int
	notes string
	color string
}

var swatches = []string{"#1a73e8", "#e8710a", "#0f9d58", "#d93025", "#8430ce", "#00838f"}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — chrome tabs", 960, 640)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())
	qui.SetTheme(qui.LightTheme)

	// The one line that turns the title area over to the widget tree. Do it
	// before the first frame so there is no visible reflow.
	overlay := window.SetTitlebarStyle(qui.TitlebarOverlay)

	h.Mount(window, styleCSS, func() h.Node { return App(window, overlay) })

	if srv, err := agent.BindEnv(window); err == nil && srv != nil {
		window.OnClose(func() { _ = srv.Stop() })
	}

	app.Run()
}

// App renders the strip plus one pane per tab.
func App(window *qui.Window, overlay bool) h.Node {
	tabs, setTabs := reactive.UseState([]tab{{id: 1, kind: paneCounter}, {id: 2, kind: paneNotes}})
	active, setActive := reactive.UseState(1)
	nextID := reactive.UseRef(3)
	// Per-tab pane state, keyed by tab id: a hidden pane keeps it, and a
	// closed tab's entry goes away with the tab.
	panes, setPanes := reactive.UseState(map[int]paneData{})
	editPane := func(id int, fn func(*paneData)) {
		next := make(map[int]paneData, len(panes)+1)
		for k, v := range panes {
			next[k] = v
		}
		d := next[id]
		fn(&d)
		next[id] = d
		setPanes(next)
	}
	// dropTarget is the tab the pointer is over mid-drag, and which side —
	// read when the drag ends to compute the new order.
	dropTarget := reactive.UseRef(struct {
		id    int
		after bool
	}{id: -1})

	// The system's window buttons overlap the top-left of the content in
	// overlay mode; ask how much room they need rather than guessing. Read
	// per render, so it follows the window — the one case it lags is native
	// fullscreen, where macOS hides the traffic lights but nothing here
	// re-renders until the next interaction.
	insets := window.TitlebarInsets()
	stripHeight := float32(40)
	if insets.Top > stripHeight {
		stripHeight = insets.Top
	}

	addTab := func() {
		id := *nextID
		*nextID++
		kind := paneKind(id % 4)
		setTabs(append(append([]tab{}, tabs...), tab{id: id, kind: kind}))
		setActive(id)
	}
	closeTab := func(id int) {
		rest := make([]tab, 0, len(tabs))
		for _, t := range tabs {
			if t.id != id {
				rest = append(rest, t)
			}
		}
		next := make(map[int]paneData, len(panes))
		for k, v := range panes {
			if k != id {
				next[k] = v
			}
		}
		setPanes(next)
		setTabs(rest)
		if active == id && len(rest) > 0 {
			setActive(rest[0].id)
		}
	}
	// moveRelative reorders src to just before/after dst — the drop side
	// comes from OnDragOver's `after`.
	moveRelative := func(src, dst int, after bool) {
		if src == dst {
			return
		}
		out := make([]tab, 0, len(tabs))
		var moved tab
		for _, t := range tabs {
			if t.id == src {
				moved = t
			}
		}
		for _, t := range tabs {
			if t.id == src {
				continue
			}
			if t.id == dst && !after {
				out = append(out, moved)
			}
			out = append(out, t)
			if t.id == dst && after {
				out = append(out, moved)
			}
		}
		setTabs(out)
	}

	// --- the strip ---------------------------------------------------------
	stripKids := []h.Node{
		// Gap for the traffic lights. Zero-width when the platform has no
		// overlay title bar, so the strip starts at the window edge.
		h.Div().Class("lights").Attr("style", fmt.Sprintf("width:%.0fpx", insets.Left)),
	}
	for _, t := range tabs {
		t := t
		key := strconv.Itoa(t.id)
		cls := "tab"
		if t.id == active {
			cls = "tab tab-active"
		}
		stripKids = append(stripKids, h.Div(
			h.Icon(t.kind.icon()).Class("favicon"),
			h.Span(t.kind.title()).Class("tab-title"),
			h.Button("").Icon(icons.Close).Class("tab-close").ID("close-"+key).
				Title("Close tab").OnClick(func() { closeTab(t.id) }),
		).
			Class(cls).
			ID("tab-"+key).
			Key(key).
			OnClick(func() { setActive(t.id) }).
			// Drag to reorder. The 4px dead zone means a plain click still
			// activates the tab instead of starting a drag.
			Draggable(key).
			DragHorizontal().
			OnDragOver(func(_ string, after bool) {
				dropTarget.id, dropTarget.after = t.id, after
			}).
			OnDragEnd(func() {
				if dropTarget.id >= 0 {
					moveRelative(t.id, dropTarget.id, dropTarget.after)
				}
				dropTarget.id = -1
			}))
	}
	stripKids = append(stripKids,
		h.Button("").Icon(icons.Add).Class("newtab").ID("new-tab").
			Title("New tab").OnClick(addTab),
		// The filler is what's left of the strip: pressing it drags the
		// window (app-region: drag is inherited from .tabstrip).
		h.Div().Class("strip-fill"),
	)
	strip := h.Div(stripKids).Class("tabstrip").
		Attr("style", fmt.Sprintf("height:%.0fpx", stripHeight))

	// --- panes: all mounted, only the active one displayed ----------------
	paneKids := []h.Node{}
	for _, t := range tabs {
		t := t
		cls := "pane"
		if t.id == active {
			cls = "pane pane-active"
		}
		paneKids = append(paneKids, h.Div(pane(window, t, panes[t.id], editPane, overlay)).
			Class(cls).Key("pane-"+strconv.Itoa(t.id)))
	}
	if len(tabs) == 0 {
		paneKids = append(paneKids, h.Div(
			h.P("No tabs open."),
			h.Button("New tab").Class("primary").OnClick(addTab),
		).Class("pane pane-active empty"))
	}

	return h.Div(strip, h.Div(paneKids).Class("content")).Class("window")
}

// pane builds the body of one tab. edit mutates that tab's slice of pane
// state through the component's setState, so a change re-renders this pane
// and leaves the others alone.
func pane(window *qui.Window, t tab, data paneData,
	edit func(int, func(*paneData)), overlay bool) []h.Node {

	switch t.kind {
	case paneCounter:
		return []h.Node{
			h.H2("Counter"),
			h.P("This pane keeps its value while other tabs are on screen — panes stay mounted, hidden by display:none."),
			h.Div(
				h.Button("−").Class("round").
					OnClick(func() { edit(t.id, func(d *paneData) { d.count-- }) }),
				h.Span(strconv.Itoa(data.count)).Class("count"),
				h.Button("+").Class("round").
					OnClick(func() { edit(t.id, func(d *paneData) { d.count++ }) }),
			).Class("row"),
		}
	case paneNotes:
		return []h.Node{
			h.H2("Notes"),
			h.P("A real TextArea widget, one per tab."),
			h.Textarea().Class("notes").Value(data.notes).
				Placeholder("Type here, switch tabs, come back…").
				OnInput(func(s string) { edit(t.id, func(d *paneData) { d.notes = s }) }),
		}
	case panePalette:
		picked := data.color
		if picked == "" {
			picked = swatches[0]
		}
		chips := []h.Node{}
		for _, c := range swatches {
			c := c
			chips = append(chips, h.Div().Class("chip").
				Attr("style", "background:"+c).
				OnClick(func() { edit(t.id, func(d *paneData) { d.color = c }) }))
		}
		return []h.Node{
			h.H2("Palette"),
			h.P("Pick a color; the choice belongs to this tab."),
			h.Div(chips).Class("chips"),
			h.Div(h.Span(picked).Class("swatch-label")).Class("swatch").
				Attr("style", "background:"+picked),
		}
	}

	// About: the capability itself, read back live from the window.
	ins := window.TitlebarInsets()
	status := "native title bar (overlay not supported on this platform)"
	if overlay {
		status = "overlay title bar — the strip above IS the title bar"
	}
	return []h.Node{
		h.H2("About this window"),
		h.Ul(
			h.Li("Mode: "+status),
			h.Li(fmt.Sprintf("Titlebar insets: top %.0f, left %.0f, right %.0f (points)",
				ins.Top, ins.Left, ins.Right)),
			h.Li("Drag the empty part of the strip to move the window; the tabs and buttons are app-region: no-drag."),
			h.Li("Double-click the empty strip to zoom — that comes from the platform's own window drag."),
		),
		h.Div(
			h.Button("Toggle maximize").Class("primary").
				OnClick(func() { window.ToggleMaximize() }),
		).Class("row"),
	}
}
