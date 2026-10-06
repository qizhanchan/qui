// Package html is the web-like React DSL for qui: fluent HTML-element
// builders that lower to reactive Elements backed by the html-css engine's
// live element (htmlcss.El). Components return HTML elements styled by CSS
// classes — no native widget builders — and get fine-grained reconcile,
// keyed lists and signal binding for free. Conventionally imported aliased:
//
//	import h "github.com/qizhanchan/qui/reactive/html"
//
//	func App() h.Node {
//		n, set := reactive.UseState(0)
//		return h.Div(
//			h.H1("Counter").Class("title"),
//			h.Button(icons.Add, "count: ", n).
//				Class("btn").
//				OnClick(func() { set(n + 1) }),
//		).Class("app")
//	}
//
//	h.Mount(window, cssSource, App)
//
// A component can also be written as real HTML — see template.go, and
// Parse / MustParse / Template.Bind — which is the better shape once the
// markup outweighs the logic. The builders below are the substrate either
// way: a template compiles into them.
//
// The whole surface is five rules:
//
//  1. Every tag is h.Tag(...any) *Builder — the same shape for all of them.
//     A string argument is text, a builder / reactive.Element /
//     slice-of-those is a child, a number is formatted as text, a
//     qui.VectorSource is an icon, and nil is dropped; anything else panics
//     naming the tag and the argument. Text and elements mix, as in
//     h.P("Hello ", h.B("world"), "!"). For the four elements that hold no
//     text a string is their payload instead: img→src, input/textarea→value,
//     select→options.
//  2. Props chain off the builder under their HTML names: .Class, .ID,
//     .Href, .Type, .Disabled, .OnClick, .OnInput, …
//  3. A component is a plain Go func returning h.Node. There is no wrapper
//     to apply and no .Build() to remember — a *Builder and a
//     reactive.Element are both Nodes already.
//  4. Appearance is CSS. Never reach for an inline style call; put a class
//     on the element and a rule in the stylesheet.
//  5. Hooks (UseState/UseMemo/…), signals, Show/For, portals and context
//     come from the reactive package unchanged. Call hooks unconditionally
//     at the top of a component — never inside an if or a loop.
//
// Rule 4 is the one that costs the most to relearn: qui has no inline
// style API on this layer at all, by design. If a template or a builder
// tree seems to need one, the answer is a class and a CSS rule.
package html

import (
	"errors"
	"strconv"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	qw "github.com/qizhanchan/qui/widgets"
)

// engineOf resolves the style engine for the runtime executing the
// current render / reconcile / scoped-sync pass. Mount stores the engine
// on its Runtime (HostData), so every runtime — one per window — styles
// against its own stylesheet; multiple windows never cross.
func engineOf(rt *reactive.Runtime) *htmlcss.StyleEngine {
	if rt == nil {
		return nil
	}
	eng, _ := rt.HostData().(*htmlcss.StyleEngine)
	return eng
}

// Builder is a chainable HTML-element builder. Zero value is not useful;
// construct via the tag functions (Div, Span, Button, …).
type Builder struct {
	tag           string
	key           string
	id            string
	class         string
	attrs         map[string]string
	onClick       func()
	onClickMods   func(mods qui.Modifiers)
	onContextMenu func(x, y float32)
	onMouseEnter  func()
	onMouseLeave  func()
	onDoubleClick func()
	onWheel       func(dx, dy float32) bool
	onKeyDown     func(qui.KeyEvent) bool
	onKeyUp       func(qui.KeyEvent) bool
	onFocus       func()
	onBlur        func()
	onInput       func(string)
	value         *string
	bindText      *reactive.Signal[string]
	bindClass     *reactive.Signal[string]
	children      []Node

	onSubmit func(string)
	onCommit func(string)

	// form-control extras
	onToggle       func(bool)
	checked        *bool
	onSelect       func(int, string)
	selectItems    []string
	selectedIdx    *int
	optionValues   []string
	optionDisabled []bool
	suggestions    []string

	// element ref: called with the element after every render, with nil
	// on unmount; refTo is the UseRef flavor.
	ref   func(*htmlcss.El)
	refTo **htmlcss.El

	// pointer / click / drop / paste / form / canvas / style hooks
	onPointerDown func(htmlcss.PointerEvent) bool
	onPointerMove func(htmlcss.PointerEvent) bool
	onPointerUp   func(htmlcss.PointerEvent) bool
	onClickEvent  func(htmlcss.PointerEvent)
	onFileDrop    func([]string)
	onPaste       func(string) bool
	onFormSubmit  func(map[string]string)
	canvasPaint   func(qui.Canvas, htmlcss.CanvasContext)
	onStyle       func(*htmlcss.ComputedStyle)

	// leaf: a native widget hosted as this element's content (Leaf)
	leafCreate func() qui.Widget
	leafUpdate func(qui.Widget) reactive.Flags

	// icon + drag/drop
	icon       qui.VectorSource
	draggable  bool
	dragKey    string
	dragHandle bool
	dragAxis   htmlcss.DragAxis
	onDrop     func(sourceKey string)
	onDragOver func(sourceKey string, after bool)
	onDragEnd  func()
}

// --- chainable setters ---

// Class sets the CSS class list.
func (b *Builder) Class(c string) *Builder { b.class = c; return b }

// ID sets the element id (addressable via #id and Window.Find).
func (b *Builder) ID(id string) *Builder { b.id = id; return b }

// Key sets the reconciliation key for dynamic siblings.
func (b *Builder) Key(k string) *Builder { b.key = k; return b }

