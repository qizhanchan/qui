//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdint.h>
#include <stdlib.h>

// Implemented in platform_cocoa_darwin.m. Windows are addressed by an
// integer handle throughout: cgo forbids parking Go pointers in C, so C
// owns the AppKit objects in a dictionary and both sides speak in ids.
int  quiCocoaInit(int policy);
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
void   quiCocoaSetFullscreen(uintptr_t handle, int on, int x, int y, int w, int h);
int    quiCocoaShouldClose(uintptr_t handle);
void   quiCocoaSetShouldClose(uintptr_t handle, int value);

void quiCocoaGetCursorPos(uintptr_t handle, double* x, double* y);
void quiCocoaSetCursor(uintptr_t handle, int shape);
void quiCocoaSetDropEnabled(uintptr_t handle, int enabled);
int  quiCocoaCurrentMods(void);

char* quiCocoaClipboardText(void);
void  quiCocoaSetClipboardText(const char* text);

void quiCocoaMakeCurrent(uintptr_t handle);
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

func init() { registerPlatformBackend("cocoa", newCocoaApp) }

func newCocoaApp() (platformApp, error) {
	// Read the policy here rather than at package init: SetActivationPolicy
	// is documented as "before NewApp", and the backend is created lazily
	// on the first activePlatform() call, which NewApp triggers.
	if C.quiCocoaInit(C.int(ActivationPolicy())) == 0 {
		return nil, errors.New("qui: NSApplication initialization failed")
	}
	return cocoaApp{}, nil
}

func (cocoaApp) newWindow(cfg platformWindowConfig) (platformWindow, error) {
	w := &cocoaWindow{}
	w.handle = registerCocoaWindow(w)

	title := C.CString(cfg.Title)
	defer C.free(unsafe.Pointer(title))

	var share C.uintptr_t
	if cw, ok := cfg.Share.(*cocoaWindow); ok && cw != nil {
		share = C.uintptr_t(cw.handle)
	}
	kind := C.int(0)
	if cfg.Kind == WindowOverlayPanel {
		kind = 1
	}
	if C.quiCocoaCreateWindow(C.uintptr_t(w.handle), C.int(cfg.Width), C.int(cfg.Height),
		title, share, kind) == 0 {
		unregisterCocoaWindow(w.handle)
		return nil, errors.New("qui: could not create NSWindow")
	}
	return w, nil
}

func (cocoaApp) pumpEvents(timeout time.Duration) {
	C.quiCocoaPump(C.double(timeout.Seconds()))
}

func (cocoaApp) wake() { C.quiCocoaWake() }

func (cocoaApp) monitors() []platformMonitor {
	n := int(C.quiCocoaScreenCount())
	out := make([]platformMonitor, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, newCocoaMonitor(i))
	}
	return out
}

