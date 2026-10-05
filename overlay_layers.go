package qui

import "slices"

// OverlayLayer orders the overlay stack. Overlays in a higher layer always
// paint above, and take hits before, overlays in a lower one, whatever the
// push order; within a layer the latest push is on top.
type OverlayLayer int

const (
	// OverlayLayerDefault is where PushOverlay puts dialogs, popups and
	// menus.
	OverlayLayerDefault OverlayLayer = 0
	// OverlayLayerNotification is for toasts and banners that must stay
	// visible (and clickable) above a modal opened after them.
	OverlayLayerNotification OverlayLayer = 100
	// OverlayLayerTooltip is above everything else.
	OverlayLayerTooltip OverlayLayer = 200
)

// OverlayExiter is implemented by an overlay that animates out. When it is
// removed (RemoveOverlay / PopOverlay / its own Close), it leaves the stack
// at once — it takes no input and focus moves on — but stays attached and
// painted, with Tick still running, until it calls done. BeginOverlayExit
// returning false skips the animation (detach immediately). Pushing the
// overlay again before done cancels the exit; a late done is then ignored.
type OverlayExiter interface {
	BeginOverlayExit(done func()) bool
}

func (w *Window) beginOverlayExit(widget Widget) bool {
	ex, ok := widget.(OverlayExiter)
	if !ok {
		return false
	}
	w.exitingOverlays = append(w.exitingOverlays, widget)
	finished := false
	done := func() {
		if finished {
			return
		}
		finished = true
		w.finishOverlayExit(widget)
	}
	if !ex.BeginOverlayExit(done) {
		finished = true
		w.exitingOverlays = removeWidget(w.exitingOverlays, widget)
		return false
	}
	return true
}

func (w *Window) finishOverlayExit(widget Widget) {
	i := slices.Index(w.exitingOverlays, widget)
	if i < 0 {
		return // re-pushed meanwhile, or the window went away
	}
	w.exitingOverlays = slices.Delete(w.exitingOverlays, i, i+1)
	w.InvalidateRect(PaintBoundsInWindow(widget))
	DetachWidgetTree(widget)
}

func (w *Window) cancelOverlayExit(widget Widget) {
	w.exitingOverlays = removeWidget(w.exitingOverlays, widget)
}

// OverlayExiting reports whether widget is still playing its exit
// animation after removal.
func (w *Window) OverlayExiting(widget Widget) bool {
	return w != nil && slices.Contains(w.exitingOverlays, widget)
}

func removeWidget(list []Widget, widget Widget) []Widget {
	if i := slices.Index(list, widget); i >= 0 {
		return slices.Delete(list, i, i+1)
	}
	return list
}