// Text replaces the element's content with a text string (DOM
// textContent semantics). Passing the string straight to the tag function
// — h.Span("hi") — is the usual way; this is for the cases where the text
// is decided after the builder exists.
func (b *Builder) Text(t string) *Builder { b.children = nil; b.addString(t); return b }

// Attr sets an arbitrary attribute (href, data-*, type, …).
func (b *Builder) Attr(k, v string) *Builder {
	if b.attrs == nil {
		b.attrs = map[string]string{}
	}
	b.attrs[k] = v
	return b
}

// OnClick installs a click handler.
func (b *Builder) OnClick(fn func()) *Builder { b.onClick = fn; return b }

// OnClickMods is OnClick with the keyboard modifiers held at click time —
// what a row in a multi-selectable list needs so Shift can extend and Cmd
// can toggle the selection. Both handlers fire when both are installed.
func (b *Builder) OnClickMods(fn func(mods qui.Modifiers)) *Builder {
	b.onClickMods = fn
	return b
}

// OnContextMenu installs a right-click handler; it receives the cursor
// position so a caller can anchor a context menu there (see ContextMenu).
func (b *Builder) OnContextMenu(fn func(x, y float32)) *Builder { b.onContextMenu = fn; return b }

// OnMouseEnter / OnMouseLeave fire once per pointer crossing (no bubbling,
// DOM mouseenter/mouseleave semantics). Use them for hover logic that has to
// DO something — prefetch, preview, tooltip state; pure appearance belongs in
// a `:hover` CSS rule, which costs no render pass.
func (b *Builder) OnMouseEnter(fn func()) *Builder { b.onMouseEnter = fn; return b }

// OnMouseLeave is the leave counterpart of OnMouseEnter.
func (b *Builder) OnMouseLeave(fn func()) *Builder { b.onMouseLeave = fn; return b }

// OnDoubleClick installs a double-click handler. The OnClick handler still
// fires for each of the two clicks, as in the DOM.
func (b *Builder) OnDoubleClick(fn func()) *Builder { b.onDoubleClick = fn; return b }

// OnWheel installs a scroll-wheel handler receiving the deltas; return true
// to consume the event so an enclosing scroll container does not also scroll.
func (b *Builder) OnWheel(fn func(dx, dy float32) bool) *Builder { b.onWheel = fn; return b }

// OnKeyDown installs a key handler; return true to consume the key. The
// element becomes focusable so it can receive keys once clicked or tabbed to,
// and keys from a focused DESCENDANT bubble through it too — which is how a
// dialog wires Esc / Cmd+Enter without stealing focus from its text fields.
func (b *Builder) OnKeyDown(fn func(qui.KeyEvent) bool) *Builder { b.onKeyDown = fn; return b }

// OnKeyUp is the release counterpart of OnKeyDown.
func (b *Builder) OnKeyUp(fn func(qui.KeyEvent) bool) *Builder { b.onKeyUp = fn; return b }

// OnFocus / OnBlur fire when the element gains or loses keyboard focus.
func (b *Builder) OnFocus(fn func()) *Builder { b.onFocus = fn; return b }

// OnBlur is the counterpart of OnFocus.
func (b *Builder) OnBlur(fn func()) *Builder { b.onBlur = fn; return b }

// OnInput installs an <input> change handler.
func (b *Builder) OnInput(fn func(string)) *Builder { b.onInput = fn; return b }

// Value sets an <input>/<textarea>'s current value (controlled input).
func (b *Builder) Value(v string) *Builder { b.value = &v; return b }

// Checked sets a checkbox's state (controlled).
func (b *Builder) Checked(v bool) *Builder { b.checked = &v; return b }

// OnToggle installs a checkbox change handler.
func (b *Builder) OnToggle(fn func(bool)) *Builder { b.onToggle = fn; return b }

// Options sets a <select>'s option list.
func (b *Builder) Options(items ...string) *Builder { b.selectItems = items; return b }

// Suggest installs an autocomplete list on a text input — the programmatic
// form of `<input list>` + `<datalist>`. Typing filters the list
// (case-insensitive substring); ↓/↑ move, Enter accepts, Esc closes.
func (b *Builder) Suggest(items ...string) *Builder { b.suggestions = items; return b }

// OptionValues sets the values a <select> submits, parallel to Options (HTML
// `<option value>`). Without it the visible labels double as values.
func (b *Builder) OptionValues(values ...string) *Builder { b.optionValues = values; return b }

// OptionDisabled marks unselectable options, parallel to Options (HTML
// `<option disabled>`). Disabled rows still show, greyed and inert.
func (b *Builder) OptionDisabled(disabled ...bool) *Builder { b.optionDisabled = disabled; return b }

// Selected sets a <select>'s current index (controlled).
func (b *Builder) Selected(i int) *Builder { b.selectedIdx = &i; return b }

// OnSelect installs a <select> change handler (index, value).
func (b *Builder) OnSelect(fn func(int, string)) *Builder { b.onSelect = fn; return b }

// --- attribute shorthands (each is just .Attr under an HTML name) ---

// Placeholder sets an <input>'s placeholder.
func (b *Builder) Placeholder(p string) *Builder { return b.Attr("placeholder", p) }

// Href sets an <a>'s target URL.
func (b *Builder) Href(url string) *Builder { return b.Attr("href", url) }

// Src sets an <img>'s source path.
func (b *Builder) Src(path string) *Builder { return b.Attr("src", path) }

// Type sets an <input>'s type (text, number, password, color, …).
func (b *Builder) Type(t string) *Builder { return b.Attr("type", t) }

// Name sets the form-control name — radios sharing one form a group.
func (b *Builder) Name(n string) *Builder { return b.Attr("name", n) }

