package qui

// Pointer transparency: a widget that the pointer passes THROUGH, so
// whatever sits behind it receives the event instead. This is the engine
// side of CSS `pointer-events: none` — a scrim that shows but doesn't
// swallow clicks, a decorative badge over a button, a drag ghost.
//
// The rule is applied where targets are chosen (Container.HitTest and
// Window.hitTestAll), not where events are dispatched, so hover, focus,
// tooltips, cursor resolution and drag all agree on the same target.
//
// Transparency is NOT inherited at this layer: each widget answers for
// itself, and hit testing filters per level. That is deliberate — it lets a
// descendant opt back in (CSS `pointer-events: auto` inside a `none`
// subtree) while the enclosing box stays transparent. Style layers that
// DO inherit (htmlcss) simply set the flag on every element in the subtree.

// PointerTransparency is implemented by widgets that can decline to be a
// pointer target. BaseWidget implements it, so every widget participates.
type PointerTransparency interface {
	PointerTransparent() bool
}

// PointerTransparent reports whether this widget declines pointer targeting.
func (b *BaseWidget) PointerTransparent() bool { return b.pointerThrough }

// SetPointerTransparent makes the pointer pass through this widget (true) or
// target it normally (false). The sink for CSS `pointer-events: none/auto`.
//
// A transparent widget still lays out, paints and animates; it simply never
// becomes a hit-test target, so it also never hovers, focuses on click, or
// shows a tooltip. Its children are unaffected — hit testing descends into
// them first, which is what makes a re-enabling descendant work.
func (b *BaseWidget) SetPointerTransparent(through bool) {
	b.assertUIThread("BaseWidget.SetPointerTransparent")
	b.pointerThrough = through
}

// isWidgetPointerTransparent reports an explicit pointer-transparent state
// on w itself (no ancestor walk — see the note on inheritance above).
func isWidgetPointerTransparent(w Widget) bool {
	if w == nil {
		return false
	}
	if pt, ok := w.(PointerTransparency); ok {
		return pt.PointerTransparent()
	}
	return false
}

// acceptHit filters a hit-test result: a transparent target is discarded so
// the caller keeps looking (next sibling in z-order, then the parent). Nil
// in, nil out, so call sites read as a single guard.
func acceptHit(hit Widget) Widget {
	if hit == nil || isWidgetPointerTransparent(hit) {
		return nil
	}
	return hit
}
