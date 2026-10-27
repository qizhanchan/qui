package qui

import (
	"errors"
	"slices"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// WindowKind selects a window's role at the OS level. It is fixed at
// creation: whether a window can take keyboard focus, and whether its
// framebuffer has an alpha channel the compositor honors, are decided when
// the OS window and its GPU context are made.
type WindowKind = contract.WindowKind

const (
	// WindowNormal is an ordinary application window — title bar, in the
	// window cycle, takes keyboard focus when clicked.
	WindowNormal = contract.WindowNormal

	// WindowOverlayPanel is a borderless, transparent, always-on-top
	// surface that NEVER takes keyboard focus away from whatever the user
	// is typing into.
	//
	// This is not "a normal window with the chrome switched off". Three
	// properties make it a different kind of object, and all three are
	// load-bearing:
	//
	//   - Non-activating. Clicking the panel must not deactivate the app
	//     the user is typing into. An input method's candidate window that
	//     steals focus is not a candidate window — the host app's caret
	//     disappears and the next keystroke goes nowhere.
	//   - Transparent framebuffer. The panel draws its own rounded card and
	//     shadow, so the pixels outside that card must composite through to
	//     the desktop rather than being cleared to a background color.
	//   - Above everything, on every Space, including over fullscreen apps.
	//     A candidate window that is hidden behind the app it belongs to is
	//     useless.
	//
	// Because a panel cannot receive key events, it is a display-and-click
	// surface: mouse hit-testing works normally, keyboard input does not
	// reach it at all. The keystrokes belong to whatever the OS considers
	// focused; an input method receives them through its own OS channel and
	// drives the panel's widget tree from there.
	//
	// Not every windowing backend can offer this. NewOverlayPanel returns
	// an error rather than silently handing back a normal window: a
	// focus-stealing panel is worse than no panel, because the failure
	// shows up as the user's typing going to the wrong place rather than as
	// a missing feature.
	WindowOverlayPanel = contract.WindowOverlayPanel
)

// ErrOverlayPanelUnsupported is returned by NewOverlayPanel on a backend
// that cannot make a non-activating window.
var ErrOverlayPanelUnsupported = contract.ErrOverlayPanelUnsupported

// NewOverlayPanel creates a WindowOverlayPanel — see that constant for
// what it is and why it is a distinct kind.
//
// The panel starts HIDDEN. An overlay exists to appear at a point the app
// computes at the moment it is needed (a caret rect reported by an input
// method, the mouse position, an anchor widget), so showing it at creation
// would flash it at the wrong place. Call Window.ShowAt.
//
// The window is not registered with any App; use App.NewOverlayPanel for
// that, so the app's Run loop steps it and tears it down.
func NewOverlayPanel(width, height int) (*Window, error) {
	assertProcessUIThread("NewOverlayPanel")
	plat, err := activePlatform()
	if err != nil {
		return nil, err
	}
	return newWindowFromConfig(plat, platformWindowConfig{
		Width:  width,
		Height: height,
		Kind:   WindowOverlayPanel,
	})
}

// NewOverlayPanel creates an overlay panel and registers it with the app so
// the Run loop steps it. See the package-level NewOverlayPanel.
func (a *App) NewOverlayPanel(width, height int) (*Window, error) {
	if a == nil {
		return nil, errors.New("app is nil")
	}
	w, err := NewOverlayPanel(width, height)
	if err != nil {
		return nil, err
	}
	a.windows = append(a.windows, w)
	return w, nil
}

// Kind reports the OS-level role this window was created with.
func (w *Window) Kind() WindowKind {
	if w == nil {
		return WindowNormal
	}
	return w.kind
}

// ShowAt moves the window so its top-left corner sits at the given GLOBAL
// logical coordinates, then makes it visible WITHOUT activating it.
//
// Position first, then show: the reverse order makes the panel appear at
// its previous location for a frame before jumping, which reads as a
// flicker every single keystroke in an input method.
//
// On a platform with no absolute positioning (Wayland) the move is skipped
// and the panel is shown wherever the compositor puts it — the same
// degradation as Window.SetPosition.
func (w *Window) ShowAt(x, y int) {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.ShowAt")
	w.plat.setPos(x, y)
	w.plat.setVisible(true, false)
	// A hidden window skips painting entirely, so whatever was on screen
	// last time is stale by definition. Force a full repaint rather than
	// trusting the accumulated dirty region.
	w.Invalidate()
}

// Show makes the window visible without moving it, and without activating
// it. Use ShowAt when the position matters, which for an overlay panel is
// almost always.
func (w *Window) Show() {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.Show")
	w.plat.setVisible(true, w.kind != WindowOverlayPanel)
	w.Invalidate()
}

// Hide orders the window off screen without destroying it.
//
// Hide rather than destroy is the point of the API: an overlay panel is
// shown and hidden on nearly every keystroke, and recreating an OS window
// plus a GPU context at that rate is far too slow. The widget tree, the
// context and the cached frame all survive.
func (w *Window) Hide() {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.Hide")
	w.plat.setVisible(false, false)
}

// IsVisible reports whether the OS considers this window on screen.
func (w *Window) IsVisible() bool {
	if w == nil || w.plat == nil {
		return false
	}
	w.assertUIThread("Window.IsVisible")
	return w.plat.isVisible()
}

// OverlayResizer is the optional interface an overlay implements to react
// to a logical window-size change while it sits on the overlay stack.
// Overlays are otherwise pre-positioned by their caller at show time and
// the frame loop never re-lays them out (their geometry is caller-driven),
// so without this hook a resize leaves them anchored to a stale absolute
// position — a bottom-anchored dropdown floats away from its trigger, a
// centered dialog drifts off-center. Anchored menus re-place against their
// (moved) trigger; modal dialogs re-center. Overlays that don't implement
// it keep the old behavior.
type OverlayResizer interface {
	OnWindowResize(newSize Size)
}

// OverlayLayouter is the optional interface an overlay implements to
// re-lay itself out after its subtree invalidated layout while shown —
// content that grew (a validation message, rows loaded asynchronously),
// or text re-measured by a theme / locale switch. The frame loop skips
// overlays in its layout pass (their geometry is caller-driven), so
// without this hook such a change keeps the stale show-time geometry.
// Step calls RelayoutOverlay with the current logical window size once
// per frame for each layout-dirty overlay, then clears the flag.
// Overlays that don't implement it just have the flag cleared.
type OverlayLayouter interface {
	RelayoutOverlay(winSize Size)
}

// relayoutDirtyOverlays runs OverlayLayouter for every layout-dirty
// overlay. Snapshotted like notifyOverlaysResize: a relayout may close
// its own overlay.
func (w *Window) relayoutDirtyOverlays() {
	if len(w.overlays) == 0 {
		return
	}
	snapshot := append([]Widget(nil), w.overlays...)
	for _, ov := range snapshot {
		if !ov.IsLayoutDirty() {
			continue
		}
		if l, ok := ov.(OverlayLayouter); ok {
			l.RelayoutOverlay(w.lastSize)
		}
		ov.ClearLayoutDirty()
	}
}

// notifyOverlaysResize fans a logical size change out to overlays that
// implement OverlayResizer. It snapshots the stack first because a handler
// may Close()/RemoveOverlay itself (a dismissing popup) and mutate the
// live slice mid-iteration.
func (w *Window) notifyOverlaysResize(size Size) {
	w.assertUIThread("Window.notifyOverlaysResize")
	if len(w.overlays) == 0 {
		return
	}
	snapshot := append([]Widget(nil), w.overlays...)
	for _, ov := range snapshot {
		if r, ok := ov.(OverlayResizer); ok {
			r.OnWindowResize(size)
		}
	}
}

// PushOverlay adds a widget on top of the overlay stack (within
// OverlayLayerDefault — see PushOverlayLayer). The caller is responsible
// for pre-laying out the widget (call widget.Layout(rect) before pushing)
// so it knows where to render. The overlay's bounds are automatically
// invalidated so the next Step paints it in. Pushing an overlay that is
// already shown raises it to the top of its layer.
func (w *Window) PushOverlay(widget Widget) {
	if w == nil || widget == nil {
		return
	}
	w.assertUIThread("Window.PushOverlay")
	layer := OverlayLayerDefault
	if l, ok := w.overlayLayers[widget]; ok {
		layer = l
	}
	w.pushOverlay(widget, layer)
}

// PushOverlayLayer is PushOverlay into a specific layer. The stack is kept
// ordered by layer, so an overlay in a higher layer stays above any later
// push into a lower one — a toast pushed now still sits above a modal
// opened a second later, and tooltips above both.
func (w *Window) PushOverlayLayer(widget Widget, layer OverlayLayer) {
	if w == nil || widget == nil {
		return
	}
	w.assertUIThread("Window.PushOverlayLayer")
	w.pushOverlay(widget, layer)
}

func (w *Window) pushOverlay(widget Widget, layer OverlayLayer) {
	w.cancelOverlayExit(widget)
	if w.overlayLayers == nil {
		w.overlayLayers = map[Widget]OverlayLayer{}
	}
	for i, overlay := range w.overlays {
		if overlay != widget {
			continue
		}
		scope := w.overlayFocusScopes[i]
		w.overlays = append(w.overlays[:i], w.overlays[i+1:]...)
		w.overlayFocusScopes = append(w.overlayFocusScopes[:i], w.overlayFocusScopes[i+1:]...)
		w.overlayLayers[widget] = layer
		w.insertOverlay(widget, scope, layer)
		w.InvalidateRect(PaintBoundsInWindow(widget))
		return
	}
	scope := w.makeOverlayFocusScope(widget)
	if !AdoptWidgetTree(widget, nil, w) {
		return
	}
	w.overlayLayers[widget] = layer
	w.insertOverlay(widget, scope, layer)
	w.InvalidateRect(PaintBoundsInWindow(widget))
}

// insertOverlay places widget above every overlay whose layer is <= layer.
func (w *Window) insertOverlay(widget Widget, scope overlayFocusScope, layer OverlayLayer) {
	at := len(w.overlays)
	for at > 0 && w.overlayLayers[w.overlays[at-1]] > layer {
		at--
	}
	w.overlays = slices.Insert(w.overlays, at, widget)
	w.overlayFocusScopes = slices.Insert(w.overlayFocusScopes, at, scope)
}

// PopOverlay removes and returns the topmost overlay. Returns nil if
// the stack is empty. The removed overlay's bounds are invalidated so
// the area underneath gets repainted.
func (w *Window) PopOverlay() Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.PopOverlay")
	if len(w.overlays) == 0 {
		return nil
	}
	idx := len(w.overlays) - 1
	return w.removeOverlayAt(idx)
}

