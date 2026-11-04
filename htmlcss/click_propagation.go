package htmlcss

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/qizhanchan/qui"
)

// Click propagation — Chrome semantics.
//
// A click (and a double click, and a context menu) bubbles: the element
// under the pointer hears it first, then every ancestor with a handler,
// until one stops it — PointerEvent.StopPropagation from an OnClickEvent
// handler, or an element declared a barrier with SetStopClickPropagation
// (the DOM's `e.stopPropagation()` in an onclick, as a property, so plain
// func() handlers can have it too). A clickable row holding a ✕ button
// therefore runs both handlers unless the button stops the click.
//
// Stopping is click-level, not a stop of the underlying mouse event: the
// ancestors' pointerup / pointerdown listeners still hear the release /
// press (in the DOM those are separate events that had already bubbled),
// and a form control behind the element still gets the release that ends
// its drag-selection. The record below is what the ancestors consult.
//
// The default action — a <button>'s form submit, a <label> activating its
// control, a link — runs once per click, after the dispatch, for the
// innermost element that has one, unless a handler called PreventDefault.

// clickDispatch is the shared state of one click as it bubbles through
// the elements of one window dispatch (identified by SameDispatch).
type clickDispatch struct {
	ev        qui.Event
	stopped   bool // a handler / barrier stopped it: ancestors skip their handlers
	prevented bool // PreventDefault: no default action
	// activator is the click's activation target — the innermost element
	// with a default action — run after the dispatch (activate).
	activator      *El
	activatorAt    qui.MouseEvent
	activatorActed bool
}

// curClick is the click being dispatched. All dispatch runs on the UI
// thread, one event at a time, so a single record is enough.
var curClick clickDispatch

// clickFor returns the record for ev's dispatch, starting a fresh one
// when ev is a new event.
func clickFor(ev qui.Event) *clickDispatch {
	if d, ok := ev.(interface{ SameDispatch(qui.Event) bool }); ok && curClick.ev != nil && d.SameDispatch(curClick.ev) {
		return &curClick
	}
	curClick = clickDispatch{ev: ev}
	return &curClick
}

// syntheticClick is a click with no pointer event behind it (keyboard
// activation, El.Click): its own record, not shared with any dispatch.
func syntheticClick() *clickDispatch {
	curClick = clickDispatch{}
	return &curClick
}

// SetStopClickPropagation makes the element a click-propagation barrier:
// clicks, double clicks and context menus on it or inside it run its own
// handlers and stop there, never reaching an ancestor's. It is the DOM's
// `onclick = e => { e.stopPropagation(); … }`, declared — use it on a
// button inside a clickable row, or on a panel inside a click-to-dismiss
// surface. Default actions (form submit, link, label) are unaffected.
func (e *El) SetStopClickPropagation(on bool) { e.stopClicks = on }

// SetHearsNestedClicks declares that the element's click / context-menu
// handlers are meant to hear clicks bubbling up from its descendants — a
// pane that focuses on a click anywhere inside it. It changes no dispatch;
// AuditClickNesting just stops reporting nestings under it.
func (e *El) SetHearsNestedClicks(on bool) { e.hearsNested = on }

// StopsClickPropagation reports whether the element is a click barrier.
func (e *El) StopsClickPropagation() bool { return e.stopClicks }

// SetClickSite records where the element's click handlers were installed
// (file:line), for AuditClickNesting's report. reactive/html sets it while
// QUI_DEBUG_CLICKS is on; nothing reads it otherwise.
func (e *El) SetClickSite(site string) { e.clickSite = site }

// hasClickHandler reports whether a left click runs author code here.
func (e *El) hasClickHandler() bool {
	return e.onClick != nil || e.onClickMods != nil || e.onClickEvent != nil || e.onDoubleClick != nil
}

// at maps a release in e's space into a's (window space between).
func (e *El) at(a *El, me qui.MouseEvent) qui.MouseEvent {
	if a == e {
		return me
	}
	w := qui.LocalPointToWindow(e, qui.Point{X: me.X, Y: me.Y})
	p := qui.WindowPointToLocal(a, w)
	me.X, me.Y = p.X, p.Y
	return me
}