func (a cocoaApp) primaryMonitor() platformMonitor {
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

func (m cocoaMonitor) name() string                   { return m.label }
func (m cocoaMonitor) position() (int, int)           { return m.x, m.y }
func (m cocoaMonitor) videoMode() (int, int, int)     { return m.w, m.h, m.refreshHz }
func (m cocoaMonitor) workArea() (int, int, int, int) { return m.wx, m.wy, m.ww, m.wh }
func (m cocoaMonitor) contentScale() (float32, float32) {
	return m.scale, m.scale
}

// --- window ----------------------------------------------------------

// Compile-time proof this backend covers the whole contract, including
// the optional file-drop capability. Without these, a method added to
// platformWindow would only fail where a *cocoaWindow is assigned to the
// interface — which on a non-darwin machine is nowhere.
var (
	_ platformWindow     = (*cocoaWindow)(nil)
	_ platformDropTarget = (*cocoaWindow)(nil)
	_ platformApp        = cocoaApp{}
	_ platformMonitor    = cocoaMonitor{}
	_ platformSurface    = cocoaSurface{}
)

type cocoaWindow struct {
	handle    uintptr
	handler   platformHandler
	destroyed bool
	// dropPaths accumulates the paths of one drag operation. AppKit hands
	// over a whole NSURL array, but pushing it across cgo one path at a
	// time avoids marshalling a char** — the commit callback then delivers
	// them together with the drop point.
	dropPaths []string
}

func (w *cocoaWindow) setHandler(h platformHandler) { w.handler = h }

func (w *cocoaWindow) caps() platformCaps {
	return platformCaps{
		AbsolutePosition:  true,
		ServerDecorations: true,
		// Real NSEvent gesture methods, not a swizzle — see the .m.
		NativeGestures:            true,
		ClipboardNeedsInputSerial: false,
		// NSPanel + NSWindowStyleMaskNonactivatingPanel — see the .m.
		OverlayPanels: true,
	}
}

func (w *cocoaWindow) setVisible(visible, activate bool) {
	C.quiCocoaSetVisible(w.cid(), cBool(visible), cBool(activate))
}

func (w *cocoaWindow) isVisible() bool {
	return C.quiCocoaIsVisible(w.cid()) != 0
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func (w *cocoaWindow) cid() C.uintptr_t { return C.uintptr_t(w.handle) }

func (w *cocoaWindow) size() (int, int) {
	var cw, ch C.int
	C.quiCocoaGetSize(w.cid(), &cw, &ch)
	return int(cw), int(ch)
}

func (w *cocoaWindow) framebufferSize() (int, int) {
	var cw, ch C.int
	C.quiCocoaGetFramebufferSize(w.cid(), &cw, &ch)
	return int(cw), int(ch)
}

func (w *cocoaWindow) contentScale() (float32, float32) {
	s := float32(C.quiCocoaBackingScale(w.cid()))
	return s, s
}

func (w *cocoaWindow) pos() (int, int, bool) {
	var x, y C.int
	C.quiCocoaGetPos(w.cid(), &x, &y)
	return int(x), int(y), true
}

func (w *cocoaWindow) setPos(x, y int) bool {
	C.quiCocoaSetPos(w.cid(), C.int(x), C.int(y))
	return true
}

func (w *cocoaWindow) setTitle(title string) {
	c := C.CString(title)
	defer C.free(unsafe.Pointer(c))
	C.quiCocoaSetTitle(w.cid(), c)
}

func (w *cocoaWindow) setSize(width, height int) {
	C.quiCocoaSetSize(w.cid(), C.int(width), C.int(height))
}

func (w *cocoaWindow) setSizeLimits(minW, minH, maxW, maxH int) {
	C.quiCocoaSetSizeLimits(w.cid(), C.int(minW), C.int(minH), C.int(maxW), C.int(maxH))
}

func (w *cocoaWindow) iconify()  { C.quiCocoaIconify(w.cid()) }
func (w *cocoaWindow) maximize() { C.quiCocoaMaximize(w.cid()) }
func (w *cocoaWindow) restore()  { C.quiCocoaRestore(w.cid()) }
func (w *cocoaWindow) focus()    { C.quiCocoaFocus(w.cid()) }

func (w *cocoaWindow) setFullscreen(m platformMonitor, x, y, width, height, _ int) {
	on := C.int(0)
	if m != nil {
		on = 1
	}
	C.quiCocoaSetFullscreen(w.cid(), on, C.int(x), C.int(y), C.int(width), C.int(height))
}

func (w *cocoaWindow) shouldClose() bool {
	if w.destroyed {
		return true
	}
	return C.quiCocoaShouldClose(w.cid()) != 0
}

func (w *cocoaWindow) setShouldClose(v bool) {
	value := C.int(0)
	if v {
		value = 1
	}
	C.quiCocoaSetShouldClose(w.cid(), value)
}

func (w *cocoaWindow) destroy() {
	if w.destroyed {
		return
	}
	w.destroyed = true
	C.quiCocoaDestroyWindow(w.cid())
	unregisterCocoaWindow(w.handle)
}

func (w *cocoaWindow) cursorPos() (float64, float64) {
	var x, y C.double
	C.quiCocoaGetCursorPos(w.cid(), &x, &y)
	return float64(x), float64(y)
}

func (w *cocoaWindow) setCursor(shape CursorShape) {
	C.quiCocoaSetCursor(w.cid(), C.int(cocoaCursorShape(shape)))
}

// cocoaCursorShape maps to the small integers the .m switches on. Kept as
// an explicit translation rather than relying on CursorShape's ordinals so
// reordering the Go enum can't silently change cursors.
func cocoaCursorShape(shape CursorShape) int {
	switch shape {
	case CursorText:
		return 1
	case CursorCrosshair:
		return 2
	case CursorHand:
		return 3
	case CursorResizeEW:
		return 4
	case CursorResizeNS:
		return 5
	default:
		return 0
	}
}

// currentMods polls the live flags. Unlike GLFW this needs no per-key
// GetKey sweep: NSEvent exposes the current modifier state directly.
func (w *cocoaWindow) currentMods() Modifiers {
	return mapNSMods(C.quiCocoaCurrentMods())
}

func (w *cocoaWindow) clipboardText() string {
	c := C.quiCocoaClipboardText()
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}

func (w *cocoaWindow) setClipboardText(text string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.quiCocoaSetClipboardText(c)
}

func (w *cocoaWindow) makeCurrent() { C.quiCocoaMakeCurrent(w.cid()) }

func (w *cocoaWindow) surface() platformSurface { return cocoaSurface{win: w} }

func (w *cocoaWindow) wake() { C.quiCocoaWake() }

func (w *cocoaWindow) nativeWindow() unsafe.Pointer {
	return C.quiCocoaNSWindow(w.cid())
}

// setDropEnabled implements platformDropTarget (see filedrop.go).
func (w *cocoaWindow) setDropEnabled(enabled bool) {
	value := C.int(0)
	if enabled {
		value = 1
	}
	C.quiCocoaSetDropEnabled(w.cid(), value)
}

// cocoaSurface is the GL presentation path: draw into the NSOpenGLContext
// backbuffer, flush.
//
// ready and presentCPU decline for now, which keeps pacing on the event
// loop's timeout and routes every frame through GL — identical to the GLFW
// backend. Turning them on (CADisplayLink + CALayer contents) is what
// removes the per-frame full-screen texture upload; it is deliberately a
// separate change so a regression here can be attributed to the window
// and input rewrite alone.
type cocoaSurface struct{ win *cocoaWindow }

func (cocoaSurface) ready() <-chan struct{}      { return nil }
func (cocoaSurface) presentCPU(*image.RGBA) bool { return false }
func (s cocoaSurface) present()                  { C.quiCocoaPresent(s.win.cid()) }

// --- keyboard --------------------------------------------------------

// cocoaKeyCodes maps macOS virtual keycodes to engine keys.
//
// The values are the kVK_* constants from Carbon's Events.h, written
// literally so this file needn't link Carbon. They are positions on the
// physical keyboard, NOT characters: 0x00 is the key labelled A on a US
// layout and stays "KeyA" under Dvorak or AZERTY, which is what shortcut
// handling wants.
var cocoaKeyCodes = map[int]Key{
	0x00: KeyA, 0x0B: KeyB, 0x08: KeyC, 0x02: KeyD, 0x0E: KeyE,
	0x03: KeyF, 0x05: KeyG, 0x04: KeyH, 0x22: KeyI, 0x26: KeyJ,
	0x28: KeyK, 0x25: KeyL, 0x2E: KeyM, 0x2D: KeyN, 0x1F: KeyO,
	0x23: KeyP, 0x0C: KeyQ, 0x0F: KeyR, 0x01: KeyS, 0x11: KeyT,
	0x20: KeyU, 0x09: KeyV, 0x0D: KeyW, 0x07: KeyX, 0x10: KeyY,
	0x06: KeyZ,

	0x1D: Key0, 0x12: Key1, 0x13: Key2, 0x14: Key3, 0x15: Key4,
	0x17: Key5, 0x16: Key6, 0x1A: Key7, 0x1C: Key8, 0x19: Key9,

	0x1B: KeyMinus, 0x18: KeyEqual, 0x2C: KeySlash,

	0x31: KeySpace,
	0x24: KeyEnter,
	0x4C: KeyEnter, // keypad Enter
	0x30: KeyTab,
	0x33: KeyBackspace,
	0x35: KeyEscape,
	0x75: KeyDelete, // forward delete
	0x72: KeyInsert, // Help; PC keyboards send Insert here
	0x73: KeyHome,
	0x77: KeyEnd,
	0x74: KeyPageUp,
	0x79: KeyPageDown,

	0x7B: KeyLeft, 0x7C: KeyRight, 0x7E: KeyUp, 0x7D: KeyDown,

	0x7A: KeyF1, 0x78: KeyF2, 0x63: KeyF3, 0x76: KeyF4,
	0x60: KeyF5, 0x61: KeyF6, 0x62: KeyF7, 0x64: KeyF8,
	0x65: KeyF9, 0x6D: KeyF10, 0x67: KeyF11, 0x6F: KeyF12,
}

func mapCocoaKey(code int) Key {
	if k, ok := cocoaKeyCodes[code]; ok {
		return k
	}
	return KeyUnknown
}

// --- callbacks from AppKit -------------------------------------------

//export quiCocoaOnMouseMove
func quiCocoaOnMouseMove(id C.uintptr_t, x, y C.double, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onMouseMove(float64(x), float64(y), mapNSMods(mods))
}

//export quiCocoaOnMouseButton
func quiCocoaOnMouseButton(id C.uintptr_t, button, down, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	var btn MouseButton
	switch button {
	case 1:
		btn = MouseButtonRight
	case 2:
		btn = MouseButtonMiddle
	default:
		btn = MouseButtonLeft
	}
	w.handler.onMouseButton(btn, down != 0, mapNSMods(mods))
}

//export quiCocoaOnScroll
func quiCocoaOnScroll(id C.uintptr_t, x, y, dx, dy C.double, mods, phase C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onScroll(float64(x), float64(y), float64(dx), float64(dy),
		mapNSMods(mods), mapNSGesturePhase(phase))
}

//export quiCocoaOnGesture
func quiCocoaOnGesture(id C.uintptr_t, kind, phase C.int, dScale, dRotation C.float, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onGesture(cocoaGestureKind(int(kind)), mapNSGesturePhase(phase),
		float32(dScale), float32(dRotation), mapNSMods(mods))
}

// cocoaGestureKind decodes the small integer the responder methods send
// rather than passing EventType ordinals through C, so the Go enum stays
// free to change.
func cocoaGestureKind(kind int) EventType {
	switch kind {
	case 1:
		return EventGestureRotate
	case 2:
		return EventGestureSmartMagnify
	default:
		return EventGesturePinch
	}
}

//export quiCocoaOnKey
func quiCocoaOnKey(id C.uintptr_t, keyCode, down, repeat, mods C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onKey(mapCocoaKey(int(keyCode)), int(keyCode),
		down != 0, repeat != 0, mapNSMods(mods))
}

//export quiCocoaOnChar
func quiCocoaOnChar(id C.uintptr_t, codepoint C.uint32_t) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onChar(rune(codepoint))
}

//export quiCocoaOnFocus
func quiCocoaOnFocus(id C.uintptr_t, focused C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onFocus(focused != 0)
}

//export quiCocoaOnResize
func quiCocoaOnResize(id C.uintptr_t, width, height C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onResize(int(width), int(height))
}

//export quiCocoaOnFramebufferResize
func quiCocoaOnFramebufferResize(id C.uintptr_t, width, height C.int) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onFramebufferResize(int(width), int(height))
}

//export quiCocoaOnRefresh
func quiCocoaOnRefresh(id C.uintptr_t) {
	w := lookupCocoaWindow(uintptr(id))
	if w == nil || w.handler == nil {
		return
	}
	w.handler.onRefresh()
}

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
	w.handler.onFileDrop(paths, x, y)
}
