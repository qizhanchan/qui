package contract

import (
	"image"
	"time"
	"unsafe"
)

// The platform seam.
//
// Everything the engine needs from an OS windowing layer — create a
// window, pump events, deliver input, present a frame — goes through the
// interfaces in this file. Each backend is its own package under
// internal/platform (cocoa, x11, web, headless, glfw). Nothing in the
// engine may reference a windowing library directly.
//
// # Designing against three platforms while implementing one
//
// The interfaces below are deliberately shaped for the UNION of macOS,
// Windows and Wayland semantics, not for whichever one is implemented
// today. An interface written against a single platform silently grows
// that platform's assumptions and only reveals them years later, when
// there are dozens of call sites. The two constraints that visibly bend
// this API:
//
//   - Wayland clients cannot know or set their absolute position, so pos
//     and setPos are FALLIBLE. Callers must degrade, not assume.
//   - DPI scale is not an integer on Windows (arbitrary percentages) or
//     Wayland (fractional scaling), so contentScale returns float32 —
//     never round it to a device-pixel-ratio of 1, 2 or 3.
//
//   - Presentation is asynchronous, not a synchronous "swap now". Wayland
//     requires waiting for a wl_surface.frame callback before drawing the
//     next frame; macOS wants CADisplayLink; Windows has a waitable
//     swapchain. Surface.Ready expresses that, and a backend
//     without such a signal returns nil to keep the timeout-driven
//     behavior.
//
// # Coordinates and units
//
// Sizes and positions crossing this boundary are LOGICAL (points), except
// framebufferSize which is PHYSICAL (device pixels). Everything above the
// seam works in logical units; the ratio between the two is how HiDPI
// reaches the renderer.

// App is the process-level surface: initialization, the event
// pump, display enumeration, teardown. One instance per process.
type App interface {
	// newWindow creates a window. The returned Window is non-nil
	// only on success — engine code distinguishes "no platform window"
	// (test windows) by a nil interface value.
	NewWindow(cfg WindowConfig) (Window, error)

	// pumpEvents blocks until input arrives or timeout elapses, then
	// dispatches everything queued to the relevant handlers. A zero
	// timeout means "poll, don't block".
	PumpEvents(timeout time.Duration)

	// wake unblocks a concurrent pumpEvents from another goroutine. This
	// is how PostJob gets the main loop to notice new work without
	// waiting out the timeout.
	Wake()

	Monitors() []Monitor
	PrimaryMonitor() Monitor
}

// WindowConfig describes a window to create. Kept as a struct so
// adding a field (decorations, transparency, always-on-top) doesn't churn
// every backend's signature.
type WindowConfig struct {
	Title  string
	Width  int
	Height int
	// Share, when non-nil, requests that the new window's GPU context
	// share resources (textures, shaders, buffers) with that window's.
	Share Window
	// Kind is the window's OS-level role. A backend that cannot honor the
	// requested kind must return an error, NOT a window of a different
	// kind: WindowOverlayPanel's whole point is not taking focus, and
	// quietly substituting a normal window turns a missing feature into
	// the user's keystrokes going somewhere they didn't intend. See
	// window_overlay.go.
	Kind WindowKind
}

