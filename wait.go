package qui

import (
	"errors"
	"strings"
	"time"
)

// Wait primitives — IdleState + WaitIdle + WaitOverlay.
//
// Agents commonly need to synchronize against the UI: after a click,
// wait for the resulting dialog to appear; after typing, wait for
// validation to settle; before screenshotting, wait for animations
// to stop. These primitives expose the necessary signals.

// IdleState summarizes everything that would cause Window.Step to
// repaint on its next tick. An idle window has zero of every count
// and an empty DirtyRegion: the framebuffer is stable until the
// next external event.
//
// Used by WaitIdle to detect quiescence.
type IdleState struct {
	DirtyRegion     Rect `json:"dirtyRegion"`
	LayoutDirty     bool `json:"layoutDirty"`
	ActiveAnimators int  `json:"activeAnimators"`
	// QuietAnimators are registered animators that have not painted a
	// frame for a while (see idle.go). They are excluded from
	// ActiveAnimators and do not block IsIdle.
	QuietAnimators  int `json:"quietAnimators"`
	ActiveTickables int `json:"activeTickables"`
	PendingJobs     int `json:"pendingJobs"`
	OverlayCount    int `json:"overlayCount"`
}

// IsIdle reports whether all repaint-driving signals are zero.
//
// ActiveTickables is reported in the struct (so agents can see how
// many text-cursor-blink-style sources exist) but is NOT included
// in the idle check — a Input's blink is always "active" while
// focused, yet doesn't represent meaningful work the agent should
// wait for. WaitIdle therefore looks at dirty region, pending
// layout, registered animators (one-shot Tween/Spring/Timeline),
// and queued jobs.
func (s IdleState) IsIdle() bool {
	return s.DirtyRegion.IsEmpty() &&
		!s.LayoutDirty &&
		s.ActiveAnimators == 0 &&
		s.PendingJobs == 0
}

// IdleState captures the current quiescence snapshot. Counting
// Tickables walks both the main tree and overlays — every tickable
// is one potential source of dirty pixels next frame. Animators
// outside the tree (Tween / Spring / Timeline registered via
// RegisterAnimator) are counted directly.
func (w *Window) IdleState() IdleState {
	if w == nil {
		return IdleState{}
	}
	w.assertUIThread("Window.IdleState")
	tickables := 0
	if w.root != nil {
		tickables += countTickables(w.root)
	}
	for _, ov := range w.overlays {
		tickables += countTickables(ov)
	}
	layoutDirty := false
	if w.root != nil {
		layoutDirty = w.root.IsLayoutDirty()
	}
	active, quiet := len(w.animators), 0
	if w.animatorsQuiet(time.Now()) {
		active, quiet = 0, active
	}
	return IdleState{
		DirtyRegion:     w.dirtyRegion,
		LayoutDirty:     layoutDirty,
		ActiveAnimators: active,
		QuietAnimators:  quiet,
		ActiveTickables: tickables,
		PendingJobs:     w.PendingJobs(),
		OverlayCount:    len(w.overlays),
	}
}

// IdleStateSynced captures IdleState on the window's main goroutine. Use
// this from HTTP handlers, background workers, and other off-main callers;
// IdleState itself remains the zero-overhead variant for frame callbacks and
// tests that already run on the UI goroutine.
func (w *Window) IdleStateSynced() IdleState {
	if w == nil {
		return IdleState{}
	}
	var state IdleState
	_ = w.synchronously(func() error {
		state = w.IdleState()
		return nil
	})
	return state
}

// countTickables walks widget and its children counting any that
// implement Tickable. Cheap: each Tickable widget is one counter
// increment, no allocation.
func countTickables(widget Widget) int {
	if widget == nil {
		return 0
	}
	n := 0
	if _, ok := widget.(Tickable); ok {
		n++
	}
	if cl, ok := widget.(childLister); ok {
		for _, c := range cl.ChildList() {
			n += countTickables(c)
		}
	}
	return n
}

// ErrWaitTimeout is returned when WaitIdle / WaitOverlay times out.
var ErrWaitTimeout = errors.New("qui: wait timed out")

