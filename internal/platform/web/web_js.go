//go:build js && wasm

// Package web is the browser implementation of the platform seam (GOOS=js GOARCH=wasm).
//
// A qui window is a <canvas>. The first window fills the page — the page
// IS the app's window, and it follows the browser viewport the way a
// maximized desktop window follows its screen. Further windows (dialogs
// spawned as windows, multi-window apps) become absolutely positioned
// canvases layered over it.
//
// Frames are rasterized by CPURenderer and composited with putImageData,
// only over the damaged rectangle. There is no GL context
// (platformCaps.GPUContext false), so GLRenderer requests fall back to the
// CPU renderer; a WebGL2 path is a later, separate surface.
//
// Threading: Go's wasm port runs JS callbacks on the event-loop goroutine
// while the main goroutine is parked in pumpEvents. Callbacks therefore do
// nothing but record the event (and call preventDefault, which must happen
// synchronously); pumpEvents drains the queue on the UI goroutine, exactly
// where a desktop backend would dispatch.
//
// Mapping to the seam's union-of-platforms rules: the page window cannot
// be moved or resized by the app (AbsolutePosition false, setSize ignored)
// — the same degradation Wayland already forces on callers.
package web

import (
	"bytes"
	"image"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

func init() {
	contract.Register("web", contract.PriorityNative, newWebApp)
}

type webApp struct {
	doc    js.Value
	wakeCh chan struct{}

	mu      sync.Mutex
	queue   []func()
	windows []*webWindow

	// clipboard caches the text clipboard. Reads of the system clipboard
	// are asynchronous and need a user gesture in the browser, so the
	// cache is refreshed from every paste event and from our own writes.
	clipboard string
	// pendingPaste holds a Ctrl/Cmd+V key press until the paste event that
	// follows it has refreshed the cache, so the engine's paste handler
	// reads the text actually being pasted.
	pendingPaste func()
	mods         contract.Modifiers
	funcs        []js.Func

	// textInput is the hidden <textarea> that holds keyboard focus. Text
	// reaches a web page as input events — typing, IME commits, mobile
	// keyboards, dictation, paste-as-typing all arrive there and only
	// some of them as keydown — so keys are read from keydown but text
	// only from the textarea.
	textInput js.Value
	// focused is the window whose canvas last took the pointer or focus;
	// keyboard and text input go to it.
	focused *webWindow
	// composing is true between compositionstart and compositionend.
	composing bool
	// caret is the last IME anchor applied to the textarea (CSS px).
	caret [4]float64

	// dropped holds the contents of files dropped onto a canvas, under
	// the virtual paths delivered to the engine (contract.DropOpener):
	// the browser hands over contents, never paths.
	dropped   map[string][]byte
	dropSeq   int
	dprListen js.Value
}

func newWebApp() (contract.App, error) {
	a := &webApp{doc: js.Global().Get("document"), wakeCh: make(chan struct{}, 1)}
	a.listen(a.doc, "paste", false, func(e js.Value) {
		text := e.Get("clipboardData").Call("getData", "text")
		a.mu.Lock()
		if text.Type() == js.TypeString {
			a.clipboard = text.String()
		}
		pending := a.pendingPaste
		a.pendingPaste = nil
		a.mu.Unlock()
		if pending != nil {
			a.post(pending)
		}
	})
	a.installTextInput()
	a.listen(js.Global(), "resize", false, func(js.Value) { a.post(a.syncAllSizes) })
	a.watchDevicePixelRatio()
	// A file dropped anywhere outside an accepting canvas must not make the
	// browser navigate away from the app to display it.
	for _, ev := range []string{"dragover", "drop"} {
		a.listen(a.doc, ev, false, func(e js.Value) { e.Call("preventDefault") })
	}
	if bt := newBrowserText(); bt != nil && contract.Host.SetTextFallback != nil {
		contract.Host.SetTextFallback(bt)
	}
	if contract.Host.SetMacKeyboard != nil {
		contract.Host.SetMacKeyboard(browserOnApplePlatform())
	}
	return a, nil
}

func (a *webApp) syncAllSizes() {
	for _, w := range a.windows {
		w.syncSize()
	}
}

// watchDevicePixelRatio resyncs every canvas when the device pixel ratio
// changes — browser zoom, or the window moving to a display with another
// scale — which resizes no element and so fires no resize event. A
// resolution media query matches only the current ratio, so it is
// re-armed with the new one after each change.
func (a *webApp) watchDevicePixelRatio() {
	if js.Global().Get("matchMedia").Type() != js.TypeFunction {
		return
	}
	var f js.Func
	f = js.FuncOf(func(js.Value, []js.Value) any {
		f.Release()
		a.post(func() {
			a.syncAllSizes()
			a.watchDevicePixelRatio()
		})
		return nil
	})
	q := "(resolution: " + strconv.FormatFloat(devicePixelRatio(), 'f', -1, 64) + "dppx)"
	mql := js.Global().Call("matchMedia", q)
	opts := js.Global().Get("Object").New()
	opts.Set("once", true)
	mql.Call("addEventListener", "change", f, opts)
	a.dprListen = mql
}

// listen adds an event listener whose handler runs in the JS callback.
// passive=false lets the handler call preventDefault on wheel/touch.
func (a *webApp) listen(target js.Value, event string, passive bool, fn func(e js.Value)) {
	f := js.FuncOf(func(_ js.Value, args []js.Value) any {
		fn(args[0])
		return nil
	})
	a.funcs = append(a.funcs, f)
	opts := js.Global().Get("Object").New()
	opts.Set("passive", passive)
	target.Call("addEventListener", event, f, opts)
}

// post queues fn for the UI goroutine and wakes the pump.
func (a *webApp) post(fn func()) {
	a.mu.Lock()
	a.queue = append(a.queue, fn)
	a.mu.Unlock()
	a.Wake()
}

func (a *webApp) PumpEvents(timeout time.Duration) {
	if !a.drain() && timeout > 0 {
		t := time.NewTimer(timeout)
		select {
		case <-a.wakeCh:
		case <-t.C:
		}
		t.Stop()
	}
	a.drain()
}

func (a *webApp) drain() bool {
	a.mu.Lock()
	q := a.queue
	a.queue = nil
	a.mu.Unlock()
	for _, fn := range q {
		fn()
	}
	return len(q) > 0
}

func (a *webApp) Wake() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

func (a *webApp) Monitors() []contract.Monitor { return []contract.Monitor{a.PrimaryMonitor()} }

func (a *webApp) PrimaryMonitor() contract.Monitor { return webMonitor{} }

type webMonitor struct{}

func (webMonitor) Name() string         { return "browser" }
func (webMonitor) Position() (int, int) { return 0, 0 }
func (webMonitor) VideoMode() (int, int, int) {
	s := js.Global().Get("screen")
	return s.Get("width").Int(), s.Get("height").Int(), 60
}
func (webMonitor) WorkArea() (int, int, int, int) {
	g := js.Global()
	return 0, 0, g.Get("innerWidth").Int(), g.Get("innerHeight").Int()
}
func (webMonitor) ContentScale() (float32, float32) {
	d := float32(devicePixelRatio())
	return d, d
}

func devicePixelRatio() float64 {
	if v := js.Global().Get("devicePixelRatio"); v.Type() == js.TypeNumber && v.Float() > 0 {
		return v.Float()
	}
	return 1
}

type webWindow struct {
	app     *webApp
	handler contract.Handler
	canvas  js.Value
	ctx     js.Value
	main    bool

	w, h       int // logical (CSS px)
	fbW, fbH   int // physical (canvas backing store)
	x, y       int
	cursorX    float64
	cursorY    float64
	closing    bool
	visible    bool
	buttons    int
	scratch    []byte
	jsBuf      js.Value
	jsBufBytes int
	cursorCSS  string

	dropEnabled bool
	// rAF pacing (Ready): rafPending while a requestAnimationFrame is
	// outstanding, frameReady once it fired and before a frame used it.
	rafPending bool
	frameReady bool
}

func (a *webApp) NewWindow(cfg contract.WindowConfig) (contract.Window, error) {
	doc := a.doc
	w := &webWindow{app: a, main: len(a.windows) == 0, visible: true}

	canvas := doc.Call("getElementById", "qui")
	if !w.main || canvas.IsNull() || canvas.IsUndefined() {
		canvas = doc.Call("createElement", "canvas")
		doc.Get("body").Call("appendChild", canvas)
	}
	style := canvas.Get("style")
	style.Set("display", "block")
	style.Set("outline", "none")
	style.Set("touchAction", "none")
	if w.main {
		body := doc.Get("body").Get("style")
		body.Set("margin", "0")
		body.Set("overflow", "hidden")
		style.Set("position", "fixed")
		style.Set("left", "0")
		style.Set("top", "0")
		style.Set("width", "100vw")
		style.Set("height", "100vh")
		if cfg.Title != "" {
			doc.Set("title", cfg.Title)
		}
	} else {
		style.Set("position", "fixed")
		style.Set("boxShadow", "0 12px 40px rgba(0,0,0,.25)")
		style.Set("borderRadius", "8px")
		w.w, w.h = max(cfg.Width, 1), max(cfg.Height, 1)
		w.x = max((js.Global().Get("innerWidth").Int()-w.w)/2, 0)
		w.y = max((js.Global().Get("innerHeight").Int()-w.h)/2, 0)
		style.Set("left", itoa(w.x)+"px")
		style.Set("top", itoa(w.y)+"px")
		style.Set("width", itoa(w.w)+"px")
		style.Set("height", itoa(w.h)+"px")
	}
	canvas.Set("tabIndex", -1)
	w.canvas = canvas
	opts := js.Global().Get("Object").New()
	opts.Set("alpha", cfg.Kind == contract.WindowOverlayPanel)
	w.ctx = canvas.Call("getContext", "2d", opts)
	w.syncSizeQuiet()
	w.installListeners()
	a.windows = append(a.windows, w)
	a.focusWindow(w)
	return w, nil
}

// installTextInput creates the hidden textarea and the keyboard/text
// listeners on it.
func (a *webApp) installTextInput() {
	ta := a.doc.Call("createElement", "textarea")
	st := ta.Get("style")
	for k, v := range map[string]string{
		"position": "fixed", "left": "0", "top": "0", "width": "1px", "height": "1px",
		"opacity": "0", "border": "0", "padding": "0", "resize": "none",
		"pointerEvents": "none", "zIndex": "-1",
	} {
		st.Set(k, v)
	}
	ta.Set("autocapitalize", "off")
	ta.Set("autocomplete", "off")
	ta.Set("spellcheck", false)
	ta.Call("setAttribute", "aria-hidden", "true")
	a.doc.Get("body").Call("appendChild", ta)
	a.textInput = ta

	key := func(down bool) func(e js.Value) {
		return func(e js.Value) {
			if e.Get("isComposing").Bool() || e.Get("keyCode").Int() == 229 {
				return // the IME owns this key; its result comes as text
			}
			code := e.Get("code").String()
			k := domKey(code)
			mods := modsOf(e)
			repeat := e.Get("repeat").Bool()
			shortcut := mods&(contract.ModControl|contract.ModSuper) != 0
			printable := len([]rune(e.Get("key").String())) == 1 && !shortcut
			paste := down && k == contract.KeyV && shortcut
			// A printable key must reach the textarea, or no input event
			// (and so no text) follows. Everything else — Tab, arrows,
			// Backspace, app shortcuts — is the app's, not the page's.
			// Paste stays default so the browser fires a paste event.
			if !printable && !paste && !(down && (code == "F5" || code == "F12")) {
				e.Call("preventDefault")
			}
			deliver := func() {
				a.mods = mods
				if w := a.focused; w != nil && w.handler != nil {
					w.handler.OnKey(k, 0, down, repeat, mods)
				}
			}
			if paste {
				a.mu.Lock()
				a.pendingPaste = deliver
				a.mu.Unlock()
				// A paste event normally follows within the same task; if
				// the browser declines (no permission), deliver anyway.
				var f js.Func
				f = js.FuncOf(func(js.Value, []js.Value) any {
					f.Release()
					a.mu.Lock()
					p := a.pendingPaste
					a.pendingPaste = nil
					a.mu.Unlock()
					if p != nil {
						a.post(p)
					}
					return nil
				})
				js.Global().Call("setTimeout", f, 50)
				return
			}
			a.post(deliver)
		}
	}
	a.listen(ta, "keydown", false, key(true))
	a.listen(ta, "keyup", false, key(false))
	commit := func(text string) {
		if text == "" {
			return
		}
		a.post(func() {
			if w := a.focused; w != nil && w.handler != nil {
				for _, r := range text {
					w.handler.OnChar(r)
				}
			}
		})
	}
	a.listen(ta, "compositionstart", true, func(js.Value) {
		a.composing = true
		a.post(a.placeTextInput)
	})
	a.listen(ta, "compositionupdate", true, func(e js.Value) {
		text := ""
		if d := e.Get("data"); d.Type() == js.TypeString {
			text = d.String()
		}
		cursor := len([]rune(text))
		// The caret inside the composition (an IME moving it with the arrow
		// keys) is the textarea's selection, counted in UTF-16 units.
		if sel := ta.Get("selectionStart"); sel.Type() == js.TypeNumber {
			cursor = min(utf16ToRuneIndex(ta.Get("value").String(), sel.Int()), len([]rune(text)))
		}
		a.post(func() {
			if w := a.focused; w != nil && w.handler != nil {
				w.handler.OnPreedit(text, cursor)
			}
		})
	})
	a.listen(ta, "input", true, func(e js.Value) {
		if e.Get("isComposing").Bool() || a.composing {
			return
		}
		if t := e.Get("inputType"); t.Type() == js.TypeString && t.String() == "insertFromPaste" {
			ta.Set("value", "") // paste goes through the clipboard path
			return
		}
		text := ta.Get("value").String()
		ta.Set("value", "")
		commit(text)
	})
	a.listen(ta, "compositionend", true, func(e js.Value) {
		a.composing = false
		ta.Set("value", "")
		text := ""
		if d := e.Get("data"); d.Type() == js.TypeString {
			text = d.String()
		}
		// CommitIME replaces the preedit with the final text; an empty
		// commit (composition cancelled with Escape) still clears it.
		a.post(func() {
			if w := a.focused; w != nil && w.handler != nil {
				if text == "" {
					w.handler.OnPreedit("", 0)
				} else {
					w.handler.OnCommitText(text)
				}
			}
		})
	})
	a.listen(ta, "focus", true, func(js.Value) {
		a.post(func() {
			if w := a.focused; w != nil && w.handler != nil {
				w.handler.OnFocus(true)
			}
		})
	})
	a.listen(ta, "blur", true, func(js.Value) {
		a.post(func() {
			if w := a.focused; w != nil && w.handler != nil {
				w.handler.OnFocus(false)
			}
		})
	})
}

// placeTextInput moves the hidden textarea onto the focused field's caret,
// so the IME's candidate window lands where the user is typing. UI
// goroutine only.
func (a *webApp) placeTextInput() {
	w := a.focused
	if w == nil || w.handler == nil {
		return
	}
	x, y, _, h, ok := w.handler.IMECaretRect()
	if !ok {
		return
	}
	r := w.canvas.Call("getBoundingClientRect")
	left := r.Get("left").Float() + x
	top := r.Get("top").Float() + y
	if h <= 0 {
		h = 16
	}
	next := [4]float64{left, top, h, 0}
	if next == a.caret {
		return
	}
	a.caret = next
	px := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) + "px" }
	st := a.textInput.Get("style")
	st.Set("left", px(left))
	st.Set("top", px(top))
	st.Set("height", px(h))
	st.Set("fontSize", px(h*0.8))
	st.Set("lineHeight", px(h))
}

