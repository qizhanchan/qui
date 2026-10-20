//go:build linux && cgo

// Package x11 is the native Linux backend: Xlib directly, no GLFW.
//
// Frames are rasterized by the engine's CPURenderer and presented with
// XPutImage, only over the damaged rectangle; there is no GL context
// (Caps.GPUContext false), so GLRenderer requests fall back to the CPU
// renderer. Text input goes through XIM (Xutf8LookupString), so input
// methods configured for the session work. The clipboard is the
// CLIPBOARD selection, served and read directly.
//
// HiDPI: X11 has no per-window scale; the desktop's choice is published as
// Xft.dpi (96 = 1x), which this backend reads. QUI_X11_SCALE overrides it.
// Logical sizes are physical pixels divided by that scale.
//
// Selected by default on linux+cgo builds; a Wayland backend is the
// natural sibling (Caps already models what Wayland withholds).
package x11

/*
#cgo LDFLAGS: -lX11
#include <stdlib.h>
#include "x11_linux.h"
*/
import "C"

import (
	"errors"
	"image"
	"math"
	"os"
	"strconv"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

func init() {
	contract.Register("x11", contract.PriorityNative, newApp)
}

type app struct {
	scale   float32
	windows map[C.ulong]*window
}

func newApp() (contract.App, error) {
	if C.quiX11Open() == 0 {
		return nil, errors.New("qui: cannot open X display (is DISPLAY set?); try QUI_PLATFORM=headless")
	}
	scale := float32(1)
	if dpi := float64(C.quiX11DPI()); dpi > 0 {
		scale = float32(dpi / 96)
	}
	if v := os.Getenv("QUI_X11_SCALE"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			scale = float32(f)
		}
	}
	return &app{scale: scale, windows: map[C.ulong]*window{}}, nil
}

func (a *app) phys(v int) int    { return int(math.Round(float64(float32(v) * a.scale))) }
func (a *app) logical(v int) int { return int(math.Round(float64(float32(v) / a.scale))) }

func (a *app) NewWindow(cfg contract.WindowConfig) (contract.Window, error) {
	if cfg.Kind == contract.WindowOverlayPanel {
		return nil, contract.ErrOverlayPanelUnsupported
	}
	title := C.CString(cfg.Title)
	defer C.free(unsafe.Pointer(title))
	id := C.quiX11CreateWindow(C.int(a.phys(max(cfg.Width, 1))), C.int(a.phys(max(cfg.Height, 1))), title)
	if id == 0 {
		return nil, errors.New("qui: XCreateWindow failed")
	}
	w := &window{app: a, id: id, fbW: a.phys(max(cfg.Width, 1)), fbH: a.phys(max(cfg.Height, 1)), visible: true}
	a.windows[id] = w
	return w, nil
}

func (a *app) PumpEvents(timeout time.Duration) {
	if a.drain() {
		return
	}
	if timeout > 0 {
		ms := -1
		if timeout != contract.PumpForever {
			ms = int(timeout / time.Millisecond)
			if ms == 0 {
				ms = 1
			}
		}
		C.quiX11Wait(C.int(ms))
	}
	a.drain()
}

func (a *app) drain() bool {
	var ev C.quiX11Event
	any := false
	for C.quiX11Next(&ev) != 0 {
		any = true
		if w := a.windows[ev.window]; w != nil {
			w.dispatch(&ev)
		}
	}
	return any
}

func (a *app) Wake() { C.quiX11Wake() }

func (a *app) Monitors() []contract.Monitor { return []contract.Monitor{a.PrimaryMonitor()} }

func (a *app) PrimaryMonitor() contract.Monitor { return monitor{a} }

type monitor struct{ a *app }

func (monitor) Name() string         { return "X11 screen" }
func (monitor) Position() (int, int) { return 0, 0 }
func (m monitor) VideoMode() (int, int, int) {
	var w, h C.int
	C.quiX11ScreenSize(&w, &h)
	return m.a.logical(int(w)), m.a.logical(int(h)), 0
}
func (m monitor) WorkArea() (int, int, int, int) {
	w, h, _ := m.VideoMode()
	return 0, 0, w, h
}
func (m monitor) ContentScale() (float32, float32) { return m.a.scale, m.a.scale }

type window struct {
	app     *app
	id      C.ulong
	handler contract.Handler

	fbW, fbH  int
	closing   bool
	visible   bool
	destroyed bool
	mods      contract.Modifiers
	capsLock  bool
	cursor    contract.CursorShape
}

var (
	_ contract.Window     = (*window)(nil)
	_ contract.Iconifier  = (*window)(nil)
	_ contract.CapsLocker = (*window)(nil)
)

