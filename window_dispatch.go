package qui

// cursorPos returns the cursor position in logical window coordinates.
// Gesture anchoring needs it because indirect touch devices report finger
// positions relative to the trackpad, not the window.
func (w *Window) cursorPos() (float32, float32) {
	if w == nil || w.plat == nil {
		return 0, 0
	}
	x, y := w.plat.cursorPos()
	// The platform reports window-logical points; callers hit-test in
	// viewport space.
	return w.toViewport(x, y)
}

// Size returns the logical size of the CONTENT VIEWPORT — the coordinate
// space the widget tree is laid out and hit-tested in. Overlays (Popup,
// Dialog) use this to size themselves before ShowAt positions them.
//
// Under viewport zoom this is the window size divided by the zoom factor
// (see window_zoom.go), which is what content-space callers want. It equals the
// OS window's logical size at 100% zoom; use WindowSize when you
// specifically mean the window rather than its content.
// PointerCaptured reports whether a mouse press is in progress: some
// widget received a MouseDown and its release hasn't arrived yet. Unlike a
// widget's own pressed flag it survives the pointer leaving that widget,
// which is what a drag handler asking "is the button still held?" needs.
func (w *Window) PointerCaptured() bool { return w != nil && w.mouseCaptured != nil }

// Focused returns the widget that currently has keyboard focus, or nil.
func (w *Window) Focused() Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.Focused")
	return w.focused
}

// WidgetAt returns the widget that would receive a click at p (window
// coordinates): the topmost overlay hit, a modal's guard, or the main
// tree's hit. Nil when nothing is there.
func (w *Window) WidgetAt(p Point) Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.WidgetAt")
	return w.hitTestAll(p)
}

// hitTestAll returns the topmost widget at p, checking overlays from
// top to bottom before falling through to the main root.
//
// Overlays that implement modalOverlay and report Modal()==true act
// as event firewalls: events that would have routed to a widget
// UNDERNEATH the modal (lower overlay or root) route to the modal
// overlay itself instead. Overlays above the modal still receive
// events (nested popups, tooltips on top of a dialog). Dialog's
// full-window HitTest achieves the same effect via geometry, but
// partial-geometry modals (a menu that doesn't cover the whole
// window yet still wants to swallow outside clicks) rely on this.
// topModalIndex returns the overlay-stack index of the topmost modal
// overlay, or -1 when none is shown.
func (w *Window) topModalIndex() int {
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if m, ok := w.overlays[i].(modalOverlay); ok && m.Modal() {
			return i
		}
	}
	return -1
}

// keyTargetBehindModal reports whether target lies beneath the topmost
// modal overlay — in the main tree or a lower overlay — and so must not
// receive keyboard input. Overlays above the modal (a select dropdown
// opened from inside a dialog) are reachable.
func (w *Window) keyTargetBehindModal(target Widget) bool {
	m := w.topModalIndex()
	if m < 0 {
		return false
	}
	for _, ov := range w.overlays[m:] {
		if widgetInSubtree(ov, target) {
			return false
		}
	}
	return true
}

func (w *Window) hitTestAll(p Point) Widget {
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if isWidgetHidden(w.overlays[i]) {
			continue
		}
		if hit := acceptHit(w.overlays[i].HitTest(p)); hit != nil {
			return hit
		}
		if m, ok := w.overlays[i].(modalOverlay); ok && m.Modal() {
			// Modal guard: anything below this overlay is unreachable.
			return w.overlays[i]
		}
	}
	if w.root == nil {
		return nil
	}
	return acceptHit(w.root.HitTest(p))
}

