package qui

import (
	"image"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Adapter between the engine's view of the platform (the unexported
// interfaces in platform.go) and the backends, which implement
// contract's exported ones in their own packages under internal/platform.
//
// The engine keeps its unexported interfaces so that nothing outside this
// file — call sites, test fakes — depends on how backends are packaged.

// adaptApp wraps a backend's App for the engine.
func adaptApp(a contract.App) platformApp { return contractApp{a} }

type contractApp struct{ a contract.App }

func (c contractApp) newWindow(cfg platformWindowConfig) (platformWindow, error) {
	cc := contract.WindowConfig{Title: cfg.Title, Width: cfg.Width, Height: cfg.Height, Kind: cfg.Kind}
	if sw, ok := cfg.Share.(contractWindow); ok {
		cc.Share = sw.w
	}
	w, err := c.a.NewWindow(cc)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errNoPlatformWindow
	}
	return contractWindow{w}, nil
}

func (c contractApp) pumpEvents(timeout time.Duration) { c.a.PumpEvents(timeout) }
func (c contractApp) wake()                            { c.a.Wake() }

func (c contractApp) monitors() []platformMonitor {
	ms := c.a.Monitors()
	out := make([]platformMonitor, 0, len(ms))
	for _, m := range ms {
		if m != nil {
			out = append(out, contractMonitor{m})
		}
	}
	return out
}

func (c contractApp) primaryMonitor() platformMonitor {
	if m := c.a.PrimaryMonitor(); m != nil {
		return contractMonitor{m}
	}
	return nil
}

type contractMonitor struct{ m contract.Monitor }

func (c contractMonitor) name() string                     { return c.m.Name() }
func (c contractMonitor) position() (int, int)             { return c.m.Position() }
func (c contractMonitor) videoMode() (int, int, int)       { return c.m.VideoMode() }
func (c contractMonitor) workArea() (int, int, int, int)   { return c.m.WorkArea() }
func (c contractMonitor) contentScale() (float32, float32) { return c.m.ContentScale() }
func (c contractMonitor) unwrap() contract.Monitor         { return c.m }

type contractWindow struct{ w contract.Window }

func (c contractWindow) setHandler(h platformHandler) {
	if h == nil {
		c.w.SetHandler(nil)
		return
	}
	c.w.SetHandler(handlerAdapter{h})
}

func (c contractWindow) caps() platformCaps                { return c.w.Caps() }
func (c contractWindow) size() (int, int)                  { return c.w.Size() }
func (c contractWindow) framebufferSize() (int, int)       { return c.w.FramebufferSize() }
func (c contractWindow) contentScale() (float32, float32)  { return c.w.ContentScale() }
func (c contractWindow) pos() (int, int, bool)             { return c.w.Pos() }
func (c contractWindow) setPos(x, y int) bool              { return c.w.SetPos(x, y) }
func (c contractWindow) setTitle(t string)                 { c.w.SetTitle(t) }
func (c contractWindow) setSize(w, h int)                  { c.w.SetSize(w, h) }
func (c contractWindow) setSizeLimits(a, b, d, e int)      { c.w.SetSizeLimits(a, b, d, e) }
func (c contractWindow) iconify()                          { c.w.Iconify() }
func (c contractWindow) maximize()                         { c.w.Maximize() }
func (c contractWindow) restore()                          { c.w.Restore() }
func (c contractWindow) focus()                            { c.w.Focus() }
func (c contractWindow) setVisible(visible, activate bool) { c.w.SetVisible(visible, activate) }
func (c contractWindow) isVisible() bool                   { return c.w.IsVisible() }
func (c contractWindow) shouldClose() bool                 { return c.w.ShouldClose() }
func (c contractWindow) setShouldClose(v bool)             { c.w.SetShouldClose(v) }
func (c contractWindow) destroy()                          { c.w.Destroy() }
func (c contractWindow) cursorPos() (float64, float64)     { return c.w.CursorPos() }
func (c contractWindow) setCursor(s CursorShape)           { c.w.SetCursor(s) }
func (c contractWindow) currentMods() Modifiers            { return c.w.CurrentMods() }
func (c contractWindow) clipboardText() string             { return c.w.ClipboardText() }
func (c contractWindow) setClipboardText(t string)         { c.w.SetClipboardText(t) }
func (c contractWindow) makeCurrent()                      { c.w.MakeCurrent() }
func (c contractWindow) wake()                             { c.w.Wake() }
func (c contractWindow) nativeWindow() unsafe.Pointer      { return c.w.NativeWindow() }
func (c contractWindow) surface() platformSurface          { return contractSurface{c.w.Surface()} }