// dispatchRelease runs a release for e and every element above it, in the
// DOM's order: all the pointerup listeners, innermost first, then — for a
// left button that completes a click — all the click handlers. e is the
// innermost element that hears the release (the one under the pointer, or
// the form control whose backing widget it targets); the ancestors are
// marked so their own Handle doesn't run it again. selfOver is whether
// the release completes a click on e itself (an ancestor's click needs
// the release over it too — sliding off a button cancels its click but
// not its row's). A pointerup listener returning true consumes the
// release: no further pointerup, no click; dispatchRelease reports it.
func (e *El) dispatchRelease(me qui.MouseEvent, selfOver bool) bool {
	for a := e; a != nil; a = a.elParent {
		if a != e {
			a.earlyEvent = me
		}
		if a.onPointerUp != nil && a.Enabled() && a.onPointerUp(a.pointerEvent(e.at(a, me), nil)) {
			return true
		}
	}
	// A release that ended a drag is not a click: reordering a row by
	// dropping it must not also fire the row's click handler.
	if me.Button != qui.MouseButtonLeft || me.AfterDrag {
		return false
	}
	cs := clickFor(me)
	var doubles []*El
	for a := e; a != nil; a = a.elParent {
		over := selfOver
		if a != e {
			over = me.ReleasedOver(a)
		}
		// A press dragged off the element and released elsewhere is a
		// cancelled click — the "slide off the Delete button" escape.
		if !over || !a.Enabled() {
			continue
		}
		am := e.at(a, me)
		acted, double := a.fireClick(am, cs)
		if double {
			doubles = append(doubles, a)
		}
		if a.stopClicks {
			cs.stopped = true
		}
		if cs.activator == nil && a.hasActivation(am) {
			cs.activator, cs.activatorAt, cs.activatorActed = a, am, acted
		}
	}
	// dblclick is its own event, after the click: it bubbles anew, and
	// only a barrier stops it (a click handler's StopPropagation stopped
	// the click).
	for _, a := range doubles {
		if a.onDoubleClick != nil {
			a.onDoubleClick()
		}
		if a.stopClicks {
			break
		}
	}
	// The default action follows the dispatch, so an ancestor's handler
	// can still prevent it.
	if a := cs.activator; a != nil && !cs.prevented {
		a.activate(cs.activatorAt, cs.activatorActed)
	}
	return false
}

// hasActivation reports whether a click here has a default action — the
// element is the click's activation target when it is the innermost one
// that does: a button (with or without a form to submit), a file / color
// input, a <label> wrapping a control, a link (its own href, or a folded
// link span under the pointer).
func (e *El) hasActivation(me qui.MouseEvent) bool {
	switch {
	case e.tag == "button", e.isButtonInput(), e.isFileInput(), e.isColorInput():
		return true
	case e.tag == "label" && e.labeledControl() != nil:
		return true
	case e.tag == "a" && e.attrs["href"] != "":
		return true
	}
	for _, ib := range e.inlinePool {
		if href, _, ok := ib.LinkSourceAt(qui.Point{X: me.X, Y: me.Y}); ok && href != "" {
			return true
		}
	}
	return false
}

// activate runs the click's default action: the built-in control behavior
// (color palette / file dialog / form submit), label activation, or link
// navigation — invoked directly rather than wired into e.onClick, which
// the reconciler overwrites on every re-render. acted is whether e's own
// click handler ran: an author handler on the element that renders a
// link owns the click (the link doesn't also navigate) — the in-app
// router pattern, where Chrome would navigate unless prevented.
func (e *El) activate(me qui.MouseEvent, acted bool) {
	if e.runBuiltinClick() || e.activateLabeledControl(me.Target()) || acted {
		return
	}
	// Hyperlink navigation (anchor semantics without a reactive handler):
	// the element itself (<a href>) or a folded link span inside one of
	// its inline runs opens the target with the system handler — matching
	// widgets.Anchor.
	if e.tag == "a" {
		if href := e.attrs["href"]; href != "" {
			e.followLink(href, e)
			return
		}
	}
	for _, ib := range e.inlinePool {
		if href, src, ok := ib.LinkSourceAt(qui.Point{X: me.X, Y: me.Y}); ok && href != "" {
			// The link handler hears about the folded <a> itself, so a
			// router can read its data-* / id, not only its href.
			from := e
			if a, ok := src.(*El); ok && a != nil {
				from = a
			}
			e.followLink(href, from)
			return
		}
	}
}

// dispatchContextMenu runs a right press's context-menu handlers for e and
// every element above it, innermost first, until a barrier stops it; the
// ancestors are marked so their own Handle doesn't run it again. It
// reports whether any handler ran.
func (e *El) dispatchContextMenu(me qui.MouseEvent) bool {
	cs := clickFor(me)
	// Event coordinates arrive in e's own space; the handler anchors a
	// window-level overlay, so map back out (a no-op outside scroll
	// containers / transformed subtrees).
	p := qui.LocalPointToWindow(e, qui.Point{X: me.X, Y: me.Y})
	ran := false
	for a := e; a != nil; a = a.elParent {
		if a != e {
			a.earlyEvent = me
		}
		if cs.stopped || !a.Enabled() {
			continue
		}
		if a.onContextMenu != nil {
			if !ran {
				// contextmenu follows the press's focus move, as in the DOM.
				if win := a.Window(); win != nil {
					win.FlushPressFocus()
				}
			}
			ran = true
			a.onContextMenu(p.X, p.Y)
		}
		if a.stopClicks {
			cs.stopped = true
		}
	}
	return ran
}