// focusWindow routes keyboard input to w and gives the page focus to the
// text input. Safe from JS callbacks: the focus/blur notifications it
// triggers are themselves queued.
func (a *webApp) focusWindow(w *webWindow) {
	if a.focused != w {
		prev := a.focused
		a.focused = w
		if prev != nil {
			a.post(func() {
				if prev.handler != nil {
					prev.handler.OnFocus(false)
				}
			})
			a.post(func() {
				if w.handler != nil {
					w.handler.OnFocus(true)
				}
			})
		}
	}
	if !a.doc.Get("activeElement").Equal(a.textInput) {
		a.textInput.Call("focus", map[string]any{"preventScroll": true})
	}
}

// syncSizeQuiet reads the canvas's CSS box and DPR and sizes the backing
// store to match, without notifying the handler.
func (w *webWindow) syncSizeQuiet() (changedLogical, changedFB bool) {
	dpr := devicePixelRatio()
	lw, lh := w.w, w.h
	if w.main {
		lw = js.Global().Get("innerWidth").Int()
		lh = js.Global().Get("innerHeight").Int()
	}
	lw, lh = max(lw, 1), max(lh, 1)
	fw := int(math.Round(float64(lw) * dpr))
	fh := int(math.Round(float64(lh) * dpr))
	changedLogical = lw != w.w || lh != w.h
	changedFB = fw != w.fbW || fh != w.fbH
	w.w, w.h = lw, lh
	if changedFB {
		w.fbW, w.fbH = fw, fh
		// Resizing the backing store clears it; the handler's resize
		// notification triggers the full repaint that refills it.
		w.canvas.Set("width", fw)
		w.canvas.Set("height", fh)
	}
	return changedLogical, changedFB
}

