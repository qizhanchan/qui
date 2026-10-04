package qui

// InteractionCanceler lets a widget unwind transient pointer state when its
// subtree leaves a window while a press or gesture is active.
type InteractionCanceler interface {
	CancelInteraction()
}

type childTransferOwner interface {
	ReleaseChildForTransfer(Widget) bool
}

func widgetWindow(widget Widget) *Window {
	if owner, ok := widget.(interface{ Window() *Window }); ok {
		return owner.Window()
	}
	return nil
}

// AdoptWidgetTree atomically moves widget under parent and onto window. It
// removes prior Container/custom-owner and Window root/overlay ownership.
// Same-window moves preserve focus and capture; cross-window moves release all
// old-window interaction state. Returns false when an unsupported old owner
// cannot release the child or the move would create a parent cycle.
func AdoptWidgetTree(widget, parent Widget, window *Window) bool {
	if widget == nil || widget == parent {
		return false
	}
	if window != nil {
		window.assertUIThread("AdoptWidgetTree")
	}
	if widgetIsDescendant(parent, widget) {
		return false
	}
	oldWindow := widgetWindow(widget)
	if oldWindow != nil && oldWindow != window {
		oldWindow.assertUIThread("AdoptWidgetTree")
	}
	oldParent := widget.Parent()
	if oldParent != nil && oldParent != parent {
		if owner, ok := oldParent.(childTransferOwner); ok {
			if !owner.ReleaseChildForTransfer(widget) {
				return false
			}
		} else if remover, ok := oldParent.(interface{ RemoveChild(Widget) }); ok {
			remover.RemoveChild(widget)
		} else {
			return false
		}
	}
	if oldWindow != nil {
		oldWindow.releaseTopLevelForTransfer(widget)
	}

	currentWindow := widgetWindow(widget)
	if currentWindow != window {
		if currentWindow != nil {
			currentWindow.releaseSubtreeInteractions(widget)
		}
		attachWindowTree(widget, window)
	}
	widget.SetParent(parent)
	if parent != nil {
		widget.ClearLayoutDirty()
	}
	if currentWindow != nil && currentWindow == window {
		currentWindow.reconcileSubtreeMove(widget)
	}
	return true
}

// DetachWidgetTree removes all structural ownership and window state.
func DetachWidgetTree(widget Widget) bool { return AdoptWidgetTree(widget, nil, nil) }
