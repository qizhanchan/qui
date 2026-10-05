package reactive

import (
	"time"

	"github.com/qizhanchan/qui"
)

// Portal mounts its child into the window's OVERLAY stack instead of
// the parent widget tree — the declarative form of PushOverlay. Combine
// with When for a dialog in three lines:
//
//	open, setOpen := reactive.UseState(false)
//	...
//	ui.When(open, func() ui.Node {
//	    return ui.Portal(dialogBox)   // appears above everything
//	})
//
// Toggling the condition pushes/removes the overlay; the portal's
// subtree stays fully reactive while shown. The portal contributes NO
// widget to its parent's layout.
//
// A Portal requires a Runtime bound to a Window and cannot be the
// runtime root.

// PortalAlign selects how the portal positions its content within the
// window.
type PortalAlign int

const (
	// PortalCenter sizes content naturally and centers it (dialogs).
	PortalCenter PortalAlign = iota
	// PortalFill stretches content to the full window (scrims, sheets
	// that manage their own inner layout).
	PortalFill
	// PortalAtPosition places content at (X, Y) at its natural size
	// (context menus).
	PortalAtPosition
	// PortalAnchored places content beside the rect Anchor returns — a
	// popover: on the Placement side, flipped to the opposite side when it
	// doesn't fit there, shifted to stay inside the window, and re-placed
	// whenever the anchor moves (scrolling, window resize, relayout).
	PortalAnchored
)

// PortalPlacement is the preferred side of the anchor for PortalAnchored.
type PortalPlacement int

const (
	PlaceBelow PortalPlacement = iota
	PlaceAbove
	PlaceRight
	PlaceLeft
)

// PortalOptions configures overlay behavior.
type PortalOptions struct {
	Align PortalAlign
	X, Y  float32 // used by PortalAtPosition

	// Anchor returns the anchor rect in window coordinates for
	// PortalAnchored (typically qui.InteractionBoundsOf(trigger)). Placement
	// picks the side, Offset the gap; AlignEnd lines the content up with
	// the anchor's end edge instead of its start; MatchAnchorWidth makes a
	// below/above popover at least as wide as the anchor.
	Anchor           func() qui.Rect
	Placement        PortalPlacement
	Offset           float32
	AlignEnd         bool
	MatchAnchorWidth bool

	// Modal blocks pointer input to everything underneath and traps
	// focus in the portal subtree (qui's modalOverlay contract).
	Modal bool

	// Backdrop fills the whole window behind the content (typically a
	// translucent scrim for modals). Zero = no backdrop paint.
	Backdrop qui.Color

	// OnBackdropClick fires when a click lands on the backdrop rather
	// than the content — the standard dismiss gesture.
	OnBackdropClick func()

	// OnEscape fires when Escape is pressed while the portal is up — the
	// keyboard dismiss gesture. The host grabs focus on mount so the key
	// reaches it without a focusable child in the content.
	OnEscape func()

	// OnEnter fires when Enter reaches the portal unconsumed — the
	// dialog's default action. Handled on the way back up, so the
	// focused content gets first say (a <textarea> newline, a focused
	// button pressing itself, an autocomplete list picking a row).
	OnEnter func()

	// Role / Label describe the overlay to the accessibility tree and
	// the agent. Role defaults to "dialog" for a modal portal and
	// "portal" otherwise; Label is its accessible name (a dialog title).
	Role  string
	Label string
}

const portalKind = "#portal"

// portalGeometry is opts without its callbacks — the part whose change
// needs a relayout / repaint.
func portalGeometry(opts PortalOptions) PortalOptions {
	opts.OnBackdropClick, opts.OnEscape, opts.OnEnter, opts.Anchor = nil, nil, nil, nil
	return opts
}

// Portal shows child centered, non-modal, no backdrop.
func Portal(child Element) Element {
	return PortalWith(PortalOptions{}, child)
}

// PortalWith shows child as an overlay with explicit options.
func PortalWith(opts PortalOptions, child Element) Element {
	return Element{
		Kind:     portalKind,
		portal:   &opts,
		Children: []Element{child},
	}
}

// ---- overlay host widget ----------------------------------------------

// portalHost is the overlay root: a full-window container that paints
// the backdrop, positions the content per Align, follows window
// resizes (window does NOT re-layout overlays), and implements the
// modal contract.
type portalHost struct {
	*qui.Container
	opts PortalOptions
	// lastAnchor is the anchor rect the content was last placed against.
	lastAnchor qui.Rect
}