func (w *webWindow) syncSize() {
	lc, fc := w.syncSizeQuiet()
	if w.handler == nil {
		return
	}
	if lc {
		w.handler.OnResize(w.w, w.h)
	}
	if fc {
		w.handler.OnFramebufferResize(w.fbW, w.fbH)
	}
	if lc || fc {
		w.handler.OnRefresh()
	}
}

func modsOf(e js.Value) contract.Modifiers {
	var m contract.Modifiers
	if e.Get("shiftKey").Bool() {
		m |= contract.ModShift
	}
	if e.Get("ctrlKey").Bool() {
		m |= contract.ModControl
	}
	if e.Get("altKey").Bool() {
		m |= contract.ModAlt
	}
	if e.Get("metaKey").Bool() {
		m |= contract.ModSuper
	}
	return m
}

func domButton(b int) (contract.MouseButton, bool) {
	switch b {
	case 0:
		return contract.MouseButtonLeft, true
	case 1:
		return contract.MouseButtonMiddle, true
	case 2:
		return contract.MouseButtonRight, true
	}
	return 0, false
}

func (w *webWindow) installListeners() {
	a, c := w.app, w.canvas
	pos := func(e js.Value) (float64, float64) {
		r := c.Call("getBoundingClientRect")
		return e.Get("clientX").Float() - r.Get("left").Float(), e.Get("clientY").Float() - r.Get("top").Float()
	}
	a.listen(c, "pointermove", true, func(e js.Value) {
		x, y, mods := 0.0, 0.0, modsOf(e)
		x, y = pos(e)
		a.post(func() {
			a.mods = mods
			w.cursorX, w.cursorY = x, y
			if w.handler != nil {
				w.handler.OnMouseMove(x, y, mods)
			}
		})
	})
	a.listen(c, "pointerdown", false, func(e js.Value) {
		btn, ok := domButton(e.Get("button").Int())
		if !ok {
			return
		}
		a.focusWindow(w)
		c.Call("setPointerCapture", e.Get("pointerId"))
		e.Call("preventDefault")
		x, y := pos(e)
		mods := modsOf(e)
		a.post(func() {
			a.mods = mods
			w.cursorX, w.cursorY = x, y
			if w.handler != nil {
				w.handler.OnMouseMove(x, y, mods)
				w.handler.OnMouseButton(btn, true, mods)
			}
		})
	})
	a.listen(c, "pointerup", false, func(e js.Value) {
		btn, ok := domButton(e.Get("button").Int())
		if !ok {
			return
		}
		x, y := pos(e)
		mods := modsOf(e)
		a.post(func() {
			a.mods = mods
			w.cursorX, w.cursorY = x, y
			if w.handler != nil {
				w.handler.OnMouseButton(btn, false, mods)
			}
		})
	})
	a.listen(c, "contextmenu", false, func(e js.Value) { e.Call("preventDefault") })
	a.listen(c, "wheel", false, func(e js.Value) {
		e.Call("preventDefault")
		x, y := pos(e)
		mods := modsOf(e)
		dx, dy := e.Get("deltaX").Float(), e.Get("deltaY").Float()
		precise := true
		switch e.Get("deltaMode").Int() {
		case 0: // pixels: same 0.1 scale the desktop backends apply to precise deltas
			dx, dy = dx*0.1, dy*0.1
		case 1: // lines
			precise = false
		case 2: // pages
			dx, dy = dx*10, dy*10
			precise = false
		}
		// DOM deltas point where the content moves; qui's (like AppKit's)
		// point where the view moves.
		dx, dy = -dx, -dy
		a.post(func() {
			a.mods = mods
			if w.handler != nil {
				w.handler.OnScroll(x, y, dx, dy, mods, contract.GesturePhaseNone, precise)
			}
		})
	})
	a.listen(c, "focus", true, func(js.Value) { a.focusWindow(w) })
	a.listen(c, "dragover", false, func(e js.Value) {
		e.Call("preventDefault")
		if dt := e.Get("dataTransfer"); dt.Truthy() {
			if w.dropEnabled {
				dt.Set("dropEffect", "copy")
			} else {
				dt.Set("dropEffect", "none")
			}
		}
	})
	a.listen(c, "drop", false, func(e js.Value) {
		e.Call("preventDefault")
		if !w.dropEnabled {
			return
		}
		x, y := pos(e)
		files := e.Get("dataTransfer").Get("files")
		n := files.Get("length").Int()
		list := make([]js.Value, 0, n)
		for i := 0; i < n; i++ {
			list = append(list, files.Call("item", i))
		}
		go w.readDrop(list, x, y)
	})
}