// dispatch routes an event through the widget tree.
//
// Semantics:
//   - MouseDown finds the hit target, claims mouse capture, and runs
//     the three-phase dispatch along the root→target path.
//   - MouseUp prefers the captured widget (so Button.pressed gets
//     released even if the cursor wandered off) and releases capture.
//   - Scroll goes to the hit target under the cursor.
//   - Key/Char go to the focused widget (or root if no focus) along
//     the path from root to that widget.
//   - Drag events are synthesized here and dispatched directly to the
//     drag candidate — they don't participate in phased routing.
//
// During phased dispatch: capture runs root→parent-of-target, then the
// target fires, then bubble runs parent-of-target→root. At any point
// a handler may call event.StopPropagation() to halt further phases.
// A handler returning true is treated as an implicit StopPropagation
// for backward compatibility with pre-phased Handle implementations.
func (w *Window) dispatch(event Event) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.dispatch")
	if w.root == nil {
		return
	}
	defer w.reportPanic("dispatch")
	if w.handleInspectEvent(event) {
		return
	}
	if me, ok := event.(MouseEvent); ok && me.Clicks == 0 &&
		(me.eventType == EventMouseDown || me.eventType == EventMouseUp) {
		me.Clicks = w.clicks.count(me)
		event = me
	}
	if w.runEventFilters(event) {
		return
	}

	// Raw keyboard capture (keyboard_raw.go): the escape chord is taken
	// here; every other key skips focus navigation, cross-widget copy and
	// accelerators below.
	raw := false
	switch e := event.(type) {
	case KeyEvent:
		// The escape chord's KeyUp is taken even when the escape action
		// just turned raw mode off.
		if e.eventType == EventKeyUp && w.raw.escDown != KeyUnknown && e.Key == w.raw.escDown {
			w.raw.escDown = KeyUnknown
			return
		}
		if w.rawKeyboardTarget() != nil {
			if w.handleRawEscape(e) {
				return
			}
			raw = true
		}
	case CharEvent:
		raw = w.rawKeyboardTarget() != nil
	}

	// Escape cancels a drag in progress (the platform convention): the
	// source gets DragEnd with Accepted false, the target under the cursor
	// DragLeave, and the release drops nothing. The key is consumed.
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && ke.Key == KeyEscape && w.dragging {
		w.cancelDrag()
		w.dragCancelled = true
		return
	}

	// Drag is synthesized from mouse events but dispatched out-of-band
	// (directly to the drag source/target) so it isn't affected by
	// phased routing.
	if me, ok := event.(MouseEvent); ok {
		switch me.eventType {
		case EventMouseDown:
			w.dragCancelled = false
			w.handleDragStartCandidate(me)
		case EventMouseMove:
			w.handleDragMove(me)
		case EventMouseUp:
			dragged := w.dragging || w.dragCancelled
			w.dragCancelled = false
			w.handleDragEnd(me)
			if dragged {
				// The release that ended a drag keeps flowing (pressed
				// state has to unwind), but it is flagged so nothing
				// downstream reads it as a click on the dropped element.
				me.AfterDrag = true
				event = me
			}
		}
	}

	// Focus moves as the press's default action, after its dispatch — the
	// DOM order: pointerdown listeners see the focus from before the press,
	// then blur / focus fire. The target is picked from what lies under the
	// pointer now, before a handler opens a popup there. A handler that
	// moved focus itself during the dispatch keeps that focus.
	if me, ok := event.(MouseEvent); ok && me.eventType == EventMouseDown {
		// Any press switches the modality to pointer, even one that moves
		// no focus, so a focus move a click handler makes programmatically
		// doesn't light up a keyboard ring. The widget focused now keeps
		// its own visible state until focus moves.
		w.focusVisible = false
		if next, ok := w.focusTargetFromMouse(me); ok {
			w.pressFocus = pendingPressFocus{pending: true, next: next, before: w.focused}
			defer w.FlushPressFocus()
		}
	}

	// Tab / Shift-Tab navigate focus — as the keydown's default action, the
	// DOM order: the keydown dispatches first (capture → target → bubble),
	// so element key listeners see Tab and can keep it by consuming it,
	// then focus moves.
	tabHookRan := false
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && ke.Key == KeyTab && !raw {
		// The focused widget's OnKeyDown hooks see Tab before it navigates
		// — an autocomplete accepts its suggestion on Tab.
		if f := w.focused; f != nil && !w.keyTargetBehindModal(f) {
			tabHookRan = true
			if runKeyHook(f, event) {
				return
			}
			if w.dispatchTabKeydown(f, ke) {
				return
			}
		}
		// A focused widget (or ancestor) implementing TabConsumer can keep
		// Tab for itself; otherwise Tab navigates focus.
		if !w.tabConsumedByFocus(ke.Mods&ModShift != 0) {
			if ke.Mods&ModShift != 0 {
				w.FocusPrev()
			} else {
				w.FocusNext()
			}
			return
		}
	}

	// Any keyboard interaction with a mouse-focused widget upgrades its
	// focus to visible — matches CSS :focus-visible's "user is now
	// driving with keyboard" heuristic. Subsequent MouseDown elsewhere
	// flips it back to false.
	if !w.focusVisible && w.focused != nil {
		if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown {
			w.focusVisible = true
			if fv, ok := w.focused.(focusVisibleAware); ok {
				fv.SetFocusVisible(true)
			}
		}
	}

	// MouseMove: synthesize Enter/Leave based on hit-path diff, then
	// dispatch the MouseMove itself to the captured widget (if any,
	// for drag-select) or the hit target.
	if me, ok := event.(MouseEvent); ok && me.eventType == EventMouseMove {
		w.syncHoverPath(me)
		// Fall through to phased dispatch below; compute target next.
	}

	// Determine target for phased dispatch.
	target := w.eventTarget(event)
	hit := target
	if target == nil {
		target = w.root
	}
	// The keyboard counterpart of hitTestAll's modal guard: with nothing
	// focused the key would fall back to the main root, and focus can be
	// set programmatically behind a modal. Either way the modal takes it.
	switch event.(type) {
	case KeyEvent, CharEvent:
		if w.keyTargetBehindModal(target) {
			target = w.overlays[w.topModalIndex()]
		}
	}

	// Recording fan-out — publish the event before widget handlers
	// run so listeners see the canonical (event, target) pair the
	// framework chose. Cheap when no listeners are registered.
	w.publishEventRecord(event, target)

	// Cross-widget copy. When a selection spans more than one widget,
	// Cmd/Ctrl+C is aggregated at the window level (in document order)
	// before the event reaches the focused widget's own single-widget
	// Cmd+C. A single-widget selection falls through unchanged.
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && !raw &&
		ke.Key == KeyC && IsCommandMod(ke.Mods) {
		if w.copyTextSelection() {
			return
		}
	}
	// Cross-widget select-all. When a read-only selectable (Label) holds
	// focus, Cmd/Ctrl+A selects the whole document; editable widgets keep
	// their own field-scoped select-all (they aren't TextSelectable).
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && !raw &&
		ke.Key == KeyA && IsCommandMod(ke.Mods) {
		if w.selectAllText() {
			return
		}
	}

	// Mouse capture lifecycle. MouseDown claims; MouseUp releases.
	// MouseMove during capture routes to the captured widget so
	// drag-selection (TextArea) or button press-state updates keep
	// working even when the cursor wanders off the original widget.
	if me, ok := event.(MouseEvent); ok {
		switch me.eventType {
		case EventMouseDown:
			w.mouseCaptured = target
			w.pressOrphaned = false
			w.beginTextSelectionDrag(target, me)
		case EventMouseMove:
			// A widget drag (handled out-of-band above) owns the gesture:
			// don't extend a text selection or route the move to the
			// captured widget's own drag-select.
			if w.dragging {
				return
			}
			if w.mouseCaptured != nil {
				// While a cross-widget text selection has taken over, the
				// controller owns the drag — skip routing to the captured
				// widget so its local selection doesn't fight the run.
				if w.updateTextSelectionDrag(me) {
					return
				}
				target = w.mouseCaptured
			}
		case EventMouseUp:
			if w.pressOrphaned {
				// The press's target left the window mid-gesture (see
				// pressOrphaned). Its release is dropped rather than
				// re-targeted by hit test — DOM semantics: no click fires
				// on an element the press never reached.
				w.pressOrphaned = false
				w.mouseCaptured = nil
				w.endTextSelectionDrag()
				return
			}
			if st := me.state(); st != nil {
				st.releaseHit, st.releaseHitSet = hit, true
			}
			if w.mouseCaptured != nil {
				target = w.mouseCaptured
			}
			w.endTextSelectionDrag()
			defer func() { w.mouseCaptured = nil }()
		}
	}

	// Gesture capture — mouse capture's multi-touch analogue. Began
	// claims the receiver, later events in the same gesture redirect to
	// it. See event_gesture.go.
	if ge, ok := event.(GestureEvent); ok {
		target = w.updateGestureCapture(ge, target)
	}

	path := pathToRoot(target)
	state := event.state()
	if state == nil {
		// Defensive: events created externally may not have shared state.
		// Install a fresh one so phase/target accessors return sane values.
		return
	}
	state.target = target

	// A selection drag armed by this MouseDown is provisional. If another
	// widget consumes the press — a scrollbar thumb or slider knob under a
	// blank press, or an ancestor's pointer handler (a dialog title bar
	// dragging the box, a splitter) under a press on its text — that
	// widget's own gesture owns the mouse: disarm, or drag-to-select would
	// swallow the captured moves. A text target consuming its own press
	// keeps the selection; that is how it selects.
	if me, ok := event.(MouseEvent); ok && me.eventType == EventMouseDown && w.textSel.active {
		defer func() {
			if state.stopped && (w.textSel.fromBlank || state.currentTarget != target) {
				w.disarmTextSelection()
			}
		}()
	}

	// Each node receives the event in its OWN coordinate space (see
	// eventInWidgetSpace) — identity for an untransformed tree, the inverse
	// scroll/CSS transform inside one.
	// Capture phase: root → parent-of-target (excludes target).
	for i := 0; i < len(path)-1; i++ {
		state.phase = PhaseCapture
		state.currentTarget = path[i]
		if path[i].Handle(eventInWidgetSpace(event, path[i])) {
			state.stopped = true
		}
		if state.stopped {
			return
		}
	}

	// Target phase. An OnKeyDown hook gets the key before the widget.
	state.phase = PhaseTarget
	state.currentTarget = target
	if !(tabHookRan && target == w.focused) && runKeyHook(target, event) {
		state.stopped = true
		return
	}
	if target.Handle(eventInWidgetSpace(event, target)) {
		state.stopped = true
	}
	if state.stopped {
		return
	}

	// Bubble phase: parent-of-target → root.
	for i := len(path) - 2; i >= 0; i-- {
		state.phase = PhaseBubble
		state.currentTarget = path[i]
		if path[i].Handle(eventInWidgetSpace(event, path[i])) {
			state.stopped = true
		}
		if state.stopped {
			return
		}
	}

	// A raw keyboard target owns every key it did not consume as well.
	if raw {
		return
	}

	// Accelerator fallback. Only fires for unhandled KeyDown events so
	// focused widgets' local shortcuts (Input Cmd+Z, etc.) still win
	// when they want to. Menus bind their items' shortcuts here so
	// Cmd+S / Cmd+O work without the grid having to intercept them.
	//
	// The agent overlay shortcut (Cmd+Shift+A) is checked unconditionally
	// — it self-configures, unlike the layout overlay which requires a
	// prior EnableDebugOverlay() opt-in.
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && w.accels != nil {
		if w.handleZoomShortcut(ke) {
			return
		}
		if w.handleAgentOverlayShortcut(ke) {
			return
		}
		if w.handleDebugOverlayShortcut(ke) {
			return
		}
		// App shortcuts act on the content behind a modal (Cmd+S saving
		// the document under a confirm dialog), so they wait for it to
		// close. The view shortcuts above stay live.
		// Scoped entries inside the modal stay live.
		w.accels.match(ke, target, w.topModalIndex() >= 0)
	}
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && w.accels == nil {
		if w.handleZoomShortcut(ke) {
			return
		}
		if w.handleAgentOverlayShortcut(ke) {
			return
		}
		w.handleDebugOverlayShortcut(ke)
	}
}

