package qui

import (
	"errors"
	"image"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// The platform seam, engine side.
//
// Backends live in their own packages under internal/platform (cocoa,
// x11, web, headless, glfw) and implement the exported interfaces in
// internal/platform/contract, which also carries the design notes for the
// seam (union-of-platforms shape, logical vs physical units, async
// presentation). platform_adapter.go wraps those for the engine.
//
// The interfaces below are the engine's private view of the same seam.
// They mirror contract method for method; keeping them unexported is what
// lets every engine call site and test fake stay independent of how the
// backends are packaged. Nothing in the engine may reference a windowing
// library directly.

// platformApp is the process-level surface: initialization, the event
// pump, display enumeration, teardown. One instance per process.
type platformApp interface {
	// newWindow creates a window. The returned platformWindow is non-nil
	// only on success — engine code distinguishes "no platform window"
	// (test windows) by a nil interface value.
	newWindow(cfg platformWindowConfig) (platformWindow, error)

	// pumpEvents blocks until input arrives or timeout elapses, then
	// dispatches everything queued to the relevant handlers. A zero
	// timeout means "poll, don't block".
	pumpEvents(timeout time.Duration)

	// wake unblocks a concurrent pumpEvents from another goroutine. This
	// is how PostJob gets the main loop to notice new work without
	// waiting out the timeout.
	wake()

	monitors() []platformMonitor
	primaryMonitor() platformMonitor
}

// platformWindowConfig describes a window to create. Kept as a struct so
// adding a field (decorations, transparency, always-on-top) doesn't churn
// every backend's signature.
type platformWindowConfig struct {
	Title  string
	Width  int
	Height int
	// Share, when non-nil, requests that the new window's GPU context
	// share resources (textures, shaders, buffers) with that window's.
	Share platformWindow
	// Kind is the window's OS-level role. A backend that cannot honor the
	// requested kind must return an error, NOT a window of a different
	// kind: WindowOverlayPanel's whole point is not taking focus, and
	// quietly substituting a normal window turns a missing feature into
	// the user's keystrokes going somewhere they didn't intend. See
	// window_overlay.go.
	Kind WindowKind
}

// platformCaps reports abilities that genuinely differ between platforms
// (see contract.Caps).
type platformCaps = contract.Caps

// platformMonitor is one connected display. Methods return logical
// (point) units so window placement needs no DPI conversion.
type platformMonitor interface {
	name() string
	position() (x, y int)
	// videoMode reports the current mode. refreshHz is 0 when unknown.
	videoMode() (w, h, refreshHz int)
	// workArea excludes OS chrome (menu bar, dock, taskbar).
	workArea() (x, y, w, h int)
	contentScale() (x, y float32)
}

// platformWindow is one OS window plus its input and presentation.
type platformWindow interface {
	// setHandler installs the receiver for this window's input and
	// lifecycle events. One handler replaces the traditional pile of
	// per-event callback setters so a backend can't partially wire the
	// engine up: adding an event to the contract breaks every backend at
	// compile time, which is the point.
	setHandler(h platformHandler)

	caps() platformCaps

	// size is the logical (point) size of the content area.
	size() (w, h int)
	// framebufferSize is the PHYSICAL pixel size of the content area.
	framebufferSize() (w, h int)
	// contentScale is the display's DPI scale. Not necessarily an
	// integer — see the package doc above.
	contentScale() (x, y float32)

	// pos reports the window's top-left in global logical coordinates.
	// ok is false where the platform has no notion of global position
	// (Wayland) — see platformCaps.AbsolutePosition.
	pos() (x, y int, ok bool)
	// setPos moves the window, reporting whether the platform allowed it.
	setPos(x, y int) bool

	setTitle(title string)
	// setSize resizes the CONTENT area to w x h logical points, keeping the
	// window's TOP-left corner where it is. Top-left rather than the
	// platform-native anchor because that is the origin every coordinate
	// crossing this seam uses: a caller that positioned a panel under a
	// text caret expects it to grow downward, not to push its own top edge
	// up off the anchor.
	setSize(w, h int)
	// setSizeLimits constrains interactive resizing. A bound <= 0 means
	// "unconstrained on that axis".
	setSizeLimits(minW, minH, maxW, maxH int)
	iconify()
	maximize()
	restore()
	focus()

	// setVisible orders the window on or off screen without destroying it.
	//
	// activate is a REQUEST to also take keyboard focus while showing.
	// Passing false is what makes an overlay panel usable: on macOS it is
	// orderFront: rather than makeKeyAndOrderFront:. A backend whose
	// windows always activate on show must still accept false — but then
	// it must also have declined to create the panel in the first place
	// (platformCaps.OverlayPanels), so the case cannot arise for panels.
	setVisible(visible, activate bool)
	// isVisible reports whether the OS currently has this window on screen.
	isVisible() bool

	// setFullscreen puts the window on monitor m at the given geometry, or
	// returns it to windowed mode at that geometry when m is nil.
	// refreshHz <= 0 means "whatever the display prefers".
	setFullscreen(m platformMonitor, x, y, w, h, refreshHz int)

	shouldClose() bool
	setShouldClose(v bool)
	destroy()

	cursorPos() (x, y float64)
	setCursor(shape CursorShape)
	// currentMods polls the live modifier state. Needed because most
	// platforms report modifiers with key and button events but not with
	// motion or wheel events, and "is Ctrl held right now" decides
	// scroll-versus-zoom.
	currentMods() Modifiers

	clipboardText() string
	setClipboardText(text string)

	// makeCurrent binds this window's GPU context to the calling thread.
	// Multi-window apps have one context per window, so this runs before
	// any drawing. A backend with no GPU context makes this a no-op.
	makeCurrent()

	// surface is this window's presentation target. Never nil.
	surface() platformSurface

	// wake unblocks the event loop servicing this window, from any
	// goroutine. Per-window rather than per-app because in a mixed-backend
	// process (a native window plus a fallback one) the loops are not
	// necessarily the same, and waking the wrong one strands queued work.
	wake()

	// nativeWindow is the platform object (NSWindow*, HWND, wl_surface*)
	// for the native bridges that must reach past the seam — IME, native
	// menus, native fullscreen. Returns nil when there is none.
	nativeWindow() unsafe.Pointer
}

// platformSurface is where a finished frame goes.
//
// Two presentation paths exist because they have genuinely different
// costs. The GPU path is what a GL/Metal renderer wants: it has already
// drawn into a backbuffer and only needs a swap. The CPU path exists
// because qui's reference rasterizer produces an *image.RGBA, and handing
// that straight to the compositor (CALayer contents, wl_shm buffer, DXGI)
// avoids uploading a full-screen texture every frame purely to blit it
// back down — which is what the GL path has to do today.
//
// A backend implements whichever it can. presentCPU returning false is a
// normal answer, not a failure.
type platformSurface interface {
	// ready fires when the display is ready for another frame —
	// CADisplayLink on macOS, wl_surface.frame on Wayland, a waitable
	// swapchain on Windows.
	//
	// Returns nil when the platform offers no such signal, in which case
	// the caller falls back to pacing on its event-pump timeout. Callers
	// must not block on this channel indefinitely: a window whose display
	// link is stopped (occluded, off-screen) would otherwise never run
	// another frame, and jobs posted from other goroutines would stall.
	ready() <-chan struct{}

	// presentCPU hands a finished CPU-rasterized frame to the compositor,
	// reporting whether it did so. False means "this surface has no CPU
	// path" and the caller should present through the GPU path instead.
	//
	// The image is owned by the renderer and reused next frame: a backend
	// must copy or retain-until-composited rather than hold the pointer.
	//
	// damage is the physical-pixel area that changed since the previous
	// presentCPU; empty means "all of it". A surface that keeps the last
	// frame (a browser canvas, an X pixmap) may composite only that area.
	presentCPU(img *image.RGBA, damage image.Rectangle) bool

	// present displays whatever the GPU path drew into this surface's
	// backbuffer (the traditional buffer swap).
	present()
}

// platformHandler receives everything a platform window produces. The
// engine's *Window implements it (see window_handler.go); backends only
// ever see this interface, never Window's internals.
//
// All coordinates are logical, relative to the window's content area,
// top-left origin.
type platformHandler interface {
	onMouseMove(x, y float64, mods Modifiers)
	onMouseButton(btn MouseButton, down bool, mods Modifiers)
	// onScroll carries modifiers and phase, which most raw platform
	// callbacks omit — obtaining them is the backend's problem, not the
	// engine's. phase is GesturePhaseNone where unavailable.
	onScroll(x, y, dx, dy float64, mods Modifiers, phase GesturePhase, precise bool)
	// onGesture delivers multi-touch. dScale is a magnification fraction
	// (increment, not cumulative), dRotation is degrees.
	onGesture(kind EventType, phase GesturePhase, dScale, dRotation float32, mods Modifiers)
	onKey(k Key, scancode int, down, repeat bool, mods Modifiers)
	onChar(r rune)
	onFocus(focused bool)
	// onResize reports a new logical size; onFramebufferResize a new
	// physical one. They fire independently: moving a window between
	// displays of different DPI changes the framebuffer without changing
	// the logical size.
	onResize(w, h int)
	onFramebufferResize(w, h int)
	onRefresh()
	onFileDrop(paths []string, x, y float64)
}

// errNoPlatformWindow is returned when a backend reports success without
// a window.
var errNoPlatformWindow = errors.New("qui: platform backend returned no window")