func (w *window) dispatch(ev *C.quiX11Event) {
	h := w.handler
	s := float64(w.app.scale)
	x, y := float64(ev.x)/s, float64(ev.y)/s
	mods := contract.Modifiers(ev.mods)
	switch ev._type {
	case C.QUI_X11_MOTION:
		w.mods = mods
		if h != nil {
			h.OnMouseMove(x, y, mods)
		}
	case C.QUI_X11_BUTTON_DOWN, C.QUI_X11_BUTTON_UP:
		w.mods = mods
		down := ev._type == C.QUI_X11_BUTTON_DOWN
		if h == nil {
			return
		}
		switch ev.button {
		case 1:
			h.OnMouseButton(contract.MouseButtonLeft, down, mods)
		case 2:
			h.OnMouseButton(contract.MouseButtonMiddle, down, mods)
		case 3:
			h.OnMouseButton(contract.MouseButtonRight, down, mods)
		case 4, 5, 6, 7:
			// Wheel notches arrive as button presses; the release carries
			// nothing new.
			if !down {
				return
			}
			dx, dy := 0.0, 0.0
			switch ev.button {
			case 4:
				dy = 1
			case 5:
				dy = -1
			case 6:
				dx = 1
			case 7:
				dx = -1
			}
			h.OnScroll(x, y, dx, dy, mods, contract.GesturePhaseNone, false)
		}
	case C.QUI_X11_KEY_DOWN, C.QUI_X11_KEY_UP:
		w.mods = mods
		w.capsLock = ev.capsLock != 0
		down := ev._type == C.QUI_X11_KEY_DOWN
		if h == nil {
			return
		}
		h.OnKey(mapKeysym(uint64(ev.keysym)), int(ev.keycode), down, false, mods)
		if down && mods&(contract.ModControl|contract.ModSuper) == 0 {
			for _, r := range C.GoString(&ev.text[0]) {
				if r >= 0x20 && r != 0x7f {
					h.OnChar(r)
				}
			}
		}
	case C.QUI_X11_FOCUS_IN, C.QUI_X11_FOCUS_OUT:
		if h != nil {
			h.OnFocus(ev._type == C.QUI_X11_FOCUS_IN)
		}
	case C.QUI_X11_CONFIGURE:
		fw, fh := int(ev.width), int(ev.height)
		if fw == w.fbW && fh == w.fbH {
			return
		}
		w.fbW, w.fbH = fw, fh
		if h != nil {
			h.OnResize(w.app.logical(fw), w.app.logical(fh))
			h.OnFramebufferResize(fw, fh)
		}
	case C.QUI_X11_EXPOSE:
		// The last frame is retained here, so damage from the window
		// system needs no engine repaint.
		C.quiX11Put(w.id, 0, 0, C.int(w.fbW), C.int(w.fbH))
		if h != nil {
			h.OnRefresh()
		}
	case C.QUI_X11_CLOSE:
		w.closing = true
		contract.Host.WakeIfBlocked()
	case C.QUI_X11_STATE:
		contract.Host.WakeIfBlocked()
	}
}

func (w *window) SetHandler(h contract.Handler) { w.handler = h }

func (w *window) Caps() contract.Caps {
	return contract.Caps{
		AbsolutePosition:  true,
		ServerDecorations: true,
		GPUContext:        false,
	}
}

func (w *window) Size() (int, int)            { return w.app.logical(w.fbW), w.app.logical(w.fbH) }
func (w *window) FramebufferSize() (int, int) { return w.fbW, w.fbH }
func (w *window) ContentScale() (float32, float32) {
	return w.app.scale, w.app.scale
}

func (w *window) Pos() (int, int, bool) {
	var x, y, ww, hh C.int
	C.quiX11Geometry(w.id, &x, &y, &ww, &hh)
	return w.app.logical(int(x)), w.app.logical(int(y)), true
}

func (w *window) SetPos(x, y int) bool {
	C.quiX11Move(w.id, C.int(w.app.phys(x)), C.int(w.app.phys(y)))
	return true
}

func (w *window) SetTitle(title string) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	C.quiX11SetTitle(w.id, t)
}

// SetSize resizes the content area. X keeps the top-left corner where it
// is, which is what the seam asks for.
func (w *window) SetSize(width, height int) {
	C.quiX11Resize(w.id, C.int(w.app.phys(width)), C.int(w.app.phys(height)))
}

func (w *window) SetSizeLimits(minW, minH, maxW, maxH int) {
	p := func(v int) C.int {
		if v <= 0 {
			return 0
		}
		return C.int(w.app.phys(v))
	}
	C.quiX11SizeLimits(w.id, p(minW), p(minH), p(maxW), p(maxH))
}