// eventTarget chooses the target widget for phased dispatch based on
// event type. Returns nil if no natural target exists (caller falls
// back to root).
func (w *Window) eventTarget(event Event) Widget {
	if w.focused != nil && !w.validFocusTarget(w.focused) {
		w.SetFocus(nil)
	}
	switch e := event.(type) {
	case MouseEvent:
		return w.hitTestAll(Point{X: e.X, Y: e.Y})
	case GestureEvent:
		return w.hitTestAll(Point{X: e.X, Y: e.Y})
	case KeyEvent:
		if w.focused != nil {
			return w.focused
		}
	case CharEvent:
		if w.focused != nil {
			return w.focused
		}
	case FocusEvent:
		if w.focused != nil {
			return w.focused
		}
	}
	return nil
}

// pendingPressFocus is a press's deferred focus move: next, unless focus
// has left before (a handler moved it) by the time it is applied.
type pendingPressFocus struct {
	pending      bool
	next, before Widget
}

// FlushPressFocus applies the focus move owed by the MouseDown being
// dispatched now, ahead of the end of its dispatch. A widget whose press
// does more than its listeners — a <select> opening its popup, a context
// menu opening — calls it first, so focus lands where the DOM puts it:
// after pointerdown listeners, before the rest of the default action.
// Outside a MouseDown dispatch, or once applied, it does nothing.
func (w *Window) FlushPressFocus() {
	if w == nil || !w.pressFocus.pending {
		return
	}
	pf := w.pressFocus
	w.pressFocus = pendingPressFocus{}
	if w.focused != pf.before {
		return // a handler moved focus itself; that wins
	}
	// Skip a widget the dispatch detached (a row the press removed):
	// focus never lands outside the tree.
	if hw, ok := pf.next.(interface{ Window() *Window }); ok && hw.Window() != w {
		return
	}
	w.focusVisible = false
	w.SetFocus(pf.next)
}