// Alt sets an <img>'s alternative text, which is also its accessible name.
func (b *Builder) Alt(t string) *Builder { return b.Attr("alt", t) }

// For points a <label> at the control it labels (HTML `for`).
func (b *Builder) For(id string) *Builder { return b.Attr("for", id) }

// Min / Max / Step bound a numeric or range <input>.
func (b *Builder) Min(v float64) *Builder { return b.Attr("min", fmtNum(v)) }

// Max is the upper bound of a numeric or range <input>.
func (b *Builder) Max(v float64) *Builder { return b.Attr("max", fmtNum(v)) }

// Step is the increment of a numeric or range <input>.
func (b *Builder) Step(v float64) *Builder { return b.Attr("step", fmtNum(v)) }

// Rows sets a <textarea>'s visible line count.
func (b *Builder) Rows(n int) *Builder { return b.Attr("rows", strconv.Itoa(n)) }

// Disabled greys out and inerts a control. Passing false REMOVES the
// attribute, so a conditionally-enabled control works as written.
func (b *Builder) Disabled(v bool) *Builder {
	if !v {
		if b.attrs != nil {
			delete(b.attrs, "disabled")
		}
		return b
	}
	return b.Attr("disabled", "")
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// Title sets the HTML title attribute — shown as a hover tooltip.
func (b *Builder) Title(t string) *Builder { return b.Attr("title", t) }

// Autofocus sets the `autofocus` attribute: the element takes keyboard
// focus once, when it first mounts — the field a dialog opens on. For a
// form control focus lands on its editing control.
func (b *Builder) Autofocus() *Builder { return b.Attr("autofocus", "") }

// OnSubmit installs an <input> Enter handler.
func (b *Builder) OnSubmit(fn func(string)) *Builder { b.onSubmit = fn; return b }

// OnCommit fires when the user FINISHES a value — Enter, or focus leaving the
// field after an edit. Prefer it over OnInput for anything expensive to apply
// per keystroke (a size, a count, a colour): OnInput sees every intermediate
// rune, and OnSubmit alone loses a typed value when the user clicks away
// instead of pressing Enter.
func (b *Builder) OnCommit(fn func(string)) *Builder { b.onCommit = fn; return b }

// Icon renders a vector source (e.g. a qui/icons glyph) as the
// element's content. Compose an icon + label by nesting an icon element
// beside a text element inside a button.
func (b *Builder) Icon(src qui.VectorSource) *Builder { b.icon = src; return b }

// Draggable marks the element as a drag source, identified by key (read by
// a drop target's OnDrop handler).
func (b *Builder) Draggable(key string) *Builder { b.draggable = true; b.dragKey = key; return b }

// DragHandle marks the element as a drag handle: pair it with Draggable so
// only pressing this element starts the drag, and the drag ghost lifts the
// handle's parent row instead of the small handle itself.
func (b *Builder) DragHandle() *Builder { b.dragHandle = true; return b }

// DragHorizontal switches drag feedback to the horizontal axis: the ghost
// follows the cursor's X and OnDragOver's `after` means "past the right
// half" — for left-to-right strips like sheet tabs. Set it on both the
// draggable elements and the drop targets. Default is vertical.
func (b *Builder) DragHorizontal() *Builder { b.dragAxis = htmlcss.DragAxisHorizontal; return b }

// DragFree lets the ghost follow the cursor on BOTH axes — for a wrapped
// grid of cells, where a drag is genuinely two-dimensional. OnDragOver's
// `after` still reads the horizontal midpoint, because a wrapped grid is one
// linear sequence in reading order. Set it on both the draggable elements
// and the drop targets.
func (b *Builder) DragFree() *Builder { b.dragAxis = htmlcss.DragAxisFree; return b }

// OnDrop makes the element a drop target; fn receives the dragged source's
// Draggable key.
func (b *Builder) OnDrop(fn func(sourceKey string)) *Builder { b.onDrop = fn; return b }

// OnDragOver fires while a drag hovers this element (also makes it a drop
// target). Use it to show a drop indicator; fn gets the source's key and
// whether the cursor is past this element's vertical midpoint (after), for
// deciding insert-before vs insert-after.
func (b *Builder) OnDragOver(fn func(sourceKey string, after bool)) *Builder {
	b.onDragOver = fn
	return b
}

// OnDragEnd fires when THIS element's own drag ends (dropped or cancelled)
// — the place to clear a drop indicator.
func (b *Builder) OnDragEnd(fn func()) *Builder { b.onDragEnd = fn; return b }

// Ref hands the backing htmlcss element to fn — the escape hatch for
// imperative needs the DSL doesn't cover (anchoring a popup to the
// element's Bounds(), reading its Window(), …). fn runs after every render
// with the element, so whatever it installs sees the current render's
// state, and once more with nil when the element unmounts (release
// anything it set up there).
func (b *Builder) Ref(fn func(*htmlcss.El)) *Builder { b.ref = fn; return b }

// RefTo stores the element in *ref (typically a reactive.UseRef) while it
// is mounted, and nil after it unmounts:
//
//	anchor := reactive.UseRef[*htmlcss.El](nil)
//	h.Button("More").RefTo(anchor)
func (b *Builder) RefTo(ref **htmlcss.El) *Builder { b.refTo = ref; return b }

// OnPointerDown / OnPointerMove / OnPointerUp install raw pointer handlers
// (position, button, modifiers, click count); return true to consume. A
// press captures the pointer, so moves and the release keep arriving when
// it leaves the element — what splitters, custom sliders, marquee
// selection and drawing need.
func (b *Builder) OnPointerDown(fn func(htmlcss.PointerEvent) bool) *Builder {
	b.onPointerDown = fn
	return b
}

// OnPointerMove: see OnPointerDown.
func (b *Builder) OnPointerMove(fn func(htmlcss.PointerEvent) bool) *Builder {
	b.onPointerMove = fn
	return b
}

// OnPointerUp: see OnPointerDown.
func (b *Builder) OnPointerUp(fn func(htmlcss.PointerEvent) bool) *Builder {
	b.onPointerUp = fn
	return b
}

// OnClickEvent is OnClick with the event: position, button, modifiers,
// click count, and PreventDefault to stop the built-in behavior (a submit
// button submitting, a link navigating, a file input opening its dialog).
func (b *Builder) OnClickEvent(fn func(htmlcss.PointerEvent)) *Builder {
	b.onClickEvent = fn
	return b
}

// OnFileDrop makes the element a drop zone for files dragged in from the
// OS; fn receives their paths.
func (b *Builder) OnFileDrop(fn func(paths []string)) *Builder { b.onFileDrop = fn; return b }

// OnPaste intercepts Cmd/Ctrl+V inside the element; fn receives the
// clipboard text and returns true to take over the paste.
func (b *Builder) OnPaste(fn func(text string) bool) *Builder { b.onPaste = fn; return b }

// OnFormSubmit installs a <form>'s submit handler (control name → value).
func (b *Builder) OnFormSubmit(fn func(values map[string]string)) *Builder {
	b.onFormSubmit = fn
	return b
}

// OnDraw paints a <canvas> element (see Canvas) with its computed style at
// hand: ctx.Color, ctx.Font, ctx.Style.Var("accent").
func (b *Builder) OnDraw(fn func(cv qui.Canvas, ctx htmlcss.CanvasContext)) *Builder {
	b.canvasPaint = fn
	return b
}

// OnStyle runs after every restyle of the element with its computed
// style — the hook for feeding CSS (a --var, the text color) into
// something imperative.
func (b *Builder) OnStyle(fn func(cs *htmlcss.ComputedStyle)) *Builder { b.onStyle = fn; return b }

// BindText binds the element's text to a signal (updates skip reconcile).
func (b *Builder) BindText(sig *reactive.Signal[string]) *Builder { b.bindText = sig; return b }

// BindClass binds the element's CSS class to a signal (updates skip
// reconcile — the class swap and its restyle happen straight off the
// signal). Overrides any static .Class value.
func (b *Builder) BindClass(sig *reactive.Signal[string]) *Builder { b.bindClass = sig; return b }

// Children sets the element's child nodes (overriding constructor children).
func (b *Builder) Children(kids ...Node) *Builder { b.children = kids; return b }

// bindingHolder collects signal-binding unbinds for release on unmount.
type bindingHolder struct{ unbinds []func() }

func (h *bindingHolder) add(u func()) { h.unbinds = append(h.unbinds, u) }
func (h *bindingHolder) release() {
	for _, u := range h.unbinds {
		u()
	}
	h.unbinds = nil
}

// Build lowers the builder to a reactive.Element backed by an htmlcss.El.
func (b *Builder) Build() reactive.Element {
	bb := *b // snapshot so later mutation of the builder can't leak in
	holder := &bindingHolder{}

	text, content := splitContent(bb.children)
	kids := make([]reactive.Element, 0, len(content))
	for _, c := range content {
		if c == nil {
			continue
		}
		kids = append(kids, c.Build())
	}

	elem := reactive.Node[*htmlcss.El](bb.tag, bb.key,
		func() *htmlcss.El {
			// Create hooks always run inside a runtime pass (render,
			// reconcile, or Show/For scoped sync), so the engine resolves
			// per runtime — correct with any number of mounted windows.
			eng := engineOf(reactive.CurrentRuntime())
			if eng == nil {
				panic("reactive/html: element created outside an html.Mount runtime")
			}
			el := eng.NewEl(bb.tag)
			if bb.bindText != nil {
				holder.add(reactive.BindWidget(el, bb.bindText, el.SetTextContent))
			}
			if bb.bindClass != nil {
				holder.add(reactive.BindWidget(el, bb.bindClass, el.SetClass))
			}
			if bb.leafCreate != nil {
				el.SetHostedWidget(bb.leafCreate())
			}
			return el
		},
		func(el *htmlcss.El) reactive.Flags {
			flags := reactive.FlagNone
			if bb.leafUpdate != nil {
				flags = bb.leafUpdate(el.HostedWidget())
			}
			el.SetOnPointerDown(bb.onPointerDown)
			el.SetOnPointerMove(bb.onPointerMove)
			el.SetOnPointerUp(bb.onPointerUp)
			el.SetOnClickEvent(bb.onClickEvent)
			el.SetOnFileDrop(bb.onFileDrop)
			el.SetOnPaste(bb.onPaste)
			el.SetOnStyle(bb.onStyle)
			if bb.onFormSubmit != nil || bb.tag == "form" {
				el.SetOnFormSubmit(bb.onFormSubmit)
			}
			if bb.canvasPaint != nil {
				el.SetCanvasPaint(bb.canvasPaint)
			}
			// The latest ref closures are kept on the element so the
			// unmount call (wired at mount) reaches the current ones.
			el.SetUserData(refKey, bb.ref)
			el.SetUserData(refToKey, bb.refTo)
			defer func() {
				if bb.ref != nil {
					bb.ref(el)
				}
				if bb.refTo != nil {
					*bb.refTo = el
				}
			}()
			el.SetOnClick(bb.onClick)
			el.SetOnClickMods(bb.onClickMods)
			el.SetOnContextMenu(bb.onContextMenu)
			el.SetSuggestions(bb.suggestions)
			el.SetOnMouseEnter(bb.onMouseEnter)
			el.SetOnMouseLeave(bb.onMouseLeave)
			el.SetOnDoubleClick(bb.onDoubleClick)
			el.SetOnWheel(bb.onWheel)
			el.SetOnKeyDown(bb.onKeyDown)
			el.SetOnKeyUp(bb.onKeyUp)
			el.SetOnFocus(bb.onFocus)
			el.SetOnBlur(bb.onBlur)
			el.SetDraggable(bb.draggable)
			el.SetDragKey(bb.dragKey)
			el.SetDragHandle(bb.dragHandle)
			el.SetDragAxis(bb.dragAxis)
			el.SetOnDrop(bb.onDrop)
			el.SetOnDragOver(bb.onDragOver)
			el.SetOnDragEnd(bb.onDragEnd)
			if bb.icon != nil {
				el.SetIcon(bb.icon)
			}
			if bb.id != "" {
				el.SetElementID(bb.id)
			}
			if bb.bindClass == nil {
				el.SetClass(bb.class)
			}
			// SetManagedAttrs (not SetAttr per key) so an attribute that
			// disappears between renders — `disabled` on an enabled-again
			// button, a conditional aria-* — is actually removed.
			el.SetManagedAttrs(bb.attrs)
			switch {
			case bb.tag == "input" && (bb.attrs["type"] == "checkbox" || bb.attrs["type"] == "radio"):
				el.SetOnToggle(bb.onToggle)
				if bb.checked != nil {
					el.SetChecked(*bb.checked)
				}
			case bb.tag == "input", bb.tag == "textarea":
				el.SetOnInput(bb.onInput)
				el.SetOnSubmit(bb.onSubmit)
				el.SetOnCommit(bb.onCommit)
				if bb.value != nil {
					el.SetInputValue(*bb.value)
				}
			case bb.tag == "select":
				el.SetSelectOptions(bb.selectItems)
				el.SetSelectValues(bb.optionValues)
				el.SetSelectDisabled(bb.optionDisabled)
				el.SetOnSelect(bb.onSelect)
				if bb.selectedIdx != nil {
					el.SetSelectedIndex(*bb.selectedIdx)
				}
			default:
				// Unconditional (not only when text is non-empty) so text
				// that disappears between renders is actually cleared.
				if bb.bindText == nil {
					el.SetTextContent(text)
				}
			}
			// Style/text mutations trigger the engine's own coalesced
			// restyle + InvalidateLayout, so no window flag is needed here.
			return flags
		},
		func(el *htmlcss.El, children []qui.Widget) {
			el.SetElementChildren(children)
		},
		kids...,
	)
	// Always deregister the element from the engine's restyle roots on
	// unmount (matters for portal/dialog roots), releasing signal binds too.
	elem.Destroy = func(w qui.Widget) {
		holder.release()
		if el, ok := w.(*htmlcss.El); ok {
			if ref, _ := el.UserData(refKey).(func(*htmlcss.El)); ref != nil {
				ref(nil)
			}
			if refTo, _ := el.UserData(refToKey).(**htmlcss.El); refTo != nil && *refTo == el {
				*refTo = nil
			}
			el.Unmount()
		}
	}
	return elem
}

const (
	refKey   = "h.ref"
	refToKey = "h.refTo"
)

// --- node wrappers ---

type rawNode struct{ e reactive.Element }

func (r rawNode) Build() reactive.Element { return r.e }

// El wraps a raw reactive.Element as a Node.
//
// Deprecated: reactive.Element satisfies Node directly — pass components,
// Show/For nodes and portals straight into a tag function.
func El(e reactive.Element) Node { return e }

// Frag groups nodes without a wrapper element (reactive.Fragment).
func Frag(args ...any) Node {
	b := (&Builder{tag: "#fragment"}).apply(args)
	return rawNode{reactive.Fragment(buildAll(b.children)...)}
}

// Each maps a slice into a fragment of nodes — the plain, non-reactive list
// render, re-run whenever the enclosing component renders. For a list that
// must update WITHOUT a render pass, use For over a signal instead.
func Each[T any](items []T, render func(i int, item T) Node) Node {
	out := make([]reactive.Element, 0, len(items))
	for i, item := range items {
		if n := render(i, item); n != nil {
			out = append(out, n.Build())
		}
	}
	return rawNode{reactive.Fragment(out...)}
}

// Component instantiates a component with its own hook state, isolated
// from its parent's. name identifies it in DebugTree/Profile; key keeps it
// stable among dynamic siblings; props is retained for memoization.
func Component[P any](name, key string, props P, render func(P) Node) Node {
	return rawNode{reactive.Component(name, key, props, func(p P) reactive.Element {
		if n := render(p); n != nil {
			return n.Build()
		}
		return reactive.Empty()
	})}
}

// Leaf hosts a native qui widget as a childless element — the seam for
// the parts an app draws itself (a grid, a ruler, a chart) inside an
// otherwise CSS-styled tree. The element's tag is kind, so it is a real
// node of the styled tree: `.Class(...)` and CSS margins, sizes and flex /
// grid placement apply, and it counts for :nth-child and sibling
// selectors. create builds the widget once; update (optional) runs every
// render. Size it in CSS, or let the widget's own Measure decide.
func Leaf[T qui.Widget](kind, key string, create func() T, update func(widget T) reactive.Flags) *Builder {
	b := &Builder{tag: kind, key: key}
	b.leafCreate = func() qui.Widget { return create() }
	if update != nil {
		b.leafUpdate = func(w qui.Widget) reactive.Flags {
			t, ok := w.(T)
			if !ok {
				return reactive.FlagNone
			}
			return update(t)
		}
	}
	return b
}

// Canvas is a <canvas> element painted by draw each frame, with the
// element's computed style in ctx (so it follows the stylesheet and a dark
// mode). Size it with CSS width / height (300×150 otherwise).
//
//	h.Canvas(func(cv qui.Canvas, ctx htmlcss.CanvasContext) {
//		cv.StrokeRect(ctx.Bounds, ctx.Color, 1)
//	}).Class("sparkline")
func Canvas(draw func(cv qui.Canvas, ctx htmlcss.CanvasContext), args ...any) *Builder {
	return tag("canvas", args).OnDraw(draw)
}

func buildAll(nodes []Node) []reactive.Element {
	out := make([]reactive.Element, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			out = append(out, n.Build())
		}
	}
	return out
}

