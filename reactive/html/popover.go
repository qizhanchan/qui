package html

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// PopoverOptions configures Popover / PopoverAt.
type PopoverOptions struct {
	// Placement is the preferred side of the anchor (default below); the
	// popover flips to the other side when it doesn't fit, and is shifted
	// to stay inside the window.
	Placement reactive.PortalPlacement
	// Offset is the gap between anchor and popover (default 4).
	Offset float32
	// AlignEnd lines the popover up with the anchor's end edge.
	AlignEnd bool
	// MatchWidth makes a below / above popover at least as wide as its
	// anchor (a combobox list).
	MatchWidth bool
	// OnDismiss fires on Escape and on a press outside the popover (which
	// is swallowed, the menu convention). Nil keeps it open until the app
	// removes it.
	OnDismiss func()
	// Modal blocks input to everything else while it is open.
	Modal bool
	// Label names it for the accessibility tree.
	Label string
}

// Popover shows child next to anchor (capture it with RefTo / Ref) and
// keeps it there: it follows the anchor through scrolling, relayout and
// window resizes, and flips when it would leave the window. Render it
// conditionally — it is open while it is in the tree:
//
//	anchor := reactive.UseRef[*htmlcss.El](nil)
//	open, setOpen := reactive.UseState(false)
//	return h.Div(
//		h.Button("Filter").RefTo(anchor).OnClick(func() { setOpen(!open) }),
//		h.If(open, h.Popover(*anchor, h.PopoverOptions{OnDismiss: func() { setOpen(false) }},
//			h.Div(...).Class("filter-panel"))),
//	)
func Popover(anchor *htmlcss.El, opts PopoverOptions, child Node) Node {
	return PopoverAt(func() qui.Rect {
		if anchor == nil {
			return qui.Rect{}
		}
		return qui.InteractionBoundsOf(anchor)
	}, opts, child)
}

// PopoverAt is Popover with an anchor rect function (window coordinates),
// for anchoring to something that isn't an element — a text caret, a cell
// inside a hosted grid.
func PopoverAt(anchor func() qui.Rect, opts PopoverOptions, child Node) Node {
	offset := opts.Offset
	if offset == 0 {
		offset = 4
	}
	po := reactive.PortalOptions{
		Align:            reactive.PortalAnchored,
		Anchor:           anchor,
		Placement:        opts.Placement,
		Offset:           offset,
		AlignEnd:         opts.AlignEnd,
		MatchAnchorWidth: opts.MatchWidth,
		Modal:            opts.Modal,
		OnBackdropClick:  opts.OnDismiss,
		OnEscape:         opts.OnDismiss,
		Role:             "dialog",
		Label:            opts.Label,
	}
	return rawNode{reactive.PortalWith(po, child.Build())}
}

// Engine returns the style engine of a runtime started by Mount — the
// handle for engine-wide settings such as SetLinkHandler (in-app routes
// for `<a href="#/settings">`).
func Engine(rt *reactive.Runtime) *htmlcss.StyleEngine { return engineOf(rt) }