func newPortalHost(opts PortalOptions) *portalHost {
	h := &portalHost{Container: qui.NewContainer(nil)}
	h.SetSelf(h)
	h.applyOpts(opts)
	return h
}

func (h *portalHost) applyOpts(opts PortalOptions) {
	h.opts = opts
	h.Style().Background = opts.Backdrop
	h.Style().Padding = qui.Insets{}
}

func (h *portalHost) Role() string {
	switch {
	case h.opts.Role != "":
		return h.opts.Role
	case h.opts.Modal:
		return qui.RoleDialog
	}
	return "portal"
}

// AccessibleName is PortalOptions.Label.
func (h *portalHost) AccessibleName() string { return h.opts.Label }

// Modal satisfies qui's modalOverlay contract.
func (h *portalHost) Modal() bool { return h.opts.Modal }

func (h *portalHost) Measure(available qui.Size) qui.Size { return available }

func (h *portalHost) Layout(rect qui.Rect) {
	h.Container.Layout(rect) // stores bounds; LayoutEngine is nil
	for _, child := range h.ChildList() {
		var r qui.Rect
		switch h.opts.Align {
		case PortalFill:
			r = rect
		case PortalAtPosition:
			sz := portalMeasure(child, rect)
			r = qui.Rect{X: h.opts.X, Y: h.opts.Y, W: sz.W, H: sz.H}
		case PortalAnchored:
			sz := portalMeasure(child, rect)
			var anchor qui.Rect
			if h.opts.Anchor != nil {
				anchor = h.opts.Anchor()
			}
			h.lastAnchor = anchor
			r = placeAnchored(rect, anchor, sz, h.opts)
		default: // PortalCenter
			sz := portalMeasure(child, rect)
			r = qui.Rect{
				X: rect.X + (rect.W-sz.W)/2,
				Y: rect.Y + (rect.H-sz.H)/2,
				W: sz.W,
				H: sz.H,
			}
			// Content taller (wider) than the window keeps its top (left)
			// edge on screen — a dialog's title and close button stay
			// reachable; the overflow runs off the bottom. Author
			// `max-height: 90%; overflow-y: auto` makes it scroll instead.
			if r.Y < rect.Y {
				r.Y = rect.Y
			}
			if r.X < rect.X {
				r.X = rect.X
			}
		}
		child.Layout(r)
	}
}

// portalMeasure is Measure with the child's declarative size constraints
// applied — explicit width/height (incl. percentages of the window) and
// min/max — exactly what qui's layout engines do before positioning a
// child. Without the explicit-size step a CSS-sized dialog (`width:740px`)
// would be laid out at whatever its content measured to.
func portalMeasure(child qui.Widget, rect qui.Rect) qui.Size {
	return qui.MeasureConstrained(child, qui.Size{W: rect.W, H: rect.H})
}

// Tick re-layouts when the window size changed OR the overlay subtree
// marked itself layout-dirty — overlays are caller-positioned, so the
// window's layout pass skips them, and InvalidateLayout from a descendant
// bubbles up to this (parent-less) host. Without the dirty check, content
// that changes size/layout after mount (e.g. a live restyle that swaps a
// child's layout engine, or dynamic dialog content) would keep its stale
// initial geometry.
func (h *portalHost) Tick(now time.Time) qui.Rect {
	dirty := h.Container.Tick(now)
	if w := h.Window(); w != nil {
		sz := w.Size()
		moved := h.opts.Align == PortalAnchored && h.opts.Anchor != nil && h.opts.Anchor() != h.lastAnchor
		if h.Bounds().W != sz.W || h.Bounds().H != sz.H || h.IsLayoutDirty() || moved {
			h.Layout(qui.Rect{W: sz.W, H: sz.H})
			h.ClearLayoutDirty()
			dirty = qui.Rect{W: sz.W, H: sz.H}
		}
	}
	return dirty
}

// HitTest: content first; the backdrop itself only intercepts when the
// portal is modal or wants dismiss clicks — a plain non-modal portal is
// pointer-transparent outside its content.
func (h *portalHost) HitTest(p qui.Point) qui.Widget {
	if !h.Bounds().Contains(p) {
		return nil
	}
	for i := h.ChildCount() - 1; i >= 0; i-- {
		if hit := h.ChildAt(i).HitTest(p); hit != nil {
			return hit
		}
	}
	if h.opts.Modal || h.opts.OnBackdropClick != nil {
		return h
	}
	return nil
}