// --- tag constructors ---
//
// Every tag function has the SAME shape: `Tag(...any) *Builder`. See the
// argument rule in args.go — strings are text, builders/elements/slices are
// children, and the four content-less elements (img/input/textarea/select)
// read a string as their own payload instead.

func tag(name string, args []any) *Builder { return (&Builder{tag: name}).apply(args) }

// Text wraps a string as a standalone text node, for the rare spot that
// needs a Node rather than a bare string (a []Node built up in a loop).
func Text(s string) Node { return textNode(s) }

// Structural + block elements.
func Div(args ...any) *Builder        { return tag("div", args) }
func Section(args ...any) *Builder    { return tag("section", args) }
func Article(args ...any) *Builder    { return tag("article", args) }
func Aside(args ...any) *Builder      { return tag("aside", args) }
func Header(args ...any) *Builder     { return tag("header", args) }
func Footer(args ...any) *Builder     { return tag("footer", args) }
func Nav(args ...any) *Builder        { return tag("nav", args) }
func Main(args ...any) *Builder       { return tag("main", args) }
func Ul(args ...any) *Builder         { return tag("ul", args) }
func Ol(args ...any) *Builder         { return tag("ol", args) }
func Li(args ...any) *Builder         { return tag("li", args) }
func Dl(args ...any) *Builder         { return tag("dl", args) }
func Dt(args ...any) *Builder         { return tag("dt", args) }
func Dd(args ...any) *Builder         { return tag("dd", args) }
func Form(args ...any) *Builder       { return tag("form", args) }
func Fieldset(args ...any) *Builder   { return tag("fieldset", args) }
func Legend(args ...any) *Builder     { return tag("legend", args) }
func Blockquote(args ...any) *Builder { return tag("blockquote", args) }
func Figure(args ...any) *Builder     { return tag("figure", args) }
func Figcaption(args ...any) *Builder { return tag("figcaption", args) }
func Pre(args ...any) *Builder        { return tag("pre", args) }
func Hr(args ...any) *Builder         { return tag("hr", args) }