// readDrop reads dropped files into memory and delivers them as virtual
// paths (/dropped/<n>/<name>) that Window.OpenDroppedFile resolves. Runs
// on its own goroutine: reading a File is a promise, and the JS callback
// that saw the drop must return without waiting for it.
func (w *webWindow) readDrop(files []js.Value, x, y float64) {
	type item struct {
		name string
		data []byte
	}
	items := make([]item, 0, len(files))
	for _, f := range files {
		done := make(chan []byte, 1)
		var ok, fail js.Func
		ok = js.FuncOf(func(_ js.Value, args []js.Value) any {
			arr := js.Global().Get("Uint8Array").New(args[0])
			b := make([]byte, arr.Get("length").Int())
			js.CopyBytesToGo(b, arr)
			done <- b
			return nil
		})
		fail = js.FuncOf(func(js.Value, []js.Value) any { done <- nil; return nil })
		f.Call("arrayBuffer").Call("then", ok, fail)
		data := <-done
		ok.Release()
		fail.Release()
		if data != nil {
			items = append(items, item{name: f.Get("name").String(), data: data})
		}
	}
	if len(items) == 0 {
		return
	}
	a := w.app
	a.post(func() {
		if a.dropped == nil {
			a.dropped = map[string][]byte{}
		}
		a.dropSeq++
		paths := make([]string, 0, len(items))
		for _, it := range items {
			name := strings.NewReplacer("/", "_", "\\", "_").Replace(it.name)
			p := "/dropped/" + strconv.Itoa(a.dropSeq) + "/" + name
			a.dropped[p] = it.data
			paths = append(paths, p)
		}
		if w.handler != nil {
			w.handler.OnFileDrop(paths, x, y)
		}
	})
}

