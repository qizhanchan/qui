// html-css example — renders a real HTML page + CSS with the htmlcss
// engine into the qui widget tree (no browser, no WebView).
//
// Run: go run ./examples/html-css
//
//	go run ./examples/html-css -raw   # no CSS — bare default widgets
//
// page.html + style.css are embedded so it runs from anywhere. The page is
// organized into numbered feature MODULES (see the comments in page.html and
// htmlcss/COVERAGE.md); it doubles as a verification harness for the engine.
//
// A few modules are interactive and wired here against element ids: the
// display:none toggle (module 09), the <form> submit (module 11), the
// pointer-events / DOM-event demos (module 13), and the
// <canvas> demos (module 12), which show the engine's "draw it yourself"
// hook — SetCanvasDraw for a paint callback, SetCanvas for a full custom
// widget with event handling.
//
// The -raw flag drops the stylesheet entirely so the page renders with only
// the engine's default (unstyled) look — a live counterpart to the
// widgets/scripts/compare-html.sh harness.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/widgets"
)

//go:embed page.html
var pageHTML string

//go:embed style.css
var pageCSS string

func main() {
	raw := flag.Bool("raw", false, "render without any CSS (bare default widgets)")
	flag.Parse()

	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	title := "qui — html-css engine"
	if *raw {
		title = "qui — html-css engine (raw, no CSS)"
	}
	window, err := app.NewWindow(title, 960, 720)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	// This demo page is a light HTML page, so run under the light theme —
	// otherwise qui's theme-derived chrome (e.g. the outer ScrollView's
	// scrollbar / gutter) stays dark by default and clashes with the content.
	qui.SetTheme(qui.LightTheme)

	css := pageCSS
	if *raw {
		css = "" // no stylesheet: fall back to the engine's default look
	}
	// RenderDoc (vs Render) hands back id/class indexes so we can wire the
	// interactive modules. The returned El tree stays mutable/restyleable.
	doc := htmlcss.RenderDoc(pageHTML, css, htmlcss.Options{
		Window:  window,
		BaseDir: "examples/html-css",
	})
	body := doc.Root

	wireToggle(doc)   // module 09: runtime display:none
	wireForm(doc)     // module 11: <form> submit
	wireCanvases(doc) // module 12: <canvas> widget self-draw
	wirePointer(doc)  // module 13: pointer-events fall-through
	wireEvents(doc)   // module 13: DOM-shaped events
	wirePicker(doc)   // module 11: <select> option values

	// Wrap in a ScrollView so pages taller than the window scroll.
	viewportW := window.Size().W
	content := body.Measure(qui.Size{W: viewportW, H: 0})

	scroll := widgets.NewScrollView()
	scroll.SetContent(body, qui.Size{W: viewportW, H: content.H})

	window.SetRoot(scroll)
	// Browser-style page zoom: Cmd/Ctrl+= / − / 0, trackpad pinch, and
	// two-finger double-tap. Opt-in, so an app that means something else by
	// "pinch" is unaffected — see Window.SetViewportZoomEnabled.
	window.SetViewportZoomEnabled(true)
	agent.BindEnv(window)
	app.Run()
}

// wireToggle wires the runtime display:none demo: clicking the button hides
// or shows a panel by swapping its class, which restyles it in place.
func wireToggle(doc htmlcss.RenderResult) {
	btn, ok := doc.ByID["toggle-btn"].(*htmlcss.El)
	if !ok {
		return
	}
	panel, _ := doc.ByID["toggle-panel"].(*htmlcss.El)
	hidden := false
	btn.SetOnClick(func() {
		if panel == nil {
			return
		}
		hidden = !hidden
		if hidden {
			panel.SetClass("panel hidden")
			btn.SetTextContent("Show panel")
		} else {
			panel.SetClass("panel")
			btn.SetTextContent("Hide panel")
		}
	})
}

// wireForm collects the form's named controls on submit and echoes them into
// the status line. Enter in the text field or clicking a submit button fires it.
func wireForm(doc htmlcss.RenderResult) {
	form, ok := doc.ByID["signup"].(*htmlcss.El)
	if !ok {
		return
	}
	status, _ := doc.ByID["form-status"].(*htmlcss.El)
	form.SetOnFormSubmit(func(data map[string]string) {
		if status == nil {
			return
		}
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+data[k])
		}
		status.SetTextContent("已提交：" + strings.Join(parts, "  ·  "))
	})
}