// Caps reports abilities that genuinely differ between
// platforms, so callers can branch or degrade BEFORE attempting an
// operation instead of interpreting a failure after the fact.
//
// This is the alternative to the ErrNotSupported-stub pattern: a UI can
// hide an affordance it can't offer rather than presenting one that
// errors when used.
type Caps struct {
	// AbsolutePosition is false on Wayland, where a client has no access
	// to global coordinates. When false, pos/setPos always fail and any
	// feature built on absolute geometry (restoring a saved window rect,
	// positioning a window on a chosen monitor) must fall back to letting
	// the compositor decide.
	AbsolutePosition bool
	// ServerDecorations is false where the platform will not draw a title
	// bar (Wayland compositors without xdg-decoration, notably GNOME).
	// The app must then draw its own chrome.
	ServerDecorations bool
	// NativeGestures reports a real multi-touch source. When false, pinch
	// still reaches widgets through the Ctrl/Cmd+wheel synthesis in
	// gesture.go, but scroll phase and two-finger rotation are absent.
	NativeGestures bool
	// ClipboardNeedsInputSerial is true on Wayland, where setting the
	// selection requires a serial from a recent input event — a clipboard
	// write outside an input handler simply will not take effect.
	ClipboardNeedsInputSerial bool
	// OverlayPanels reports whether this backend can create a
	// non-activating, transparent, always-on-top window
	// (WindowOverlayPanel). False on GLFW, which can turn off decorations
	// and float a window but cannot stop it taking keyboard focus — the
	// one property that matters most.
	OverlayPanels bool
	// GPUContext reports that the window carries an OpenGL context that
	// makeCurrent binds and present swaps. False for compositor-only
	// surfaces (browser canvas, X11 without GLX, headless): those present
	// CPU frames through presentCPU, and a GL renderer handed to such a
	// window is swapped for a CPURenderer (renderer_caps.go).
	GPUContext bool
}

// Monitor is one connected display. Methods return logical
// (point) units so window placement needs no DPI conversion.
type Monitor interface {
	Name() string
	Position() (x, y int)
	// videoMode reports the current mode. refreshHz is 0 when unknown.
	VideoMode() (w, h, refreshHz int)
	// workArea excludes OS chrome (menu bar, dock, taskbar).
	WorkArea() (x, y, w, h int)
	ContentScale() (x, y float32)
}

// Window is one OS window plus its input and presentation.
type Window interface {
	// setHandler installs the receiver for this window's input and
	// lifecycle events. One handler replaces the traditional pile of
	// per-event callback setters so a backend can't partially wire the
	// engine up: adding an event to the contract breaks every backend at
	// compile time, which is the point.
	SetHandler(h Handler)

	Caps() Caps

	// size is the logical (point) size of the content area.
	Size() (w, h int)
	// framebufferSize is the PHYSICAL pixel size of the content area.
	FramebufferSize() (w, h int)
	// contentScale is the display's DPI scale. Not necessarily an
	// integer — see the package doc above.
	ContentScale() (x, y float32)

	// pos reports the window's top-left in global logical coordinates.
	// ok is false where the platform has no notion of global position
	// (Wayland) — see Caps.AbsolutePosition.
	Pos() (x, y int, ok bool)
	// setPos moves the window, reporting whether the platform allowed it.
	SetPos(x, y int) bool

	SetTitle(title string)
	// setSize resizes the CONTENT area to w x h logical points, keeping the
	// window's TOP-left corner where it is. Top-left rather than the
	// platform-native anchor because that is the origin every coordinate
	// crossing this seam uses: a caller that positioned a panel under a
	// text caret expects it to grow downward, not to push its own top edge
	// up off the anchor.
	SetSize(w, h int)
	// setSizeLimits constrains interactive resizing. A bound <= 0 means
	// "unconstrained on that axis".
	SetSizeLimits(minW, minH, maxW, maxH int)
	Iconify()
	Maximize()
	Restore()
	Focus()

	// setVisible orders the window on or off screen without destroying it.
	//
	// activate is a REQUEST to also take keyboard focus while showing.
	// Passing false is what makes an overlay panel usable: on macOS it is
	// orderFront: rather than makeKeyAndOrderFront:. A backend whose
	// windows always activate on show must still accept false — but then
	// it must also have declined to create the panel in the first place
	// (Caps.OverlayPanels), so the case cannot arise for panels.
	SetVisible(visible, activate bool)
	// isVisible reports whether the OS currently has this window on screen.
	IsVisible() bool

	// setFullscreen puts the window on monitor m at the given geometry, or
	// returns it to windowed mode at that geometry when m is nil.
	// refreshHz <= 0 means "whatever the display prefers".
	SetFullscreen(m Monitor, x, y, w, h, refreshHz int)

	ShouldClose() bool
	SetShouldClose(v bool)
	Destroy()

	CursorPos() (x, y float64)
	SetCursor(shape CursorShape)
	// currentMods polls the live modifier state. Needed because most
	// platforms report modifiers with key and button events but not with
	// motion or wheel events, and "is Ctrl held right now" decides
	// scroll-versus-zoom.
	CurrentMods() Modifiers

	ClipboardText() string
	SetClipboardText(text string)

	// makeCurrent binds this window's GPU context to the calling thread.
	// Multi-window apps have one context per window, so this runs before
	// any drawing. A backend with no GPU context makes this a no-op.
	MakeCurrent()

	// surface is this window's presentation target. Never nil.
	Surface() Surface

	// wake unblocks the event loop servicing this window, from any
	// goroutine. Per-window rather than per-app because in a mixed-backend
	// process (a native window plus a fallback one) the loops are not
	// necessarily the same, and waking the wrong one strands queued work.
	Wake()

	// nativeWindow is the platform object (NSWindow*, HWND, wl_surface*)
	// for the native bridges that must reach past the seam — IME, native
	// menus, native fullscreen. Returns nil when there is none.
	NativeWindow() unsafe.Pointer
}