// SetDropEnabled implements contract.DropTarget.
func (w *webWindow) SetDropEnabled(enabled bool) { w.dropEnabled = enabled }

// OpenDropped implements contract.DropOpener.
func (w *webWindow) OpenDropped(path string) (io.ReadCloser, bool) {
	data, ok := w.app.dropped[path]
	if !ok {
		return nil, false
	}
	return io.NopCloser(bytes.NewReader(data)), true
}

var (
	_ contract.Window     = (*webWindow)(nil)
	_ contract.DropTarget = (*webWindow)(nil)
	_ contract.DropOpener = (*webWindow)(nil)
	_ contract.Focuser    = (*webWindow)(nil)
)

func (w *webWindow) SetHandler(h contract.Handler) { w.handler = h }

func (w *webWindow) Caps() contract.Caps {
	return contract.Caps{
		// The page window has no position the app can know or set.
		AbsolutePosition:  !w.main,
		ServerDecorations: false,
		NativeGestures:    false,
		OverlayPanels:     false,
		GPUContext:        false,
	}
}

func (w *webWindow) IsFocused() bool {
	return w.app.focused == w && w.app.doc.Get("activeElement").Equal(w.app.textInput)
}

func (w *webWindow) Size() (int, int)            { return w.w, w.h }
func (w *webWindow) FramebufferSize() (int, int) { return w.fbW, w.fbH }
func (w *webWindow) ContentScale() (float32, float32) {
	d := float32(devicePixelRatio())
	return d, d
}

