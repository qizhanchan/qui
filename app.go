package qui

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	platformOnce sync.Once
	platformMu   sync.RWMutex
	platformImpl platformApp
	platformErr  error

	appMu      sync.Mutex
	processApp *App
)

// activePlatform returns the process-wide platform backend, initializing
// it on first use. Window creation goes through this rather than a global
// set by NewApp so a window can be created in a test or a tool that never
// builds an App.
func activePlatform() (platformApp, error) {
	platformOnce.Do(func() {
		impl, err := newPlatformApp()
		platformMu.Lock()
		platformImpl, platformErr = impl, err
		platformMu.Unlock()
	})
	if platformErr != nil {
		return nil, platformErr
	}
	if platformImpl == nil {
		return nil, errors.New("qui: no platform backend")
	}
	return platformImpl, nil
}

// initializedPlatform returns the platform backend only if it has already
// been created, without initializing one. Callers that may run off the main
// goroutine must use this: windowing libraries require initialization on
// the main thread, so a lazy init from a worker would crash rather than
// fail.
func initializedPlatform() platformApp {
	platformMu.RLock()
	defer platformMu.RUnlock()
	return platformImpl
}

// shutdownDebug, set by QUI_SHUTDOWN_DEBUG=1, turns on timing logs around
// the main loop's event-pump and the window close/teardown path. It exists
// to diagnose "clicking close takes seconds to exit after the machine has
// slept" — it isolates whether the delay is the Cocoa run loop waking
// slowly (event-pump gap) or window/GL teardown blocking. Off by default,
// zero cost when disabled.
var shutdownDebug = os.Getenv("QUI_SHUTDOWN_DEBUG") != ""

func init() {
	// GLFW/Cocoa requires all event handling on the main OS thread.
	runtime.LockOSThread()
}

// App manages the lifecycle of GUI windows.
type App struct {
	windows     []*Window
	statusItems []*StatusItem
	running     bool

	// closing flips true from a signal goroutine on SIGINT/SIGTERM
	// (see Run). RunStep observes it on the main thread and routes
	// shutdown through the same path as a user-clicked window close
	// — keeps OnClose callbacks (agent socket unlink, dirty buffer
	// flush, etc.) running on the GLFW thread where they belong.
	closing atomic.Bool
}

// ErrAppAlreadyExists is returned when NewApp is called more than once in
// a process. qui has one main-thread event loop and one process-wide platform
// backend; all application windows belong to that single App.
var ErrAppAlreadyExists = errors.New("qui: only one App may exist per process")

// NewApp initializes the process-wide platform backend and claims the single
// App slot. The platform remains initialized for the life of the process;
// App.Run tears down app-owned windows and status items, not the backend.
func NewApp() (*App, error) {
	assertProcessUIThread("NewApp")
	appMu.Lock()
	defer appMu.Unlock()
	if processApp != nil {
		return nil, ErrAppAlreadyExists
	}
	if _, err := activePlatform(); err != nil {
		return nil, err
	}
	a := &App{}
	processApp = a
	return a, nil
}

// NewWindow creates a window and registers it with the app.
func (a *App) NewWindow(title string, width, height int) (*Window, error) {
	if a == nil {
		return nil, errors.New("app is nil")
	}
	w, err := NewWindow(title, width, height)
	if err != nil {
		return nil, err
	}
	a.windows = append(a.windows, w)
	return w, nil
}

// NewSharedWindow creates a window that shares its OpenGL context
// with an existing window so they can reuse GPU resources (textures,
// VBOs, shaders). Passing share=nil is equivalent to NewWindow.
func (a *App) NewSharedWindow(title string, width, height int, share *Window) (*Window, error) {
	if a == nil {
		return nil, errors.New("app is nil")
	}
	w, err := NewSharedWindow(title, width, height, share)
	if err != nil {
		return nil, err
	}
	a.windows = append(a.windows, w)
	return w, nil
}

// CloseWindow destroys a window and removes it from the app's tracked
// list. The main loop exits when the last window is gone.
func (a *App) CloseWindow(w *Window) {
	if a == nil || w == nil {
		return
	}
	for i, existing := range a.windows {
		if existing == w {
			a.windows = append(a.windows[:i], a.windows[i+1:]...)
			break
		}
	}
	w.Destroy()
}

// MainWindow returns the first window created by the app (nil if none).
// Creative-tool apps use this as a stable reference point for docking
// secondary inspector windows.
func (a *App) MainWindow() *Window {
	if a == nil || len(a.windows) == 0 {
		return nil
	}
	return a.windows[0]
}

