//go:build darwin && cgo

package cocoa

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework CoreVideo

#include <stdint.h>
#include <stdlib.h>

// Implemented in platform_cocoa_darwin.m. Windows are addressed by an
// integer handle throughout: cgo forbids parking Go pointers in C, so C
// owns the AppKit objects in a dictionary and both sides speak in ids.
int  quiCocoaInit(int policy);
void quiCocoaSetRawKeyHook(void* fn);
void quiCocoaPump(double seconds);
void quiCocoaWake(void);

int   quiCocoaCreateWindow(uintptr_t handle, int width, int height, const char* title, uintptr_t shareHandle, int kind);
void  quiCocoaDestroyWindow(uintptr_t handle);
void* quiCocoaNSWindow(uintptr_t handle);

void   quiCocoaGetSize(uintptr_t handle, int* width, int* height);
void   quiCocoaGetFramebufferSize(uintptr_t handle, int* width, int* height);
double quiCocoaBackingScale(uintptr_t handle);
void   quiCocoaGetPos(uintptr_t handle, int* x, int* y);
void   quiCocoaSetPos(uintptr_t handle, int x, int y);
void   quiCocoaSetTitle(uintptr_t handle, const char* title);
void   quiCocoaSetSize(uintptr_t handle, int width, int height);
void   quiCocoaSetSizeLimits(uintptr_t handle, int minW, int minH, int maxW, int maxH);
void   quiCocoaIconify(uintptr_t handle);
void   quiCocoaMaximize(uintptr_t handle);
void   quiCocoaRestore(uintptr_t handle);
void   quiCocoaFocus(uintptr_t handle);
void   quiCocoaSetVisible(uintptr_t handle, int visible, int activate);
int    quiCocoaIsVisible(uintptr_t handle);
int    quiCocoaIsKey(uintptr_t handle);
void   quiCocoaSetFullscreen(uintptr_t handle, int on, int x, int y, int w, int h);
int    quiCocoaShouldClose(uintptr_t handle);
void   quiCocoaSetShouldClose(uintptr_t handle, int value);

void quiCocoaGetCursorPos(uintptr_t handle, double* x, double* y);
void quiCocoaSetCursor(uintptr_t handle, int shape);
void quiCocoaSetCursorObject(uintptr_t handle, void* cursor);
void quiCocoaSetDropEnabled(uintptr_t handle, int enabled);
int  quiCocoaCurrentMods(void);
int  quiCocoaCapsLock(void);

char* quiCocoaClipboardText(void);
void  quiCocoaSetClipboardText(const char* text);

void quiCocoaMakeCurrent(uintptr_t handle);
int  quiCocoaPresentCPU(uintptr_t handle, const uint8_t* pix, int stride, int width, int height,
                        int dx, int dy, int dw, int dh);
int  quiCocoaTakeFrameReady(uintptr_t handle);
void quiCocoaPresent(uintptr_t handle);

int   quiCocoaScreenCount(void);
void  quiCocoaScreenInfo(int index, int* x, int* y, int* w, int* h,
                         int* wx, int* wy, int* ww, int* wh,
                         double* scale, int* refreshHz);
char* quiCocoaScreenName(int index);
*/
import "C"

