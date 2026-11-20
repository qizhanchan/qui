package qui

import (
	"image"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// The embedded window backend (RFC 0005 §3.1): the platformApp /
// platformWindow a PageHost's Window runs on. There is no OS window behind
// it. Size and scale come from the host (PageView), input arrives as tasks
// on the page thread, and presentation copies the finished CPU frame into
// the host's commit store instead of handing it to a compositor.
//
// It lives in the root package rather than internal/platform/embedded: it
// implements the engine-private seam directly (it has to reach the
// PageHost), and archtest keeps root from importing backends by name.

// embeddedApp is the platformApp of one PageHost. newWindow hands back the
// host's pre-built window exactly once; popups and overlay panels are
// refused, so callers fall back to in-window overlays (RFC 0005 P4 routes
// them through the host instead).
type embeddedApp struct {
	host *PageHost
	used bool
}

func (a *embeddedApp) newWindow(cfg platformWindowConfig) (platformWindow, error) {
	switch cfg.Kind {
	case WindowPopup:
		return nil, ErrPopupWindowUnsupported
	case WindowOverlayPanel:
		return nil, ErrOverlayPanelUnsupported
	}
	if a.used {
		return nil, ErrPopupWindowUnsupported
	}
	a.used = true
	return a.host.pw, nil
}

func (a *embeddedApp) pumpEvents(time.Duration)        {}
func (a *embeddedApp) wake()                           { a.host.thread.wake() }
func (a *embeddedApp) monitors() []platformMonitor     { return nil }
func (a *embeddedApp) primaryMonitor() platformMonitor { return nil }

// embeddedWindow is the platformWindow of a PageHost.
//
// Geometry is written by the host (any goroutine) and read by the page
// thread, so it is atomic. Everything else is page-thread state.
type embeddedWindow struct {
	host *PageHost

	geomMu        sync.Mutex
	w, h          int
	fbW, fbH      int
	scale         float32
	handler       platformHandler
	cursorX       float64
	cursorY       float64
	mods          Modifiers
	title         string
	closeReq      atomic.Bool
	visible       atomic.Bool
	cursorShape   atomic.Int32
	clipboardMu   sync.Mutex
	surfaceHandle embeddedSurface
}

func newEmbeddedWindow(h *PageHost, width, height int, scale float32) *embeddedWindow {
	if scale <= 0 {
		scale = 1
	}
	pw := &embeddedWindow{host: h}
	pw.setGeometry(width, height, scale)
	pw.visible.Store(true)
	pw.surfaceHandle = embeddedSurface{host: h}
	return pw
}

// setGeometry records a new logical size and scale; it reports whether
// anything changed.
func (pw *embeddedWindow) setGeometry(width, height int, scale float32) bool {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if scale <= 0 {
		scale = 1
	}
	fbW := int(float32(width)*scale + 0.5)
	fbH := int(float32(height)*scale + 0.5)
	pw.geomMu.Lock()
	defer pw.geomMu.Unlock()
	if pw.w == width && pw.h == height && pw.scale == scale {
		return false
	}
	pw.w, pw.h, pw.fbW, pw.fbH, pw.scale = width, height, fbW, fbH, scale
	return true
}

func (pw *embeddedWindow) geometry() (w, h, fbW, fbH int, scale float32) {
	pw.geomMu.Lock()
	defer pw.geomMu.Unlock()
	return pw.w, pw.h, pw.fbW, pw.fbH, pw.scale
}

func (pw *embeddedWindow) setHandler(h platformHandler) { pw.handler = h }
func (pw *embeddedWindow) caps() platformCaps           { return platformCaps{} }

func (pw *embeddedWindow) size() (int, int) {
	w, h, _, _, _ := pw.geometry()
	return w, h
}

func (pw *embeddedWindow) framebufferSize() (int, int) {
	_, _, fw, fh, _ := pw.geometry()
	return fw, fh
}

func (pw *embeddedWindow) contentScale() (float32, float32) {
	_, _, _, _, s := pw.geometry()
	return s, s
}

func (pw *embeddedWindow) pos() (int, int, bool)                                  { return 0, 0, false }
func (pw *embeddedWindow) setPos(int, int) bool                                   { return false }
func (pw *embeddedWindow) setTitle(t string)                                      { pw.title = t; pw.host.noteTitle(t) }
func (pw *embeddedWindow) setSize(int, int)                                       {}
func (pw *embeddedWindow) setSizeLimits(int, int, int, int)                       {}
func (pw *embeddedWindow) iconify()                                               {}
func (pw *embeddedWindow) maximize()                                              {}
func (pw *embeddedWindow) restore()                                               {}
func (pw *embeddedWindow) focus()                                                 {}
func (pw *embeddedWindow) setVisible(v, _ bool)                                   { pw.visible.Store(v) }
func (pw *embeddedWindow) isVisible() bool                                        { return pw.visible.Load() }
func (pw *embeddedWindow) setFullscreen(platformMonitor, int, int, int, int, int) {}
func (pw *embeddedWindow) shouldClose() bool                                      { return pw.closeReq.Load() }
func (pw *embeddedWindow) setShouldClose(v bool)                                  { pw.closeReq.Store(v) }
func (pw *embeddedWindow) destroy()                                               {}
func (pw *embeddedWindow) cursorPos() (float64, float64)                          { return pw.cursorX, pw.cursorY }
func (pw *embeddedWindow) currentMods() Modifiers                                 { return pw.mods }
func (pw *embeddedWindow) surface() platformSurface                               { return pw.surfaceHandle }
func (pw *embeddedWindow) wake()                                                  { pw.host.thread.wake() }
func (pw *embeddedWindow) nativeWindow() unsafe.Pointer                           { return nil }

func (pw *embeddedWindow) setCursor(shape CursorShape) {
	if CursorShape(pw.cursorShape.Swap(int32(shape))) != shape {
		pw.host.noteCursor(shape)
	}
}

// The clipboard is process-wide: the page reads and writes it through the
// provider the host app installed (NSPasteboard and friends are safe off
// the main thread for plain text).
func (pw *embeddedWindow) clipboardText() string {
	pw.clipboardMu.Lock()
	defer pw.clipboardMu.Unlock()
	return GetClipboardText()
}

func (pw *embeddedWindow) setClipboardText(text string) {
	pw.clipboardMu.Lock()
	defer pw.clipboardMu.Unlock()
	SetClipboardText(text)
}

// embeddedSurface turns presentation into a commit to the host.
type embeddedSurface struct{ host *PageHost }

func (s embeddedSurface) ready() <-chan struct{} { return nil }
func (s embeddedSurface) present()               {}
func (s embeddedSurface) presentCPU(img *image.RGBA, damage image.Rectangle) bool {
	s.host.commitPixels(img, damage)
	return true
}

func isEmbeddedPlatform(pw platformWindow) bool {
	_, ok := pw.(*embeddedWindow)
	return ok
}