// Tables.
func Table(args ...any) *Builder   { return tag("table", args) }
func Thead(args ...any) *Builder   { return tag("thead", args) }
func Tbody(args ...any) *Builder   { return tag("tbody", args) }
func Tfoot(args ...any) *Builder   { return tag("tfoot", args) }
func Tr(args ...any) *Builder      { return tag("tr", args) }
func Th(args ...any) *Builder      { return tag("th", args) }
func Td(args ...any) *Builder      { return tag("td", args) }
func Caption(args ...any) *Builder { return tag("caption", args) }

// Text + inline elements.
func Span(args ...any) *Builder   { return tag("span", args) }
func P(args ...any) *Builder      { return tag("p", args) }
func H1(args ...any) *Builder     { return tag("h1", args) }
func H2(args ...any) *Builder     { return tag("h2", args) }
func H3(args ...any) *Builder     { return tag("h3", args) }
func H4(args ...any) *Builder     { return tag("h4", args) }
func H5(args ...any) *Builder     { return tag("h5", args) }
func H6(args ...any) *Builder     { return tag("h6", args) }
func B(args ...any) *Builder      { return tag("b", args) }
func Strong(args ...any) *Builder { return tag("strong", args) }
func I(args ...any) *Builder      { return tag("i", args) }
func Em(args ...any) *Builder     { return tag("em", args) }
func U(args ...any) *Builder      { return tag("u", args) }
func S(args ...any) *Builder      { return tag("s", args) }
func Small(args ...any) *Builder  { return tag("small", args) }
func Code(args ...any) *Builder   { return tag("code", args) }
func Br(args ...any) *Builder     { return tag("br", args) }
func Label(args ...any) *Builder  { return tag("label", args) }
func Button(args ...any) *Builder { return tag("button", args) }

