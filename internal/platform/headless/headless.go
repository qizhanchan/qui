// Package headless is the headless implementation of the platform seam.
//
// Windows exist only in memory: nothing is shown, no display connection
// is needed, and frames are rasterized by CPURenderer into an *image.RGBA
// that Window.Snapshot (and the agent's /shot) read back. It is pure Go
// and compiled into every build, which makes it the backend for
//
//   - offscreen rendering checks in CI and containers (no X server, no
//     GPU, CGO_ENABLED=0 is fine);
//   - driving an app through the agent surface with nothing on screen;
//   - the platform default on targets that have no native backend yet.
//
// Select it with QUI_PLATFORM=headless. QUI_HEADLESS_SCALE sets the
// simulated display scale (default 1; 2 renders a HiDPI framebuffer).
//
// Input reaches headless windows the same way it reaches any qui window
// from outside: the agent's click/type/scroll actions dispatch through the
// engine, so no platform events are needed.
package headless

import (
	"image"
	"os"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

func init() {
	contract.Register("headless", contract.PriorityFallback, newHeadlessApp)
}

type headlessApp struct {
	wakeCh chan struct{}
	scale  float32

	mu        sync.Mutex
	clipboard string
}

func newHeadlessApp() (contract.App, error) {
	scale := float32(1)
	if v := os.Getenv("QUI_HEADLESS_SCALE"); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			scale = float32(f)
		}
	}
	return New(scale), nil
}

// New creates a headless App whose simulated display has the given scale
// (device pixels per point). Tests use it directly; apps select the
// backend with QUI_PLATFORM=headless.
func New(scale float32) contract.App {
	if scale <= 0 {
		scale = 1
	}
	return &headlessApp{wakeCh: make(chan struct{}, 1), scale: scale}
}

func (a *headlessApp) NewWindow(cfg contract.WindowConfig) (contract.Window, error) {
	w, h := cfg.Width, cfg.Height
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return &headlessWindow{app: a, title: cfg.Title, w: w, h: h, visible: true}, nil
}

// PumpEvents has no OS events to dispatch; it only waits — for a wake
// (PostJob, RequestRedraw) or the timeout — so the frame loop paces the
// same way it does under a real backend.
func (a *headlessApp) PumpEvents(timeout time.Duration) {
	if timeout <= 0 {
		select {
		case <-a.wakeCh:
		default:
		}
		return
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-a.wakeCh:
	case <-t.C:
	}
}

func (a *headlessApp) Wake() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

func (a *headlessApp) Monitors() []contract.Monitor {
	return []contract.Monitor{a.PrimaryMonitor()}
}

func (a *headlessApp) PrimaryMonitor() contract.Monitor {
	return headlessMonitor{scale: a.scale}
}

type headlessMonitor struct{ scale float32 }

func (headlessMonitor) Name() string                   { return "headless" }
func (headlessMonitor) Position() (int, int)           { return 0, 0 }
func (headlessMonitor) VideoMode() (int, int, int)     { return 1920, 1080, 60 }
func (headlessMonitor) WorkArea() (int, int, int, int) { return 0, 0, 1920, 1080 }
func (m headlessMonitor) ContentScale() (float32, float32) {
	return m.scale, m.scale
}

type headlessWindow struct {
	app     *headlessApp
	handler contract.Handler

	title       string
	x, y        int
	w, h        int
	visible     bool
	closing     bool
	cursorX     float64
	cursorY     float64
	minW, minH  int
	maxW, maxH  int
	frames      int
	lastPresent image.Rectangle
}

func (w *headlessWindow) SetHandler(h contract.Handler) { w.handler = h }

func (w *headlessWindow) Caps() contract.Caps {
	return contract.Caps{
		AbsolutePosition: true,
		// Nothing draws a title bar around an in-memory window.
		ServerDecorations: false,
		NativeGestures:    false,
		// A window nobody can see takes no keyboard focus, which is all an
		// overlay panel asks for.
		OverlayPanels: true,
		GPUContext:    false,
	}
}