// wirePointer wires module 13: both buttons log their clicks, so the demo
// shows that the pointer-events:none scrim lets the press through while the
// plain scrim swallows it (that button never reports a click).
func wirePointer(doc htmlcss.RenderResult) {
	log, _ := doc.ByID["ptr-log"].(*htmlcss.El)
	wire := func(id, msg string) {
		btn, ok := doc.ByID[id].(*htmlcss.El)
		if !ok {
			return
		}
		btn.SetOnClick(func() {
			if log != nil {
				log.SetTextContent(msg)
			}
		})
	}
	wire("ptr-btn", "点击穿透了 pointer-events:none 的遮罩 ✓")
	wire("ptr-blocked", "这条永远不会出现——遮罩拦住了点击")
}

// wirePicker echoes the option-semantics form's submitted pairs, which is how
// the demo shows that <select> submits an option's `value` (e.g. "cn") rather
// than its visible label ("中国").
func wirePicker(doc htmlcss.RenderResult) {
	form, ok := doc.ByID["picker"].(*htmlcss.El)
	if !ok {
		return
	}
	status, _ := doc.ByID["picker-status"].(*htmlcss.El)
	form.SetOnFormSubmit(func(data map[string]string) {
		if status == nil {
			return
		}
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+data[k])
		}
		status.SetTextContent("提交值：" + strings.Join(parts, "  ·  "))
	})
}

// wireEvents wires the DOM-event demos in module 13. Each handler just writes
// what happened into the shared log line, which is enough to verify that the
// event fired, with the right payload, on the right element.
func wireEvents(doc htmlcss.RenderResult) {
	log, _ := doc.ByID["ev-log"].(*htmlcss.El)
	say := func(msg string) {
		if log != nil {
			log.SetTextContent(msg)
		}
	}
	if el, ok := doc.ByID["ev-hover"].(*htmlcss.El); ok {
		el.SetOnMouseEnter(func() { say("mouseenter ev-hover") })
		el.SetOnMouseLeave(func() { say("mouseleave ev-hover") })
	}
	if el, ok := doc.ByID["ev-dbl"].(*htmlcss.El); ok {
		clicks := 0
		el.SetOnClick(func() { clicks++; say(fmt.Sprintf("click #%d", clicks)) })
		el.SetOnDoubleClick(func() { say("dblclick ✓（注意单击计数也涨了两次）") })
	}
	if el, ok := doc.ByID["ev-wheel"].(*htmlcss.El); ok {
		// Returning true consumes the wheel, so the page does NOT scroll while
		// the pointer is over this card — the visible half of the contract.
		el.SetOnWheel(func(dx, dy float32) bool {
			say(fmt.Sprintf("wheel dx=%.1f dy=%.1f（已消费，页面不滚）", dx, dy))
			return true
		})
	}
	if el, ok := doc.ByID["ev-keys"].(*htmlcss.El); ok {
		el.SetOnFocus(func() { say("focus ev-keys —— 现在按键试试") })
		el.SetOnBlur(func() { say("blur ev-keys") })
		el.SetOnKeyDown(func(ke qui.KeyEvent) bool {
			say(fmt.Sprintf("keydown key=%v mods=%v", ke.Key, ke.Mods))
			return true
		})
	}
}

// wireCanvases wires module 12: two paint-callback canvases (a bar chart and a
// generative curve) via SetCanvasDraw, and one interactive custom widget (a
// scribble pad) via SetCanvas. The fourth canvas is left un-wired on purpose
// to show the engine's built-in placeholder.
func wireCanvases(doc htmlcss.RenderResult) {
	if bars, ok := doc.ByID["cv-bars"].(*htmlcss.El); ok {
		bars.SetCanvasDraw(drawBars)
	}
	if art, ok := doc.ByID["cv-art"].(*htmlcss.El); ok {
		art.SetCanvasDraw(drawArt)
	}
	if pad, ok := doc.ByID["cv-pad"].(*htmlcss.El); ok {
		pad.SetCanvas(newScribblePad())
	}
}