func (w *window) Iconify()  { C.quiX11Iconify(w.id) }
func (w *window) Maximize() { C.quiX11Maximize(w.id, 1) }
func (w *window) Restore() {
	C.quiX11Maximize(w.id, 0)
	C.quiX11SetVisible(w.id, 1)
}
func (w *window) Focus() { C.quiX11Focus(w.id) }

func (w *window) SetVisible(visible, activate bool) {
	w.visible = visible
	C.quiX11SetVisible(w.id, cBool(visible))
	if visible && activate {
		C.quiX11Focus(w.id)
	}
}

func (w *window) IsVisible() bool   { return w.visible && C.quiX11IsViewable(w.id) != 0 }
func (w *window) IsIconified() bool { return C.quiX11IsIconified(w.id) != 0 }

func (w *window) SetFullscreen(m contract.Monitor, x, y, width, height, _ int) {
	if m != nil {
		C.quiX11Fullscreen(w.id, 1)
		return
	}
	C.quiX11Fullscreen(w.id, 0)
	w.SetPos(x, y)
	w.SetSize(width, height)
}

func (w *window) ShouldClose() bool     { return w.closing }
func (w *window) SetShouldClose(v bool) { w.closing = v }

func (w *window) Destroy() {
	if w.destroyed {
		return
	}
	w.destroyed = true
	w.handler = nil
	delete(w.app.windows, w.id)
	C.quiX11DestroyWindow(w.id)
}

func (w *window) CursorPos() (float64, float64) {
	var x, y C.int
	var mask C.uint
	C.quiX11Pointer(w.id, &x, &y, &mask)
	s := float64(w.app.scale)
	return float64(x) / s, float64(y) / s
}

func (w *window) SetCursor(shape contract.CursorShape) {
	if shape == w.cursor {
		return
	}
	w.cursor = shape
	n := 0
	switch shape {
	case contract.CursorText:
		n = 1
	case contract.CursorCrosshair:
		n = 2
	case contract.CursorHand:
		n = 3
	case contract.CursorResizeEW:
		n = 4
	case contract.CursorResizeNS:
		n = 5
	case contract.CursorResizeNWSE:
		n = 6
	case contract.CursorResizeNESW:
		n = 7
	case contract.CursorHidden:
		n = -1
	}
	C.quiX11SetCursor(w.id, C.int(n))
}

func (w *window) CurrentMods() contract.Modifiers {
	return contract.Modifiers(C.quiX11ModsOf(C.quiX11CurrentState(w.id)))
}

func (w *window) CapsLock() bool { return w.capsLock }

func (w *window) ClipboardText() string {
	p := C.quiX11GetClipboard(w.id)
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

func (w *window) SetClipboardText(text string) {
	t := C.CString(text)
	defer C.free(unsafe.Pointer(t))
	C.quiX11SetClipboard(w.id, t)
}

func (w *window) MakeCurrent()                 {}
func (w *window) Surface() contract.Surface    { return surface{w} }
func (w *window) Wake()                        { C.quiX11Wake() }
func (w *window) NativeWindow() unsafe.Pointer { return nil }

type surface struct{ w *window }

func (surface) Ready() <-chan struct{} { return nil }
func (surface) Present()               {}

// PresentCPU copies the damaged rows into the window's XImage, converting
// premultiplied RGBA to the visual's byte order, and puts that rectangle.
func (s surface) PresentCPU(img *image.RGBA, damage image.Rectangle) bool {
	w := s.w
	fw, fh := img.Rect.Dx(), img.Rect.Dy()
	var bgr C.int
	base := C.quiX11Pixels(w.id, C.int(fw), C.int(fh), &bgr)
	if base == nil {
		return false
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(base)), fw*fh*4)
	r := img.Rect
	if !damage.Empty() {
		r = damage.Intersect(img.Rect)
	}
	if r.Empty() {
		return true
	}
	r = r.Sub(img.Rect.Min)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		src := img.Pix[y*img.Stride+r.Min.X*4 : y*img.Stride+r.Max.X*4]
		out := dst[(y*fw+r.Min.X)*4 : (y*fw+r.Max.X)*4]
		if bgr != 0 {
			for i := 0; i < len(src); i += 4 {
				out[i], out[i+1], out[i+2], out[i+3] = src[i+2], src[i+1], src[i], 0xff
			}
		} else {
			for i := 0; i < len(src); i += 4 {
				out[i], out[i+1], out[i+2], out[i+3] = src[i], src[i+1], src[i+2], 0xff
			}
		}
	}
	C.quiX11Put(w.id, C.int(r.Min.X), C.int(r.Min.Y), C.int(r.Dx()), C.int(r.Dy()))
	return true
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}