func (w *headlessWindow) Size() (int, int) { return w.w, w.h }

func (w *headlessWindow) FramebufferSize() (int, int) {
	s := w.app.scale
	return int(float32(w.w)*s + 0.5), int(float32(w.h)*s + 0.5)
}

func (w *headlessWindow) ContentScale() (float32, float32) { return w.app.scale, w.app.scale }

func (w *headlessWindow) Pos() (int, int, bool) { return w.x, w.y, true }

func (w *headlessWindow) SetPos(x, y int) bool {
	w.x, w.y = x, y
	return true
}

func (w *headlessWindow) SetTitle(title string) { w.title = title }

func (w *headlessWindow) SetSize(width, height int) {
	width, height = clampLimit(width, w.minW, w.maxW), clampLimit(height, w.minH, w.maxH)
	if width == w.w && height == w.h {
		return
	}
	w.w, w.h = width, height
	if w.handler != nil {
		w.handler.OnResize(width, height)
		fw, fh := w.FramebufferSize()
		w.handler.OnFramebufferResize(fw, fh)
	}
}

func clampLimit(v, lo, hi int) int {
	if lo > 0 && v < lo {
		v = lo
	}
	if hi > 0 && v > hi {
		v = hi
	}
	if v < 1 {
		v = 1
	}
	return v
}

func (w *headlessWindow) SetSizeLimits(minW, minH, maxW, maxH int) {
	w.minW, w.minH, w.maxW, w.maxH = minW, minH, maxW, maxH
}

func (w *headlessWindow) Iconify()  { w.visible = false }
func (w *headlessWindow) Maximize() { w.SetSize(1920, 1080) }
func (w *headlessWindow) Restore()  { w.visible = true }
func (w *headlessWindow) Focus()    {}

func (w *headlessWindow) SetVisible(visible, _ bool) { w.visible = visible }
func (w *headlessWindow) IsVisible() bool            { return w.visible }

func (w *headlessWindow) SetFullscreen(_ contract.Monitor, x, y, width, height, _ int) {
	w.x, w.y = x, y
	w.SetSize(width, height)
}

func (w *headlessWindow) ShouldClose() bool     { return w.closing }
func (w *headlessWindow) SetShouldClose(v bool) { w.closing = v }
func (w *headlessWindow) Destroy()              { w.handler = nil }

func (w *headlessWindow) CursorPos() (float64, float64)   { return w.cursorX, w.cursorY }
func (w *headlessWindow) SetCursor(contract.CursorShape)  {}
func (w *headlessWindow) CurrentMods() contract.Modifiers { return 0 }

func (w *headlessWindow) ClipboardText() string {
	w.app.mu.Lock()
	defer w.app.mu.Unlock()
	return w.app.clipboard
}

func (w *headlessWindow) SetClipboardText(text string) {
	w.app.mu.Lock()
	w.app.clipboard = text
	w.app.mu.Unlock()
}

func (w *headlessWindow) MakeCurrent()                 {}
func (w *headlessWindow) Surface() contract.Surface    { return headlessSurface{win: w} }
func (w *headlessWindow) Wake()                        { w.app.Wake() }
func (w *headlessWindow) NativeWindow() unsafe.Pointer { return nil }

// headlessSurface accepts CPU frames and keeps nothing: the renderer
// already retains the frame, which is what Snapshot reads.
type headlessSurface struct{ win *headlessWindow }

func (headlessSurface) Ready() <-chan struct{} { return nil }

func (s headlessSurface) PresentCPU(img *image.RGBA, damage image.Rectangle) bool {
	s.win.frames++
	if damage.Empty() {
		damage = img.Rect
	}
	s.win.lastPresent = damage
	return true
}

func (headlessSurface) Present() {}

// Frames reports how many frames have been presented to w, for tests.
func (w *headlessWindow) Frames() int { return w.frames }