// Surface is where a finished frame goes.
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
type Surface interface {
	// ready fires when the display is ready for another frame —
	// CADisplayLink on macOS, wl_surface.frame on Wayland, a waitable
	// swapchain on Windows.
	//
	// Returns nil when the platform offers no such signal, in which case
	// the caller falls back to pacing on its event-pump timeout. Callers
	// must not block on this channel indefinitely: a window whose display
	// link is stopped (occluded, off-screen) would otherwise never run
	// another frame, and jobs posted from other goroutines would stall.
	Ready() <-chan struct{}

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
	PresentCPU(img *image.RGBA, damage image.Rectangle) bool

	// present displays whatever the GPU path drew into this surface's
	// backbuffer (the traditional buffer swap).
	Present()
}

// Handler receives everything a platform window produces. The
// engine's *Window implements it (qui/window_handler.go); backends only
// ever see this interface, never Window's internals.
//
// All coordinates are logical, relative to the window's content area,
// top-left origin.
type Handler interface {
	OnMouseMove(x, y float64, mods Modifiers)
	OnMouseButton(btn MouseButton, down bool, mods Modifiers)
	// onScroll carries modifiers and phase, which most raw platform
	// callbacks omit — obtaining them is the backend's problem, not the
	// engine's. phase is GesturePhaseNone where unavailable.
	OnScroll(x, y, dx, dy float64, mods Modifiers, phase GesturePhase, precise bool)
	// onGesture delivers multi-touch. dScale is a magnification fraction
	// (increment, not cumulative), dRotation is degrees.
	OnGesture(kind GestureKind, phase GesturePhase, dScale, dRotation float32, mods Modifiers)
	OnKey(k Key, scancode int, down, repeat bool, mods Modifiers)
	OnChar(r rune)
	OnFocus(focused bool)
	// onResize reports a new logical size; onFramebufferResize a new
	// physical one. They fire independently: moving a window between
	// displays of different DPI changes the framebuffer without changing
	// the logical size.
	OnResize(w, h int)
	OnFramebufferResize(w, h int)
	OnRefresh()
	OnFileDrop(paths []string, x, y float64)
}

// GestureKind names a multi-touch gesture delivered through
// Handler.OnGesture.
type GestureKind int

const (
	// GesturePinch is a two-finger magnify; dScale carries the increment.
	GesturePinch GestureKind = iota
	// GestureRotate is a two-finger rotation; dRotation is degrees.
	GestureRotate
	// GestureSmartMagnify is the two-finger double-tap "smart zoom".
	GestureSmartMagnify
)
