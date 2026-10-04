package qui

import (
	"fmt"
	"os"
)

// UI-thread checks are diagnostics, not synchronization. Production windows
// are driven by the process main OS thread; retaining that native thread
// identity lets qui fail at the first unsupported access instead of surfacing
// later as a race, stale tree pointer, or renderer corruption.
var uiThreadChecksByDefault = os.Getenv("QUI_DEBUG_THREAD") == "1"
var processUIThreadID = currentUIThreadID()

type uiThreadOwnership struct {
	owner   uint64
	enabled bool
}

// UIThreadViolation is raised when code touches UI-owned state from a
// OS thread other than the one bound to the Window. PostJob, TryPostJob,
// PostPriorityJob, PendingJobs, SetJobQueueLimit, WakeEventLoop, and the
// *Synced introspection APIs are the supported cross-goroutine surface.
type UIThreadViolation struct {
	Operation string
	Owner     uint64
	Current   uint64
}

func (v UIThreadViolation) Error() string {
	op := v.Operation
	if op == "" {
		op = "UI state access"
	}
	return fmt.Sprintf(
		"qui: %s must run on the window UI thread (owner=%d current=%d); use Window.PostJob",
		op, v.Owner, v.Current,
	)
}

func (w *Window) initUIThreadOwnership() {
	if w == nil || !uiThreadChecksByDefault {
		return
	}
	w.uiThread.owner = currentUIThreadID()
	w.uiThread.enabled = true
}

// EnableUIThreadChecks binds w to the calling OS thread and enables fail-fast
// ownership diagnostics for that Window. Set QUI_DEBUG_THREAD=1 to enable the
// same behavior automatically for newly-created production windows. Test
// windows opt in explicitly because their synchronous agent helpers are
// intentionally allowed to execute inline without a frame pump.
//
// Call this while constructing the window, before handing it to background
// workers. Enabling an already-bound Window from another thread panics.
func (w *Window) EnableUIThreadChecks() {
	if w == nil {
		return
	}
	current := currentUIThreadID()
	if owner := w.uiThread.owner; owner != 0 && owner != current {
		panic(UIThreadViolation{Operation: "EnableUIThreadChecks", Owner: owner, Current: current})
	}
	w.uiThread.owner = current
	w.uiThread.enabled = true
}

// UIThreadChecksEnabled reports whether ownership diagnostics are active.
// It is safe to call from any goroutine after the Window has been published;
// EnableUIThreadChecks must run during construction before that publication.
func (w *Window) UIThreadChecksEnabled() bool {
	return w != nil && w.uiThread.enabled
}

// IsUIThread reports whether the caller is running on the OS thread bound to
// w. It is safe to call from any goroutine. When checks are disabled it returns
// true so callers do not accidentally turn diagnostics into production logic.
func (w *Window) IsUIThread() bool {
	if w == nil || !w.uiThread.enabled {
		return true
	}
	return w.uiThread.owner == currentUIThreadID()
}

// AssertUIThread verifies that the caller owns w. Custom widgets and
// application-level retained models can call this at the beginning of their
// own mutation methods to participate in the same diagnostic boundary.
func (w *Window) AssertUIThread(operation string) {
	if w == nil {
		return
	}
	w.assertUIThread(operation)
}

func (w *Window) assertUIThread(operation string) {
	if w == nil || !w.uiThread.enabled {
		return
	}
	owner := w.uiThread.owner
	current := currentUIThreadID()
	if owner == current {
		return
	}
	panic(UIThreadViolation{Operation: operation, Owner: owner, Current: current})
}

func assertProcessUIThread(operation string) {
	if !uiThreadChecksByDefault {
		return
	}
	current := currentUIThreadID()
	if current == processUIThreadID {
		return
	}
	panic(UIThreadViolation{Operation: operation, Owner: processUIThreadID, Current: current})
}

func (b *BaseWidget) assertUIThread(operation string) {
	if b != nil && b.window != nil {
		b.window.assertUIThread(operation)
	}
}