// bubbleSyntheticClick runs the ancestors' click handlers for a click
// with no pointer behind it (Enter / Space on a button, El.Click) — the
// browser's synthetic click bubbles like a real one. Position-dependent
// OnClickEvent handlers are not run, as on the element itself.
func (e *El) bubbleSyntheticClick(mods qui.Modifiers, cs *clickDispatch) {
	if e.stopClicks {
		cs.stopped = true
	}
	for a := e.elParent; a != nil && !cs.stopped; a = a.elParent {
		if !a.Enabled() {
			continue
		}
		if a.onClick != nil {
			a.onClick()
		}
		if a.onClickMods != nil {
			a.onClickMods(mods)
		}
		if a.stopClicks {
			cs.stopped = true
		}
	}
}

// --- audit -----------------------------------------------------------

// ClickNesting is one place where a click would run two elements'
// handlers: Inner (an element with a click handler, or a form control)
// sits inside Outer (an element with a handler for the same gesture) with
// no barrier between them. Under Chrome semantics both run; when only
// Inner should, Inner needs SetStopClickPropagation (h: StopClickPropagation).
type ClickNesting struct {
	Kind  string // "click" or "contextmenu"
	Inner string
	Outer string
}

func (n ClickNesting) String() string {
	return fmt.Sprintf("%s: %s  inside  %s", n.Kind, n.Inner, n.Outer)
}

// AuditClickNesting lists every nested pair under root (an element and
// its subtree; pass the topmost one). It is a static look at the mounted
// tree: an OnClickEvent handler that stops the click itself still shows
// up — declare it with SetStopClickPropagation instead, which also keeps
// the intent visible.
func AuditClickNesting(root *El) []ClickNesting {
	var out []ClickNesting
	var walk func(x *El, click, menu *El)
	walk = func(x *El, click, menu *El) {
		// A barrier keeps its own clicks too: nothing of it reaches up.
		if (x.hasClickHandler() || x.isControl()) && click != nil && !x.stopClicks {
			out = append(out, ClickNesting{"click", x.describeClick(), click.describeClick()})
		}
		if x.onContextMenu != nil && menu != nil && !x.stopClicks {
			out = append(out, ClickNesting{"contextmenu", x.describeClick(), menu.describeClick()})
		}
		if x.stopClicks {
			click, menu = nil, nil
		} else {
			if x.hasClickHandler() {
				click = x
			}
			if x.onContextMenu != nil {
				menu = x
			}
			if x.hearsNested {
				// Descendants' clicks are meant to reach it; pairs below
				// pair with a nearer handler, if any.
				if click == x {
					click = nil
				}
				if menu == x {
					menu = nil
				}
			}
		}
		for _, k := range x.elementKids {
			if ke, ok := k.(*El); ok {
				walk(ke, click, menu)
			}
		}
	}
	if root != nil {
		walk(root, nil, nil)
	}
	return out
}

// TopEl is the topmost element above e (e itself when it has no element
// parent) — the root to audit.
func (e *El) TopEl() *El {
	for e.elParent != nil {
		e = e.elParent
	}
	return e
}

// describeClick names an element for the audit: its click site when
// known, else its tag / id / classes and the nearest ancestor's site.
func (e *El) describeClick() string {
	if e.clickSite != "" {
		return e.clickSite + " <" + e.tag + e.selectorHint() + ">"
	}
	s := "<" + e.tag + e.selectorHint() + ">"
	for a := e.elParent; a != nil; a = a.elParent {
		if a.clickSite != "" {
			return s + " (under " + a.clickSite + ")"
		}
	}
	return s
}

func (e *El) selectorHint() string {
	var b strings.Builder
	if id := e.attrs["id"]; id != "" {
		b.WriteString("#" + id)
	}
	if c := strings.Fields(e.attrs["class"]); len(c) > 0 {
		b.WriteString("." + strings.Join(c, "."))
	}
	return b.String()
}

// ReportClickNesting audits root and writes each pair not reported before
// to stderr, or appends it to the file QUI_DEBUG_CLICKS names (any value
// other than "1"). Used by reactive/html while QUI_DEBUG_CLICKS is on.
func ReportClickNesting(root *El) {
	pairs := AuditClickNesting(root)
	var fresh []string
	for _, p := range pairs {
		s := p.String()
		if !reportedNesting[s] {
			reportedNesting[s] = true
			fresh = append(fresh, s)
		}
	}
	if len(fresh) == 0 {
		return
	}
	sort.Strings(fresh)
	dest := os.Getenv("QUI_DEBUG_CLICKS")
	text := ""
	for _, s := range fresh {
		text += "qui: nested click handlers — " + s + "\n"
	}
	if dest == "" || dest == "1" {
		fmt.Fprint(os.Stderr, text)
		return
	}
	if f, err := os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.WriteString(text)
		f.Close()
	}
}

var reportedNesting = map[string]bool{}
