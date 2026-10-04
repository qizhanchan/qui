//go:build darwin && cgo && webview_cef

package webview

/*
#cgo CPPFLAGS: -I${SRCDIR}/lib/darwin/cef
#cgo CXXFLAGS: -std=c++20 -x objective-c++ -fobjc-arc -mmacosx-version-min=12.0
#cgo LDFLAGS: -L${SRCDIR}/lib/darwin/cef/build/libcef_dll_wrapper -lcef_dll_wrapper
#cgo LDFLAGS: -framework Cocoa -framework AppKit -framework Foundation
// Linking notes:
//   * We deliberately do NOT statically link the Chromium Embedded
//     Framework dylib. libcef_dll_wrapper.a contains dlopen-stubs
//     for every CEF C-API function (see CEF's libcef_dll/libcef_dll_dylib.cc)
//     that load the framework lazily via CefScopedLibraryLoader.
//     This means:
//       - the binary can run outside a .app bundle without crashing
//         at exec time (great for `go test`, dev iteration); calls
//         into CEF only fail when actually invoked.
//       - inside the .app, the wrapper dlopens the framework using
//         its install name, which already points at
//         @executable_path/../Frameworks/...
//   * We can't use -framework on "Chromium Embedded Framework" either
//     way, because cgo splits LDFLAGS on whitespace with no shell
//     escaping. The dlopen path sidesteps the whole problem.

#include <stdint.h>
#include <stdlib.h>

// extern "C" surface implemented in backend_darwin.cc.
int  qui_webview_cef_load_library(void);
int  qui_webview_cef_initialize(int argc, char** argv);
void qui_webview_cef_tick(void);
void qui_webview_cef_shutdown(void);
int  qui_webview_cef_is_initialized(void);
int  qui_webview_cef_use_host_init(void);

int  qui_webview_cef_page_new(uintptr_t handle, int w, int h, float scale, const char* url);
void qui_webview_cef_page_destroy(uintptr_t handle);
void qui_webview_cef_page_load_url(uintptr_t handle, const char* url);
void qui_webview_cef_page_reload(uintptr_t handle);
void qui_webview_cef_page_stop_load(uintptr_t handle);
void qui_webview_cef_page_go_back(uintptr_t handle);
void qui_webview_cef_page_go_forward(uintptr_t handle);
void qui_webview_cef_page_resize(uintptr_t handle, int w, int h, float scale);
void qui_webview_cef_page_send_mouse_move(uintptr_t handle, int x, int y, unsigned int mods, int leave);
void qui_webview_cef_page_send_mouse_click(uintptr_t handle, int x, int y, int button, int mouseUp, int clickCount, unsigned int mods);
void qui_webview_cef_page_send_mouse_wheel(uintptr_t handle, int x, int y, int dx, int dy, unsigned int mods);
void qui_webview_cef_page_send_key(uintptr_t handle, int type, int windowsKeyCode, int nativeKeyCode, int character, unsigned int mods);
void qui_webview_cef_page_open_devtools(uintptr_t handle);
void qui_webview_cef_page_set_focus(uintptr_t handle, int focused);
void qui_webview_cef_page_ime_set_composition(uintptr_t handle, const char* utf8, int cursor);
void qui_webview_cef_page_ime_commit(uintptr_t handle, const char* utf8);
void qui_webview_cef_page_ime_cancel(uintptr_t handle);
void qui_webview_cef_page_caret_rect(uintptr_t handle, int* x, int* y, int* w, int* h);
int  qui_webview_cef_page_send_devtools(uintptr_t handle, const char* json, int json_len);
void qui_webview_cef_page_send_handler_reply(uintptr_t handle, int message_id, const char* reply_ptr, int reply_len, const char* err);
void qui_webview_cef_shutdown(void);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
)

// darwinBackend is the CEF-backed Backend for macOS. It wires up
// CefInitialize + Tick + per-page CreateBrowserSync + OnPaint into a
// Go-side image.RGBA so the WebView widget can blit web pixels through
// the standard canvas.DrawImage path. IOSurface zero-copy + GL
// compositing are not implemented yet.
type darwinBackend struct {
	mu          sync.Mutex
	initStarted bool
	initErr     error
}

// init runs when the Go runtime starts (before main). We just register
// the backend pointer here; CEF library loading + CefInitialize is
// deferred to the first NewPage call so:
//
//   - tests / non-.app runs don't abort on dyld lookup failure
//     (CefScopedLibraryLoader::LoadInMain CHECK-aborts on failure;
//     deferring lets us return a clean error instead).
//   - widget construction at package level doesn't pay the multi-
//     second CEF startup cost when no WebView is ever opened.
func init() {
	setBackend(&darwinBackend{})
}

// Capabilities reports what the CEF backend can do. CEF is Chromium-
// based, so the answer is essentially "everything modern".
func (b *darwinBackend) Capabilities() Capabilities {
	return Capabilities{
		WebGL:    true,
		WebRTC:   true,
		Video:    true,
		JSBridge: true,
	}
}

// NewPage creates a CEF browser via CreateBrowserSync (windowless OSR
// mode) and returns a darwinPage that forwards all Page operations
// to the underlying QuiCefClient. Must be called on the qui main
// goroutine — CefBrowserHost::CreateBrowserSync expects the CEF UI
// thread, which is the same thread that pumps CefDoMessageLoopWork
// (= the main goroutine in qui's single-threaded loop).
func (b *darwinBackend) NewPage(cfg PageConfig) (Page, error) {
	if err := b.ensureInit(); err != nil {
		return nil, err
	}

	handle := nextPageHandle()
	page := &darwinPage{
		handle: handle,
		scale:  cfg.DeviceScale,
		width:  cfg.Width,
		height: cfg.Height,
		url:    cfg.InitialURL,
		state:  LoadStateLoading,
	}
	if page.scale <= 0 {
		page.scale = 1
	}
	if page.width <= 0 {
		page.width = 1
	}
	if page.height <= 0 {
		page.height = 1
	}
	registerDarwinPage(handle, page)

	cURL := C.CString(cfg.InitialURL)
	defer C.free(unsafe.Pointer(cURL))
	if rc := C.qui_webview_cef_page_new(
		C.uintptr_t(handle),
		C.int(page.width),
		C.int(page.height),
		C.float(page.scale),
		cURL,
	); rc != 0 {
		unregisterDarwinPage(handle)
		return nil, fmt.Errorf("webview: CefBrowserHost::CreateBrowserSync failed for handle=%d", handle)
	}
	return page, nil
}

func (b *darwinBackend) preInitialize() error {
	return b.ensureInit()
}

// Tick pumps the CEF message loop. Called from every WebView.Tick;
// CefDoMessageLoopWork is documented as cheap-when-idle so we don't
// dedupe per-frame.
func (b *darwinBackend) Tick() {
	if C.qui_webview_cef_is_initialized() == 0 {
		return
	}
	C.qui_webview_cef_tick()
}

// shutdown is the darwin implementation of webview.Shutdown. Closes
// every page, pumps the CEF message loop until each browser fires
// OnBeforeClose (or until a 2-second deadline), then calls CefShutdown.
//
// Why this is the entry point and not Page.Destroy in a loop:
//
//   - CefBrowserHost::CloseBrowser is async — it queues a task on the
//     CEF UI thread. With qui's main loop already drained (we're called
//     from Window.OnClose, before the window event loop stops), there's nothing
//     pumping CefDoMessageLoopWork unless we do it here.
//   - CefShutdown must be the LAST CEF call; calling it before all
//     browsers have closed can crash on internal asserts. We watch the
//     g_pages registry shrinking via the OnBeforeClose unregister path.
//
// Idempotent: re-entry sees an empty page registry and a non-initialized
// CEF runtime and returns immediately.
func (b *darwinBackend) shutdown() error {
	if C.qui_webview_cef_is_initialized() == 0 {
		return nil
	}
	// Snapshot the pages so we can drive each one's Destroy without
	// holding the registry mutex across CEF calls.
	darwinPagesMu.RLock()
	pages := make([]*darwinPage, 0, len(darwinPages))
	for _, p := range darwinPages {
		pages = append(pages, p)
	}
	darwinPagesMu.RUnlock()

	// Triggers CloseBrowser on every still-live page. Each Destroy
	// also bumps pendingCloseCount; the corresponding OnBeforeClose
	// callback decrements it.
	for _, p := range pages {
		p.Destroy()
	}

	// Pump until every Destroy has paired with an OnBeforeClose, or
	// until the deadline expires. The 2-second cap is a safety net for
	// the unlikely case CEF wedges; in practice every page closes in
	// well under 50ms on macOS.
	deadline := time.Now().Add(2 * time.Second)
	for pendingCloseCount.Load() > 0 {
		if time.Now().After(deadline) {
			break
		}
		C.qui_webview_cef_tick()
		time.Sleep(2 * time.Millisecond)
	}

	// One last pump to drain any close-completion tasks queued during
	// the final iteration, then shut CEF down. After this point, no
	// CEF function may be called from this process again.
	C.qui_webview_cef_tick()
	C.qui_webview_cef_shutdown()
	return nil
}

// ensureInit lazily loads the CEF dylib + calls CefInitialize the first
// time NewPage is invoked. Re-entrant safe (returns the cached error
// on subsequent attempts after a failure, rather than retrying — once
// CefInitialize fails the wrapper state is unsafe to retry against).
func (b *darwinBackend) ensureInit() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.initStarted {
		return b.initErr
	}
	b.initStarted = true

	// Host-mode short-circuit: when the binary runs as a Go c-shared
	// library inside a C++ host that already called CefInitialize (see
	// webview/host/main.mm + examples/webview/hostlib), the host calls
	// qui_webview_cef_use_host_init() before driving the tick loop,
	// which sets g_initialized=true in this TU. Skip our own init —
	// CEF allows exactly one CefInitialize per process.
	if C.qui_webview_cef_is_initialized() != 0 {
		return nil
	}

	// Load the CEF framework dylib via CefScopedLibraryLoader. Fails
	// here if the binary isn't running inside a .app bundle with the
	// framework at Contents/Frameworks/Chromium Embedded Framework.framework
	// — which is the expected state during `go test` or running the
	// raw binary outside the .app.
	if rc := C.qui_webview_cef_load_library(); rc != 0 {
		b.initErr = fmt.Errorf("%w: cannot dlopen Chromium Embedded "+
			"Framework — the binary must run inside a properly packaged "+
			".app bundle (use webview/scripts/package-app.sh)",
			ErrNotInitialized)
		return b.initErr
	}

	// Marshal os.Args into argv. CEF wants a real char** and reads
	// strings via the strings; ownership stays with us (CEF copies
	// internally), but the slice + the C strings must remain alive
	// for the duration of CefInitialize. We allocate via C.malloc /
	// C.CString so the pointers don't move from under us.
	argv := make([]*C.char, len(os.Args))
	for i, a := range os.Args {
		argv[i] = C.CString(a)
	}
	defer func() {
		for _, p := range argv {
			C.free(unsafe.Pointer(p))
		}
	}()
	var argvPtr **C.char
	if len(argv) > 0 {
		argvPtr = (**C.char)(unsafe.Pointer(&argv[0]))
	}

	if rc := C.qui_webview_cef_initialize(C.int(len(argv)), argvPtr); rc != 0 {
		b.initErr = fmt.Errorf("%w: CefInitialize failed "+
			"(check stderr for details; common causes: .app structure "+
			"wrong, helper apps missing, framework path unresolvable)",
			ErrNotInitialized)
		return b.initErr
	}
	return nil
}

// -----------------------------------------------------------------------------
// darwinPage — one CEF browser instance.
// -----------------------------------------------------------------------------

// darwinPage holds Go-side state for one CEF browser. The C side owns
// the CefBrowser + QuiCefClient; the uintptr handle keys both sides'
// registries so callbacks find their way back.
type darwinPage struct {
	handle uintptr

	mu  sync.Mutex
	img *image.RGBA // BGRA-from-CEF, swizzled into RGBA on each OnPaint

	width  int
	height int
	scale  float32

	url     string
	title   string
	state   LoadState
	closed  atomic.Bool
	created atomic.Bool // true once OnAfterCreated has fired

	// buttonState is a bitmask of mouse buttons currently held down
	// (bit0=left, bit1=right, bit2=middle). Folded into CEF event
	// modifiers as EVENTFLAG_*_MOUSE_BTN so Chromium recognizes
	// drag-select and active focus sessions. Without these bits CEF
	// can still route a single MouseDown to set DOM focus, but the
	// caret blink animation and text-selection drag never start.
	buttonState atomic.Uint32

	// Listeners — set/cleared from any goroutine; invoked from the
	// CEF UI thread (= qui main goroutine via Tick).
	onNav     func(NavigationEvent)
	onLoad    func(LoadEvent)
	onTitle   func(string)
	onCursor  func(CursorKind)
	onConsole func(level int, message string)

	// JS↔Go bridge state.
	//
	// nextEvalID assigns DevTools-protocol message ids. We start at 1
	// because the CEF/DevTools convention treats 0 as "auto-assign"
	// for ExecuteDevToolsMethod; keeping our ids in the positive
	// 32-bit range is safe and matches the protocol's int field width.
	nextEvalID atomic.Int32

	evalsMu sync.Mutex
	// evals maps a pending DevTools message id → the channel waiting
	// on the Runtime.evaluate result. The receiving goroutine in
	// EvaluateJS pulls one value; the //export thunk
	// quiWebviewOnDevToolsResult delivers it; Destroy fans out
	// ErrClosed to any survivors.
	evals map[int]chan evalReply

	handlersMu sync.RWMutex
	// handlers is the per-page Go-side dispatch table for window.qui
	// calls coming from the renderer. The widget's higher-level
	// handlerTable is the source of truth that's reseeded via
	// RegisterHandler on Open; this map only exists to give
	// quiWebviewOnHandlerCall a cheap O(1) lookup keyed by page handle
	// without bouncing through the widget.
	handlers map[string]MessageHandler
}

// evalReply is the bridge value between quiWebviewOnDevToolsResult (the
// //export thunk) and the goroutine inside EvaluateJS that's waiting on
// its message id. Either Value is meaningful or Err is set.
type evalReply struct {
	Value JSValue
	Err   error
}

// LatestCPUFrame returns the most recently rasterized page contents as
// a Go-owned image.RGBA. Signature deliberately matches the
// cpuFrameProvider interface in webview.go — WebView.Draw probes via
// type assertion and silently falls through to the GL path on a
// mismatch, so the single-return-value shape is load-bearing. nil
// means OnPaint hasn't fired yet.
func (p *darwinPage) LatestCPUFrame() *image.RGBA {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.img
}

func (p *darwinPage) LoadURL(url string) error {
	if p.closed.Load() {
		return ErrClosed
	}
	cs := C.CString(url)
	defer C.free(unsafe.Pointer(cs))
	C.qui_webview_cef_page_load_url(C.uintptr_t(p.handle), cs)
	return nil
}

func (p *darwinPage) LoadHTML(html, baseURL string) error {
	if p.closed.Load() {
		return ErrClosed
	}
	// CEF removed CefFrame::LoadString; the canonical replacement is a
	// data URL. baseURL is currently unused; a future
	// improvement is to spin up a custom CefSchemeHandlerFactory to
	// honor it for relative resource resolution.
	_ = baseURL
	url := "data:text/html;charset=utf-8," + urlEncode(html)
	return p.LoadURL(url)
}

func (p *darwinPage) Reload() error {
	if p.closed.Load() {
		return ErrClosed
	}
	C.qui_webview_cef_page_reload(C.uintptr_t(p.handle))
	return nil
}

func (p *darwinPage) StopLoad() error {
	if p.closed.Load() {
		return ErrClosed
	}
	C.qui_webview_cef_page_stop_load(C.uintptr_t(p.handle))
	return nil
}

func (p *darwinPage) GoBack() error {
	if p.closed.Load() {
		return ErrClosed
	}
	C.qui_webview_cef_page_go_back(C.uintptr_t(p.handle))
	return nil
}

func (p *darwinPage) GoForward() error {
	if p.closed.Load() {
		return ErrClosed
	}
	C.qui_webview_cef_page_go_forward(C.uintptr_t(p.handle))
	return nil
}

func (p *darwinPage) Resize(w, h int, scale float32) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	if scale <= 0 {
		scale = 1
	}
	p.mu.Lock()
	p.width = w
	p.height = h
	p.scale = scale
	p.mu.Unlock()
	C.qui_webview_cef_page_resize(C.uintptr_t(p.handle),
		C.int(w), C.int(h), C.float(scale))
	return nil
}

// AcquireFrame is part of the GL path; this backend returns (0, …, false)
// to signal "no GL texture available" so WebView.Draw falls through to
// the CPU path via LatestCPUFrame.
func (p *darwinPage) AcquireFrame() (uint32, int, int, qui.Rect, bool) {
	return 0, 0, 0, qui.Rect{}, false
}

func (p *darwinPage) ReleaseFrame() {}

// -----------------------------------------------------------------------------
// Input injection
// -----------------------------------------------------------------------------

func (p *darwinPage) InjectMouseMove(x, y float32, mods qui.Modifiers) error {
	if p.closed.Load() {
		return ErrClosed
	}
	leave := 0
	if x < 0 || y < 0 {
		leave = 1
	}
	C.qui_webview_cef_page_send_mouse_move(
		C.uintptr_t(p.handle),
		C.int(int(x+0.5)), C.int(int(y+0.5)),
		C.uint(cefModifiers(mods)|p.mouseButtonFlags()),
		C.int(leave),
	)
	return nil
}

func (p *darwinPage) InjectMouseButton(x, y float32, btn qui.MouseButton, down bool, clickCount int, mods qui.Modifiers) error {
	if p.closed.Load() {
		return ErrClosed
	}
	var cButton C.int
	var btnBit uint32
	switch btn {
	case qui.MouseButtonLeft:
		cButton, btnBit = 0, 1<<0
	case qui.MouseButtonMiddle:
		cButton, btnBit = 1, 1<<2
	case qui.MouseButtonRight:
		cButton, btnBit = 2, 1<<1
	default:
		cButton, btnBit = 0, 1<<0
	}
	mouseUp := 1
	if down {
		mouseUp = 0
		// Set the bit BEFORE sending so the down event carries it.
		p.buttonState.Store(p.buttonState.Load() | btnBit)
	}
	if clickCount < 1 {
		clickCount = 1
	}
	// The button bit is included in BOTH down and up events. Chromium
	// expects the up event to still report "this button is the one
	// being released" — clearing it before sending would make the up
	// look like a hover.
	C.qui_webview_cef_page_send_mouse_click(
		C.uintptr_t(p.handle),
		C.int(int(x+0.5)), C.int(int(y+0.5)),
		cButton,
		C.int(mouseUp),
		C.int(clickCount),
		C.uint(cefModifiers(mods)|p.mouseButtonFlags()),
	)
	if !down {
		p.buttonState.Store(p.buttonState.Load() &^ btnBit)
	}
	return nil
}

func (p *darwinPage) InjectScroll(x, y, dx, dy float32, mods qui.Modifiers) error {
	if p.closed.Load() {
		return ErrClosed
	}
	// CEF expects integer pixel deltas; qui delivers float "line"
	// deltas. Multiply by ~20px/line, which matches Chromium's own
	// default ticker quantum for wheel events.
	pxX := int(dx * 20)
	pxY := int(dy * 20)
	C.qui_webview_cef_page_send_mouse_wheel(
		C.uintptr_t(p.handle),
		C.int(int(x+0.5)), C.int(int(y+0.5)),
		C.int(pxX), C.int(pxY),
		C.uint(cefModifiers(mods)|p.mouseButtonFlags()),
	)
	return nil
}

// mouseButtonFlags collapses the currently-pressed mouse buttons into
// the CEF event-flag bits Chromium reads for drag / selection /
// caret-active state. Reads the atomic mask written by InjectMouseButton.
func (p *darwinPage) mouseButtonFlags() uint32 {
	const (
		EVENTFLAG_LEFT_MOUSE_BTN   = 1 << 4
		EVENTFLAG_MIDDLE_MOUSE_BTN = 1 << 5
		EVENTFLAG_RIGHT_MOUSE_BTN  = 1 << 6
	)
	bs := p.buttonState.Load()
	var f uint32
	if bs&(1<<0) != 0 {
		f |= EVENTFLAG_LEFT_MOUSE_BTN
	}
	if bs&(1<<1) != 0 {
		f |= EVENTFLAG_RIGHT_MOUSE_BTN
	}
	if bs&(1<<2) != 0 {
		f |= EVENTFLAG_MIDDLE_MOUSE_BTN
	}
	return f
}

func (p *darwinPage) InjectKey(key qui.Key, scancode int, down bool, mods qui.Modifiers) error {
	if p.closed.Load() {
		return ErrClosed
	}
	// cef_key_event_type_t: KEYEVENT_RAWKEYDOWN=0, KEYEVENT_KEYDOWN=1,
	// KEYEVENT_KEYUP=2, KEYEVENT_CHAR=3. For physical key down/up we
	// use RAWKEYDOWN and KEYUP — CHAR events come through InjectChar.
	//
	// CefKeyEvent.windows_key_code expects Windows VK_* codes on EVERY
	// platform — Chromium normalizes against that namespace internally
	// for input dispatch. Passing the raw qui.Key int (small enum
	// 0..~50) lands on whatever VK_ happens to share that low value,
	// which is why arrow keys and enter were misbehaving.
	vk := quiKeyToWindowsVK(key)
	// CefKeyEvent.native_key_code on macOS is the Quartz keycode, which
	// is exactly what GLFW reports as the scancode for keys it knows.
	// Pass it through so CEF can populate KeyboardEvent.code correctly.
	C.qui_webview_cef_page_send_key(
		C.uintptr_t(p.handle),
		C.int(boolToInt(!down)*2), // 0=RAWKEYDOWN, 2=KEYUP
		C.int(vk),
		C.int(scancode),
		C.int(0),
		C.uint(cefModifiers(mods)),
	)
	return nil
}

// boolToInt is a tiny helper that lets the InjectKey expression stay on
// one line (RAWKEYDOWN=0, KEYUP=2).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// quiKeyToWindowsVK maps the qui.Key enum to the Windows Virtual Key
// (VK_*) namespace that CefKeyEvent.windows_key_code expects. The set
// is intentionally narrow — match the keys qui currently surfaces in
// event.go; extend as more enum members land. Unknown keys return 0
// (which CEF treats as "no logical key", letting the char event drive
// input alone — fine for shortcut-free typing).
//
// Reference: https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
func quiKeyToWindowsVK(k qui.Key) int {
	switch {
	case k >= qui.KeyA && k <= qui.KeyZ:
		return 0x41 + int(k-qui.KeyA) // 'A'..'Z' VK codes are ASCII
	case k >= qui.Key0 && k <= qui.Key9:
		return 0x30 + int(k-qui.Key0) // '0'..'9' VK codes are ASCII
	}
	switch k {
	case qui.KeySpace:
		return 0x20 // VK_SPACE
	case qui.KeyEnter:
		return 0x0D // VK_RETURN
	case qui.KeyEscape:
		return 0x1B // VK_ESCAPE
	case qui.KeyLeft:
		return 0x25 // VK_LEFT
	case qui.KeyUp:
		return 0x26 // VK_UP
	case qui.KeyRight:
		return 0x27 // VK_RIGHT
	case qui.KeyDown:
		return 0x28 // VK_DOWN
	case qui.KeyHome:
		return 0x24 // VK_HOME
	case qui.KeyEnd:
		return 0x23 // VK_END
	case qui.KeyBackspace:
		return 0x08 // VK_BACK
	case qui.KeyTab:
		return 0x09 // VK_TAB
	case qui.KeyDelete:
		return 0x2E // VK_DELETE
	case qui.KeyInsert:
		return 0x2D // VK_INSERT
	case qui.KeyPageUp:
		return 0x21 // VK_PRIOR
	case qui.KeyPageDown:
		return 0x22 // VK_NEXT
	case qui.KeyF1:
		return 0x70 // VK_F1
	case qui.KeyF2:
		return 0x71 // VK_F2
	case qui.KeyF3:
		return 0x72 // VK_F3
	case qui.KeyF4:
		return 0x73 // VK_F4
	case qui.KeyF5:
		return 0x74 // VK_F5
	case qui.KeyF6:
		return 0x75 // VK_F6
	case qui.KeyF7:
		return 0x76 // VK_F7
	case qui.KeyF8:
		return 0x77 // VK_F8
	case qui.KeyF9:
		return 0x78 // VK_F9
	case qui.KeyF10:
		return 0x79 // VK_F10
	case qui.KeyF11:
		return 0x7A // VK_F11
	case qui.KeyF12:
		return 0x7B // VK_F12
	case qui.KeyMinus:
		return 0xBD // VK_OEM_MINUS
	case qui.KeyEqual:
		return 0xBB // VK_OEM_PLUS
	case qui.KeySlash:
		return 0xBF // VK_OEM_2
	}
	return 0
}

func (p *darwinPage) InjectChar(r rune, mods qui.Modifiers) error {
	if p.closed.Load() {
		return ErrClosed
	}
	C.qui_webview_cef_page_send_key(
		C.uintptr_t(p.handle),
		C.int(3), // KEYEVENT_CHAR
		C.int(0),
		C.int(0),
		C.int(int(r)),
		C.uint(cefModifiers(mods)),
	)
	return nil
}

func (p *darwinPage) OpenDevTools() error {
	if p.closed.Load() {
		return ErrClosed
	}
	C.qui_webview_cef_page_open_devtools(C.uintptr_t(p.handle))
	return nil
}

func (p *darwinPage) InjectFocus(focused bool) error {
	if p.closed.Load() {
		return ErrClosed
	}
	f := 0
	if focused {
		f = 1
	}
	C.qui_webview_cef_page_set_focus(C.uintptr_t(p.handle), C.int(f))
	return nil
}

// InjectIMEPreedit forwards a composition update to CEF. An empty
// text string clears the composition (qui's IMEClient contract:
// CommitIME("") is a no-op; SetPreedit("",0) tears down the composing
// state).
func (p *darwinPage) InjectIMEPreedit(text string, cursor int) error {
	if p.closed.Load() {
		return ErrClosed
	}
	if text == "" {
		C.qui_webview_cef_page_ime_cancel(C.uintptr_t(p.handle))
		return nil
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.qui_webview_cef_page_ime_set_composition(C.uintptr_t(p.handle), cs, C.int(cursor))
	return nil
}

// InjectIMECommit finalizes the composition. An empty text string is
// treated as a finish-composing (commit whatever is currently
// composing) rather than a no-op so a stuck composition can be
// cleared by passing "".
func (p *darwinPage) InjectIMECommit(text string) error {
	if p.closed.Load() {
		return ErrClosed
	}
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.qui_webview_cef_page_ime_commit(C.uintptr_t(p.handle), cs)
	return nil
}

// CaretRect returns the last caret rectangle CEF reported via
// CefRenderHandler::OnImeCompositionRangeChanged, in widget-local DIPs.
// Empty until the renderer has fired its first composition update —
// the WebView widget swaps that in for a fallback (its own top-left)
// so the IME candidate window doesn't end up at the screen origin.
func (p *darwinPage) CaretRect() qui.Rect {
	if p.closed.Load() {
		return qui.Rect{}
	}
	var x, y, w, h C.int
	C.qui_webview_cef_page_caret_rect(C.uintptr_t(p.handle), &x, &y, &w, &h)
	return qui.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}
}

// -----------------------------------------------------------------------------
// JS bridge.
//
// EvaluateJS uses the DevTools Protocol's Runtime.evaluate method via
// CefBrowserHost::SendDevToolsMessage. Replies come back asynchronously
// to a CefDevToolsMessageObserver registered per-browser; the C++ side
// forwards them into quiWebviewOnDevToolsResult, which finds the
// matching channel by message-id and unblocks the EvaluateJS goroutine.
//
// We use SendDevToolsMessage (raw JSON) rather than
// ExecuteDevToolsMethod (structured params) because building a
// CefDictionaryValue from cgo would mean a flurry of small allocator
// calls plus an awkward translation of nested params. A 200-byte JSON
// string is cheaper and easier to maintain.
// -----------------------------------------------------------------------------

// evalTimeout is the maximum time EvaluateJS will block waiting for a
// DevTools reply. Five seconds is generous for any sane synchronous
// expression while protecting callers from a hung renderer. Callers
// that need longer-running JS (e.g. heavy DOM scans) should run it via
// a handler that returns a Promise instead.
const evalTimeout = 5 * time.Second

func (p *darwinPage) EvaluateJS(script string) (JSValue, error) {
	if p.closed.Load() {
		return JSValue{}, ErrClosed
	}
	// The browser must have finished CefBrowserHost::CreateBrowserSync
	// before we can talk to its DevTools agent — SendDevToolsMessage
	// silently drops requests aimed at a nascent or torn-down browser.
	// Surface that as a clear error instead of timing out.
	if !p.created.Load() {
		return JSValue{}, fmt.Errorf(
			"webview: page not ready; call EvaluateJS after OnLoadFinish")
	}

	id := int(p.nextEvalID.Add(1))
	ch := make(chan evalReply, 1)
	p.evalsMu.Lock()
	if p.evals == nil {
		p.evals = map[int]chan evalReply{}
	}
	p.evals[id] = ch
	p.evalsMu.Unlock()

	// Defer-style cleanup: if we leave by any path other than channel
	// receive, drop the pending entry so a late reply (or Destroy)
	// doesn't try to write into an orphan channel.
	cleanup := func() {
		p.evalsMu.Lock()
		delete(p.evals, id)
		p.evalsMu.Unlock()
	}

	msg := buildEvaluateMessage(id, script)
	cs := C.CString(msg)
	rc := C.qui_webview_cef_page_send_devtools(
		C.uintptr_t(p.handle), cs, C.int(len(msg)))
	C.free(unsafe.Pointer(cs))
	if rc != 0 {
		cleanup()
		return JSValue{}, fmt.Errorf("webview: SendDevToolsMessage failed " +
			"(must be called on the CEF UI thread / qui main goroutine)")
	}

	// Pump the CEF message loop while waiting for the reply.
	//
	// EvaluateJS is the only Page method that *waits* on CEF — everything
	// else is fire-and-forget. The qui frame loop normally drives
	// CefDoMessageLoopWork from WebView.Tick, but since EvaluateJS is
	// designed to be called from a button click (which runs synchronously
	// on the qui main goroutine), blocking on the reply channel parks the
	// exact goroutine that would otherwise pump the CEF UI thread. Without
	// manual pumping, the DevTools reply never gets a chance to fire
	// QuiDevToolsObserver::OnDevToolsMethodResult and EvaluateJS times out.
	//
	// 1ms sleep keeps the loop responsive (a DevTools roundtrip is
	// typically well under 5ms on macOS) without burning a core; the
	// non-blocking select gives us "return immediately on reply" with
	// no extra latency. This pattern is safe because we're on the same
	// thread that's already responsible for pumping the loop — CEF
	// doesn't care if the call comes from inside a button handler vs.
	// the top-level Tick.
	deadline := time.Now().Add(evalTimeout)
	for {
		select {
		case r := <-ch:
			return r.Value, r.Err
		default:
		}
		if time.Now().After(deadline) {
			cleanup()
			return JSValue{}, fmt.Errorf("webview: EvaluateJS timed out after %s", evalTimeout)
		}
		C.qui_webview_cef_tick()
		time.Sleep(1 * time.Millisecond)
	}
}

// buildEvaluateMessage formats a Runtime.evaluate DevTools-Protocol JSON
// message. We turn the user's script + the requested message id into a
// well-formed call: returnByValue gives us a JSON-encoded result we can
// translate to JSValue without a second roundtrip; awaitPromise lets
// async expressions resolve before we get the reply; replMode lets bare
// statements like `var x = 1` succeed without a syntax error.
func buildEvaluateMessage(id int, script string) string {
	// json.Marshal handles all string escaping concerns (quotes,
	// backslashes, control chars, unicode) correctly. Hand-rolled
	// escaping has bitten too many DevTools wrappers — never roll
	// your own here.
	params := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
		AwaitPromise  bool   `json:"awaitPromise"`
		UserGesture   bool   `json:"userGesture"`
		ReplMode      bool   `json:"replMode"`
	}{
		Expression:    script,
		ReturnByValue: true,
		AwaitPromise:  true,
		UserGesture:   true,
		ReplMode:      true,
	}
	payload, _ := json.Marshal(params) // struct cannot produce an error
	return `{"id":` + strconv.Itoa(id) + `,"method":"Runtime.evaluate","params":` + string(payload) + `}`
}

func (p *darwinPage) RegisterHandler(name string, fn MessageHandler) error {
	if p.closed.Load() {
		return ErrClosed
	}
	p.handlersMu.Lock()
	if p.handlers == nil {
		p.handlers = map[string]MessageHandler{}
	}
	if fn == nil {
		delete(p.handlers, name)
	} else {
		p.handlers[name] = fn
	}
	p.handlersMu.Unlock()
	return nil
}

func (p *darwinPage) UnregisterHandler(name string) error {
	if p.closed.Load() {
		return ErrClosed
	}
	p.handlersMu.Lock()
	if p.handlers != nil {
		delete(p.handlers, name)
	}
	p.handlersMu.Unlock()
	return nil
}

// lookupHandler returns the page's registered handler for name (or nil).
// Read-locked; cheap to call from the CEF UI thread.
func (p *darwinPage) lookupHandler(name string) MessageHandler {
	p.handlersMu.RLock()
	defer p.handlersMu.RUnlock()
	return p.handlers[name]
}

// drainPendingEvals fans ErrClosed out to every EvaluateJS goroutine
// still parked on a reply. Called from Destroy so callers blocked on a
// page that just went away don't hang for evalTimeout. Safe to call
// multiple times; the map is emptied each call.
func (p *darwinPage) drainPendingEvals() {
	p.evalsMu.Lock()
	pending := p.evals
	p.evals = nil
	p.evalsMu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- evalReply{Err: ErrClosed}:
		default:
			// Receiver already saw a reply or timed out — drop.
		}
	}
}

// -----------------------------------------------------------------------------
// Listener registration
// -----------------------------------------------------------------------------

func (p *darwinPage) SetNavigationListener(fn func(NavigationEvent)) {
	p.mu.Lock()
	p.onNav = fn
	p.mu.Unlock()
}
func (p *darwinPage) SetLoadListener(fn func(LoadEvent)) {
	p.mu.Lock()
	p.onLoad = fn
	p.mu.Unlock()
}
func (p *darwinPage) SetTitleListener(fn func(string)) {
	p.mu.Lock()
	p.onTitle = fn
	p.mu.Unlock()
}
func (p *darwinPage) SetCursorListener(fn func(CursorKind)) {
	p.mu.Lock()
	p.onCursor = fn
	p.mu.Unlock()
}
func (p *darwinPage) SetConsoleListener(fn func(level int, message string)) {
	p.mu.Lock()
	p.onConsole = fn
	p.mu.Unlock()
}

// -----------------------------------------------------------------------------
// State queries
// -----------------------------------------------------------------------------

func (p *darwinPage) URL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url
}
func (p *darwinPage) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.title
}
func (p *darwinPage) State() LoadState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Destroy releases the underlying CefBrowser. Idempotent. The actual
// teardown happens asynchronously on the CEF UI thread; the Go-side
// page is removed from the registry immediately so late callbacks
// no-op, but pendingCloseCount stays elevated until OnBeforeClose
// fires so shutdown() knows when it's safe to call CefShutdown.
func (p *darwinPage) Destroy() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}
	// Unblock any EvaluateJS goroutines still parked on a reply before
	// the C-side teardown — once unregisterDarwinPage runs, no more
	// DevTools results can route back to them.
	p.drainPendingEvals()
	pendingCloseCount.Add(1)
	C.qui_webview_cef_page_destroy(C.uintptr_t(p.handle))
	unregisterDarwinPage(p.handle)
}

// -----------------------------------------------------------------------------
// Page registry — uintptr handles map to *darwinPage. Cgo can't hold
// Go pointers in C state, so we keep the indirection on the Go side.
// -----------------------------------------------------------------------------

var (
	darwinPagesMu sync.RWMutex
	darwinPages   = map[uintptr]*darwinPage{}
	pageHandleSeq atomic.Uint64

	// pendingCloseCount tracks browsers between Destroy() and the
	// C++ OnBeforeClose callback. Used by shutdown() to know when
	// it's safe to call CefShutdown — Destroy() removes the page
	// from darwinPages right away (so late //export thunks no-op),
	// but the actual CefBrowser close is async and we must wait for
	// the matching OnBeforeClose before tearing CEF down or
	// CefShutdown will trip an internal assertion.
	pendingCloseCount atomic.Int32
)

func nextPageHandle() uintptr {
	// Start at 1 so 0 stays reserved for "no page".
	return uintptr(pageHandleSeq.Add(1))
}

func registerDarwinPage(h uintptr, p *darwinPage) {
	darwinPagesMu.Lock()
	darwinPages[h] = p
	darwinPagesMu.Unlock()
}

func unregisterDarwinPage(h uintptr) {
	darwinPagesMu.Lock()
	delete(darwinPages, h)
	darwinPagesMu.Unlock()
}

func lookupDarwinPage(h uintptr) *darwinPage {
	darwinPagesMu.RLock()
	p := darwinPages[h]
	darwinPagesMu.RUnlock()
	return p
}

// -----------------------------------------------------------------------------
// //export thunks — invoked from QuiCefClient on the CEF UI thread.
// -----------------------------------------------------------------------------

//export quiWebviewOnPaint
func quiWebviewOnPaint(handle C.uintptr_t, buf unsafe.Pointer, width, height C.int) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil || buf == nil || width <= 0 || height <= 0 {
		return
	}
	w := int(width)
	h := int(height)
	stride := w * 4
	srcBytes := unsafe.Slice((*byte)(buf), stride*h)

	p.mu.Lock()
	if p.img == nil || p.img.Rect.Dx() != w || p.img.Rect.Dy() != h {
		p.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	dst := p.img.Pix
	p.mu.Unlock()

	// CEF on macOS hands us BGRA premultiplied; image.RGBA wants RGBA.
	// Swap byte 0 ↔ byte 2 per pixel. The full-frame copy is the
	// fastest correct approach here; IOSurface zero-copy removes the
	// copy entirely.
	bgraToRGBA(dst, srcBytes)
}

// bgraToRGBA swizzles a BGRA byte slice into RGBA in dst. Both slices
// must be the same length and a multiple of 4. A 4-byte stride means
// we can keep the alpha byte unmoved.
func bgraToRGBA(dst, src []byte) {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	for i := 0; i+3 < n; i += 4 {
		dst[i+0] = src[i+2]
		dst[i+1] = src[i+1]
		dst[i+2] = src[i+0]
		dst[i+3] = src[i+3]
	}
}

//export quiWebviewOnAfterCreated
func quiWebviewOnAfterCreated(handle C.uintptr_t) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	p.created.Store(true)
}

//export quiWebviewOnBeforeClose
func quiWebviewOnBeforeClose(handle C.uintptr_t) {
	// The page is already marked closed via Destroy() before the
	// browser was asked to close; we only use this callback to drive
	// the shutdown() wait — every CloseBrowser eventually pairs with
	// one OnBeforeClose, so this counter going to zero means CEF has
	// fully released its browser objects and CefShutdown is safe.
	_ = handle
	pendingCloseCount.Add(-1)
}

//export quiWebviewOnLoadStart
func quiWebviewOnLoadStart(handle C.uintptr_t, url *C.char) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	u := C.GoString(url)
	p.mu.Lock()
	p.url = u
	p.state = LoadStateLoading
	fn := p.onLoad
	p.mu.Unlock()
	if fn != nil {
		fn(LoadEvent{State: LoadStateLoading, URL: u})
	}
}

//export quiWebviewOnLoadEnd
func quiWebviewOnLoadEnd(handle C.uintptr_t, url *C.char, httpStatus C.int) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	_ = httpStatus
	u := C.GoString(url)
	p.mu.Lock()
	p.url = u
	p.state = LoadStateLoaded
	fn := p.onLoad
	p.mu.Unlock()
	if fn != nil {
		fn(LoadEvent{State: LoadStateLoaded, URL: u})
	}
}

//export quiWebviewOnLoadError
func quiWebviewOnLoadError(handle C.uintptr_t, url *C.char, errCode C.int, errText *C.char) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	u := C.GoString(url)
	msg := C.GoString(errText)
	p.mu.Lock()
	p.state = LoadStateError
	fn := p.onLoad
	p.mu.Unlock()
	if fn != nil {
		fn(LoadEvent{
			State: LoadStateError,
			URL:   u,
			Err:   fmt.Errorf("%w: %s (code=%d)", ErrLoadFailed, msg, int(errCode)),
		})
	}
}

//export quiWebviewOnAddressChange
func quiWebviewOnAddressChange(handle C.uintptr_t, url *C.char) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	p.mu.Lock()
	p.url = C.GoString(url)
	p.mu.Unlock()
}

//export quiWebviewOnTitleChange
func quiWebviewOnTitleChange(handle C.uintptr_t, title *C.char) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	t := C.GoString(title)
	p.mu.Lock()
	p.title = t
	fn := p.onTitle
	p.mu.Unlock()
	if fn != nil {
		fn(t)
	}
}

//export quiWebviewOnConsoleMessage
func quiWebviewOnConsoleMessage(handle C.uintptr_t, level C.int, msg *C.char) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	m := C.GoString(msg)
	p.mu.Lock()
	fn := p.onConsole
	p.mu.Unlock()
	if fn != nil {
		fn(int(level), m)
	}
}

//export quiWebviewOnDevToolsResult
func quiWebviewOnDevToolsResult(handle C.uintptr_t, messageID C.int, success C.int,
	result unsafe.Pointer, resultLen C.int) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	id := int(messageID)
	p.evalsMu.Lock()
	ch, ok := p.evals[id]
	if ok {
		delete(p.evals, id)
	}
	p.evalsMu.Unlock()
	if !ok {
		// No waiter — likely a duplicate delivery, a result that arrived
		// after timeout, or a result for an internal message we sent
		// before we started tracking (none in v1).
		return
	}

	// Copy the JSON bytes into a Go-owned slice before the C buffer goes
	// away when this callback returns.
	var raw []byte
	if result != nil && resultLen > 0 {
		raw = make([]byte, int(resultLen))
		copy(raw, unsafe.Slice((*byte)(result), int(resultLen)))
	}

	reply := parseEvaluateResult(raw, success != 0)
	select {
	case ch <- reply:
	default:
		// Receiver gave up (Destroy or timeout already drained); drop.
	}
}

// parseEvaluateResult turns the DevTools-Protocol "result" or "error"
// dict from a Runtime.evaluate reply into an evalReply. On success the
// dict shape is documented at
// https://chromedevtools.github.io/devtools-protocol/tot/Runtime/#method-evaluate :
//
//	{
//	  "result": { "type": "number", "value": 3, "description": "3" },
//	  "exceptionDetails": { ... } // optional
//	}
//
// On failure (success=false) we receive the "error" dict instead:
//
//	{ "code": -32000, "message": "Cannot find context with specified id" }
func parseEvaluateResult(raw []byte, success bool) evalReply {
	if !success {
		var errDoc struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &errDoc)
		if errDoc.Message == "" {
			errDoc.Message = "unknown DevTools error"
		}
		return evalReply{Err: fmt.Errorf("webview: EvaluateJS: %s (code=%d)",
			errDoc.Message, errDoc.Code)}
	}

	var doc struct {
		Result *struct {
			Type                string          `json:"type"`
			Subtype             string          `json:"subtype"`
			Value               json.RawMessage `json:"value"`
			UnserializableValue string          `json:"unserializableValue"`
			Description         string          `json:"description"`
			ClassName           string          `json:"className"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
				ClassName   string `json:"className"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return evalReply{Err: fmt.Errorf("webview: malformed Runtime.evaluate reply: %w", err)}
	}

	// JS exception (parse / runtime error). Use Exception.Description if
	// present (it includes the stack); fall back to text.
	if doc.ExceptionDetails != nil {
		desc := doc.ExceptionDetails.Text
		if doc.ExceptionDetails.Exception != nil &&
			doc.ExceptionDetails.Exception.Description != "" {
			desc = doc.ExceptionDetails.Exception.Description
		}
		if desc == "" {
			desc = "JavaScript exception"
		}
		return evalReply{Err: fmt.Errorf("webview: %s", desc)}
	}
	if doc.Result == nil {
		return evalReply{Value: JSValue{Kind: JSKindNull}}
	}

	switch doc.Result.Type {
	case "undefined":
		return evalReply{Value: JSValue{Kind: JSKindNull}}
	case "boolean":
		var b bool
		if len(doc.Result.Value) > 0 {
			_ = json.Unmarshal(doc.Result.Value, &b)
		}
		return evalReply{Value: JSValue{Kind: JSKindBool, Bool: b}}
	case "number":
		if doc.Result.UnserializableValue != "" {
			// "NaN", "Infinity", "-Infinity", "-0" arrive here as
			// strings because JSON has no encoding for them. Surface as
			// JSKindString so callers can distinguish (the description
			// is the human-readable form).
			return evalReply{Value: JSValue{
				Kind:   JSKindString,
				String: doc.Result.UnserializableValue,
			}}
		}
		var n float64
		if len(doc.Result.Value) > 0 {
			_ = json.Unmarshal(doc.Result.Value, &n)
		}
		return evalReply{Value: JSValue{Kind: JSKindNumber, Number: n}}
	case "string":
		var s string
		if len(doc.Result.Value) > 0 {
			_ = json.Unmarshal(doc.Result.Value, &s)
		}
		return evalReply{Value: JSValue{Kind: JSKindString, String: s}}
	case "object":
		if doc.Result.Subtype == "null" {
			return evalReply{Value: JSValue{Kind: JSKindNull}}
		}
		// Pass through the raw JSON; caller json.Unmarshals into the
		// shape they want.
		if len(doc.Result.Value) == 0 {
			// returnByValue couldn't serialize (function, DOM node, …)
			// — surface a string description so the caller doesn't get
			// a silent null. Description is what Chrome's console shows.
			return evalReply{Value: JSValue{Kind: JSKindString, String: doc.Result.Description}}
		}
		return evalReply{Value: JSValue{Kind: JSKindJSON, JSON: append([]byte(nil), doc.Result.Value...)}}
	}
	// Unknown type — surface description as a string so debugging is
	// possible without losing the reply entirely.
	return evalReply{Value: JSValue{Kind: JSKindString, String: doc.Result.Description}}
}

//export quiWebviewOnHandlerCall
func quiWebviewOnHandlerCall(handle C.uintptr_t, name *C.char, messageID C.int,
	payload unsafe.Pointer, payloadLen C.int) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	handlerName := C.GoString(name)
	var payloadBytes []byte
	if payload != nil && payloadLen > 0 {
		payloadBytes = make([]byte, int(payloadLen))
		copy(payloadBytes, unsafe.Slice((*byte)(payload), int(payloadLen)))
	}
	msgID := int(messageID)

	// Look up the handler from the page's table. If unknown, reply with
	// an error immediately so the JS Promise rejects in a bounded way
	// instead of hanging.
	fn := p.lookupHandler(handlerName)
	if fn == nil {
		go p.replyHandlerError(msgID,
			"webview: no Go handler registered for "+strconv.Quote(handlerName))
		return
	}
	// Run the handler off the CEF UI thread — handlers are user code and
	// must not block the loop pumping CefDoMessageLoopWork.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.replyHandlerError(msgID, fmt.Sprintf("webview: handler %q panicked: %v", handlerName, r))
			}
		}()
		reply, err := fn(payloadBytes)
		if err != nil {
			p.replyHandlerError(msgID, err.Error())
			return
		}
		p.replyHandlerSuccess(msgID, reply)
	}()
}

// replyHandlerSuccess delivers a handler reply back to the renderer's
// window.qui Proxy via CefFrame::SendProcessMessage. No-op once the
// page is closed (the C side checks browser_ and drops the call).
func (p *darwinPage) replyHandlerSuccess(messageID int, reply []byte) {
	if p.closed.Load() {
		return
	}
	var replyPtr *C.char
	if len(reply) > 0 {
		replyPtr = (*C.char)(unsafe.Pointer(&reply[0]))
	}
	emptyErr := C.CString("")
	defer C.free(unsafe.Pointer(emptyErr))
	C.qui_webview_cef_page_send_handler_reply(
		C.uintptr_t(p.handle),
		C.int(messageID),
		replyPtr,
		C.int(len(reply)),
		emptyErr,
	)
}

func (p *darwinPage) replyHandlerError(messageID int, msg string) {
	if p.closed.Load() {
		return
	}
	errStr := C.CString(msg)
	defer C.free(unsafe.Pointer(errStr))
	C.qui_webview_cef_page_send_handler_reply(
		C.uintptr_t(p.handle),
		C.int(messageID),
		nil,
		0,
		errStr,
	)
}

//export quiWebviewOnCursorChange
func quiWebviewOnCursorChange(handle C.uintptr_t, cursorType C.int) {
	p := lookupDarwinPage(uintptr(handle))
	if p == nil {
		return
	}
	p.mu.Lock()
	fn := p.onCursor
	p.mu.Unlock()
	if fn != nil {
		fn(cefCursorToKind(int(cursorType)))
	}
}

// cefCursorToKind translates a cef_cursor_type_t (defined in
// cef_types.h around line 2678) into qui's CursorKind enum. The CEF
// enum is much richer (~50 variants — panning directions, drag-and-drop
// affordances, zoom-in/out) than what qui exposes, so anything we
// don't have a direct mapping for falls back to CursorArrow. App code
// that needs finer-grained cursors can register its own cursor
// listener and tap the raw enum via a future API.
func cefCursorToKind(t int) CursorKind {
	// Hardcoded against cef_cursor_type_t's iota order in cef_types.h.
	// If CEF ever reorders this enum (very rare — would break ABI for
	// every embedder), the build will keep working but cursors will be
	// wrong; an integration test would catch it.
	const (
		ctPointer = iota
		ctCross
		ctHand
		ctIBeam
		ctWait
		ctHelp
		ctEastResize
		ctNorthResize
		ctNortheastResize
		ctNorthwestResize
		ctSouthResize
		ctSoutheastResize
		ctSouthwestResize
		ctWestResize
		ctNorthSouthResize
		ctEastWestResize
		ctNortheastSouthwestResize
		ctNorthwestSoutheastResize
		ctColumnResize
		ctRowResize
		ctMiddlePanning
		ctEastPanning
		ctNorthPanning
		ctNortheastPanning
		ctNorthwestPanning
		ctSouthPanning
		ctSoutheastPanning
		ctSouthwestPanning
		ctWestPanning
		ctMove
		ctVerticalText
		ctCell
		ctContextMenu
		ctAlias
		ctProgress
		ctNoDrop
		ctCopy
		ctNone
		ctNotAllowed
		ctZoomIn
		ctZoomOut
		ctGrab
		ctGrabbing
		ctMiddlePanningVertical
		ctMiddlePanningHorizontal
		ctCustom
	)
	switch t {
	case ctIBeam, ctVerticalText:
		return CursorIBeam
	case ctHand, ctGrab, ctGrabbing:
		return CursorHand
	case ctCross, ctCell:
		return CursorCrosshair
	case ctMove, ctMiddlePanning, ctMiddlePanningVertical, ctMiddlePanningHorizontal,
		ctEastPanning, ctNorthPanning, ctNortheastPanning, ctNorthwestPanning,
		ctSouthPanning, ctSoutheastPanning, ctSouthwestPanning, ctWestPanning:
		return CursorMove
	case ctWait, ctProgress:
		return CursorWait
	case ctNotAllowed, ctNoDrop:
		return CursorNotAllowed
	case ctEastResize, ctWestResize, ctEastWestResize, ctColumnResize:
		return CursorResizeHorizontal
	case ctNorthResize, ctSouthResize, ctNorthSouthResize, ctRowResize:
		return CursorResizeVertical
	case ctNortheastResize, ctSouthwestResize, ctNortheastSouthwestResize:
		return CursorResizeNESW
	case ctNorthwestResize, ctSoutheastResize, ctNorthwestSoutheastResize:
		return CursorResizeNWSE
	case ctPointer, ctNone:
		return CursorArrow
	default:
		// ctContextMenu, ctAlias, ctCopy, ctZoomIn/Out, ctCustom — no
		// direct qui equivalent yet. Falling back to Arrow keeps the
		// cursor sensible.
		return CursorArrow
	}
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// cefModifiers maps qui.Modifiers into CEF's cef_event_flags_t bits.
// See include/internal/cef_types.h for the canonical bit values; we
// reproduce the subset the qui modifier set covers.
func cefModifiers(mods qui.Modifiers) uint32 {
	const (
		EVENTFLAG_CAPS_LOCK_ON     = 1 << 0
		EVENTFLAG_SHIFT_DOWN       = 1 << 1
		EVENTFLAG_CONTROL_DOWN     = 1 << 2
		EVENTFLAG_ALT_DOWN         = 1 << 3
		EVENTFLAG_LEFT_MOUSE_BTN   = 1 << 4
		EVENTFLAG_MIDDLE_MOUSE_BTN = 1 << 5
		EVENTFLAG_RIGHT_MOUSE_BTN  = 1 << 6
		EVENTFLAG_COMMAND_DOWN     = 1 << 7
	)
	var f uint32
	if mods&qui.ModShift != 0 {
		f |= EVENTFLAG_SHIFT_DOWN
	}
	if mods&qui.ModControl != 0 {
		f |= EVENTFLAG_CONTROL_DOWN
	}
	if mods&qui.ModAlt != 0 {
		f |= EVENTFLAG_ALT_DOWN
	}
	if mods&qui.ModSuper != 0 {
		f |= EVENTFLAG_COMMAND_DOWN
	}
	return f
}

// urlEncode is a small percent-encoder used to embed HTML into a
// data: URL. We only need to cover the bytes that confuse Chromium's
// data: parser; everything else passes through.
func urlEncode(s string) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			out = append(out, '%', '2', '0')
		case c == '#' || c == '%' || c == '?' || c == '&' || c == '+':
			out = append(out, '%', hex[c>>4], hex[c&0x0f])
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