// Run starts the main loop and exits when all windows are closed.
// SIGINT/SIGTERM are trapped and route shutdown through the normal
// window-close path (OnClose callbacks + Destroy) so external kills
// — `kill <pid>`, Ctrl-C from a terminal, a parent supervisor — leave
// the system as clean as a user-driven close. signal.Notify is
// additive, so apps that install their own handler beforehand still
// see the signals.
func (a *App) Run() {
	if a == nil || a.running {
		return
	}
	assertProcessUIThread("App.Run")
	a.running = true
	defer a.destroyStatusItems()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sigDone := make(chan struct{})
	defer func() {
		signal.Stop(sigCh)
		close(sigDone)
	}()
	go func() {
		select {
		case <-sigDone:
			return
		case <-sigCh:
			a.closing.Store(true)
			WakeEventLoop()
		}
	}()

	const frameTimeout = 1.0 / 60.0
	for a.running {
		if !a.RunStep(frameTimeout) {
			break
		}
	}
}

func (a *App) destroyStatusItems() {
	if a == nil {
		return
	}
	// Destroy unregisters each item from a.statusItems, so iterate a snapshot.
	for _, si := range append([]*StatusItem(nil), a.statusItems...) {
		si.Destroy()
	}
}

// frameDue reports whether a window should produce a frame this iteration.
//
// Backends that expose a frame-ready signal (CADisplayLink, a Wayland
// frame callback, a waitable swapchain) pace painting to the display
// instead of to the loop's timeout: skipping Step when the display hasn't
// asked for a frame avoids rendering work that would only be discarded.
// Input is unaffected — events dispatch during pumpEvents, not during
// Step, so responsiveness doesn't depend on the paint cadence.
//
// The PendingJobs escape hatch matters: a display link stops while its
// window is occluded or off-screen, and without this an agent action or an
// async loader that posted work would wait for a frame that never comes.
func frameDue(w *Window) bool {
	if w == nil || w.plat == nil {
		return true
	}
	surface := w.plat.surface()
	if surface == nil {
		return true
	}
	ready := surface.ready()
	if ready == nil {
		// No signal from this platform: the loop's own timeout is the clock.
		return true
	}
	select {
	case <-ready:
		return true
	default:
		return w.PendingJobs() > 0
	}
}

// RunStep processes one main-loop iteration and returns whether at
// least one window is still alive. waitTimeoutSeconds is in seconds;
// 0 means "poll, don't block".
func (a *App) RunStep(waitTimeoutSeconds float64) bool {
	if a == nil {
		return false
	}
	assertProcessUIThread("App.RunStep")
	plat, err := activePlatform()
	if err != nil {
		return false
	}
	timeout := time.Duration(waitTimeoutSeconds * float64(time.Second))
	if shutdownDebug {
		t0 := time.Now()
		plat.pumpEvents(timeout)
		// A WaitEventsTimeout(1/60) call should return within ~16ms
		// (timeout) or sooner (an event woke it). Anything much larger
		// means the Cocoa run loop was throttled — the smoking gun for a
		// post-sleep stall where the close event isn't observed promptly.
		if blocked := time.Since(t0); blocked > 200*time.Millisecond {
			log.Printf("qui/shutdown: event pump blocked %v (timeout was %.0fms)", blocked, waitTimeoutSeconds*1000)
		}
	} else {
		plat.pumpEvents(timeout)
	}
	// Menu actions fired by the OS on the main thread during event
	// pumping — drain here, on the Go main goroutine, so user
	// callbacks can safely mutate the widget tree.
	drainActions()
	if a.closing.CompareAndSwap(true, false) {
		// Tear down system tray icons first so SIGINT / Ctrl-C / a
		// supervisor `kill` doesn't leave orphan icons in the menu
		// bar. Destroy unregisters each item from a.statusItems, so
		// snapshot the slice before iterating.
		a.destroyStatusItems()
		for _, w := range a.windows {
			if w == nil || w.plat == nil {
				continue
			}
			w.plat.setShouldClose(true)
		}
	}
	alive := 0
	for _, w := range a.windows {
		if w == nil {
			continue
		}
		if w.ShouldClose() {
			// runCloseHandlers fires user-registered OnClose callbacks
			// while the GLFW window is still alive and the main loop
			// can still pump platform message queues (CEF, etc.).
			// Destroy() also calls it defensively but the slice is
			// cleared first, so this is a no-op the second time.
			if shutdownDebug {
				t0 := time.Now()
				w.runCloseHandlers()
				t1 := time.Now()
				w.Destroy()
				log.Printf("qui/shutdown: runCloseHandlers %v, Destroy %v", t1.Sub(t0), time.Since(t1))
				continue
			}
			w.runCloseHandlers()
			w.Destroy()
			continue
		}
		alive++
		if !frameDue(w) {
			continue
		}
		w.Step()
	}
	// Remove destroyed windows after callbacks have finished. A callback may
	// append a fresh window while this loop is running, so filter the current
	// slice rather than compacting the range in place.
	a.pruneDestroyedWindows()
	alive = len(a.windows)
	a.running = alive > 0
	return a.running
}

func (a *App) pruneDestroyedWindows() {
	if a == nil {
		return
	}
	kept := a.windows[:0]
	for _, w := range a.windows {
		if w != nil && !w.IsHeadless() {
			kept = append(kept, w)
		}
	}
	a.windows = kept
}
