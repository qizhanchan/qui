package qui

import "time"

// Focus navigation — Tab / Shift+Tab to cycle through focusable widgets,
// Esc to clear. Focus order follows DFS traversal of the widget tree
// (document order), which is the least-surprising default. Custom tab
// orders can come later via a per-widget TabIndex hint.

// childLister mirrors Container.ChildList without importing dispatch
// concerns. Kept private: only framework code walks the tree.
type childLister interface {
	ChildList() []Widget
}

// focusVisibleAware is optional — widgets implement it to be told
// whether the current focus is keyboard-induced (true) or mouse-induced
// (false). Mirrors the CSS :focus-visible distinction so widgets can
// suppress the focus state-layer / ring after a click while keeping
// real keyboard focus indicated. Window calls SetFocusVisible right
// after SetFocused(true) when focus changes.
type focusVisibleAware interface {
	SetFocusVisible(bool)
}

// ClickFocusPolicy is optional for focusable widgets. One whose
// FocusOnClick returns false is reached by Tab but a click on it — or on
// anything inside it, such as its caption — leaves focus where it was — the macOS push-button convention, which keeps a
// text field or editor focused while a toolbar button acts on it.
type ClickFocusPolicy interface {
	FocusOnClick() bool
}

// TabStopper is optional for focusable widgets. One whose TabStop returns
// false still takes focus from a click — selectable text does, so Cmd+C
// copies its selection — but Tab / Shift+Tab skip it: text is not a
// keyboard stop, as in a browser.
type TabStopper interface {
	TabStop() bool
}

func isTabStop(w Widget) bool {
	if t, ok := w.(TabStopper); ok {
		return t.TabStop()
	}
	return true
}

// modalOverlay is implemented by overlays that should trap focus —
// while one is on the stack, Tab cycles only within that overlay,
// skipping the main tree and any non-modal overlays below. Used for
// Dialog; Tooltip / Popup should not be modal.
type modalOverlay interface {
	Modal() bool
}

type overlayFocusScope struct {
	restore      Widget
	focusVisible bool
	claimed      bool
}

// TabConsumer lets the focused widget — or any of its ancestors — claim
// the Tab key for its own use instead of letting the window advance
// focus. The window walks the focused widget's ancestor chain on every
// Tab press; if any node returns true, the Tab event dispatches normally
// (reaching the widget's Handle) rather than moving focus. Used by the
// spreadsheet cell editor so Tab can accept an autocomplete suggestion
// or commit-and-move-right instead of escaping the editor.
type TabConsumer interface {
	ConsumesTab(shift bool) bool
}

// tabConsumedByFocus reports whether the focused widget or an ancestor
// wants to handle Tab itself.
func (w *Window) tabConsumedByFocus(shift bool) bool {
	for cur := w.Focused(); cur != nil; cur = cur.Parent() {
		if tc, ok := cur.(TabConsumer); ok && tc.ConsumesTab(shift) {
			return true
		}
	}
	return false
}

// CollectFocusables walks the widget tree depth-first and returns every
// widget that implements Focusable() bool with a true return, is
// enabled, is a Tab stop (TabStopper), and has a parent chain ending at
// root. Also walks overlays
// (so focusables inside a Dialog / Popup are Tab-reachable). Preserves
// tree order so Tab navigates predictably.
//
// If any overlay on the stack declares Modal() == true, focus collection
// restricts itself to the topmost such overlay — matching the mouse
// behavior where modal Dialogs absorb clicks outside their bounds.
func (w *Window) CollectFocusables() []Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.CollectFocusables")
	var result []Widget
	var walk func(Widget)
	walk = func(widget Widget) {
		if widget == nil || isWidgetHidden(widget) {
			return
		}
		if f, ok := widget.(registerFocusable); ok && f.Focusable() && widget.Enabled() && isTabStop(widget) {
			result = append(result, widget)
		}
		if h, ok := widget.(childLister); ok {
			for _, child := range h.ChildList() {
				walk(child)
			}
		}
	}
	// Walk top-down looking for a modal. If found, walk only that
	// overlay and return — focus is trapped within it.
	if m := w.topModalIndex(); m >= 0 {
		walk(w.overlays[m])
		return result
	}
	// No modal: main tree then all overlays in stack order.
	walk(w.root)
	for _, ov := range w.overlays {
		walk(ov)
	}
	return result
}