func (h *portalHost) Handle(event qui.Event) bool {
	if me, ok := event.(qui.MouseEvent); ok &&
		me.Type() == qui.EventMouseDown && event.Target() == qui.Widget(h) {
		if h.opts.OnBackdropClick != nil {
			h.opts.OnBackdropClick()
			return true
		}
	}
	// Escape dismisses on the way back up, not on the way down: during
	// capture the host is an ancestor of the focused element, and acting
	// there would take the key from content that handles Escape itself
	// (an inline editor cancelling, an autocomplete list closing).
	if ke, ok := event.(qui.KeyEvent); ok && ke.Phase() != qui.PhaseCapture &&
		ke.Type() == qui.EventKeyDown && ke.Key == qui.KeyEscape && h.opts.OnEscape != nil {
		h.opts.OnEscape()
		return true
	}
	if ke, ok := event.(qui.KeyEvent); ok && ke.Phase() != qui.PhaseCapture &&
		ke.Type() == qui.EventKeyDown && ke.Key == qui.KeyEnter && h.opts.OnEnter != nil {
		h.opts.OnEnter()
		return true
	}
	return h.Container.Handle(event)
}

// Focusable lets the host receive key events (for OnEscape / OnEnter)
// when the content has no focusable child of its own.
func (h *portalHost) Focusable() bool { return h.opts.OnEscape != nil || h.opts.OnEnter != nil }

// TabStop: the host takes focus only as the fallback key receiver on
// mount; Tab cycles the content's own controls (qui.TabStopper).
func (h *portalHost) TabStop() bool { return false }

// SetFocused is a no-op: the host has no focus-visible chrome of its own.
func (h *portalHost) SetFocused(bool) {}

// layoutPortal (re)positions the overlay to the current window size and
// invalidates its pixels.
func layoutPortal(host *portalHost, window *qui.Window) {
	if window == nil {
		return
	}
	sz := window.Size()
	host.Layout(qui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	window.InvalidateRect(host.Bounds())
}

// placeAnchored computes a popover's rect: on the preferred side of
// anchor, flipped when it would overflow there and fits better on the
// other side, then clamped into win.
func placeAnchored(win, anchor qui.Rect, sz qui.Size, o PortalOptions) qui.Rect {
	gap := o.Offset
	if (o.Placement == PlaceBelow || o.Placement == PlaceAbove) && o.MatchAnchorWidth && sz.W < anchor.W {
		sz.W = anchor.W
	}
	r := qui.Rect{W: sz.W, H: sz.H}
	switch o.Placement {
	case PlaceBelow, PlaceAbove:
		below := anchor.Y + anchor.H + gap
		above := anchor.Y - gap - sz.H
		roomBelow := win.Y + win.H - below
		roomAbove := anchor.Y - gap - win.Y
		r.Y = below
		if o.Placement == PlaceAbove {
			r.Y = above
			if roomAbove < sz.H && roomBelow > roomAbove {
				r.Y = below
			}
		} else if roomBelow < sz.H && roomAbove > roomBelow {
			r.Y = above
		}
		r.X = anchor.X
		if o.AlignEnd {
			r.X = anchor.X + anchor.W - sz.W
		}
	default: // PlaceRight, PlaceLeft
		right := anchor.X + anchor.W + gap
		left := anchor.X - gap - sz.W
		roomRight := win.X + win.W - right
		roomLeft := anchor.X - gap - win.X
		r.X = right
		if o.Placement == PlaceLeft {
			r.X = left
			if roomLeft < sz.W && roomRight > roomLeft {
				r.X = right
			}
		} else if roomRight < sz.W && roomLeft > roomRight {
			r.X = left
		}
		r.Y = anchor.Y
		if o.AlignEnd {
			r.Y = anchor.Y + anchor.H - sz.H
		}
	}
	// Keep it on screen.
	if r.X+r.W > win.X+win.W {
		r.X = win.X + win.W - r.W
	}
	if r.Y+r.H > win.Y+win.H {
		r.Y = win.Y + win.H - r.H
	}
	if r.X < win.X {
		r.X = win.X
	}
	if r.Y < win.Y {
		r.Y = win.Y
	}
	return r
}