import (
	"errors"
	"image"
	"sync"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Native Cocoa implementation of the platform seam (see platform.go).
//
// Selected with QUI_PLATFORM=cocoa (platform_select.go). Behaviorally a
// drop-in for the GLFW backend: same events, same GL context, same buffer
// swap. The differences are structural rather than visible —
//
//   - gestures are ordinary responder methods instead of a runtime
//     swizzle over GLFW's view class, so pinch/rotate/smart-magnify need
//     no class patching and can't silently fail to install;
//   - scroll events carry their real modifiers and NSEventPhase directly,
//     rather than being snapshotted alongside a callback that drops them;
//   - the event pump, window and view are ours, which is what a
//     CALayer/Metal presentation path needs in order to exist at all.
//
// The presentation surface here is still GL-only; the direct-composite
// path is the next step (platformSurface.presentCPU / ready).

// cocoaWindowRegistry resolves the integer handles C passes back on every
// callback. Same rationale as the IME and gesture registries.
var (
	cocoaRegistryMu sync.RWMutex
	cocoaRegistry           = map[uintptr]*cocoaWindow{}
	cocoaNextHandle uintptr = 1
)

func registerCocoaWindow(w *cocoaWindow) uintptr {
	cocoaRegistryMu.Lock()
	defer cocoaRegistryMu.Unlock()
	id := cocoaNextHandle
	cocoaNextHandle++
	cocoaRegistry[id] = w
	return id
}

func lookupCocoaWindow(id uintptr) *cocoaWindow {
	cocoaRegistryMu.RLock()
	defer cocoaRegistryMu.RUnlock()
	return cocoaRegistry[id]
}

func unregisterCocoaWindow(id uintptr) {
	cocoaRegistryMu.Lock()
	defer cocoaRegistryMu.Unlock()
	delete(cocoaRegistry, id)
}

// --- app -------------------------------------------------------------

type cocoaApp struct{}

func init() { contract.Register("cocoa", contract.PriorityNative, newCocoaApp) }

func newCocoaApp() (contract.App, error) {
	// Read the policy here rather than at package init: SetActivationPolicy
	// is documented as "before NewApp", and the backend is created lazily
	// on the first activePlatform() call, which NewApp triggers.
	switch C.quiCocoaInit(C.int(contract.Host.ActivationPolicy())) {
	case 0:
		return nil, errors.New("qui: NSApplication initialization failed")
	case -1:
		return nil, errors.New("qui: the cocoa backend must be initialized on the main thread")
	}
	C.quiCocoaSetRawKeyHook(contract.Host.RawKeyCaptured)
	return cocoaApp{}, nil
}

func (cocoaApp) NewWindow(cfg contract.WindowConfig) (contract.Window, error) {
	w := &cocoaWindow{}
	w.handle = registerCocoaWindow(w)

	title := C.CString(cfg.Title)
	defer C.free(unsafe.Pointer(title))

	var share C.uintptr_t
	if cw, ok := cfg.Share.(*cocoaWindow); ok && cw != nil {
		share = C.uintptr_t(cw.handle)
	}
	kind := C.int(0)
	if cfg.Kind == contract.WindowOverlayPanel {
		kind = 1
	}
	if C.quiCocoaCreateWindow(C.uintptr_t(w.handle), C.int(cfg.Width), C.int(cfg.Height),
		title, share, kind) == 0 {
		unregisterCocoaWindow(w.handle)
		return nil, errors.New("qui: could not create NSWindow")
	}
	return w, nil
}

func (cocoaApp) PumpEvents(timeout time.Duration) {
	seconds := timeout.Seconds()
	if timeout == contract.PumpForever {
		seconds = -1 // quiCocoaPump: wait for an event, however long
	}
	C.quiCocoaPump(C.double(seconds))
}

func (cocoaApp) Wake() { C.quiCocoaWake() }

func (cocoaApp) Monitors() []contract.Monitor {
	n := int(C.quiCocoaScreenCount())
	out := make([]contract.Monitor, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, newCocoaMonitor(i))
	}
	return out
}

func (a cocoaApp) PrimaryMonitor() contract.Monitor {
	if C.quiCocoaScreenCount() == 0 {
		return nil
	}
	// AppKit puts the screen carrying the menu bar first.
	return newCocoaMonitor(0)
}

// --- monitor ---------------------------------------------------------

// cocoaMonitor is a snapshot rather than a live handle. Monitor values are
// already documented as point-in-time (see monitors.go), and NSScreen
// indices shift when displays are plugged, so reading everything once at
// enumeration avoids a stale index answering later queries.
type cocoaMonitor struct {
	label          string
	x, y, w, h     int
	wx, wy, ww, wh int
	scale          float32
	refreshHz      int
}