// drawBars paints a small rounded-bar chart inside the canvas bounds — a plain
// SetCanvasDraw callback that draws straight onto the frame canvas.
func drawBars(cv qui.Canvas, b qui.Rect) {
	values := []float32{0.35, 0.62, 0.48, 0.9, 0.55, 0.72}
	colors := []qui.Color{
		{R: 0.18, G: 0.44, B: 0.93, A: 1}, {R: 0.42, G: 0.36, B: 1, A: 1},
		{R: 0.09, G: 0.62, B: 0.35, A: 1}, {R: 0.96, G: 0.62, B: 0.11, A: 1},
		{R: 0.85, G: 0.24, B: 0.30, A: 1}, {R: 0.10, G: 0.68, B: 0.72, A: 1},
	}
	const padX, padTop, padBot = 16, 16, 22
	plot := qui.Rect{X: b.X + padX, Y: b.Y + padTop, W: b.W - 2*padX, H: b.H - padTop - padBot}
	// Baseline.
	base := qui.Color{R: 0.82, G: 0.84, B: 0.89, A: 1}
	cv.DrawLine(qui.Point{X: plot.X, Y: plot.Y + plot.H}, qui.Point{X: plot.X + plot.W, Y: plot.Y + plot.H}, base, 1)

	n := len(values)
	gap := float32(10)
	bw := (plot.W - gap*float32(n-1)) / float32(n)
	font := qui.Font{Size: 10}
	label := qui.Color{R: 0.4, G: 0.42, B: 0.48, A: 1}
	for i, v := range values {
		h := plot.H * v
		x := plot.X + float32(i)*(bw+gap)
		bar := qui.Rect{X: x, Y: plot.Y + plot.H - h, W: bw, H: h}
		cv.FillRoundedRect(bar, 4, colors[i])
		cv.DrawText("Q"+string(rune('1'+i)), qui.Rect{X: x, Y: plot.Y + plot.H + 4, W: bw, H: 14}, label, font)
	}
}

// drawArt paints a Lissajous curve as a single polyline — a parametric
// "generative" figure driven purely by SetCanvasDraw + trigonometry.
func drawArt(cv qui.Canvas, b qui.Rect) {
	cx, cy := b.X+b.W/2, b.Y+b.H/2
	rx, ry := b.W*0.4, b.H*0.4
	const steps = 400
	pts := make([]qui.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / steps * 2 * math.Pi
		x := cx + rx*float32(math.Sin(3*t+math.Pi/2))
		y := cy + ry*float32(math.Sin(4*t))
		pts = append(pts, qui.Point{X: x, Y: y})
	}
	cv.DrawPolyline(pts, qui.Color{R: 0.42, G: 0.36, B: 1, A: 1}, 2)
}

// scribblePad is a custom widget plugged into a <canvas> via SetCanvas. It
// captures mouse drags into strokes and repaints itself — a full "draw it
// yourself" widget with its own state and event handling, in contrast to the
// stateless SetCanvasDraw callbacks above.
type scribblePad struct {
	qui.BaseWidget
	strokes [][]qui.Point
	cur     []qui.Point
	drawing bool
}

func newScribblePad() *scribblePad {
	p := &scribblePad{}
	p.BaseWidget = qui.NewBaseWidget()
	p.SetSelf(p)
	return p
}

func (p *scribblePad) Measure(available qui.Size) qui.Size {
	// SetCanvas pushed the CSS width/height onto this widget's Style.
	w, h := p.Style().Width, p.Style().Height
	if w == 0 {
		w = 320
	}
	if h == 0 {
		h = 160
	}
	return qui.Size{W: w, H: h}
}

func (p *scribblePad) Draw(cv qui.Canvas) {
	b := p.Bounds()
	// Clip to bounds so a stroke that runs past the edge doesn't bleed out.
	id := cv.Save()
	cv.ClipRect(b)
	defer cv.RestoreTo(id)

	cv.FillRect(b, qui.Color{R: 1, G: 1, B: 1, A: 1})
	ink := qui.Color{R: 0.11, G: 0.30, B: 0.85, A: 1}
	for _, s := range p.strokes {
		if len(s) > 1 {
			cv.DrawPolyline(s, ink, 2.5)
		}
	}
	if len(p.cur) > 1 {
		cv.DrawPolyline(p.cur, ink, 2.5)
	}
	if len(p.strokes) == 0 && len(p.cur) == 0 {
		cv.DrawText("draw here ✏", qui.Rect{X: b.X + 12, Y: b.Y + 10, W: b.W, H: 18},
			qui.Color{R: 0.6, G: 0.63, B: 0.7, A: 1}, qui.Font{Size: 12})
	}
}

func (p *scribblePad) Handle(event qui.Event) bool {
	me, ok := event.(qui.MouseEvent)
	if !ok {
		return false
	}
	pt := qui.Point{X: me.X, Y: me.Y}
	switch me.Type() {
	case qui.EventMouseDown:
		p.drawing = true
		p.cur = []qui.Point{pt}
		p.Invalidate()
		return true
	case qui.EventMouseMove:
		if p.drawing {
			p.cur = append(p.cur, pt)
			p.Invalidate()
			return true
		}
	case qui.EventMouseUp:
		if p.drawing {
			p.drawing = false
			if len(p.cur) > 1 {
				p.strokes = append(p.strokes, p.cur)
			}
			p.cur = nil
			p.Invalidate()
			return true
		}
	}
	return false
}

func (p *scribblePad) HitTest(pt qui.Point) qui.Widget {
	if p.Bounds().Contains(pt) {
		return p
	}
	return nil
}
