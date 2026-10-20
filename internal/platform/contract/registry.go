package contract

import (
	"errors"
	"image"
	"io"
	"sort"
	"time"
	"unsafe"
)

// Factory creates a backend's process-level App.
type Factory func() (App, error)

// Backend priorities. The default backend is the registered one with the
// highest priority, so which backend a platform defaults to is decided by
// which packages the build compiled in, not by init order.
const (
	// PriorityFallback: always available, never the right default when
	// anything else is (headless).
	PriorityFallback = 0
	// PriorityPortable: a cross-platform library standing in where no
	// native backend exists yet (GLFW on Windows, or anywhere with
	// -tags qui_glfw).
	PriorityPortable = 10
	// PriorityNative: the platform's own windowing API (AppKit, X11, the
	// browser DOM).
	PriorityNative = 20
)

// Registered is one compiled-in backend.
type Registered struct {
	Name     string
	Priority int
	Factory  Factory
}

var backends = map[string]Registered{}

// Register adds a backend. Backends call it from init.
func Register(name string, priority int, factory Factory) {
	backends[name] = Registered{Name: name, Priority: priority, Factory: factory}
}

// Lookup returns the backend registered under name.
func Lookup(name string) (Registered, bool) {
	b, ok := backends[name]
	return b, ok
}

// Names lists the compiled-in backends, sorted.
func Names() []string {
	names := make([]string, 0, len(backends))
	for name := range backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Default names the highest-priority registered backend, ties broken by
// name so the answer never depends on map order.
func Default() string {
	best, bestPri := "", -1
	for _, name := range Names() {
		if p := backends[name].Priority; p > bestPri {
			best, bestPri = name, p
		}
	}
	return best
}

// PumpForever is the Pump timeout meaning "until an event or a wake".
const PumpForever = time.Duration(1<<63 - 1)

// Host is the handful of engine services a backend calls back into. The
// engine fills it in before creating any backend; every field is non-nil
// from then on.
var Host struct {
	// ActivationPolicy is the app's requested macOS activation policy
	// (0 regular, 1 accessory, 2 prohibited).
	ActivationPolicy func() int
	// QuitRequested runs the app's quit-request hook (Cmd+Q, Dock →
	// Quit) and reports whether quitting may proceed.
	QuitRequested func() bool
	// WakeIfBlocked cuts short a long blocking pump when an OS callback
	// produced work the main loop must notice.
	WakeIfBlocked func()
	// NativeCursor returns the platform cursor object (NSCursor* on macOS)
	// for an image cursor or CursorHidden, or nil for the built-in shapes.
	NativeCursor func(shape CursorShape) unsafe.Pointer
	// ImageCursor returns an image cursor's one-pixel-per-point rendition
	// and hotspot, for backends that build their own cursor objects.
	ImageCursor func(shape CursorShape) (img1x *image.NRGBA, hotX, hotY int, ok bool)
	// RawKeyCaptured is a C function pointer, int (*)(void* nsEvent), that
	// reports whether the engine's raw-keyboard monitor consumes an event
	// (macOS only; nil elsewhere). A C pointer rather than a Go func because
	// the event pump consults it for every event without leaving C.
	RawKeyCaptured unsafe.Pointer
	// SetTextFallback installs the backend's text engine for clusters no
	// loaded font covers (see TextFallback); nil removes it.
	SetTextFallback func(TextFallback)
	// SetMacKeyboard tells the engine whether the user's keyboard follows
	// macOS conventions (Command shortcuts), for backends where that is not
	// implied by the build's GOOS — the browser.
	SetMacKeyboard func(bool)
}

// Optional capabilities a Window may implement. The engine type-asserts
// for them; a backend without the ability simply doesn't implement it.
type (
	// Focuser reports whether the window currently has keyboard focus.
	Focuser interface{ IsFocused() bool }
	// Iconifier reports whether the window is minimized.
	Iconifier interface{ IsIconified() bool }
	// CapsLocker reports the live Caps Lock state.
	CapsLocker interface{ CapsLock() bool }
	// DropTarget turns OS file drops onto the window on or off.
	DropTarget interface{ SetDropEnabled(enabled bool) }
	// DropOpener opens a path delivered through Handler.OnFileDrop when
	// it is not a real file — the browser hands over file contents, not
	// paths, so the web backend keeps them in memory under virtual paths.
	DropOpener interface {
		OpenDropped(path string) (io.ReadCloser, bool)
	}
	// CursorReleaser frees per-window native cursors built for an image
	// cursor that is being released.
	CursorReleaser interface{ ReleaseCursor(shape CursorShape) }
)

// ErrOverlayPanelUnsupported is what a backend returns from NewWindow for
// a WindowOverlayPanel it cannot make non-activating. Re-exported as
// qui.ErrOverlayPanelUnsupported.
var ErrOverlayPanelUnsupported = errors.New(
	"qui: the active platform backend cannot create overlay panels " +
		"(non-activating transparent windows); try QUI_PLATFORM=cocoa")