func (w *webWindow) Pos() (int, int, bool) {
	if w.main {
		return 0, 0, false
	}
	return w.x, w.y, true
}

func (w *webWindow) SetPos(x, y int) bool {
	if w.main {
		return false
	}
	w.x, w.y = x, y
	st := w.canvas.Get("style")
	st.Set("left", itoa(x)+"px")
	st.Set("top", itoa(y)+"px")
	return true
}

func (w *webWindow) SetTitle(title string) {
	if w.main {
		w.app.doc.Set("title", title)
	}
}

// SetSize resizes a secondary window; the page window follows the browser
// viewport and ignores it.
func (w *webWindow) SetSize(width, height int) {
	if w.main {
		return
	}
	w.w, w.h = max(width, 1), max(height, 1)
	st := w.canvas.Get("style")
	st.Set("width", itoa(w.w)+"px")
	st.Set("height", itoa(w.h)+"px")
	w.syncSize()
}

func (w *webWindow) SetSizeLimits(int, int, int, int) {}
func (w *webWindow) Iconify()                         {}
func (w *webWindow) Maximize()                        {}
func (w *webWindow) Restore()                         {}
func (w *webWindow) Focus()                           { w.app.focusWindow(w) }

func (w *webWindow) SetVisible(visible, activate bool) {
	w.visible = visible
	if visible {
		w.canvas.Get("style").Set("display", "block")
		if activate {
			w.app.focusWindow(w)
		}
	} else {
		w.canvas.Get("style").Set("display", "none")
	}
}