// A is an anchor; set the target with .Href("https://…").
func A(args ...any) *Builder { return tag("a", args) }

// Img is an image element: a string argument is its src.
func Img(args ...any) *Builder { return tag("img", args) }

// Icon is a leaf that renders a vector source (a qui/icons glyph, an
// svg.Document). Size it with CSS width/height; it adopts the CSS color as
// its tint. h.Icon(icons.Save) — or pass the glyph to any tag function.
func Icon(args ...any) *Builder { return tag("img", args) }

// Input is a text input: a string argument is its value.
func Input(args ...any) *Builder { return tag("input", args) }

// Textarea is a multi-line text input: a string argument is its value.
func Textarea(args ...any) *Builder { return tag("textarea", args) }

// Select is a dropdown: string arguments are its options, since a <select>
// holds options rather than text. Pair with .Selected / .OnSelect.
func Select(args ...any) *Builder { return tag("select", args) }

// Checkbox is an <input type=checkbox>. Pair with .Checked / .OnToggle.
func Checkbox(args ...any) *Builder {
	return (&Builder{tag: "input"}).Attr("type", "checkbox").apply(args)
}

// Switch is an <input type=checkbox switch> — checkbox semantics with a
// toggle-switch look (Safari's `switch` attribute). Pair with .Checked /
// .OnToggle; `accent-color` tints the on-state track.
func Switch(args ...any) *Builder {
	return (&Builder{tag: "input"}).Attr("type", "checkbox").Attr("switch", "").apply(args)
}