func newCocoaMonitor(index int) cocoaMonitor {
	var x, y, w, h, wx, wy, ww, wh, hz C.int
	var scale C.double
	C.quiCocoaScreenInfo(C.int(index), &x, &y, &w, &h, &wx, &wy, &ww, &wh, &scale, &hz)
	m := cocoaMonitor{
		x: int(x), y: int(y), w: int(w), h: int(h),
		wx: int(wx), wy: int(wy), ww: int(ww), wh: int(wh),
		scale:     float32(scale),
		refreshHz: int(hz),
	}
	if name := C.quiCocoaScreenName(C.int(index)); name != nil {
		m.label = C.GoString(name)
		C.free(unsafe.Pointer(name))
	}
	return m
}

func (m cocoaMonitor) Name() string                   { return m.label }
func (m cocoaMonitor) Position() (int, int)           { return m.x, m.y }
func (m cocoaMonitor) VideoMode() (int, int, int)     { return m.w, m.h, m.refreshHz }
func (m cocoaMonitor) WorkArea() (int, int, int, int) { return m.wx, m.wy, m.ww, m.wh }
func (m cocoaMonitor) ContentScale() (float32, float32) {
	return m.scale, m.scale
}

// --- window ----------------------------------------------------------

// Compile-time proof this backend covers the whole contract, including
// the optional file-drop capability. Without these, a method added to
// platformWindow would only fail where a *cocoaWindow is assigned to the
// interface — which on a non-darwin machine is nowhere.
var (
	_ contract.Window     = (*cocoaWindow)(nil)
	_ contract.DropTarget = (*cocoaWindow)(nil)
	_ contract.Focuser    = (*cocoaWindow)(nil)
	_ contract.CapsLocker = (*cocoaWindow)(nil)
	_ contract.App        = cocoaApp{}
	_ contract.Monitor    = cocoaMonitor{}
	_ contract.Surface    = cocoaSurface{}
)

type cocoaWindow struct {
	handle    uintptr
	handler   contract.Handler
	destroyed bool
	// dropPaths accumulates the paths of one drag operation. AppKit hands
	// over a whole NSURL array, but pushing it across cgo one path at a
	// time avoids marshalling a char** — the commit callback then delivers
	// them together with the drop point.
	dropPaths []string
}

func (w *cocoaWindow) SetHandler(h contract.Handler) { w.handler = h }

func (w *cocoaWindow) Caps() contract.Caps {
	return contract.Caps{
		AbsolutePosition:  true,
		ServerDecorations: true,
		// Real NSEvent gesture methods, not a swizzle — see the .m.
		NativeGestures:            true,
		ClipboardNeedsInputSerial: false,
		// NSPanel + NSWindowStyleMaskNonactivatingPanel — see the .m.
		OverlayPanels: true,
		// NSOpenGLContext per window — see the .m.
		GPUContext: true,
	}
}

func (w *cocoaWindow) SetVisible(visible, activate bool) {
	C.quiCocoaSetVisible(w.cid(), cBool(visible), cBool(activate))
}

// IsFocused reports whether the window is the key window of the active app.
func (w *cocoaWindow) IsFocused() bool { return C.quiCocoaIsKey(w.cid()) != 0 }

