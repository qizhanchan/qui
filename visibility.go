package qui

// VisibilityHider is implemented by widgets whose current visual state can
// hide their entire subtree while preserving layout space. It models CSS
// visibility:hidden; Collapsed models display:none.
//
// The value must be cheap to query. Hit testing, focus collection, and AX
// snapshots all consult it.
type VisibilityHider interface {
	VisibilityHidden() bool
}

// isWidgetHidden reports an explicit hidden state on w itself. It does not
// inspect ancestors; tree walkers use this to prune a whole subtree.
func isWidgetHidden(w Widget) bool {
	if w == nil || isCollapsed(w) {
		return true
	}
	if h, ok := w.(VisibilityHider); ok {
		return h.VisibilityHidden()
	}
	return false
}

// isWidgetEffectivelyHidden includes hidden/collapsed ancestors. It is used
// at API boundaries where callers may address an arbitrary leaf directly.
func isWidgetEffectivelyHidden(w Widget) bool {
	for cur := w; cur != nil; cur = cur.Parent() {
		if isWidgetHidden(cur) {
			return true
		}
	}
	return false
}