// RemoveOverlay removes the first matching overlay from the stack
// (scans top-down so topmost match wins). Returns true if removed.
// Use this when an overlay's lifecycle is driven by something other
// than Push/Pop discipline — e.g., a Tooltip that clears on timeout.
func (w *Window) RemoveOverlay(widget Widget) bool {
	if w == nil || widget == nil {
		return false
	}
	w.assertUIThread("Window.RemoveOverlay")
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if w.overlays[i] == widget {
			w.removeOverlayAt(i)
			return true
		}
	}
	return false
}

func (w *Window) removeOverlayAt(index int) Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.removeOverlayAt")
	if index < 0 || index >= len(w.overlays) {
		return nil
	}
	widget := w.overlays[index]
	focusedInside := widgetIsDescendant(w.focused, widget)
	scope := w.takeOverlayFocusScope(index, widget)
	w.overlays = append(w.overlays[:index], w.overlays[index+1:]...)
	delete(w.overlayLayers, widget)
	w.InvalidateRect(PaintBoundsInWindow(widget))

	// Transfer focus while the removed subtree is still wired, so the old
	// focused widget receives SetFocused(false) and its transformed paint
	// bounds can be invalidated in the same focus transition.
	if focusedInside || (w.focused == nil && scope.claimed) {
		restore := scope.restore
		if restore != nil && !w.validFocusTarget(restore) {
			restore = nil
		}
		w.focusVisible = scope.focusVisible
		w.SetFocus(restore)
	}
	if w.beginOverlayExit(widget) {
		return widget
	}
	DetachWidgetTree(widget)
	return widget
}

func (w *Window) releaseTopLevelForTransfer(widget Widget) bool {
	if w == nil || widget == nil {
		return false
	}
	w.assertUIThread("Window.releaseTopLevelForTransfer")
	if w.root == widget {
		w.root = nil
		w.Invalidate()
		return true
	}
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if w.overlays[i] != widget {
			continue
		}
		w.takeOverlayFocusScope(i, widget)
		w.overlays = append(w.overlays[:i], w.overlays[i+1:]...)
		delete(w.overlayLayers, widget)
		w.InvalidateRect(PaintBoundsInWindow(widget))
		return true
	}
	return false
}