func (w *cocoaWindow) IsVisible() bool {
	return C.quiCocoaIsVisible(w.cid()) != 0
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func (w *cocoaWindow) cid() C.uintptr_t { return C.uintptr_t(w.handle) }

func (w *cocoaWindow) Size() (int, int) {
	var cw, ch C.int
	C.quiCocoaGetSize(w.cid(), &cw, &ch)
	return int(cw), int(ch)
}

func (w *cocoaWindow) FramebufferSize() (int, int) {
	var cw, ch C.int
	C.quiCocoaGetFramebufferSize(w.cid(), &cw, &ch)
	return int(cw), int(ch)
}

func (w *cocoaWindow) ContentScale() (float32, float32) {
	s := float32(C.quiCocoaBackingScale(w.cid()))
	return s, s
}

func (w *cocoaWindow) Pos() (int, int, bool) {
	var x, y C.int
	C.quiCocoaGetPos(w.cid(), &x, &y)
	return int(x), int(y), true
}

func (w *cocoaWindow) SetPos(x, y int) bool {
	C.quiCocoaSetPos(w.cid(), C.int(x), C.int(y))
	return true
}

func (w *cocoaWindow) SetTitle(title string) {
	c := C.CString(title)
	defer C.free(unsafe.Pointer(c))
	C.quiCocoaSetTitle(w.cid(), c)
}

func (w *cocoaWindow) SetSize(width, height int) {
	C.quiCocoaSetSize(w.cid(), C.int(width), C.int(height))
}

func (w *cocoaWindow) SetSizeLimits(minW, minH, maxW, maxH int) {
	C.quiCocoaSetSizeLimits(w.cid(), C.int(minW), C.int(minH), C.int(maxW), C.int(maxH))
}

func (w *cocoaWindow) Iconify()  { C.quiCocoaIconify(w.cid()) }
func (w *cocoaWindow) Maximize() { C.quiCocoaMaximize(w.cid()) }
func (w *cocoaWindow) Restore()  { C.quiCocoaRestore(w.cid()) }
func (w *cocoaWindow) Focus()    { C.quiCocoaFocus(w.cid()) }

func (w *cocoaWindow) SetFullscreen(m contract.Monitor, x, y, width, height, _ int) {
	on := C.int(0)
	if m != nil {
		on = 1
	}
	C.quiCocoaSetFullscreen(w.cid(), on, C.int(x), C.int(y), C.int(width), C.int(height))
}

func (w *cocoaWindow) ShouldClose() bool {
	if w.destroyed {
		return true
	}
	return C.quiCocoaShouldClose(w.cid()) != 0
}

func (w *cocoaWindow) SetShouldClose(v bool) {
	value := C.int(0)
	if v {
		value = 1
	}
	C.quiCocoaSetShouldClose(w.cid(), value)
}

func (w *cocoaWindow) Destroy() {
	if w.destroyed {
		return
	}
	w.destroyed = true
	C.quiCocoaDestroyWindow(w.cid())
	unregisterCocoaWindow(w.handle)
}

func (w *cocoaWindow) CursorPos() (float64, float64) {
	var x, y C.double
	C.quiCocoaGetCursorPos(w.cid(), &x, &y)
	return float64(x), float64(y)
}

func (w *cocoaWindow) SetCursor(shape contract.CursorShape) {
	// Image cursors and the hidden pointer are NSCursor objects shared by
	// every window (cursor_darwin.go); the rest are AppKit's own.
	if ns := contract.Host.NativeCursor(shape); ns != nil {
		C.quiCocoaSetCursorObject(w.cid(), ns)
		return
	}
	C.quiCocoaSetCursor(w.cid(), C.int(cocoaCursorShape(shape)))
}

// cocoaCursorShape maps to the small integers the .m switches on. Kept as
// an explicit translation rather than relying on CursorShape's ordinals so
// reordering the Go enum can't silently change cursors.
func cocoaCursorShape(shape contract.CursorShape) int {
	switch shape {
	case contract.CursorText:
		return 1
	case contract.CursorCrosshair:
		return 2
	case contract.CursorHand:
		return 3
	case contract.CursorResizeEW:
		return 4
	case contract.CursorResizeNS:
		return 5
	case contract.CursorResizeNWSE:
		return 6
	case contract.CursorResizeNESW:
		return 7
	default:
		return 0
	}
}

// currentMods polls the live flags. Unlike GLFW this needs no per-key
// GetKey sweep: NSEvent exposes the current modifier state directly.
func (w *cocoaWindow) CurrentMods() contract.Modifiers {
	return mapNSMods(C.quiCocoaCurrentMods())
}

// CapsLock implements contract.CapsLocker.
func (w *cocoaWindow) CapsLock() bool { return C.quiCocoaCapsLock() != 0 }

func (w *cocoaWindow) ClipboardText() string {
	c := C.quiCocoaClipboardText()
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

func (w *cocoaWindow) SetClipboardText(text string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.quiCocoaSetClipboardText(c)
}

func (w *cocoaWindow) MakeCurrent() { C.quiCocoaMakeCurrent(w.cid()) }

func (w *cocoaWindow) Surface() contract.Surface { return cocoaSurface{win: w} }

func (w *cocoaWindow) Wake() { C.quiCocoaWake() }

func (w *cocoaWindow) NativeWindow() unsafe.Pointer {
	return C.quiCocoaNSWindow(w.cid())
}

// SetDropEnabled implements contract.DropTarget.
func (w *cocoaWindow) SetDropEnabled(enabled bool) {
	value := C.int(0)
	if enabled {
		value = 1
	}
	C.quiCocoaSetDropEnabled(w.cid(), value)
}

// cocoaSurface presents through one of two paths, chosen per window by
// the renderer the engine runs on it:
//
//   - GL: GLRenderer draws into the NSOpenGLContext backbuffer and Present
//     flushes it. Pacing comes from the context's swap interval, so Ready
//     returns nil and the loop keeps its timeout-driven cadence.
//   - CPU: the first PresentCPU (CPURenderer, -tags qui_nogl builds)
//     moves the window to layer presentation for good — the frame becomes
//     the view layer's contents, with no GL upload — and starts a display
//     link, so Ready paces painting to the display from then on.
type cocoaSurface struct{ win *cocoaWindow }

var (
	// readyNow is a closed channel: "the display has refreshed".
	readyNow = func() chan struct{} { c := make(chan struct{}); close(c); return c }()
	// readyLater never fires: "not yet; skip painting this iteration".
	readyLater = make(chan struct{})
)

func (s cocoaSurface) Ready() <-chan struct{} {
	switch C.quiCocoaTakeFrameReady(s.win.cid()) {
	case 1:
		return readyNow
	case 0:
		return readyLater
	}
	return nil // GL path, or no display link: pace on the loop's timeout
}

func (s cocoaSurface) PresentCPU(img *image.RGBA, damage image.Rectangle) bool {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if w <= 0 || h <= 0 || len(img.Pix) == 0 {
		return false
	}
	r := image.Rect(0, 0, w, h)
	if !damage.Empty() {
		r = damage.Sub(img.Rect.Min).Intersect(r)
		if r.Empty() {
			return true
		}
	}
	return C.quiCocoaPresentCPU(s.win.cid(), (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), C.int(img.Stride),
		C.int(w), C.int(h), C.int(r.Min.X), C.int(r.Min.Y), C.int(r.Dx()), C.int(r.Dy())) != 0
}

func (s cocoaSurface) Present() { C.quiCocoaPresent(s.win.cid()) }

// --- keyboard --------------------------------------------------------

// --- callbacks from AppKit -------------------------------------------

//export quiCocoaOnMouseMove
func quiCocoaOnMouseMove(id C.uintptr_t, x, y C.double, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnMouseMove(float64(x), float64(y), mapNSMods(mods))
}

//export quiCocoaOnMouseButton
func quiCocoaOnMouseButton(id C.uintptr_t, button, down, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	var btn contract.MouseButton
	switch button {
	case 1:
		btn = contract.MouseButtonRight
	case 2:
		btn = contract.MouseButtonMiddle
	default:
		btn = contract.MouseButtonLeft
	}
	w.handler.OnMouseButton(btn, down != 0, mapNSMods(mods))
}

//export quiCocoaOnScroll
func quiCocoaOnScroll(id C.uintptr_t, x, y, dx, dy C.double, mods, phase, precise C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnScroll(float64(x), float64(y), float64(dx), float64(dy),
		mapNSMods(mods), mapNSGesturePhase(phase), precise != 0)
}

//export quiCocoaShouldTerminate
func quiCocoaShouldTerminate() C.int {
	if contract.Host.QuitRequested() {
		return 0
	}
	return 1
}

//export quiCocoaOnGesture
func quiCocoaOnGesture(id C.uintptr_t, kind, phase C.int, dScale, dRotation C.float, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnGesture(cocoaGestureKind(int(kind)), mapNSGesturePhase(phase),
		float32(dScale), float32(dRotation), mapNSMods(mods))
}

// cocoaGestureKind decodes the small integer the responder methods send
// rather than passing contract.GestureKind ordinals through C, so the Go enum stays
// free to change.
func cocoaGestureKind(kind int) contract.GestureKind {
	switch kind {
	case 1:
		return contract.GestureRotate
	case 2:
		return contract.GestureSmartMagnify
	default:
		return contract.GesturePinch
	}
}

//export quiCocoaOnKey
func quiCocoaOnKey(id C.uintptr_t, keyCode, down, repeat, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnKey(mapCocoaKey(int(keyCode)), int(keyCode),
		down != 0, repeat != 0, mapNSMods(mods))
}

//export quiCocoaOnFlagsChanged
func quiCocoaOnFlagsChanged(id C.uintptr_t, keyCode C.int, flags C.uint64_t, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	down, ok := cocoaModifierKeyDown(int(keyCode), uint64(flags))
	if !ok {
		return
	}
	w.handler.OnKey(mapCocoaKey(int(keyCode)), int(keyCode), down, false, mapNSMods(mods))
}

//export quiCocoaOnChar
func quiCocoaOnChar(id C.uintptr_t, codepoint C.uint32_t) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnChar(rune(codepoint))
}

//export quiCocoaOnFocus
func quiCocoaOnFocus(id C.uintptr_t, focused C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnFocus(focused != 0)
}

//export quiCocoaOnResize
func quiCocoaOnResize(id C.uintptr_t, width, height C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnResize(int(width), int(height))
}

//export quiCocoaOnFramebufferResize
func quiCocoaOnFramebufferResize(id C.uintptr_t, width, height C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnFramebufferResize(int(width), int(height))
}

//export quiCocoaOnRefresh
func quiCocoaOnRefresh(id C.uintptr_t) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.OnRefresh()
}