// focusTargetFromMouse picks the widget a press at me should focus: the
// nearest focusable widget at or above the hit target. ok is false when
// the press moves no focus (nothing focusable there, or a widget that
// opts out of click focus).
func (w *Window) focusTargetFromMouse(me MouseEvent) (Widget, bool) {
	target := w.hitTestAll(Point{X: me.X, Y: me.Y})
	// Walk up from the hit target to the nearest focusable widget, so a
	// click on non-focusable content (e.g. plain text inside an interactive
	// container) focuses that container — matching how a browser focuses the
	// innermost focusable element under the pointer.
	// A click anywhere inside a widget that opts out of click focus (a push
	// button — its caption label included) leaves focus where it was.
	for cur := target; cur != nil; cur = cur.Parent() {
		if p, ok := cur.(ClickFocusPolicy); ok && !p.FocusOnClick() {
			return nil, false
		}
	}
	for cur := target; cur != nil; cur = cur.Parent() {
		if f, ok := cur.(registerFocusable); ok && f.Focusable() {
			// Selectable text (focusable, not a Tab stop) inside a
			// container that claims text clicks focuses the container.
			if t, ok := cur.(TabStopper); ok && !t.TabStop() {
				if c := textFocusClaimer(cur.Parent()); c != nil {
					cur = c
				}
			}
			return cur, true
		}
	}
	return nil, false
}

