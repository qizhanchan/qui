package html

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
)

// ModalOptions configures ModalPortalWith. Every callback is optional;
// a nil one leaves that gesture inert (a backdrop click is still
// absorbed — the portal is modal either way).
type ModalOptions struct {
	// Backdrop is the scrim painted behind the content. Zero = the
	// default translucent black; NoBackdrop paints none.
	Backdrop   qui.Color
	NoBackdrop bool

	// OnBackdropClick fires on a click outside the content.
	OnBackdropClick func()
	// OnEscape fires on Escape the focused content didn't consume.
	OnEscape func()
	// OnEnter fires on Enter the focused content didn't consume — the
	// dialog's default action. A focused <textarea> keeps its newline and
	// a focused button presses itself instead.
	OnEnter func()

	// Label is the overlay's accessible name (the dialog title); its role
	// is "dialog".
	Label string
}

var defaultScrim = qui.Color{R: 0, G: 0, B: 0, A: 0.4}

// ModalPortalWith is ModalPortal with every knob exposed: the scrim, and
// which of backdrop click / Escape / Enter do what.
//
//	h.ModalPortalWith(h.ModalOptions{
//		OnEscape: close,         // backdrop click does NOT dismiss
//		OnEnter:  save,
//		Label:    "Rename",
//	}, box)
func ModalPortalWith(opts ModalOptions, child Node) Node {
	backdrop := opts.Backdrop
	if backdrop == (qui.Color{}) {
		backdrop = defaultScrim
	}
	if opts.NoBackdrop {
		backdrop = qui.Color{}
	}
	return rawNode{reactive.PortalWith(reactive.PortalOptions{
		Align:           reactive.PortalCenter,
		Modal:           true,
		Backdrop:        backdrop,
		OnBackdropClick: opts.OnBackdropClick,
		OnEscape:        opts.OnEscape,
		OnEnter:         opts.OnEnter,
		Label:           opts.Label,
	}, child.Build())}
}

// DialogProps configures Dialog.
type DialogProps struct {
	// Title is the headline: a string (rendered as an <h2>) or any Node
	// (an icon + text row, a step indicator). Empty / nil = no title.
	Title any
	// Body is the dialog's content.
	Body Node
	// Actions is the button row, in order — usually h.Button(...)s, but
	// any node goes (a "Don't ask again" checkbox, a spacer).
	Actions []Node

	// OnDismiss fires on Escape, on the close ✕, and — unless
	// KeepOpenOnBackdrop — on a backdrop click. The caller closes the
	// dialog (flips the state that renders it); doing nothing vetoes.
	OnDismiss func()
	// OnConfirm fires on Enter that the focused content didn't consume —
	// the default action, typically the same func as the primary button.
	OnConfirm func()

	// CloseButton adds a ✕ to the header that calls OnDismiss.
	CloseButton bool
	// KeepOpenOnBackdrop stops a backdrop click from dismissing.
	KeepOpenOnBackdrop bool

	// Class is appended to the box's "q-dialog" class, for per-dialog CSS.
	Class string
	// Backdrop overrides the scrim color (zero = default).
	Backdrop qui.Color
}

// Dialog is the modal dialog shell: a centered box with a header (title
// and optional ✕), a body and an action row, above a scrim. It renders
// plain elements with stable classes, so CSS restyles every part:
//
//	div.q-dialog                (+ props.Class)
//	  header.q-dialog-header
//	    h2.q-dialog-title       (or div.q-dialog-title around a Node title)
//	    button.q-dialog-close   (CloseButton)
//	  div.q-dialog-body
//	  footer.q-dialog-actions
//
// The built-in look comes from framework-origin CSS: any author rule
// beats it, whatever its specificity, and the custom properties
// --q-dialog-bg / --q-dialog-fg / --q-dialog-radius / --q-dialog-shadow
// theme it without restating the rules. Render it conditionally:
//
//	if open {
//		dlg = h.Dialog(h.DialogProps{
//			Title:     "Rename sheet",
//			Body:      h.Input(name).Autofocus().OnInput(setName),
//			Actions:   []h.Node{h.Button("Cancel").OnClick(close), h.Button("Rename").OnClick(rename)},
//			OnDismiss: close,
//			OnConfirm: rename,
//		})
//	}
//
// Buttons are Tab-reachable and press with Enter / Space; Tab stays
// inside the dialog; focus returns where it was when it closes. Give the
// field the dialog opens on Autofocus(). For content that can outgrow the
// window, cap the box and scroll the body:
//
//	.my-dialog { max-height: 90%; }
//	.my-dialog .q-dialog-body { flex: 1; overflow-y: auto; }
func Dialog(p DialogProps) Node {
	var title Node
	label := ""
	switch t := p.Title.(type) {
	case string:
		if t != "" {
			title = H2(t).Class("q-dialog-title")
			label = t
		}
	case Node:
		if t != nil {
			title = Div(t).Class("q-dialog-title")
		}
	}
	var closeBtn Node
	if p.CloseButton && p.OnDismiss != nil {
		// × (U+00D7) is in every text face and takes the text color; ✕
		// (U+2715) can fall back to a color-emoji face that ignores it.
		closeBtn = Button("×").Class("q-dialog-close").
			Title(qui.TOr("qui.close", "Close")).OnClick(p.OnDismiss)
	}
	var header Node
	if title != nil || closeBtn != nil {
		header = Header(title, closeBtn).Class("q-dialog-header")
	}
	var body Node
	if p.Body != nil {
		body = Div(p.Body).Class("q-dialog-body")
	}
	var actions Node
	if len(p.Actions) > 0 {
		actions = Footer(p.Actions).Class("q-dialog-actions")
	}

	class := "q-dialog"
	if p.Class != "" {
		class += " " + p.Class
	}
	opts := ModalOptions{
		Backdrop: p.Backdrop,
		OnEscape: p.OnDismiss,
		OnEnter:  p.OnConfirm,
		Label:    label,
	}
	if !p.KeepOpenOnBackdrop {
		opts.OnBackdropClick = p.OnDismiss
	}
	return ModalPortalWith(opts, Div(header, body, actions).Class(class))
}

// dialogCSS is the Dialog shell's built-in look (framework origin — see
// htmlcss.RegisterFrameworkCSS).
const dialogCSS = `
.q-dialog {
	display: flex; flex-direction: column;
	min-width: 280px; max-width: 560px;
	background: var(--q-dialog-bg, #ffffff);
	color: var(--q-dialog-fg, #1a1a1a);
	border-radius: var(--q-dialog-radius, 8px);
	box-shadow: var(--q-dialog-shadow, 0 8px 28px rgba(0,0,0,0.28));
}
.q-dialog-header {
	display: flex; flex-direction: row; align-items: center; gap: 8px;
	padding: 20px 24px 0 24px;
}
.q-dialog-title { flex: 1; margin: 0; font-size: 20px; font-weight: bold; }
.q-dialog-close { padding: 0 8px; font-size: 22px; }
.q-dialog-body { padding: 16px 24px; }
.q-dialog-actions {
	display: flex; flex-direction: row; justify-content: flex-end; gap: 8px;
	padding: 8px 24px 20px 24px;
}
`

func init() { htmlcss.RegisterFrameworkCSS(dialogCSS) }