//export quiCocoaOnWindowState
func quiCocoaOnWindowState() { contract.Host.WakeIfBlocked() }

//export quiCocoaOnDropPath
func quiCocoaOnDropPath(id C.uintptr_t, path *C.char) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || path == nil {
		return
	}
	w.noteDropPath(C.GoString(path))
}

//export quiCocoaOnDropCommit
func quiCocoaOnDropCommit(id C.uintptr_t, x, y C.double) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil {
		return
	}
	w.commitDrop(float64(x), float64(y))
}

// noteDropPath / commitDrop are the pure-Go half of the two-step drop
// protocol, split out so the accumulate-then-deliver-once behavior is
// testable without an NSDraggingInfo.
func (w *cocoaWindow) noteDropPath(path string) {
	w.dropPaths = append(w.dropPaths, path)
}

// commitDrop delivers the accumulated paths and clears them. Clearing
// before the handler runs matters: leftovers would attach this drag's files
// to the next drop, and a handler that opens a modal dialog re-enters the
// event loop while this call is still on the stack.
func (w *cocoaWindow) commitDrop(x, y float64) {
	paths := w.dropPaths
	w.dropPaths = nil
	if w.handler == nil || len(paths) == 0 {
		return
	}
	w.handler.OnFileDrop(paths, x, y)
}