func (c contractWindow) setFullscreen(m platformMonitor, x, y, w, h, hz int) {
	var cm contract.Monitor
	if am, ok := m.(contractMonitor); ok {
		cm = am.m
	}
	c.w.SetFullscreen(cm, x, y, w, h, hz)
}

// platformOptional type-asserts one of contract's optional window
// capabilities (contract.Focuser, Iconifier, CapsLocker, DropTarget) on
// whatever implements pw — the backend behind the adapter, or a test fake
// directly.
func platformOptional[T any](pw platformWindow) (T, bool) {
	if c, ok := pw.(contractWindow); ok {
		t, ok := c.w.(T)
		return t, ok
	}
	t, ok := pw.(T)
	return t, ok
}

type contractSurface struct{ s contract.Surface }

func (c contractSurface) ready() <-chan struct{} { return c.s.Ready() }
func (c contractSurface) present()               { c.s.Present() }
func (c contractSurface) presentCPU(img *image.RGBA, damage image.Rectangle) bool {
	return c.s.PresentCPU(img, damage)
}

// handlerAdapter presents the engine's handler to a backend.
type handlerAdapter struct{ h platformHandler }

func (a handlerAdapter) OnMouseMove(x, y float64, mods Modifiers) { a.h.onMouseMove(x, y, mods) }
func (a handlerAdapter) OnMouseButton(btn MouseButton, down bool, mods Modifiers) {
	a.h.onMouseButton(btn, down, mods)
}
func (a handlerAdapter) OnScroll(x, y, dx, dy float64, mods Modifiers, phase GesturePhase, precise bool) {
	a.h.onScroll(x, y, dx, dy, mods, phase, precise)
}
func (a handlerAdapter) OnGesture(kind contract.GestureKind, phase GesturePhase, dScale, dRotation float32, mods Modifiers) {
	var t EventType
	switch kind {
	case contract.GestureRotate:
		t = EventGestureRotate
	case contract.GestureSmartMagnify:
		t = EventGestureSmartMagnify
	default:
		t = EventGesturePinch
	}
	a.h.onGesture(t, phase, dScale, dRotation, mods)
}
func (a handlerAdapter) OnKey(k Key, scancode int, down, repeat bool, mods Modifiers) {
	a.h.onKey(k, scancode, down, repeat, mods)
}
func (a handlerAdapter) OnChar(r rune)                       { a.h.onChar(r) }
func (a handlerAdapter) OnFocus(focused bool)                { a.h.onFocus(focused) }
func (a handlerAdapter) OnResize(w, h int)                   { a.h.onResize(w, h) }
func (a handlerAdapter) OnFramebufferResize(w, h int)        { a.h.onFramebufferResize(w, h) }
func (a handlerAdapter) OnRefresh()                          { a.h.onRefresh() }
func (a handlerAdapter) OnFileDrop(p []string, x, y float64) { a.h.onFileDrop(p, x, y) }
func (a handlerAdapter) OnPreedit(text string, cursor int)   { a.h.onPreedit(text, cursor) }
func (a handlerAdapter) OnCommitText(text string)            { a.h.onCommitText(text) }
func (a handlerAdapter) IMECaretRect() (float64, float64, float64, float64, bool) {
	return a.h.imeCaretRect()
}