// WaitIdle blocks until IdleState returns IsIdle()==true for two
// consecutive observations (defends against single-frame Tickable
// quiescence — cursor blink alternates "needs paint" / "stable"). On
// a test window the caller is responsible for calling Step (or
// equivalent) between polls; on a production window the main loop
// drives state and this function polls.
//
// Returns ErrWaitTimeout if the deadline passes before idle is
// observed.
func (w *Window) WaitIdle(timeout time.Duration) error {
	if w == nil {
		return errors.New("qui: nil window")
	}
	return w.waitUntil(timeout, func() bool {
		return w.IdleStateSynced().IsIdle()
	}, twoFrameConfirm)
}

// WaitOverlay blocks until an overlay matching layerPattern appears
// (when appear == true) or disappears (when appear == false). The
// pattern is matched as a substring against the overlay's Layer
// string (e.g. "modal" matches any "modal[N]", "overlay[0]" matches
// exactly that index).
//
// Pass layerPattern = "" to wait for ANY overlay change in the
// requested direction.
func (w *Window) WaitOverlay(layerPattern string, appear bool, timeout time.Duration) error {
	if w == nil {
		return errors.New("qui: nil window")
	}
	return w.waitUntil(timeout, func() bool {
		matches := 0
		tree := w.AccessibilityTreeSynced()
		for _, ov := range tree.Overlays {
			if layerPattern == "" || strings.Contains(ov.Layer, layerPattern) {
				matches++
			}
		}
		if appear {
			return matches > 0
		}
		return matches == 0
	}, oneFrameConfirm)
}

// WaitFor blocks until the widget matching selector appears (when
// appear == true) or disappears (when appear == false), confirmed
// across two consecutive observations.
//
// "appear" is satisfied when Find(selector) returns a non-nil widget
// whose Bounds are non-empty (i.e. it is laid out and visible).
// "!appear" is satisfied when Find(selector) returns nil. A malformed
// selector is reported immediately (wrapped ErrInvalidSelector).
//
// Returns ErrWaitTimeout if the deadline passes before the condition
// holds.
func (w *Window) WaitFor(selector string, appear bool, timeout time.Duration) error {
	if w == nil {
		return errors.New("qui: nil window")
	}
	// Surface selector-parse errors up front rather than silently
	// never matching.
	compiled, err := parseSelector(selector)
	if err != nil {
		return err
	}
	return w.waitUntil(timeout, func() bool {
		nodes := compiled.matchNodes(w.AccessibilityTreeSynced())
		if appear {
			for _, node := range nodes {
				if node.Visible {
					return true
				}
			}
			return false
		}
		return len(nodes) == 0
	}, twoFrameConfirm)
}

// WaitTreeStable blocks until the accessibility-tree content hash is
// unchanged across two consecutive ~16ms-spaced samples, or timeout
// elapses. Useful for synchronizing against structural settling
// (rows finishing loading, layout converging) without modeling a
// specific selector.
//
// Returns ErrWaitTimeout if the tree never stabilizes within the
// deadline.
func (w *Window) WaitTreeStable(timeout time.Duration) error {
	if w == nil {
		return errors.New("qui: nil window")
	}
	deadline := time.Now().Add(timeout)
	prev := w.AccessibilityTreeSynced().Hash()
	for time.Now().Before(deadline) {
		time.Sleep(16 * time.Millisecond)
		cur := w.AccessibilityTreeSynced().Hash()
		if cur == prev {
			return nil
		}
		prev = cur
	}
	return ErrWaitTimeout
}

// waitUntil polls cond at ~60Hz until it returns true and stays
// true for confirmFrames consecutive observations, or timeout
// elapses. Used by WaitIdle (confirm=2) and WaitOverlay (confirm=1).
func (w *Window) waitUntil(timeout time.Duration, cond func() bool, confirmFrames int) error {
	deadline := time.Now().Add(timeout)
	confirmed := 0
	for time.Now().Before(deadline) {
		if cond() {
			confirmed++
			if confirmed >= confirmFrames {
				return nil
			}
		} else {
			confirmed = 0
		}
		time.Sleep(16 * time.Millisecond)
	}
	if cond() {
		return nil
	}
	return ErrWaitTimeout
}

const (
	oneFrameConfirm = 1
	twoFrameConfirm = 2
)