// Radio is an <input type=radio>: radios sharing a .Name form a single
// selection group, and a string argument is the radio's value (its visible
// label). Pair with .Checked / .OnToggle.
func Radio(args ...any) *Builder {
	return (&Builder{tag: "input"}).Attr("type", "radio").apply(args)
}

// Range is an <input type=range> slider. Pair with .Min / .Max / .Step and
// .OnInput (which receives the numeric value as a string).
func Range(args ...any) *Builder {
	return (&Builder{tag: "input"}).Attr("type", "range").apply(args)
}

// --- overlays, menus, conditionals ---

// Nothing renders no element — a conditional placeholder that keeps
// unkeyed siblings from shifting when a branch toggles.
func Nothing() Node { return rawNode{reactive.Empty()} }

// If renders node when cond is true, otherwise nothing (eager: node is
// already built by the caller).
func If(cond bool, node Node) Node {
	if cond && node != nil {
		return node
	}
	return Nothing()
}

// Show mounts build() while cond is true and unmounts it while false,
// driven directly by a signal — no render/reconcile pass. Use it for
// signal-gated sections (a filter that toggles a subtree on/off).
func Show(key string, cond *reactive.Signal[bool], build func() Node) Node {
	return rawNode{reactive.Show(key, cond, func() reactive.Element {
		if n := build(); n != nil {
			return n.Build()
		}
		return reactive.Empty()
	})}
}

// For renders one node per item of a slice signal with keyed diffing —
// appends / removals / reorders reuse surviving elements, and the whole
// update runs as a scoped sync with no render pass. Give each row a Key.
func For[T any](key string, items *reactive.Signal[[]T], render func(i int, item T) Node) Node {
	return rawNode{reactive.For(key, items, func(i int, item T) reactive.Element {
		if n := render(i, item); n != nil {
			return n.Build()
		}
		return reactive.Empty()
	})}
}

// ForWith is For with an explicit container layout (row direction, gap,
// grow) for the host that owns the dynamic children.
func ForWith[T any](key string, layout reactive.BoundLayout, items *reactive.Signal[[]T], render func(i int, item T) Node) Node {
	return rawNode{reactive.ForWith(key, layout, items, func(i int, item T) reactive.Element {
		if n := render(i, item); n != nil {
			return n.Build()
		}
		return reactive.Empty()
	})}
}

// Portal mounts child into the window overlay stack, centered, non-modal.
func Portal(child Node) Node {
	return rawNode{reactive.Portal(child.Build())}
}

// ModalPortal shows child centered above a translucent scrim that blocks
// input to everything underneath — the dialog shell. onDismiss fires on
// both a backdrop click and Escape.
// For a ready-made header / body / action row use Dialog; for other
// gestures (Enter, a backdrop that doesn't dismiss) ModalPortalWith.
func ModalPortal(onDismiss func(), child Node) Node {
	return ModalPortalWith(ModalOptions{OnBackdropClick: onDismiss, OnEscape: onDismiss}, child)
}

// ModalSheet is a full-window modal overlay: the child stretches to the
// whole window and lays out its own chrome (top bar + body columns), the way
// a print-settings or full-screen editor sheet does. No scrim is painted —
// the content covers everything — and there is no backdrop to click, so
// onDismiss fires on Escape only; give the sheet its own Cancel control.
func ModalSheet(onDismiss func(), child Node) Node {
	return rawNode{reactive.PortalWith(reactive.PortalOptions{
		Align:    reactive.PortalFill,
		Modal:    true,
		OnEscape: onDismiss,
	}, child.Build())}
}

// ContextMenu pops a native menu anchored at (x, y) on the given window —
// the payload for a Builder.OnContextMenu handler. A row with a nil
// OnClick and Disabled unset still renders enabled (e.g. checkable rows
// whose state is read elsewhere); set Disabled explicitly to gray it out.
func ContextMenu(window *qui.Window, x, y float32, items []MenuItem) {
	if window == nil || len(items) == 0 {
		return
	}
	qw.ShowContextMenu(window, x, y, toWidgetMenuItems(items))
}

// MenuItem is one row of a context menu: a label with optional shortcut
// hint, check state, and submenu — or a separator.
type MenuItem struct {
	Label     string
	Shortcut  string
	OnClick   func()
	Disabled  bool
	Separator bool
	Checkable bool
	Checked   bool
	Submenu   []MenuItem
	// Panel hosts a caller-built widget as the whole row instead of a
	// label — the escape hatch for a command that is picked by pointing
	// (a table-size grid, a colour sheet) rather than by reading. `close`
	// dismisses the popup chain, as a plain row's OnClick does. Raw
	// widgets, not h nodes: a menu popup is a native overlay, not part of
	// the styled element tree.
	Panel func(close func()) qui.Widget
}