func (w *webWindow) IsVisible() bool { return w.visible }

func (w *webWindow) SetFullscreen(m contract.Monitor, _, _, _, _, _ int) {
	if m != nil {
		if fn := w.canvas.Get("requestFullscreen"); fn.Type() == js.TypeFunction {
			w.canvas.Call("requestFullscreen")
		}
		return
	}
	if !w.app.doc.Get("fullscreenElement").IsNull() {
		w.app.doc.Call("exitFullscreen")
	}
}

func (w *webWindow) ShouldClose() bool     { return w.closing }
func (w *webWindow) SetShouldClose(v bool) { w.closing = v }

func (w *webWindow) Destroy() {
	w.handler = nil
	if !w.main {
		w.canvas.Call("remove")
	}
	ws := w.app.windows[:0]
	for _, o := range w.app.windows {
		if o != w {
			ws = append(ws, o)
		}
	}
	w.app.windows = ws
}

func (w *webWindow) CursorPos() (float64, float64) { return w.cursorX, w.cursorY }

func (w *webWindow) SetCursor(shape contract.CursorShape) {
	css := "default"
	switch shape {
	case contract.CursorText:
		css = "text"
	case contract.CursorCrosshair:
		css = "crosshair"
	case contract.CursorHand:
		css = "pointer"
	case contract.CursorResizeEW:
		css = "ew-resize"
	case contract.CursorResizeNS:
		css = "ns-resize"
	case contract.CursorResizeNWSE:
		css = "nwse-resize"
	case contract.CursorResizeNESW:
		css = "nesw-resize"
	case contract.CursorHidden:
		css = "none"
	}
	if css != w.cursorCSS {
		w.cursorCSS = css
		w.canvas.Get("style").Set("cursor", css)
	}
}

func (w *webWindow) CurrentMods() contract.Modifiers { return w.app.mods }

func (w *webWindow) ClipboardText() string {
	w.app.mu.Lock()
	defer w.app.mu.Unlock()
	return w.app.clipboard
}

func (w *webWindow) SetClipboardText(text string) {
	w.app.mu.Lock()
	w.app.clipboard = text
	w.app.mu.Unlock()
	if cb := js.Global().Get("navigator").Get("clipboard"); cb.Truthy() {
		// Asynchronous and permission-gated; the cache above keeps paste
		// inside the app working even when the browser refuses.
		p := cb.Call("writeText", text)
		p.Call("catch", js.FuncOf(func(js.Value, []js.Value) any { return nil }))
	}
}

func (w *webWindow) MakeCurrent()                 {}
func (w *webWindow) Surface() contract.Surface    { return webSurface{win: w} }
func (w *webWindow) Wake()                        { w.app.Wake() }
func (w *webWindow) NativeWindow() unsafe.Pointer { return nil }

