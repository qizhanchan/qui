package qui

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// The live-window registry: every platform-backed window from creation to
// Destroy. The agent protocol reads it to list targets for the whole
// process, so a multi-window app is inspectable without binding each window
// by hand, and listens to it to announce windows opening and closing.

var (
	liveWindowsMu   sync.Mutex
	liveWindows     []*Window
	windowListeners []*windowListener
	targetSeq       atomic.Uint64
)

type windowListener struct{ fn func(w *Window, alive bool) }

// LiveWindows returns the platform-backed windows currently open, in
// creation order. Safe from any goroutine.
//
// Page windows (PageHost, RFC 0005) are not listed: they are part of the
// PageView that shows them, not windows of the app. allLiveWindows has
// them too.
func LiveWindows() []*Window {
	liveWindowsMu.Lock()
	defer liveWindowsMu.Unlock()
	out := make([]*Window, 0, len(liveWindows))
	for _, w := range liveWindows {
		if !w.isPageWindow {
			out = append(out, w)
		}
	}
	return out
}

// PageWindows returns the live page windows (PageHost): the agent lists
// them as child targets of the app's windows, like Chromium's
// out-of-process frames. Safe from any goroutine.
func PageWindows() []*Window {
	liveWindowsMu.Lock()
	defer liveWindowsMu.Unlock()
	var out []*Window
	for _, w := range liveWindows {
		if w.isPageWindow {
			out = append(out, w)
		}
	}
	return out
}

// IsPageWindow reports whether w is a PageHost's window.
func (w *Window) IsPageWindow() bool { return w != nil && w.isPageWindow }

// allLiveWindows is LiveWindows plus page windows: for broadcasts every
// window must see (font changes).
func allLiveWindows() []*Window {
	liveWindowsMu.Lock()
	defer liveWindowsMu.Unlock()
	return append([]*Window(nil), liveWindows...)
}

// AddWindowLifecycleListener calls fn(w, true) when a platform-backed window
// is created and fn(w, false) when it is destroyed. fn runs on the UI
// goroutine and must not block. The returned func removes the listener.
func AddWindowLifecycleListener(fn func(w *Window, alive bool)) (remove func()) {
	l := &windowListener{fn: fn}
	liveWindowsMu.Lock()
	windowListeners = append(windowListeners, l)
	liveWindowsMu.Unlock()
	return func() {
		liveWindowsMu.Lock()
		defer liveWindowsMu.Unlock()
		for i, x := range windowListeners {
			if x == l {
				windowListeners = append(windowListeners[:i:i], windowListeners[i+1:]...)
				return
			}
		}
	}
}

func registerLiveWindow(w *Window) {
	liveWindowsMu.Lock()
	liveWindows = append(liveWindows, w)
	ls := append([]*windowListener(nil), windowListeners...)
	liveWindowsMu.Unlock()
	if w.isPageWindow {
		return // not an app window (LiveWindows)
	}
	for _, l := range ls {
		l.fn(w, true)
	}
}

func unregisterLiveWindow(w *Window) {
	liveWindowsMu.Lock()
	found := false
	for i, x := range liveWindows {
		if x == w {
			liveWindows = append(liveWindows[:i:i], liveWindows[i+1:]...)
			found = true
			break
		}
	}
	ls := append([]*windowListener(nil), windowListeners...)
	liveWindowsMu.Unlock()
	if !found || w.isPageWindow {
		return
	}
	for _, l := range ls {
		l.fn(w, false)
	}
}

// TargetID is the window's protocol target ID ("window-3"): allocated on
// first use, unique for the process, never reused. Safe from any goroutine.
func (w *Window) TargetID() string {
	if w == nil {
		return ""
	}
	n := atomic.LoadUint64(&w.targetSeq)
	if n == 0 {
		atomic.CompareAndSwapUint64(&w.targetSeq, 0, targetSeq.Add(1))
		n = atomic.LoadUint64(&w.targetSeq)
	}
	return "window-" + strconv.FormatUint(n, 10)
}

// Title returns the window's title as last set by NewWindow or SetTitle.
// Safe from any goroutine.
func (w *Window) Title() string {
	if w == nil {
		return ""
	}
	w.titleMu.Lock()
	defer w.titleMu.Unlock()
	return w.title
}