// toWidgetMenuItems lowers DSL menu rows to the native widget model,
// recursively for submenus.
func toWidgetMenuItems(items []MenuItem) []*qw.MenuItem {
	if len(items) == 0 {
		return nil
	}
	mi := make([]*qw.MenuItem, 0, len(items))
	for _, it := range items {
		mi = append(mi, &qw.MenuItem{
			Label:     it.Label,
			Shortcut:  it.Shortcut,
			OnClick:   it.OnClick,
			Disabled:  it.Disabled,
			Separator: it.Separator,
			Checkable: it.Checkable,
			Checked:   it.Checked,
			Submenu:   toWidgetMenuItems(it.Submenu),
			Panel:     it.Panel,
		})
	}
	return mi
}

// AnchorMenu pops a native menu anchored below an element — the payload
// for a click handler on a menu-trigger button. Capture the element with
// Builder.Ref. No-op before the element is window-attached.
func AnchorMenu(el *htmlcss.El, items []MenuItem) {
	if win, ok := menuWindow(el, items); ok {
		// Live anchor: re-read el.Bounds() on each re-place so the menu
		// follows its trigger across a window resize (the element is a
		// retained widget whose bounds refresh in the layout pass).
		qw.ShowContextMenuForAnchorFunc(win, func() qui.Rect { return el.Bounds() }, toWidgetMenuItems(items))
	}
}

// AnchorMenuStyled is AnchorMenu with a caller palette + elevation on the
// popup surface (submenus included). App code supplies the palette;
// the h layer itself stays theme-agnostic.
func AnchorMenuStyled(el *htmlcss.El, items []MenuItem, colors qw.MenuColors, elevation int) {
	if win, ok := menuWindow(el, items); ok {
		qw.ShowContextMenuForAnchorFuncStyled(win, func() qui.Rect { return el.Bounds() }, toWidgetMenuItems(items), colors, elevation)
	}
}

// ContextMenuStyled is ContextMenu with a caller palette + elevation.
func ContextMenuStyled(window *qui.Window, x, y float32, items []MenuItem, colors qw.MenuColors, elevation int) {
	if window == nil || len(items) == 0 {
		return
	}
	qw.ShowContextMenuStyled(window, x, y, toWidgetMenuItems(items), colors, elevation)
}

// menuWindow resolves the window an element-anchored menu opens on.
func menuWindow(el *htmlcss.El, items []MenuItem) (*qui.Window, bool) {
	if el == nil || len(items) == 0 {
		return nil, false
	}
	win := el.Window()
	return win, win != nil
}

// Mount parses css into a style engine, mounts app as the window root
// backed by live html-css elements, and applies the initial styling. It
// returns the reactive Runtime so callers can inspect Profile()/DebugTree()
// or drive further renders. Each Mount owns its engine (stored on the
// Runtime), so multiple windows each style against their own stylesheet.
func Mount(window *qui.Window, css string, app func() Node) *reactive.Runtime {
	vw := float32(0)
	if window != nil {
		vw = window.Size().W
	}
	eng := htmlcss.NewStyleEngineViewport(css, vw)
	rt := reactive.NewRuntime(window, func() reactive.Element {
		if n := app(); n != nil {
			return n.Build()
		}
		return reactive.Empty()
	})
	rt.SetHostData(eng)
	// The first pass hands its root to the engine (StyleEngine.AdoptRoot)
	// and styles it before running effects. Without a window there is no
	// host commit, so adopt here.
	rt.Render()
	if eng.Root() == nil {
		eng.AdoptRoot(rt.Root())
	}
	return rt
}

// WindowSpec configures a window opened by OpenWindow.
type WindowSpec struct {
	Title         string
	Width, Height int // client size in logical px; <=0 falls back to 640x480
	// Share, if non-nil, makes the new window share its GL context (font
	// atlas, textures, shaders) with an existing window — cheaper for
	// homogeneous multi-window apps. Typically the window the "new window"
	// action came from.
	Share *qui.Window
	// MinWidth/MinHeight clamp the smallest client size; 0 leaves it unset.
	MinWidth, MinHeight int
	// CSS is the stylesheet compiled into THIS window's own StyleEngine.
	CSS string
	// Position, if non-nil, places the window's top-left at these screen
	// coordinates (e.g. a cascade offset from the spawner). nil lets the OS
	// choose the initial placement.
	Position *qui.Point
}

// OpenWindow bundles the boilerplate every reactive multi-window app
// repeats: create a (shared-context) window, give it a GL renderer, apply a
// min size and optional position, then Mount a reactive tree with its own
// stylesheet. The build closure receives the freshly-created window so
// components can retitle it, spawn siblings, or close it.
//
// The window is registered with app, so the main loop drives it and the app
// exits when the last window closes. Theme is a global concern — set it once
// via qui.SetTheme, not here.
func OpenWindow(app *qui.App, spec WindowSpec, build func(*qui.Window) Node) (*qui.Window, error) {
	if app == nil {
		return nil, errors.New("html: OpenWindow requires an App")
	}
	if build == nil {
		return nil, errors.New("html: OpenWindow requires a build function")
	}
	w, hgt := spec.Width, spec.Height
	if w <= 0 {
		w = 640
	}
	if hgt <= 0 {
		hgt = 480
	}
	win, err := app.NewSharedWindow(spec.Title, w, hgt, spec.Share)
	if err != nil {
		return nil, err
	}
	win.SetRenderer(qui.NewGLRenderer())
	if spec.MinWidth > 0 || spec.MinHeight > 0 {
		win.SetMinSize(spec.MinWidth, spec.MinHeight)
	}
	if spec.Position != nil {
		win.SetPosition(int(spec.Position.X), int(spec.Position.Y))
	}
	Mount(win, spec.CSS, func() Node { return build(win) })
	return win, nil
}