// SetFocus moves focus to the given widget, firing SetFocused(false)
// on the outgoing widget and SetFocused(true) on the incoming one.
// Pass nil to clear focus. When the target is already focused but
// w.focusVisible changed (e.g. a mouse click on the keyboard-focused
// widget), the focus-visible state is still propagated to the widget
// so the focus ring can clear.
func (w *Window) SetFocus(target Widget) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetFocus")
	if target != nil && !w.validFocusTarget(target) {
		return
	}
	w.noteOverlayFocus(target)
	if w.focused == target {
		// Same focused widget — only propagate a focus-visible flip.
		if target != nil {
			if fv, ok := target.(focusVisibleAware); ok {
				fv.SetFocusVisible(w.focusVisible)
			}
		}
		return
	}
	prevFocused := w.focused
	if w.focused != nil {
		if client, ok := w.focused.(IMEClient); ok {
			client.SetPreedit("", 0)
		}
		if prev, ok := w.focused.(registerFocusable); ok {
			prev.SetFocused(false)
		}
	}
	w.focused = target
	if target != nil {
		if f, ok := target.(registerFocusable); ok {
			f.SetFocused(true)
		}
		if fv, ok := target.(focusVisibleAware); ok {
			fv.SetFocusVisible(w.focusVisible)
		}
	}
	if prevFocused != nil {
		w.InvalidateRect(PaintBoundsInWindow(prevFocused))
	}
	if target != nil {
		w.InvalidateRect(PaintBoundsInWindow(target))
	}
	for _, fn := range w.focusListeners {
		fn()
	}
}

func (w *Window) noteOverlayFocus(target Widget) {
	if w == nil || target == nil {
		return
	}
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if widgetInSubtree(w.overlays[i], target) {
			if i < len(w.overlayFocusScopes) {
				w.overlayFocusScopes[i].claimed = true
			}
			return
		}
	}
}

func (w *Window) validFocusTarget(target Widget) bool {
	if w == nil || target == nil || !target.Enabled() || isWidgetEffectivelyHidden(target) {
		return false
	}
	if widgetInSubtree(w.root, target) {
		return true
	}
	for _, ov := range w.overlays {
		if widgetInSubtree(ov, target) {
			return true
		}
	}
	return false
}

func widgetInSubtree(root, target Widget) bool {
	if root == nil || target == nil {
		return false
	}
	if root == target {
		return true
	}
	if cl, ok := root.(childLister); ok {
		for _, child := range cl.ChildList() {
			if widgetInSubtree(child, target) {
				return true
			}
		}
	}
	return false
}

func (w *Window) makeOverlayFocusScope(exclude Widget) overlayFocusScope {
	if w == nil {
		return overlayFocusScope{}
	}
	restore := w.focused
	if widgetIsDescendant(restore, exclude) {
		restore = nil
	}
	if restore != nil && !w.validFocusTarget(restore) {
		restore = nil
	}
	return overlayFocusScope{
		restore:      restore,
		focusVisible: w.focusVisible,
	}
}

func (w *Window) takeOverlayFocusScope(index int, detached Widget) overlayFocusScope {
	if w == nil || index < 0 || index >= len(w.overlayFocusScopes) {
		return overlayFocusScope{}
	}
	scope := w.overlayFocusScopes[index]
	w.overlayFocusScopes = append(w.overlayFocusScopes[:index], w.overlayFocusScopes[index+1:]...)

	// A higher overlay may have saved a focus target inside the overlay being
	// removed. Redirect that restore chain to this overlay's own predecessor
	// so later closing the higher overlay never targets a detached widget.
	for i := range w.overlayFocusScopes {
		if widgetIsDescendant(w.overlayFocusScopes[i].restore, detached) {
			w.overlayFocusScopes[i].restore = scope.restore
			w.overlayFocusScopes[i].focusVisible = scope.focusVisible
		}
	}
	return scope
}