type webSurface struct{ win *webWindow }

var (
	// readyNow is closed: "the browser is about to paint; draw now".
	readyNow = func() chan struct{} { c := make(chan struct{}); close(c); return c }()
	// readyLater never fires: "wait for the animation frame".
	readyLater = make(chan struct{})
)

// Ready paces painting to the browser's refresh with requestAnimationFrame.
// A frame is requested only when the loop asks — never continuously — so an
// idle tab runs no callbacks; the frame callback wakes the loop, and the
// next Ready reports it. Hidden tabs get no animation frames, which simply
// stops painting there; posted jobs still run (the engine's PendingJobs
// escape).
func (s webSurface) Ready() <-chan struct{} {
	w := s.win
	if w.frameReady {
		w.frameReady = false
		return readyNow
	}
	if !w.rafPending {
		w.rafPending = true
		var f js.Func
		f = js.FuncOf(func(js.Value, []js.Value) any {
			f.Release()
			w.app.post(func() {
				w.rafPending = false
				w.frameReady = true
			})
			return nil
		})
		js.Global().Call("requestAnimationFrame", f)
	}
	return readyLater
}
func (webSurface) Present() {}

// PresentCPU copies the damaged rectangle of the frame into the canvas.
// The canvas keeps the previous frame, so undamaged pixels need no copy.
func (s webSurface) PresentCPU(img *image.RGBA, damage image.Rectangle) bool {
	w := s.win
	r := img.Rect
	if !damage.Empty() {
		r = damage.Intersect(img.Rect)
	}
	if r.Empty() {
		return true
	}
	rw, rh := r.Dx(), r.Dy()
	n := rw * rh * 4
	if cap(w.scratch) < n {
		w.scratch = make([]byte, n)
	}
	buf := w.scratch[:n]
	opaque := !w.app.windowsTransparent()
	for y := 0; y < rh; y++ {
		src := img.Pix[img.PixOffset(r.Min.X, r.Min.Y+y):]
		row := buf[y*rw*4 : (y+1)*rw*4]
		copy(row, src[:rw*4])
		if !opaque {
			unpremultiply(row)
		}
	}
	if w.jsBufBytes < n {
		w.jsBuf = js.Global().Get("Uint8ClampedArray").New(n)
		w.jsBufBytes = n
	}
	view := w.jsBuf
	if n != w.jsBufBytes {
		view = js.Global().Get("Uint8ClampedArray").New(w.jsBuf.Get("buffer"), 0, n)
	}
	js.CopyBytesToJS(view, buf)
	data := js.Global().Get("ImageData").New(view, rw, rh)
	w.ctx.Call("putImageData", data, r.Min.X-img.Rect.Min.X, r.Min.Y-img.Rect.Min.Y)
	// Keep the IME anchor on the caret as the field scrolls or the
	// composition grows; cheap when nothing moved.
	if w.app.focused == w {
		w.app.placeTextInput()
	}
	return true
}

// windowsTransparent reports whether any canvas was created with alpha;
// opaque canvases skip the unpremultiply pass entirely.
func (a *webApp) windowsTransparent() bool { return false }

// unpremultiply converts premultiplied RGBA (image.RGBA) to the straight
// alpha ImageData expects, in place.
func unpremultiply(p []byte) {
	for i := 0; i+3 < len(p); i += 4 {
		a := p[i+3]
		if a == 0 || a == 255 {
			continue
		}
		p[i] = uint8(min(int(p[i])*255/int(a), 255))
		p[i+1] = uint8(min(int(p[i+1])*255/int(a), 255))
		p[i+2] = uint8(min(int(p[i+2])*255/int(a), 255))
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// browserOnApplePlatform reports whether the browser runs on macOS or iOS,
// whose users expect Command shortcuts. userAgentData where available,
// navigator.platform otherwise (deprecated but universally present).
func browserOnApplePlatform() bool {
	nav := js.Global().Get("navigator")
	platform := ""
	if uad := nav.Get("userAgentData"); uad.Truthy() {
		if p := uad.Get("platform"); p.Type() == js.TypeString {
			platform = p.String()
		}
	}
	if platform == "" {
		if p := nav.Get("platform"); p.Type() == js.TypeString {
			platform = p.String()
		}
	}
	return isApplePlatform(platform)
}