// textFocusClaimer returns the nearest focusable ancestor-or-self of w that
// claims clicks on text inside it, or nil.
func textFocusClaimer(w Widget) Widget {
	for cur := w; cur != nil; cur = cur.Parent() {
		c, ok := cur.(TextFocusClaimer)
		if !ok || !c.ClaimsTextFocus() {
			continue
		}
		if f, ok := cur.(registerFocusable); ok && f.Focusable() && cur.Enabled() {
			return cur
		}
	}
	return nil
}

// pathToRoot returns [root, ..., widget] — the ancestor chain from the
// root down to the given widget, inclusive. Walks via Parent() pointers,
// which Container maintains on AddChild.
func pathToRoot(w Widget) []Widget {
	var path []Widget
	for cur := w; cur != nil; cur = cur.Parent() {
		path = append(path, cur)
	}
	// Reverse in place to get root-first order.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// syncHoverPath compares the current hoverPath with a freshly-computed
// hit path at the given cursor position and fires MouseLeave on
// widgets the cursor exited, MouseEnter on widgets it entered. Each
// synthesized event targets exactly one widget (no bubble — matches
// DOM mouseenter/mouseleave semantics).
func (w *Window) syncHoverPath(me MouseEvent) {
	newHit := w.hitTestAll(Point{X: me.X, Y: me.Y})
	var newPath []Widget
	if newHit != nil {
		newPath = pathToRoot(newHit)
	}

	inNew := make(map[Widget]bool, len(newPath))
	for _, wd := range newPath {
		inNew[wd] = true
	}
	inOld := make(map[Widget]bool, len(w.hoverPath))
	for _, wd := range w.hoverPath {
		inOld[wd] = true
	}

	// Leave: walk old path leaf-first so the deepest widget sees leave
	// before its ancestors.
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		wd := w.hoverPath[i]
		if inNew[wd] {
			continue
		}
		p := WindowPointToLocal(wd, Point{X: me.X, Y: me.Y})
		wd.Handle(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{target: wd, phase: PhaseTarget, currentTarget: wd}},
			eventType: EventMouseLeave,
			When:      me.When,
			X:         p.X,
			Y:         p.Y,
		})
	}
	// Enter: walk new path root-first so outer widgets enter before
	// their descendants.
	for _, wd := range newPath {
		if inOld[wd] {
			continue
		}
		p := WindowPointToLocal(wd, Point{X: me.X, Y: me.Y})
		wd.Handle(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{target: wd, phase: PhaseTarget, currentTarget: wd}},
			eventType: EventMouseEnter,
			When:      me.When,
			X:         p.X,
			Y:         p.Y,
		})
	}
	w.hoverPath = newPath
	w.updateTooltipFromHover(me.X, me.Y)
	w.updateCursorFromHover(me.X, me.Y)
}

func (w *Window) handleDragStartCandidate(me MouseEvent) {
	if me.Button != MouseButtonLeft {
		return
	}
	target := w.hitTestAll(Point{X: me.X, Y: me.Y})
	if target == nil {
		w.dragCandidate = nil
		return
	}
	// Walk ancestors: the hit target is usually a leaf (a row's inner label
	// or button), while the draggable unit is a container above it. A widget
	// that is itself Draggable is its own first ancestor, so this stays
	// backward compatible with leaf-draggable widgets.
	if src := draggableAncestor(target); src != nil {
		w.dragCandidate = src
		w.dragStart = Point{X: me.X, Y: me.Y}
		w.dragging = false
	}
}

// draggableAncestor returns the nearest self-or-ancestor of w that reports
// Draggable() == true, or nil.
func draggableAncestor(w Widget) Widget {
	for cur := w; cur != nil; cur = cur.Parent() {
		if d, ok := cur.(Draggable); ok && d.Draggable() {
			return cur
		}
	}
	return nil
}