// releaseSubtreeInteractions removes every Window-owned input reference into
// root before the subtree is hidden, detached, or moved to another window.
// Keeping this centralized prevents key, capture, hover, drag, and tooltip
// state from pointing at widgets that no longer participate in dispatch.
func (w *Window) releaseSubtreeInteractions(root Widget) {
	if w == nil || root == nil {
		return
	}
	w.assertUIThread("Window.releaseSubtreeInteractions")
	cancelInteractionTree(root)
	for i := range w.overlayFocusScopes {
		if widgetIsDescendant(w.overlayFocusScopes[i].restore, root) {
			w.overlayFocusScopes[i].restore = nil
		}
	}
	if widgetIsDescendant(w.focused, root) {
		w.SetFocus(nil)
	}
	if widgetIsDescendant(w.mouseCaptured, root) {
		w.mouseCaptured = nil
	}
	if widgetIsDescendant(w.gestureTarget, root) {
		w.gestureTarget = nil
		w.gestureScale = 0
		w.gestureRotation = 0
	}
	if widgetIsDescendant(w.dragCandidate, root) {
		if w.dragging {
			w.dragCandidate.Handle(DragEvent{
				baseEvent: baseEvent{shared: &eventState{}},
				eventType: EventDragEnd,
				When:      time.Now(),
				Source:    w.dragCandidate,
			})
		}
		w.dragCandidate = nil
		w.dragging = false
	}

	selectionTouchesRoot := widgetIsDescendant(w.textSel.anchor, root)
	if !selectionTouchesRoot {
		for _, selectable := range w.textSel.flat {
			if widgetIsDescendant(selectable, root) {
				selectionTouchesRoot = true
				break
			}
		}
	}
	if selectionTouchesRoot {
		w.disarmTextSelection()
	}

	now := time.Now()
	kept := w.hoverPath[:0]
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		widget := w.hoverPath[i]
		if !widgetIsDescendant(widget, root) {
			continue
		}
		widget.Handle(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{target: widget, phase: PhaseTarget, currentTarget: widget}},
			eventType: EventMouseLeave,
			When:      now,
		})
	}
	for _, widget := range w.hoverPath {
		if !widgetIsDescendant(widget, root) {
			kept = append(kept, widget)
		}
	}
	w.hoverPath = kept

	for widget := range w.tooltips {
		if widgetIsDescendant(widget, root) {
			delete(w.tooltips, widget)
		}
	}
	if widgetIsDescendant(w.tooltipTarget, root) {
		w.tooltipTarget = nil
		w.tooltipAnchor = Rect{}
		w.closeTooltip()
	}
}

func cancelInteractionTree(root Widget) {
	if canceler, ok := root.(InteractionCanceler); ok {
		canceler.CancelInteraction()
	}
	if children, ok := root.(childLister); ok {
		for _, child := range children.ChildList() {
			cancelInteractionTree(child)
		}
	}
}

func (w *Window) reconcileSubtreeMove(root Widget) {
	if w == nil || root == nil {
		return
	}
	for _, hovered := range w.hoverPath {
		if widgetIsDescendant(hovered, root) {
			w.clearHoverPath()
			break
		}
	}
	if widgetIsDescendant(w.textSel.anchor, root) {
		w.disarmTextSelection()
	}
	if widgetIsDescendant(w.tooltipTarget, root) {
		w.tooltipTarget = nil
		w.tooltipAnchor = Rect{}
		w.closeTooltip()
	}
}

func (w *Window) clearHoverPath() {
	now := time.Now()
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		widget := w.hoverPath[i]
		widget.Handle(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{target: widget, phase: PhaseTarget, currentTarget: widget}},
			eventType: EventMouseLeave,
			When:      now,
		})
	}
	w.hoverPath = nil
}

// AddFocusChangeListener registers fn to run after keyboard focus moves
// between widgets (including to/from nil). Listeners cannot be removed —
// register once per long-lived subsystem (the html-css engine uses this to
// repaint ancestor-:focus dependents).
func (w *Window) AddFocusChangeListener(fn func()) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.AddFocusChangeListener")
	w.focusListeners = append(w.focusListeners, fn)
}

// FocusNext advances focus to the next focusable widget in tree order,
// wrapping around. If no widget is currently focused, the first
// focusable widget receives focus.
func (w *Window) FocusNext() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.FocusNext")
	list := w.CollectFocusables()
	if len(list) == 0 {
		return
	}
	w.focusVisible = true
	w.SetFocus(list[nextFocusIndex(list, w.focused, +1)])
}

// FocusPrev advances focus to the previous focusable widget in tree
// order, wrapping around. If no widget is currently focused, the last
// focusable widget receives focus.
func (w *Window) FocusPrev() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.FocusPrev")
	list := w.CollectFocusables()
	if len(list) == 0 {
		return
	}
	w.focusVisible = true
	w.SetFocus(list[nextFocusIndex(list, w.focused, -1)])
}

// ClearFocus removes focus from any currently focused widget.
func (w *Window) ClearFocus() {
	w.SetFocus(nil)
}

// nextFocusIndex returns the index to focus next given direction +1
// (forward) or -1 (backward). If current is not in the list, forward
// starts at 0 and backward starts at len-1 — the natural "first" and
// "last" entry points when no focus exists yet.
func nextFocusIndex(list []Widget, current Widget, direction int) int {
	idx := -1
	for i, wd := range list {
		if wd == current {
			idx = i
			break
		}
	}
	n := len(list)
	if idx < 0 {
		if direction > 0 {
			return 0
		}
		return n - 1
	}
	return ((idx+direction)%n + n) % n
}
