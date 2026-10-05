package htmlcss

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
	"github.com/qizhanchan/qui/widgets"
)

// El is the retained, restyleable HTML element widget — the ONE widget
// assembly of the html-css engine. The reactive layer (reactive/html)
// mounts Els directly; the one-shot Render path compiles a parsed DOM
// into a static El tree (static.go). An El persists for the lifetime of
// its element and recomputes + re-applies its CSS in place when its
// tag/class/attrs/text/children change — fine-grained reconcile, keyed
// lists, and signal binding all ride on this.
//
// El embeds widgets.Box, so it already paints CSS box decorations from
// Style and supports Hover/Focus/Active/Transform/gradient/ClipChildren/
// relative-position offsets. On top of that El owns:
//   - a backing *Node mirror kept in sync with the live tree, so the
//     existing selector cascade (computeNode) matches against it verbatim;
//   - an internally-managed text Label (for text leaves) or widgets.Input
//     (for <input>), so a text/editable leaf never collides with the
//     reactive-managed element-child list.
type El struct {
	widgets.Box

	tag   string
	attrs map[string]string // id, class, style, href, type, placeholder, value…
	// attrMu guards the attrs MAP ONLY, and only against the one reader
	// that is not on the main goroutine: the AX / DOM introspection surface
	// (Role, AccessibleName, inspect.go), which an agent HTTP handler walks
	// while the app keeps rendering. A re-render rewrites attributes
	// (SetManagedAttrs) on every frame, so an unguarded walk hit "concurrent
	// map read and map write" — a fatal error, not a data race Go recovers
	// from. Everything else touching attrs runs on the main goroutine and
	// stays lock-free; only writers take the write lock.
	attrMu sync.RWMutex
	// managedAttrs are the keys last supplied by a declarative caller via
	// SetManagedAttrs, so a re-render can remove the ones that went away.
	managedAttrs []string
	text         string
	engine       *StyleEngine
	node         *Node // mirror used for selector matching / cascade

	// elParent is the adopting parent element (set by the parent's
	// SetElementChildren, cleared on Unmount). It is the ELEMENT-tree
	// parent, which the widget Parent() chain cannot stand in for: an
	// inline-flowed or scroll-hosted child's widget parent is the
	// InlineBox / scroll inner box, not the parent El. The engine's
	// scoped restyle resolves each dirty element's impact scope from it.
	elParent *El
	// styleParent is the element a top-level (portal) element inherits
	// from and matches ancestor selectors through — set by SetStyleParent;
	// styleKids is the reverse set, restyled along with this element.
	styleParent *El
	styleKids   map[*El]bool
	// styledPass is the StyleEngine pass that last styled this element
	// (Restyle uses it to find style-linked roots no walk reached).
	styledPass int

	elementKids []qui.Widget // element children set by the reactive layer
	textLabel   *widgets.Label

	// Inline formatting contexts: when the content mixes text with inline
	// element children (see buildFlow), consecutive inline-level children
	// are grouped into anonymous runs — each run an InlineBox where
	// foldable inline elements become styled text spans (word-level wrap,
	// shared baselines) and everything else rides along as an atomic
	// inline box — while block-level children stack between the runs.
	// flowKids is the arranged child list (nil = plain block path);
	// inlinePool reuses InlineBox instances across restyles so their tree
	// position stays stable.
	flowKids   []qui.Widget
	inlinePool []*widgets.InlineBox

	// captionRowOffset (table El only): grid rows the <caption> pushes data
	// cells down by (1 for a top caption, 0 otherwise). Subtracted when a
	// cell reports its data-relative row for clipboard reconstruction.
	captionRowOffset int
	// Table geometry (table El only), set by buildTable: column count, data
	// row count, and whether border-collapse:collapse is in effect. Cells read
	// these off their enclosing table to place collapsed single-line borders.
	tableCols      int
	tableDataRows  int
	borderCollapse bool

	// List item marker (li inside ul/ol): when non-empty the element lays
	// out as a [marker | content] row, content hosted in liContent.
	marker  string
	listTag string // parent list tag ("ol"/"ul") when this El is a marked <li>
	// Bullet markers (disc/circle/square) draw as geometry via markerShape;
	// numeric markers ("1.") are real text and use markerLabel. Exactly one
	// is non-nil at a time — see listmarker.go for why bullets can't be text.
	markerLabel *widgets.Label
	markerShape *listMarkerWidget
	liContent   *widgets.Box

	// img leaf: raster/svg loaded from the src attribute (static path).
	// h.Icon's VectorSource path uses iconSrc below instead.
	imgWidget qui.Widget
	imgSrc    string

	// <canvas> leaf: a replaced element whose pixels come from user code.
	// canvasDraw is a paint callback (wrapped in a canvasLeaf); canvasUser
	// is an arbitrary widget the caller plugs in for events/animation.
	// canvasWidget is the effective backing actually attached (one of the
	// two, or a light placeholder when neither is wired).
	canvasDraw   func(cv qui.Canvas, bounds qui.Rect)
	canvasUser   qui.Widget
	canvasWidget qui.Widget

	// Control backing: leaf form tags (input / textarea / checkbox via
	// <input type=checkbox> / select) render through a real editing widget
	// held here, styled via the shared applyCommon. Non-control elements
	// leave backing nil and use the box/text path.
	backing qui.Widget
	// <select> option model. selectItems is what the dropdown DISPLAYS;
	// selectValues is what the form SUBMITS, parallel to it (an option's
	// `value` attribute, falling back to its text — the HTML rule).
	// selectDisabled marks `<option disabled>` rows plus the inert heading
	// rows synthesized for `<optgroup label=…>`. defaultSelectedIdx
	// remembers the `selected` option so a form reset can restore it.
	selectItems        []string
	selectValues       []string
	selectDisabled     []bool
	defaultSelectedIdx int
	checked            bool
	selectedIdx        int
	// Autocomplete state for `<input list=…>` (see datalist.go).
	suggestions     []string
	suggestMatches  []string
	suggestIdx      int
	suggestPopup    *widgets.Popup
	suggestApplying bool
	radioGrp        *widgets.RadioGroup // set for <input type=radio>, keyed by name
	// fileValue holds the absolute path chosen through the native file
	// dialog for <input type=file> (empty = nothing picked yet). The
	// element renders as a push button whose label is the base filename.
	fileValue  string
	colorValue string // current <input type=color> value, e.g. "#3366ff"

	// Overflow scroll (CSS overflow:auto/scroll): the element hosts a
	// ScrollView whose inner box carries the real layout + children, while
	// the element itself keeps its box decorations + fixed/max height.
	scrollView     *widgets.ScrollView
	scrollInner    *widgets.Box
	overflowScroll bool
	lastScrollW    float32

	// Icon leaf: an element carrying a vector source renders it as an
	// svgImage child sized from CSS width/height and tinted with the
	// computed text color. Composes with element children via nesting
	// (an icon <span> next to a text <span> inside a button).
	iconSrc qui.VectorSource
	iconImg *svgImage

	// Drag & drop reorder: an El marked draggable becomes the drag source
	// (identified by dragKey); an El with an onDrop handler becomes a drop
	// target and receives the dragged source's key.
	draggable  bool
	dragKey    string
	dragHandle bool     // this element is a handle; its drag ghost applies to its parent row
	dragAxis   DragAxis // which axis the ghost follows + which midpoint decides `after`
	onDrop     func(sourceKey string)
	onDragOver func(sourceKey string, after bool) // drag hovering; after = cursor past this element's midpoint (axis per dragAxis)
	onDragEnd  func()                             // this element's own drag finished

	// Live drag feedback: while this element is the drag source it follows
	// the cursor via a PAINT-ONLY VisualTransform (not PosOffset) so the source's
	// hit-test bounds stay put — otherwise the dragged row's hit area would
	// track the cursor and the drag-over target would always resolve to the
	// source itself. We deliberately DON'T dim via Style().Opacity: opacity
	// wraps the paint in a SaveLayer sized to the row's ORIGINAL bounds, and
	// the transform then translates content out of that layer → the row gets
	// clipped ("horizontally erased") as it moves. VisualTransform stays
	// separate from authored CSS Transform state.
	dragging  bool
	dragStart qui.Point
	// dragLift counts in-flight drags happening INSIDE this element. A
	// container paints its children in z-order, so raising only the dragged
	// element lifts it above its own siblings — but not above a later
	// sibling of its WRAPPER. Any element between the ghost and the root
	// therefore rides the lift too, which is what keeps a row wrapped in a
	// "row + drop indicators" box from sliding under the next wrapper.
	dragLift             int
	savedVisualTransform *widgets.BoxTransform

	onClick func()
	// onClickMods is the modifier-aware form of onClick, for elements whose
	// click means something different with Shift / Cmd held — a
	// multi-selectable list row being the canonical case. Both fire when
	// both are set.
	onClickMods   func(mods qui.Modifiers)
	onContextMenu func(x, y float32)
	onInput       func(string)            // input / textarea
	onSubmit      func(string)            // input Enter
	onCommit      func(string)            // input Enter / blur-after-edit
	onToggle      func(bool)              // checkbox / radio
	onSelect      func(int, string)       // select
	onFormSubmit  func(map[string]string) // <form> submit (name → value)

	// --- DOM-shaped extras -------------------------------------------
	//
	// These mirror the browser events an app reaches for once the basics
	// (click / input / submit) are wired: pointer transitions, the
	// double-click gesture, wheel, keyboard, and focus changes.
	onMouseEnter  func()
	onMouseLeave  func()
	onDoubleClick func()
	// onWheel receives the scroll deltas; returning true consumes the event
	// so an enclosing ScrollView does NOT also scroll (the effect of calling
	// preventDefault on a DOM wheel handler).
	onWheel func(dx, dy float32) bool
	// onKeyDown / onKeyUp receive the raw key event; returning true consumes
	// it. Keys reach an element two ways: it holds focus itself (declaring a
	// handler makes it focusable — see El.Focusable), or the event bubbles up
	// from a focused descendant, which is how container-level shortcuts work.
	onKeyDown func(qui.KeyEvent) bool
	onKeyUp   func(qui.KeyEvent) bool
	onFocus   func()
	onBlur    func()
	// autofocused latches the one-shot `autofocus` attribute: the first
	// layout with a window posts the focus move, later layouts don't.
	autofocused bool
	// lastClickAt times the previous left release for double-click
	// synthesis (root reports no click count).
	lastClickAt time.Time
	lastClickX  float32
	lastClickY  float32

	// textStateActive is set when a :hover/:focus/:active variant changes
	// the text color or decoration, so Draw swaps the label/marker/icon
	// typography to match the box's current interactive state.
	textStateActive bool
	// Background-image: url() bitmap, cached by source URL (applyBackgroundImage).
	bgImg    image.Image
	bgImgSrc string
	// ::before / ::after generated content (text + computed style), resolved
	// each restyle from the stylesheet's pseudo-element rules.
	pbText, paText string
	pbCS, paCS     *ComputedStyle
	// hidden is set when computed visibility is hidden — the element keeps its
	// layout box (unlike display:none) but paints nothing (self + subtree).
	// This is the RESTING value; Draw resolves the effective visibility per
	// frame (effectiveHidden) so a :hover / ancestor-:hover variant can flip
	// it — e.g. `.row:hover .del { visibility: visible }` reveals the delete
	// icon while the row is hovered, and hides it again on leave.
	hidden bool
	// displayNone is set when the computed `display` is none — the element
	// renders nothing and measures to zero (runtime toggle; the static
	// Render path also prunes display:none subtrees at compile time).
	displayNone bool
	// stateTriggers are the specific elements whose interactive state
	// (hover / focus-within / press) activates this element's ancestor-state
	// styling — `.row:hover .del` → the .row, `.a:hover ~ .b` → the .a.
	// Scoping to the elements the selector names — instead of "any hovered
	// ancestor" — keeps a shared container (`.list`, the window root) that
	// carries no `:hover` from revealing every sibling at once. Rebuilt each
	// restyle by linkAncestorState.
	stateTriggers []stateTrigger
	// ancestorVariants caches the computed style for each combination of
	// currently-active triggers (keyed by triggerMask), so hovering trigger A
	// applies exactly the rules A activates — not the union of every
	// ancestor-state rule. Lazily filled at draw time (one cascade per
	// distinct combination, usually one); reset each restyle.
	ancestorVariants map[uint32]*ancestorVariant

	lastCS *ComputedStyle
}

// stateTrigger is one element whose interactive state drives another
// element's ancestor-state styling, with the states that apply.
type stateTrigger struct {
	el                   *El
	hover, focus, active bool
}

// maxStateTriggers caps recorded triggers per element so the triggerMask
// (3 bits per trigger) fits its uint32. Real selectors name one or two.
const maxStateTriggers = 10

// ancestorVariant is one cached ancestor-state style: the computed variant
// for a specific set of active triggers plus its box-decoration override
// (nil when the variant changes no decoration).
type ancestorVariant struct {
	cs  *ComputedStyle
	box *qui.Style
}

// newEl constructs a live element for the given tag, wired to an engine.
func newEl(tag string, engine *StyleEngine) *El {
	e := &El{
		tag:                tag,
		attrs:              map[string]string{},
		engine:             engine,
		node:               &Node{Type: ElementNode, Tag: tag, Attrs: map[string]string{}},
		selectedIdx:        -1,
		defaultSelectedIdx: -1,
	}
	e.BaseWidget = qui.NewBaseWidget()
	e.LayoutEngine = qui.FlowLayout{}
	e.SetSelf(e)
	engine.register(e)
	return e
}

// newTextEl constructs an anonymous text-segment element — the live
// counterpart of a DOM text node between element children (static
// render path). It matches no selectors and inherits all typography
// from its parent.
func newTextEl(text string, engine *StyleEngine) *El {
	e := newEl("#text", engine)
	e.text = text
	e.node.Type = TextNode
	e.node.Text = text
	return e
}

// isTextSeg reports whether this element is an anonymous text segment.
func (e *El) isTextSeg() bool { return e.tag == "#text" }

// Tag returns the element's tag name.
func (e *El) Tag() string { return e.tag }

// ShrinkToFitWidth reports that an auto-width <table> is only as wide as its
// content (CSS table shrink-to-fit) rather than filling its flow band like a
// normal block. A table with a declared width fills/uses that width instead.
func (e *El) ShrinkToFitWidth() bool {
	return e.lastCS != nil && e.lastCS.Display == "table" && !e.lastCS.HasWidth
}

// --- mutators used by the reactive host contract ---

// SetElementChildren replaces the element-child list (called by the
// reconciler's SetChildren hook), mirrors them into the backing node tree
// for selector matching, attaches the widgets, then marks a restyle.
func (e *El) SetElementChildren(kids []qui.Widget) {
	e.elementKids = kids
	e.flowKids = nil // stale runs may reference removed children; restyle rebuilds
	e.node.Children = e.node.Children[:0]
	for _, k := range kids {
		if ke, ok := k.(*El); ok {
			ke.node.Parent = e.node
			ke.elParent = e
			e.node.Children = append(e.node.Children, ke.node)
			// This El is now a child of e, so it's no longer a restyle
			// root — its style comes from e's subtree walk.
			if e.engine != nil {
				e.engine.unregister(ke)
			}
		}
	}
	e.syncChildren()
	e.markDirty()
}

// SetClass sets the class attribute (drives .class selectors).
func (e *El) SetClass(class string) { e.setAttr("class", class) }

// SetElementID sets the id attribute and the widget's AX id.
func (e *El) SetElementID(id string) {
	e.SetID(id) // BaseWidget AX id + Window.Find("#id")
	e.setAttr("id", id)
}

// SetAttr sets an arbitrary attribute (href, type, data-*, …).
func (e *El) SetAttr(k, v string) { e.setAttr(k, v) }

func (e *El) setAttr(k, v string) {
	// Presence-aware equality: a missing key and an empty value must NOT
	// compare equal, or boolean attributes (disabled, switch, checked)
	// could never be set to their canonical "" value.
	if old, ok := e.attrs[k]; ok && old == v {
		return
	}
	e.storeAttr(k, v)
	e.node.Attrs[k] = v
	// The `disabled` attribute is state that both the backing control and the
	// :disabled selector must see. markDirty (below) relinks :disabled;
	// propagate to the widget here so the control also enables/disables.
	if k == "disabled" {
		e.applyDisabledState(true)
	}
	e.markDirty()
}

// Attr returns an attribute's current value and whether it is present.
// Presence matters for boolean attributes (`disabled`), whose canonical
// value is the empty string.
func (e *El) Attr(k string) (string, bool) { return e.readAttr(k) }

// storeAttr / dropAttr / readAttr are the guarded accessors for the attrs
// map (see attrMu). The lock is held around the map operation ONLY — the
// callbacks a write triggers (applyDisabledState, markDirty) read
// attributes themselves and would deadlock inside it.

func (e *El) storeAttr(k, v string) {
	e.attrMu.Lock()
	e.attrs[k] = v
	e.attrMu.Unlock()
}

func (e *El) dropAttr(k string) {
	e.attrMu.Lock()
	delete(e.attrs, k)
	e.attrMu.Unlock()
}

func (e *El) readAttr(k string) (string, bool) {
	e.attrMu.RLock()
	v, ok := e.attrs[k]
	e.attrMu.RUnlock()
	return v, ok
}

// attrSnapshot copies the attributes for a reader on another goroutine (the
// DOM inspector), which must not iterate the live map.
func (e *El) attrSnapshot() map[string]string {
	e.attrMu.RLock()
	defer e.attrMu.RUnlock()
	out := make(map[string]string, len(e.attrs))
	for k, v := range e.attrs {
		out[k] = v
	}
	return out
}

// RemoveAttr deletes an attribute, relinking attribute-gated CSS (and the
// backing control's enabled state for `disabled`).
func (e *El) RemoveAttr(k string) {
	if _, ok := e.attrs[k]; !ok {
		return
	}
	e.dropAttr(k)
	if e.node != nil {
		delete(e.node.Attrs, k)
	}
	if k == "disabled" {
		e.applyDisabledState(true)
	}
	e.markDirty()
}

// SetManagedAttrs replaces the set of attributes owned by a DECLARATIVE
// caller (the reactive/html DSL): keys present in attrs are set, and keys
// this element previously received from the same caller but that are now
// absent are removed.
//
// Plain SetAttr can only add. A declarative layer that re-renders
// `disabled` (or any conditional aria-*) with SetAttr alone would set the
// attribute on the first render that needs it and never take it off again —
// a toolbar button greys out once and stays grey for the process lifetime.
// Attributes set through other paths (parsed markup, SetAttr, internal
// `checked`) are untouched.
func (e *El) SetManagedAttrs(attrs map[string]string) {
	for _, k := range e.managedAttrs {
		if _, still := attrs[k]; !still {
			e.RemoveAttr(k)
		}
	}
	e.managedAttrs = e.managedAttrs[:0]
	for k, v := range attrs {
		e.managedAttrs = append(e.managedAttrs, k)
		e.setAttr(k, v)
	}
}

// setCheckedState syncs the checked flag plus the `checked` attribute that
// the selector cascade reads for :checked, then requests a scoped restyle so
// :checked-gated rules relink live. It does NOT fire onToggle (callers do).
func (e *El) setCheckedState(on bool) {
	e.checked = on
	if on {
		e.storeAttr("checked", "")
		if e.node != nil {
			e.node.Attrs["checked"] = ""
		}
	} else {
		e.dropAttr("checked")
		if e.node != nil {
			delete(e.node.Attrs, "checked")
		}
	}
	e.markDirty()
}

// SetDisabled toggles the HTML `disabled` attribute at runtime: it disables
// the backing control and relinks :disabled-gated CSS via a scoped restyle.
func (e *El) SetDisabled(v bool) {
	if _, has := e.attrs["disabled"]; has == v {
		return
	}
	if v {
		e.storeAttr("disabled", "")
		if e.node != nil {
			e.node.Attrs["disabled"] = ""
		}
	} else {
		e.dropAttr("disabled")
		if e.node != nil {
			delete(e.node.Attrs, "disabled")
		}
	}
	e.applyDisabledState(false)
	e.markDirty()
}

// applyDisabledState pushes the current `disabled` attribute onto the
// element itself AND onto its backing control, if it has one. When ensure is
// true the backing is created first (used from setAttr on an already-built
// element).
//
// The element's own Enabled flag matters for tags that have no backing
// control — a `<button>` is just the El. Without it `disabled` would be
// purely cosmetic: the CSS greys the button out while its click handler
// still fires, and the AX tree reports it as perfectly actionable.
func (e *El) applyDisabledState(ensure bool) {
	_, off := e.attrs["disabled"]
	e.SetEnabled(!off)
	if ensure {
		e.ensureBacking()
	}
	if e.backing == nil {
		return
	}
	if en, ok := e.backing.(interface{ SetEnabled(bool) }); ok {
		en.SetEnabled(!off)
	}
}

// TextContent returns the element's own text content (DOM textContent for
// a text-leaf element), excluding descendants' text.
func (e *El) TextContent() string { return e.text }

// SetTextContent sets the element's text content (for text-leaf elements),
// the DOM `textContent` setter. The label updates immediately for
// responsiveness; a restyle re-applies typography (and any text-transform)
// on the next pass.
//
// Deliberately NOT named SetText: that name would make every element
// structurally satisfy qui.TextSink, and the agent layer's Type action
// would then "type" into any div by replacing its markup content —
// injecting a phantom text line that shifts the page it was meant to
// inspect. Text actions reach controls through TextTarget instead.
func (e *El) SetTextContent(s string) {
	if e.text == s {
		return
	}
	e.text = s
	if e.node.Type == TextNode {
		e.node.Text = s
	}
	if e.textLabel != nil {
		e.textLabel.SetText(s)
	}
	e.syncChildren()
	e.markDirty()
}

// SetOnClick installs (or clears) the click handler.
func (e *El) SetOnClick(fn func()) { e.onClick = fn }

// SetOnClickMods installs (or clears) a click handler that receives the
// keyboard modifiers held at click time. Use it for rows that support
// Shift-extend / Cmd-toggle selection; SetOnClick stays the right choice
// when the gesture has one meaning.
func (e *El) SetOnClickMods(fn func(mods qui.Modifiers)) { e.onClickMods = fn }

// SetOnContextMenu installs (or clears) the right-click handler; it
// receives the window-space cursor position for anchoring a menu.
func (e *El) SetOnContextMenu(fn func(x, y float32)) { e.onContextMenu = fn }

// SetOnMouseEnter / SetOnMouseLeave install pointer-transition handlers.
// They fire once per crossing (the window synthesizes them from hit-path
// diffs, DOM mouseenter/mouseleave semantics — no bubbling), so they are the
// right hook for hover logic that has to DO something rather than restyle;
// pure appearance changes belong in a `:hover` rule.
func (e *El) SetOnMouseEnter(fn func()) { e.onMouseEnter = fn }

// SetOnMouseLeave installs the leave counterpart of SetOnMouseEnter.
func (e *El) SetOnMouseLeave(fn func()) { e.onMouseLeave = fn }

// SetOnDoubleClick installs the double-click handler. Like the DOM, the
// single-click handler still fires for each of the two clicks; the second
// release also fires this one.
func (e *El) SetOnDoubleClick(fn func()) { e.onDoubleClick = fn }

// SetOnWheel installs a scroll-wheel handler receiving the deltas. Return
// true to consume the event so an enclosing ScrollView does not also scroll.
func (e *El) SetOnWheel(fn func(dx, dy float32) bool) { e.onWheel = fn }

// SetOnKeyDown installs a key-press handler; return true to consume the key.
// Declaring one makes the element focusable, so it can receive keys directly
// once clicked or tabbed to. Keys from a focused DESCENDANT also bubble
// through here — that is how a dialog installs Esc / Cmd+Enter shortcuts
// without stealing focus from its own text fields.
func (e *El) SetOnKeyDown(fn func(qui.KeyEvent) bool) { e.onKeyDown = fn }

// SetOnKeyUp is the release counterpart of SetOnKeyDown.
func (e *El) SetOnKeyUp(fn func(qui.KeyEvent) bool) { e.onKeyUp = fn }

// SetOnFocus / SetOnBlur fire when this element gains or loses keyboard
// focus. For a form element the focus lives on the backing control, so the
// callbacks track that control; for a plain box they track the box (which
// needs to be focusable — a `:focus` rule or a key handler).
func (e *El) SetOnFocus(fn func()) { e.onFocus = fn }

// SetOnBlur installs the blur counterpart of SetOnFocus.
func (e *El) SetOnBlur(fn func()) { e.onBlur = fn }

// Focusable reports whether the element can take keyboard focus. On top of
// Box's rule (a `:focus` style opts in) an element with a key handler is
// focusable — otherwise installing SetOnKeyDown on a div would silently
// never fire.
//
// A push button (<button>, <input type=submit|reset|button>) is always
// focusable, so a keyboard user can Tab to it and press it with Enter or
// Space — but see FocusOnClick.
func (e *El) Focusable() bool {
	if e.onKeyDown != nil || e.onKeyUp != nil || e.isPushButton() {
		return e.Enabled()
	}
	return e.Box.Focusable()
}

// FocusOnClick implements qui.ClickFocusPolicy. A push button without an
// author `:focus` rule is Tab-reachable but a click leaves focus where it
// was — the platform button convention, which keeps a text field focused
// while a toolbar button acts on it. An author `:focus` rule opts back
// into click focus.
func (e *El) FocusOnClick() bool {
	return !e.isPushButton() || e.Box.Focus != nil
}

// isPushButton reports a <button> or a push-button <input>.
func (e *El) isPushButton() bool { return e.tag == "button" || e.isButtonInput() }

// RequestFocus is the DOM's focus(), named to stay clear of the embedded
// Box.Focus style field. It moves keyboard focus to the control that owns
// the editing for a form element (input, textarea, select, checkbox,
// range), otherwise to the element itself, and reports whether focus
// landed — false when the element is detached or not focusable.
func (e *El) RequestFocus() bool {
	win := e.Window()
	if win == nil {
		return false
	}
	var target qui.Widget = e
	if f, ok := e.backing.(interface{ Focusable() bool }); ok && f.Focusable() {
		target = e.backing
	}
	win.SetFocus(target)
	return win.Focused() == target
}

// SetFocused fires the focus / blur callbacks around Box's bookkeeping.
// Called by the window's focus machinery.
func (e *El) SetFocused(focused bool) {
	changed := e.Focused() != focused
	e.Box.SetFocused(focused)
	if !changed {
		return
	}
	if focused {
		if e.onFocus != nil {
			e.onFocus()
		}
		return
	}
	if e.onBlur != nil {
		e.onBlur()
	}
}

// SetIcon renders src as this element's content (an svgImage leaf). No-op
// when the source is unchanged so a re-render doesn't force a restyle.
func (e *El) SetIcon(src qui.VectorSource) {
	if e.iconSrc == src {
		return
	}
	e.iconSrc = src
	e.markDirty()
}

// SetCanvasDraw wires a paint callback as the content of a <canvas>
// element — the "draw it yourself" hook the engine offers in place of a
// browser's 2D context. The callback receives the frame canvas and the
// canvas element's layout bounds each time it paints. The canvas sizes to
// its CSS width/height (HTML's 300×150 default otherwise); call
// Invalidate() on the element to request a repaint for animated content.
// A no-op on a non-<canvas> element.
func (e *El) SetCanvasDraw(fn func(cv qui.Canvas, bounds qui.Rect)) {
	if !e.isCanvas() {
		return
	}
	e.canvasDraw = fn
	e.canvasUser = nil
	e.markDirty()
}

// SetCanvas plugs an arbitrary widget in as a <canvas> element's content —
// use it when the drawing needs its own event handling or animation (a
// custom BaseWidget, a scene3d.Viewport, a graphs chart, …). The widget is
// sized to the canvas element's CSS width/height. A no-op on a
// non-<canvas> element.
func (e *El) SetCanvas(w qui.Widget) {
	if !e.isCanvas() {
		return
	}
	e.canvasUser = w
	e.canvasDraw = nil
	e.markDirty()
}

// SetDraggable marks (or unmarks) the element as a drag source.
func (e *El) SetDraggable(b bool) { e.draggable = b }

// SetDragHandle marks the element as a drag handle: only pressing it starts
// the drag (make the row itself non-draggable), and the drag ghost (dim +
// follow) applies to the handle's PARENT row rather than the small handle.
func (e *El) SetDragHandle(b bool) { e.dragHandle = b }

// SetDragKey sets the opaque identifier a drop target reads from this
// element when it is the drag source.
func (e *El) SetDragKey(k string) { e.dragKey = k }

// DragAxis picks the axis drag feedback follows: how the ghost moves under
// the cursor, and which midpoint decides OnDragOver's `after`.
type DragAxis int

const (
	// DragAxisVertical is the default: a stacked list. The ghost follows Y
	// and `after` means "below this element's middle".
	DragAxisVertical DragAxis = iota
	// DragAxisHorizontal is a left-to-right strip (sheet tabs). The ghost
	// follows X and `after` means "past this element's right half".
	DragAxisHorizontal
	// DragAxisFree is a WRAPPED grid (a wall of thumbnails). The ghost
	// follows the cursor on both axes, because the drag genuinely moves in
	// two dimensions, while `after` still reads the horizontal midpoint:
	// a wrapped grid is one linear sequence laid out in reading order, so
	// the insertion point is always "before or after the cell under the
	// cursor". Rows are crossed by moving onto a cell in another row, not
	// by a separate vertical test.
	DragAxisFree
)

// SetDragAxis picks which axis this element's drag feedback follows. Set it
// on both the drag source (ghost follow) and the drop targets (`after`).
func (e *El) SetDragAxis(a DragAxis) { e.dragAxis = a }

// DragAxis reports the element's drag axis.
func (e *El) DragAxis() DragAxis { return e.dragAxis }

// SetDragAxisHorizontal switches the drag ghost + drop-side test to the
// horizontal axis (for left-to-right strips such as tabs). Default is
// vertical. Shorthand for SetDragAxis.
func (e *El) SetDragAxisHorizontal(b bool) {
	if b {
		e.dragAxis = DragAxisHorizontal
		return
	}
	e.dragAxis = DragAxisVertical
}

// SetOnDrop installs a drop handler; a non-nil handler makes the element a
// drop target. The handler receives the dragged source's DragKey.
func (e *El) SetOnDrop(fn func(sourceKey string)) { e.onDrop = fn }

// SetOnDragOver installs a drag-over handler (fires while a drag hovers
// this element); a non-nil handler also makes the element a drop target.
// Use it to show a drop indicator. Receives the dragged source's DragKey
// and whether the cursor is past this element's vertical midpoint (after),
// so a list can decide insert-before vs insert-after.
func (e *El) SetOnDragOver(fn func(sourceKey string, after bool)) { e.onDragOver = fn }

// SetOnDragEnd installs a handler fired when THIS element's own drag ends
// (dropped or cancelled) — the place to clear any drop indicator.
func (e *El) SetOnDragEnd(fn func()) { e.onDragEnd = fn }

// SetOnSubmit installs the Enter handler for an <input>.
func (e *El) SetOnSubmit(fn func(string)) {
	e.onSubmit = fn
	if in, ok := e.backing.(*widgets.Input); ok {
		in.OnSubmit = fn
	}
}

// SetOnCommit installs the "value finished" handler for an <input>: Enter, or
// focus leaving the field after an edit (widgets.Input.OnCommit). Use it
// instead of SetOnInput for a field whose value is expensive to apply per
// keystroke — a size in cm, a count, anything that pushes an undo step.
func (e *El) SetOnCommit(fn func(string)) {
	e.onCommit = fn
	if in, ok := e.backing.(*widgets.Input); ok {
		in.OnCommit = fn
	}
}

// Draggable reports whether this element is a drag source (qui.Draggable).
func (e *El) Draggable() bool { return e.draggable }

// Droppable reports whether this element accepts drops (qui.Droppable) —
// true when it has a drop or a drag-over handler.
func (e *El) Droppable() bool { return e.onDrop != nil || e.onDragOver != nil }

// DragKey returns the element's drag identifier.
func (e *El) DragKey() string { return e.dragKey }

// Unmount deregisters the element from its engine's restyle roots and
// detaches it from its element parent. The reactive host's Destroy hook
// calls this so a removed portal/dialog root stops being walked and a
// removed child stops resolving a restyle scope through a stale parent.
func (e *El) Unmount() {
	e.unlinkStyleParent()
	if e.engine != nil {
		e.engine.unregister(e)
		// Drop state-dependency links in both roles: as a dependent (its
		// triggers stop invalidating it) and as a trigger (its dependents'
		// entries are relinked on their next restyle; the reverse map entry
		// must not pin the unmounted element).
		e.engine.removeStateDep(e)
		delete(e.engine.stateDeps, e)
	}
	e.elParent = nil
}

// SetStyleParent makes this top-level element — a portal's content,
// mounted into the overlay stack outside the tree — inherit from, and
// match ancestor selectors through, the nearest element at or above w:
// `.app.dark .q-dialog` reaches a dialog opened inside .app, and the
// dialog inherits .app's custom properties, color and font. Sibling
// selectors still see it as an only child. The reactive reconciler calls
// it with the host the portal was declared under. Nil, or an element of
// another style engine, unlinks. No-op for an element with a real parent.
func (e *El) SetStyleParent(w qui.Widget) {
	var p *El
	for cur := w; cur != nil; cur = cur.Parent() {
		if el, ok := cur.(*El); ok {
			p = el
			break
		}
	}
	if p == e || (p != nil && p.engine != e.engine) {
		p = nil
	}
	if p == e.styleParent || e.elParent != nil {
		return
	}
	e.unlinkStyleParent()
	if p != nil {
		e.styleParent = p
		e.node.Parent = p.node
		e.node.StyleOnlyParent = true
		if p.styleKids == nil {
			p.styleKids = map[*El]bool{}
		}
		p.styleKids[e] = true
	}
	e.markDirty()
}

// unlinkStyleParent drops the SetStyleParent link in both directions.
func (e *El) unlinkStyleParent() {
	if e.styleParent == nil {
		return
	}
	delete(e.styleParent.styleKids, e)
	e.styleParent = nil
	if e.node.StyleOnlyParent {
		e.node.Parent = nil
		e.node.StyleOnlyParent = false
	}
}

// inheritParent is the element this one inherits computed style from: its
// tree parent, or for a portal root its style parent.
func (e *El) inheritParent() *El {
	if e.elParent != nil {
		return e.elParent
	}
	return e.styleParent
}

// restyleScope resolves the subtree root a change to this element can
// affect: its parent element (covers self, descendants, and following-
// sibling subtrees — see StyleEngine), or itself when it is a top-level
// restyle root (main root, portal/dialog root, or a freshly created
// element not yet adopted). Detached elements return nil — they get
// styled when adoption walks them.
func (e *El) restyleScope() *El {
	// :has() is a parent-facing selector: a class/attribute/child mutation in
	// this subtree may change the computed style of ANY ancestor. Rewalk the
	// isolated document/portal root so all of those ancestors (and inherited
	// descendants) are recomputed correctly. The common case, without :has,
	// retains the narrower parent scope below.
	if e.engine != nil && e.engine.sheet != nil && e.engine.sheet.HasHas {
		root := e
		for root.elParent != nil {
			root = root.elParent
		}
		if e.engine.roots[root] {
			return root
		}
	}
	if e.elParent != nil {
		return e.elParent
	}
	if e.engine != nil && e.engine.roots[e] {
		return e
	}
	return nil
}

// isControl reports whether the tag renders through a backing editing
// widget (leaf form control) rather than the box/text path.
func (e *El) isControl() bool {
	switch e.tag {
	case "textarea", "select":
		return true
	case "input":
		// Push-button input types (submit/reset/button), the file picker, and
		// the color swatch render like <button> through the box/text path,
		// not an editable field.
		return !e.isButtonInput() && !e.isFileInput() && !e.isColorInput()
	}
	return false
}

// isControlSurface reports whether the element acts as a clickable control
// face rather than prose: a <label for=…> that toggles/focuses its target,
// or a push-button / file-picker input. Their captions stay out of
// drag-selection even without `user-select: none`.
func (e *El) isControlSurface() bool {
	if e.tag == "label" && strings.TrimSpace(e.attrs["for"]) != "" {
		return true
	}
	return e.isButtonInput() || e.isFileInput()
}

// isFileInput reports whether this is an <input type=file>, which renders
// as a push button that opens the native file dialog on click.
func (e *El) isFileInput() bool {
	return e.tag == "input" && e.attrs["type"] == "file"
}

// isColorInput reports whether this is an <input type=color>, rendered as a
// filled swatch button that opens an in-app color palette popup on click.
func (e *El) isColorInput() bool {
	return e.tag == "input" && e.attrs["type"] == "color"
}

// currentColor returns the swatch's current value, defaulting to CSS black
// (the HTML default for a color input with no value).
func (e *El) currentColor() string {
	if e.colorValue != "" {
		return e.colorValue
	}
	if v := strings.TrimSpace(e.attrs["value"]); v != "" {
		return v
	}
	return "#000000"
}

// fileInputLabel is the visible label for a file picker: the chosen file's
// base name, or a "no file" placeholder.
func (e *El) fileInputLabel() string {
	if e.fileValue == "" {
		return "Choose File…"
	}
	if strings.Contains(e.fileValue, string(filepath.ListSeparator)) {
		parts := strings.Split(e.fileValue, string(filepath.ListSeparator))
		return filepath.Base(parts[0]) + " (+" + strconv.Itoa(len(parts)-1) + ")"
	}
	return filepath.Base(e.fileValue)
}

// openFileDialog spins the native OS file panel and, on a successful pick,
// stores the path(s), updates the button label (via restyle), and fires
// the onInput callback with the chosen path. Blocking is fine — it runs
// inside the click handler on the main thread, the same as the filedialog
// example, and the OS panel is modal. On platforms without a dialog
// backend (qui.ErrDialogNotSupported) or on cancel it is a no-op.
func (e *El) openFileDialog() {
	opts := qui.OpenFileOptions{AllowedExtensions: acceptExtensions(e.attrs["accept"])}
	var value string
	if _, multiple := e.attrs["multiple"]; multiple {
		paths, err := qui.OpenFiles(opts)
		if err != nil || len(paths) == 0 {
			return
		}
		value = strings.Join(paths, string(filepath.ListSeparator))
	} else {
		path, err := qui.OpenFile(opts)
		if err != nil || path == "" {
			return
		}
		value = path
	}
	e.fileValue = value
	e.markDirty()
	if e.onInput != nil {
		e.onInput(value)
	}
}

// colorHueBases are the vivid "standard colors" row of the palette — one
// base per column. Each column's tint/shade ramp is derived from its base
// (mix toward white for tints, toward black for shades), mirroring the
// Google Docs color matrix.
var colorHueBases = []string{
	"#980000", "#ff0000", "#ff9900", "#ffff00", "#00ff00",
	"#00ffff", "#4a86e8", "#0000ff", "#9900ff", "#ff00ff",
}

// colorTintMix / colorShadeMix are the per-row blend fractions applied to
// each column base: tints lerp toward white (light rows, top), shades lerp
// toward black (dark rows, bottom).
var (
	colorTintMix  = []float32{0.80, 0.60, 0.40, 0.20}
	colorShadeMix = []float32{0.25, 0.50, 0.70}
)

// colorGridRows builds the palette matrix as rows of hex strings: a
// grayscale ramp, the vivid base row, then light→dark tint/shade rows.
func colorGridRows() [][]string {
	cols := len(colorHueBases)
	rows := make([][]string, 0, 1+1+len(colorTintMix)+len(colorShadeMix))

	// Grayscale ramp black → white, one cell per column.
	gray := make([]string, cols)
	for i := 0; i < cols; i++ {
		v := float32(i) / float32(cols-1)
		gray[i] = colorToHex(qui.Color{R: v, G: v, B: v, A: 1})
	}
	rows = append(rows, gray)

	// Vivid base row.
	rows = append(rows, append([]string(nil), colorHueBases...))

	white := qui.Color{R: 1, G: 1, B: 1, A: 1}
	black := qui.Color{A: 1}
	for _, mix := range colorTintMix {
		row := make([]string, cols)
		for i, base := range colorHueBases {
			c, _ := parseColor(base)
			row[i] = colorToHex(lerpColor(c, white, mix))
		}
		rows = append(rows, row)
	}
	for _, mix := range colorShadeMix {
		row := make([]string, cols)
		for i, base := range colorHueBases {
			c, _ := parseColor(base)
			row[i] = colorToHex(lerpColor(c, black, mix))
		}
		rows = append(rows, row)
	}
	return rows
}

// lerpColor blends a toward b by t (0 = a, 1 = b), keeping a's alpha.
func lerpColor(a, b qui.Color, t float32) qui.Color {
	return qui.Color{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A,
	}
}

// colorToHex formats an opaque color as "#rrggbb".
func colorToHex(c qui.Color) string {
	to := func(f float32) int {
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		return int(f*255 + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", to(c.R), to(c.G), to(c.B))
}

// openColorPopup shows a Google-Docs-style palette overlay under the
// swatch: a "None" option, a grayscale row, the vivid hue matrix, and a
// hand-written hex field with a live preview. Picking (a swatch, or Enter
// in the hex field) sets the value and closes the popup. Needs a live
// window (the overlay host); a no-op without one.
func (e *El) openColorPopup() {
	win := e.Window()
	if win == nil && e.engine != nil {
		win = e.engine.opts.Window
	}
	if win == nil {
		return
	}
	var popup *widgets.Popup
	pick := func(hex string) {
		e.setColorValue(hex)
		if popup != nil {
			popup.Close()
		}
	}
	content := colorPaletteContent(e.currentColor(), pick)
	popup = widgets.NewPopup(content)
	x, y := anchoredPopupPos(win.Size(), qui.MeasureConstrained(content, win.Size()), qui.InteractionBoundsOf(e))
	popup.ShowAt(win, x, y)
}

// anchoredPopupPos places a popup of size sz relative to the anchor rect
// within a window of winSize: below the anchor by default, flipped above
// when it would overflow the bottom and there's room above, else pinned
// to the viewport edge so the whole popup stays visible. X is clamped to
// the window width. Mirrors widgets.showAnchoredPopup (unexported).
func anchoredPopupPos(winSize, sz qui.Size, anchor qui.Rect) (x, y float32) {
	const gap = 4
	y = anchor.Y + anchor.H + gap
	switch {
	case y+sz.H <= winSize.H:
		// fits below — keep it.
	case anchor.Y-sz.H-gap >= 0:
		y = anchor.Y - sz.H - gap // flip above
	default:
		// Fits neither side fully: pin so the bottom is visible, then
		// clamp the top on-screen (tall popup in a short window).
		y = winSize.H - sz.H
		if y < 0 {
			y = 0
		}
	}
	x = anchor.X
	if x+sz.W > winSize.W {
		x = winSize.W - sz.W
	}
	if x < 0 {
		x = 0
	}
	return x, y
}

// colorPaletteContent builds the Google-Docs-style palette body: a "None"
// row, a grayscale row, the vivid hue matrix, and a hand-written hex field
// with a live preview swatch. pick(hex) is invoked on a swatch click or on
// Enter in the hex field; current seeds the hex field + preview. Kept
// window-free so it is unit-testable and renderable on its own.
func colorPaletteContent(current string, pick func(hex string)) *widgets.Box {
	// scale shrinks the whole picker uniformly (30% smaller than the base
	// geometry). All sizes — cells, gaps, padding, corner radius, and the
	// two text rows' font — derive from it.
	const (
		scale    = 0.7
		cell     = 24 * scale
		gap      = 6 * scale
		pad      = 12 * scale
		fontSize = 14 * scale
		boxRad   = 8 * scale
	)
	cols := len(colorHueBases)
	theme := qui.CurrentTheme()

	// A circular swatch button. The fill IS the color, so hover/press/focus
	// must NOT recolor it (widgets.NewButton's default states darken the
	// background — that would misrepresent the swatch). Rebuild every state
	// from the same base fill, differing only by a stronger border ring.
	swatch := func(c qui.Color, hex string) *widgets.Button {
		btn := widgets.NewButton("", nil)
		btn.Elevation, btn.HoverElevation = 0, 0
		btn.StateLayerColor = qui.Color{} // no hover/focus tint overlay
		base := *btn.Style()
		base.Background = c
		base.Width, base.Height = cell, cell
		base.Radius = cell / 2
		base.Border, base.BorderSize = qui.Color{A: 0.22}, 1
		btn.States.Base = base
		btn.States.Hover = qui.StyleWith(base, qui.Border(qui.Color{A: 0.6}, 2))
		btn.States.Pressed = qui.StyleWith(base, qui.Border(qui.Color{A: 0.85}, 2))
		btn.States.Focused = qui.StyleWith(base, qui.Border(qui.Color{A: 0.6}, 2))
		btn.States.Disabled = nil
		h := hex
		btn.OnClick = func() { pick(h) }
		return btn
	}
	rowBox := func(children ...qui.Widget) *widgets.Box {
		return widgets.NewBox(qui.FlexLayout{Direction: qui.Horizontal, Gap: gap, AlignItems: qui.AlignCenter}, children...)
	}

	var sections []qui.Widget

	// "None" row: a bordered white circle + label, clears the color.
	none := swatch(qui.Color{R: 1, G: 1, B: 1, A: 1}, "transparent")
	noneLabel := widgets.NewLabel("None")
	noneLabel.Style().Foreground = theme.Text
	noneLabel.Style().Font.Size = fontSize
	sections = append(sections, rowBox(none, noneLabel))

	// Palette matrix (grayscale row + vivid row + tint/shade rows).
	for _, row := range colorGridRows() {
		cells := make([]qui.Widget, 0, len(row))
		for _, hex := range row {
			c, ok := parseColor(hex)
			if !ok {
				continue
			}
			cells = append(cells, swatch(c, hex))
		}
		sections = append(sections, rowBox(cells...))
	}

	// Hand-written hex row: an input with a live preview swatch. Typing
	// updates the preview; Enter applies the value.
	preview := widgets.NewBox(qui.FlowLayout{})
	preview.Style().Width, preview.Style().Height = cell, cell
	preview.Style().Radius = cell / 2
	preview.Style().Border = qui.Color{A: 0.22}
	preview.Style().BorderSize = 1
	if c, ok := parseColor(current); ok {
		preview.Style().Background = c
	}
	hexInput := widgets.NewInput("#RRGGBB")
	hexInput.SetText(current)
	hexInput.Style().Font.Size = fontSize
	hexInput.Style().Width = float32(cols)*cell + float32(cols-1)*gap - cell - gap
	hexInput.OnChange = func(s string) {
		if c, ok := parseColor(strings.TrimSpace(s)); ok {
			preview.Style().Background = c
			preview.Invalidate()
		}
	}
	hexInput.OnSubmit = func(s string) {
		if _, ok := parseColor(strings.TrimSpace(s)); ok {
			pick(strings.TrimSpace(s))
		}
	}
	sections = append(sections, rowBox(hexInput, preview))

	content := widgets.NewBox(qui.FlexLayout{Direction: qui.Vertical, Gap: gap, AlignItems: qui.AlignStart}, sections...)
	content.Style().Padding = qui.Insets{Top: pad, Right: pad, Bottom: pad, Left: pad}
	content.Style().Background = theme.SurfaceRaised
	content.Style().Radius = boxRad
	return content
}

// setColorValue records a newly picked color, repaints the swatch (via
// restyle), and fires the onInput callback.
func (e *El) setColorValue(hex string) {
	if e.colorValue == hex {
		return
	}
	e.colorValue = hex
	e.markDirty()
	if e.onInput != nil {
		e.onInput(hex)
	}
}

// stepNumber applies one spinner click to an <input type=number>, following
// the HTML stepping rules: move by `step` (default 1), clamp into
// [`min`, `max`], and start from `min` (or 0) when the field is empty or
// unparseable. Fires onInput like a keystroke would, since the value changed.
//
// Kept in htmlcss rather than the widget because step / min / max are HTML
// attribute semantics; widgets.Input only reports which arrow was clicked.
func (e *El) stepNumber(dir int) {
	in, ok := e.backing.(*widgets.Input)
	if !ok {
		return
	}
	step := parseFloatOr(e.attrs["step"], 1)
	if step <= 0 {
		step = 1
	}
	min, hasMin := parseFloatAttr(e.attrs["min"])
	max, hasMax := parseFloatAttr(e.attrs["max"])

	cur, err := strconv.ParseFloat(strings.TrimSpace(in.Text), 64)
	if err != nil {
		// Empty / non-numeric: the first click lands on the range's floor.
		cur = 0
		if hasMin {
			cur = float64(min)
		}
		if dir > 0 {
			cur -= float64(step) // so the += below arrives exactly at the floor
		}
	}
	next := cur + float64(dir)*float64(step)
	if hasMin && next < float64(min) {
		next = float64(min)
	}
	if hasMax && next > float64(max) {
		next = float64(max)
	}
	text := strconv.FormatFloat(next, 'f', -1, 64)
	if text == in.Text {
		return
	}
	in.SetText(text) // fires OnChange → e.onInput
}

// parseFloatAttr parses an optional numeric attribute, reporting whether it
// was present and valid.
func parseFloatAttr(v string) (float32, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 32)
	if err != nil {
		return 0, false
	}
	return float32(f), true
}

// setColorValueSilently records a color without firing onInput — for
// programmatic changes (a form reset), where a browser fires no input event.
func (e *El) setColorValueSilently(hex string) {
	if e.colorValue == hex {
		return
	}
	e.colorValue = hex
	e.markDirty()
}

// acceptExtensions turns an HTML `accept` attribute into a bare extension
// filter for the native dialog. It keeps extension tokens (".png" → "png")
// and drops MIME types / wildcards (`image/*`), which the OS panel can't
// express through AllowedExtensions.
func acceptExtensions(accept string) []string {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return nil
	}
	var exts []string
	for _, tok := range strings.Split(accept, ",") {
		tok = strings.TrimSpace(tok)
		if strings.HasPrefix(tok, ".") {
			if ext := strings.TrimPrefix(tok, "."); ext != "" {
				exts = append(exts, strings.ToLower(ext))
			}
		}
	}
	return exts
}

// isButtonInput reports whether this is an <input type=submit|reset|button>,
// which renders as a push button (label = value attribute) rather than an
// editable text field.
func (e *El) isButtonInput() bool {
	if e.tag != "input" {
		return false
	}
	switch e.attrs["type"] {
	case "submit", "reset", "button":
		return true
	}
	return false
}

// buttonInputLabel returns the visible label for a push-button input: the
// `value` attribute, or a type-appropriate default ("Submit"/"Reset").
func buttonInputLabel(attrs map[string]string) string {
	if v := collapseText(attrs["value"]); v != "" {
		return v
	}
	switch attrs["type"] {
	case "submit":
		return qui.TOr("qui.submit", "Submit")
	case "reset":
		return qui.TOr("qui.reset", "Reset")
	}
	return ""
}

// typeFormatHint returns a placeholder that documents the expected text
// format for input types the engine renders as a plain field but that
// have a canonical shape (no native date/time picker). Empty for types
// with no obvious format.
func typeFormatHint(inputType string) string {
	switch inputType {
	case "date":
		return "YYYY-MM-DD"
	case "time":
		return "HH:MM"
	case "datetime-local":
		return "YYYY-MM-DDTHH:MM"
	case "month":
		return "YYYY-MM"
	case "week":
		return "YYYY-Www"
	}
	return ""
}

// ensureBacking lazily creates the control widget for a form tag, wiring
// the stored callbacks/state. <select> needs a live *Window (its dropdown
// is an overlay); it is created once the element is window-attached.
func (e *El) ensureBacking() {
	if e.backing != nil || !e.isControl() {
		return
	}
	switch e.tag {
	case "textarea":
		ta := widgets.NewTextArea(e.attrs["placeholder"])
		ta.OnChange = e.onInput
		if v, ok := e.attrs["value"]; ok {
			ta.SetText(v)
		}
		e.backing = ta
	case "select":
		win := e.Window()
		if win == nil && e.engine != nil {
			win = e.engine.opts.Window
		}
		if win == nil {
			return // created on the next restyle once attached
		}
		sel := widgets.NewSelect(win, e.selectItems, e.onSelect)
		sel.SetItemDisabled(e.selectDisabled)
		sel.SelectedIdx = e.selectedIdx
		e.backing = sel
	case "input":
		switch e.attrs["type"] {
		case "checkbox":
			// The change handler stays an engine wrapper so a user toggle both
			// relinks :checked (setCheckedState → scoped restyle) and fires the
			// author's onToggle, which may be attached later via SetOnToggle.
			onChange := func(on bool) {
				e.setCheckedState(on)
				if e.onToggle != nil {
					e.onToggle(on)
				}
			}
			if _, isSwitch := e.attrs["switch"]; isSwitch {
				// Safari-style `switch` attribute: same checkbox semantics
				// (checked / :checked / form serialization), toggle-switch look.
				sw := widgets.NewSwitch("", onChange)
				sw.On = e.checked
				e.backing = sw
			} else {
				cb := widgets.NewCheckBox("", nil)
				cb.OnChange = onChange
				cb.Checked = e.checked
				e.backing = cb
			}
		case "radio":
			// Radios sharing a `name` form one group (single selection). The
			// engine owns the per-name RadioGroup so members created by
			// separate elements still coordinate. The `value` attribute is
			// the visible label (mirrors how a checkbox uses its value).
			if e.engine != nil {
				e.radioGrp = e.engine.radioGroup(e.attrs["name"], e)
			} else {
				e.radioGrp = widgets.NewRadioGroup()
			}
			rb := widgets.NewRadioButton(e.radioGrp, collapseText(e.attrs["value"]))
			rb.Checked = e.checked
			e.backing = rb
			if e.checked {
				e.radioGrp.Select(rb)
			}
		case "range":
			min, max := parseFloatOr(e.attrs["min"], 0), parseFloatOr(e.attrs["max"], 100)
			val := parseFloatOr(e.attrs["value"], (min+max)/2)
			sl := widgets.NewSlider(min, max, val, func(v float32) {
				if e.onInput != nil {
					e.onInput(strconv.FormatFloat(float64(v), 'g', -1, 32))
				}
			})
			if step := parseFloatOr(e.attrs["step"], 0); step > 0 {
				sl.Step = step
			}
			e.backing = sl
		default:
			// text (default) + text-like types (email/search/tel/url/number/
			// date) render as a text field; password masks its display.
			// A type with a well-known format (date/time) gets a format-hint
			// placeholder when the author didn't supply one.
			placeholder := e.attrs["placeholder"]
			if placeholder == "" {
				placeholder = typeFormatHint(e.attrs["type"])
			}
			in := widgets.NewInput(placeholder)
			// Enter fires the input's own onSubmit, then submits the enclosing
			// <form> (if any) so form-level handlers see the field values.
			in.OnSubmit = func(s string) {
				if e.onSubmit != nil {
					e.onSubmit(s)
				}
				e.maybeSubmitForm()
			}
			in.OnCommit = func(s string) {
				if e.onCommit != nil {
					e.onCommit(s)
				}
			}
			if e.attrs["type"] == "password" {
				in.Password = true
			}
			switch e.attrs["type"] {
			case "search":
				// Browsers put a UA clear button inside a search field once
				// it has content; clearing fires input, not submit.
				in.TrailingButton = widgets.InputTrailingClear
			case "number":
				// UA spinner. The widget reports the direction; min / max /
				// step are HTML semantics, so they stay here.
				in.TrailingButton = widgets.InputTrailingStepper
				in.OnStep = func(dir int) { e.stepNumber(dir) }
			}
			if v, ok := e.attrs["value"]; ok {
				in.SetText(v)
			}
			e.backing = in
		}
	}
	// HTML `disabled` attribute → disable the backing control.
	if e.backing != nil {
		if _, off := e.attrs["disabled"]; off {
			if en, ok := e.backing.(interface{ SetEnabled(bool) }); ok {
				en.SetEnabled(false)
			}
		}
	}
	e.installEditGuards()
	e.bindInputChange()
}

// installEditGuards enforces the HTML `readonly` and `maxlength` attributes
// on a text-entry control.
//
// Both ride on the widgets' BeforeInput veto, which every mutating path
// funnels through (type / delete / cut / paste / IME commit / InsertText /
// Enter) — so this needs no widget-side support, and selection, caret
// movement and copy keep working on a readonly field exactly as in a
// browser. The closure reads e.attrs at event time, so setting or clearing
// the attribute at runtime takes effect immediately.
func (e *El) installEditGuards() {
	guard := func(ev widgets.BeforeInputEvent) bool {
		if _, ro := e.attrs["readonly"]; ro {
			return false // no mutation of any kind
		}
		max, err := strconv.Atoi(strings.TrimSpace(e.attrs["maxlength"]))
		if err != nil || max < 0 {
			return true // absent or invalid → no cap (per HTML)
		}
		switch ev.Kind {
		case "delete", "cut":
			return true // shortening is always allowed
		}
		// The cap counts UTF-16-ish "characters"; runes are the closest
		// meaningful unit here. Replacing a selection frees up its length.
		grown := utf8.RuneCountInString(ev.Value) + utf8.RuneCountInString(ev.Text)
		if a, b := ev.SelStart, ev.SelEnd; a >= 0 && b >= 0 && a != b {
			if a > b {
				a, b = b, a
			}
			grown -= b - a
		}
		return grown <= max
	}
	switch w := e.backing.(type) {
	case *widgets.Input:
		w.BeforeInput = guard
	case *widgets.TextArea:
		w.BeforeInput = guard
	}
}

// SetInputValue sets the value of an <input>/<textarea> without
// re-triggering an edit loop (skips when unchanged).
func (e *El) SetInputValue(s string) {
	// Color inputs store their value on the element, not a backing
	// text widget. Programmatic sets update the swatch silently —
	// onInput fires only for user picks (setColorValue).
	if e.isColorInput() {
		if e.colorValue != s {
			e.colorValue = s
			e.markDirty()
		}
		return
	}
	e.ensureBacking()
	switch w := e.backing.(type) {
	case *widgets.Input:
		if w.GetText() != s {
			w.SetText(s)
		}
	case *widgets.TextArea:
		if w.GetText() != s {
			w.SetText(s)
		}
	case *widgets.Slider:
		// A range input's value is its slider position. Programmatic sets
		// (a bound scrubber following playback) move the thumb silently.
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 32); err == nil {
			if float32(v) != w.Value {
				w.Value = float32(v)
				w.Invalidate()
			}
		}
	}
}

// InputValue returns the current value of an <input>/<textarea> element,
// read from the backing control (or the stored swatch for a color input).
// Empty for elements that carry no text value.
func (e *El) InputValue() string {
	if e.isColorInput() {
		return e.colorValue
	}
	switch w := e.backing.(type) {
	case *widgets.Input:
		return w.GetText()
	case *widgets.TextArea:
		return w.GetText()
	case *widgets.Slider:
		return strconv.FormatFloat(float64(w.Value), 'g', -1, 32)
	}
	return ""
}

// TextTarget implements qui.TextTargetProvider: a form element delegates
// text actions (Type / Focus / key input) to the control that owns the
// editing — the element itself only owns styling, layout and identity. Nil
// for elements with no such control, which is what makes `type` on a plain
// <div> an error instead of a content rewrite.
func (e *El) TextTarget() qui.Widget {
	if e.backing == nil {
		return nil
	}
	switch e.backing.(type) {
	case qui.TextEditable, qui.Optioned:
		// Input / TextArea (text state) and Select (option set) — the same
		// pairing the Type action requires of a TextSink.
		return e.backing
	}
	return nil
}

// SetOnInput sets the input/textarea change handler.
//
// The backing widget keeps the ENGINE's wrapper as its OnChange — never the
// author's function directly — because the engine has its own work to do on
// every value change (re-filtering `<datalist>` autocompletion). Binding fn
// straight onto the widget silently disabled that.
func (e *El) SetOnInput(fn func(string)) {
	e.onInput = fn
	e.bindInputChange()
}

// bindInputChange installs the engine's change wrapper on the backing text
// widget: author handler first (so it sees the value like a DOM input event),
// then the engine's autocomplete refresh.
func (e *El) bindInputChange() {
	onChange := func(s string) {
		if e.onInput != nil {
			e.onInput(s)
		}
		e.refreshSuggestions(s)
	}
	switch w := e.backing.(type) {
	case *widgets.Input:
		w.OnChange = onChange
	case *widgets.TextArea:
		w.OnChange = onChange
	}
}

// SetChecked sets a checkbox / radio state (no-op if unchanged). For a
// radio, selecting it goes through the group so the previously-selected
// peer clears.
func (e *El) SetChecked(v bool) {
	switch w := e.backing.(type) {
	case *widgets.CheckBox:
		if w.Checked != v {
			w.Checked = v
			w.Invalidate()
		}
		e.setCheckedState(v) // sync `checked` attr + relink :checked
		return
	case *widgets.Switch:
		if w.On != v {
			w.On = v
			w.Invalidate()
		}
		e.setCheckedState(v)
		return
	case *widgets.RadioButton:
		if w.Checked == v {
			e.checked = v
			return
		}
		if v && e.radioGrp != nil {
			e.radioGrp.Select(w) // group OnChange relinks every member
		} else {
			w.Checked = v
			w.Invalidate()
			e.setCheckedState(v)
		}
		return
	}
	e.setCheckedState(v) // no backing yet (pre-mount): record + attr
}

// SetOnToggle sets the checkbox change handler. The backing widget's
// OnChange stays the engine wrapper (which relinks :checked, then invokes
// this callback), so there is nothing to rebind on the widget itself.
func (e *El) SetOnToggle(fn func(bool)) {
	e.onToggle = fn
}

// readOptions builds the <select> option model from its `<option>` /
// `<optgroup>` children, honoring the HTML semantics the flat list used to
// drop on the floor:
//
//   - `value` is what the form submits; the option's text is only the LABEL.
//     Missing value falls back to the text (the HTML rule).
//   - `selected` sets the initial selection (last one wins, as in a browser),
//     and is remembered as the default a form reset restores.
//   - `disabled` options render greyed and inert.
//   - `<optgroup label=…>` contributes an inert heading row before its
//     options, then its options at the same (flat) level. That is an
//     approximation of real nesting, but it puts the group names back on
//     screen instead of silently flattening them away.
func (e *El) readOptions(n *Node) {
	var items, values []string
	var disabled []bool
	selected := -1

	add := func(label, value string, off bool) {
		items = append(items, label)
		values = append(values, value)
		disabled = append(disabled, off)
	}
	var collect func(*Node, bool)
	collect = func(p *Node, groupOff bool) {
		for _, c := range p.Children {
			switch {
			case c.isElement("option"):
				label := collapseText(textContent(c))
				value := label
				if v, ok := c.Attr("value"); ok {
					value = v
				}
				_, off := c.Attr("disabled")
				if _, sel := c.Attr("selected"); sel {
					selected = len(items)
				}
				add(label, value, off || groupOff)
			case c.isElement("optgroup"):
				// A disabled group disables everything inside it (HTML).
				_, off := c.Attr("disabled")
				if label := strings.TrimSpace(c.AttrOr("label", "")); label != "" {
					add(label, "", true) // inert heading row
				}
				collect(c, groupOff || off)
			}
		}
	}
	collect(n, false)

	e.selectItems, e.selectValues, e.selectDisabled = items, values, disabled
	e.selectedIdx, e.defaultSelectedIdx = selected, selected
}

// SetSelectOptions sets a <select>'s option list. Submitted values default to
// the visible labels; use SetSelectValues for a separate value list.
func (e *El) SetSelectOptions(items []string) {
	e.selectItems = items
	e.selectValues = nil // labels double as values until told otherwise
	if sel, ok := e.backing.(*widgets.Select); ok {
		sel.SetItems(items)
	}
}

// SelectOptions returns a <select>'s current option labels.
func (e *El) SelectOptions() []string { return e.selectItems }

// SetSelectValues sets the values a <select> submits, parallel to the option
// labels (HTML `<option value>`). A nil / short slice falls back to the label
// at that index.
func (e *El) SetSelectValues(values []string) {
	e.selectValues = values
}

// SetSelectDisabled marks options as unselectable, parallel to the labels
// (HTML `<option disabled>`).
func (e *El) SetSelectDisabled(disabled []bool) {
	e.selectDisabled = disabled
	if sel, ok := e.backing.(*widgets.Select); ok {
		sel.SetItemDisabled(disabled)
	}
}

// selectValueAt returns the value option i submits — its `value` attribute
// when present, else its visible label.
func (e *El) selectValueAt(i int) string {
	if i < 0 || i >= len(e.selectItems) {
		return ""
	}
	if i < len(e.selectValues) && e.selectValues[i] != "" {
		return e.selectValues[i]
	}
	return e.selectItems[i]
}

// SetSelectedIndex sets a <select>'s current selection.
func (e *El) SetSelectedIndex(i int) {
	e.selectedIdx = i
	if sel, ok := e.backing.(*widgets.Select); ok && sel.SelectedIdx != i {
		sel.SelectedIdx = i
		sel.Invalidate()
	}
}

// SetOnSelect sets the <select> change handler.
func (e *El) SetOnSelect(fn func(int, string)) {
	e.onSelect = fn
	if sel, ok := e.backing.(*widgets.Select); ok {
		sel.OnChange = fn
	}
}

// SetOnFormSubmit attaches a submit handler to a <form> element. It fires
// with a name→value map of the form's named descendant controls whenever a
// submit is triggered inside the form (Enter in a text field, or a click on
// a default/submit <button>). Unchecked checkboxes/radios and unnamed
// controls are omitted, matching HTML form serialization.
func (e *El) SetOnFormSubmit(fn func(map[string]string)) { e.onFormSubmit = fn }

// enclosingForm returns the nearest ancestor <form> element, or nil.
func (e *El) enclosingForm() *El {
	for p := e.elParent; p != nil; p = p.elParent {
		if p.tag == "form" {
			return p
		}
	}
	return nil
}

// maybeSubmitForm submits the enclosing <form>, if any.
func (e *El) maybeSubmitForm() {
	if f := e.enclosingForm(); f != nil {
		f.submitForm()
	}
}

// submitForm gathers the named descendant controls' current values and fires
// the form's onFormSubmit. A no-op when no handler is attached.
func (e *El) submitForm() {
	if e.onFormSubmit == nil {
		return
	}
	data := map[string]string{}
	var gather func(el *El)
	gather = func(el *El) {
		if name, val, ok := el.controlNameValue(); ok {
			data[name] = val
		}
		for _, k := range el.elementKids {
			if ke, ok := k.(*El); ok {
				gather(ke)
			}
		}
	}
	for _, k := range e.elementKids {
		if ke, ok := k.(*El); ok {
			gather(ke)
		}
	}
	e.onFormSubmit(data)
}

// resetForm restores the enclosing <form>'s controls to their initial
// (default) values — the behavior of an <input type=reset> button.
func (e *El) resetForm() {
	if f := e.enclosingForm(); f != nil {
		var walk func(el *El)
		walk = func(el *El) {
			el.resetControl()
			for _, k := range el.elementKids {
				if ke, ok := k.(*El); ok {
					walk(ke)
				}
			}
		}
		for _, k := range f.elementKids {
			if ke, ok := k.(*El); ok {
				walk(ke)
			}
		}
	}
}

// resetControl restores a single control element to its HTML default: text
// fields to their `value` attribute, checkbox/radio to their `checked`
// attribute, ranges/textarea/select likewise. Non-controls are ignored.
func (e *El) resetControl() {
	switch e.tag {
	case "input":
		switch e.attrs["type"] {
		case "checkbox", "radio":
			_, def := e.attrs["checked"]
			e.SetChecked(def)
		case "range":
			if sl, ok := e.backing.(*widgets.Slider); ok {
				sl.Value = parseFloatOr(e.attrs["value"], (sl.Min+sl.Max)/2)
				sl.Invalidate()
			}
		case "submit", "reset", "button":
			// nothing to reset
		case "color":
			// The swatch keeps its value on the element, not in a text
			// widget, so SetInputValue would not reach it.
			e.setColorValueSilently(strings.TrimSpace(e.attrs["value"]))
		case "file":
			e.fileValue = ""
			e.markDirty()
		default:
			e.SetInputValue(e.attrs["value"])
		}
	case "textarea":
		e.SetInputValue(e.attrs["value"])
	case "select":
		// Back to the `selected` option (or "nothing selected").
		e.SetSelectedIndex(e.defaultSelectedIdx)
	}
}

// controlNameValue returns the form field name + current value for a named
// control, or ok=false when the element is not a submittable named control
// (unnamed, not a control, or an unchecked checkbox/radio).
func (e *El) controlNameValue() (name, value string, ok bool) {
	name = e.attrs["name"]
	if name == "" {
		return "", "", false
	}
	switch e.tag {
	case "input":
		switch e.attrs["type"] {
		case "checkbox", "radio":
			if !e.checkedNow() {
				return "", "", false
			}
			v := e.attrs["value"]
			if v == "" && e.attrs["type"] == "checkbox" {
				v = "on"
			}
			return name, v, true
		case "range":
			if sl, ok := e.backing.(*widgets.Slider); ok {
				return name, strconv.FormatFloat(float64(sl.Value), 'g', -1, 32), true
			}
			return name, e.attrs["value"], true
		case "submit", "reset", "button":
			// Push buttons aren't successful controls in form serialization.
			return "", "", false
		case "file":
			// A file picker submits its chosen path (empty if nothing picked).
			return name, e.fileValue, true
		case "color":
			// The swatch's value lives on the element (there is no backing
			// text widget), so read it from there — reading `value` would
			// submit the INITIAL color no matter what the user picked.
			return name, e.currentColor(), true
		default:
			if in, ok := e.backing.(*widgets.Input); ok {
				return name, in.Text, true
			}
			return name, e.attrs["value"], true
		}
	case "textarea":
		if ta, ok := e.backing.(*widgets.TextArea); ok {
			return name, ta.GetText(), true
		}
		return name, e.text, true
	case "select":
		// HTML submits the selected option's `value`, which is NOT its
		// visible text whenever the author supplied one.
		if sel, ok := e.backing.(*widgets.Select); ok {
			return name, e.selectValueAt(sel.SelectedIdx), true
		}
		return name, e.selectValueAt(e.selectedIdx), true
	}
	return "", "", false
}

// checkedNow reports the live checked state of a checkbox/radio, preferring
// the backing widget over the stored flag.
func (e *El) checkedNow() bool {
	switch w := e.backing.(type) {
	case *widgets.CheckBox:
		return w.Checked
	case *widgets.Switch:
		return w.On
	case *widgets.RadioButton:
		return w.Checked
	}
	return e.checked
}

func (e *El) markDirty() {
	if e.engine != nil {
		e.engine.markDirty(e)
	}
}

func (e *El) isStyleRoot() bool {
	return e != nil && e.engine != nil && e.engine.roots[e]
}

// --- styling ---

// applyComputed pushes a freshly-computed style onto this element, reusing
// the one-shot builder's appliers. Called top-down by StyleEngine.restyle.
func (e *El) applyComputed(cs *ComputedStyle) {
	e.lastCS = cs
	if cs == nil {
		return
	}

	// Resolve data-i18n / data-i18n-placeholder / … before any branch
	// below reads e.text or the attribute map, so the whole apply pass
	// sees translated content and nothing downstream needs to know i18n
	// exists. Cheap no-op for elements without the attributes.
	e.applyI18n()

	// display:none — render nothing, measure to zero (see Measure/Draw).
	// Handles a runtime toggle (SetClass/SetAttr flipping display); the
	// static Render path additionally prunes such subtrees at compile time.
	e.displayNone = cs.Display == "none"
	// Mirror onto the root Collapsed flag so parent layouts drop the box
	// entirely (no leftover flex/grid gap slot) and skip it when painting.
	e.SetCollapsed(e.displayNone)
	if e.displayNone {
		e.flowKids = nil
		e.textStateActive = false
		// A hidden element can't show an ancestor-state swap — drop its
		// trigger registrations (a later toggle back restyles and relinks).
		if len(e.stateTriggers) > 0 && e.engine != nil {
			e.engine.removeStateDep(e)
		}
		e.stateTriggers = e.stateTriggers[:0]
		e.ancestorVariants = nil
		e.setContainerChildren(nil)
		e.SetStyle(qui.Style{})
		e.LayoutEngine = qui.FlowLayout{}
		return
	}
	// visibility:hidden — keeps the layout box but paints nothing (El.Draw
	// resolves the effective value per frame via effectiveHidden).
	e.hidden = cs.Hidden

	// HTML `title` attribute → hover tooltip (window-level TooltipProvider).
	// Elements folded into an inline text run don't materialize as widgets,
	// so a title there has no hover surface — use an atomic element.
	e.SetTooltip(e.attrs["title"])

	// Whether interactive-state variants change the element's text (as
	// opposed to box decoration) — drives the draw-time text-color swap.
	e.textStateActive = !e.isControl() && cs.textStateDiffers()

	// If any ancestor-state variant changes this element (box, text or
	// visibility), resolve which specific elements trigger it and register
	// them so their state boundaries repaint this element.
	e.linkAncestorState(cs)

	// Anonymous text segment (static DOM text node): pure inherited
	// typography on the internal label, no box of its own.
	if e.isTextSeg() {
		e.flowKids = nil
		e.ensureLabel()
		e.textLabel.SetText(collapseText(e.text))
		applyTextStyle(e.textLabel, cs)
		qui.UpdateStyle(e.textLabel, func(st *qui.Style) {
			st.Margin = qui.Insets{}
			st.Padding = qui.Insets{}
			st.Background = qui.Color{}
		})
		e.syncChildren()
		return
	}

	// <hr>: a thin Rule leaf. CSS border-* on an hr styles the LINE
	// (browser semantics), not a box outline — move it onto the Rule and
	// keep only the margin/sizing on the element.
	if e.tag == "hr" {
		e.flowKids = nil
		if e.imgWidget == nil {
			e.imgWidget = widgets.NewRule()
		}
		applyBox(&e.Box, cs, e.isStyleRoot())
		base := qui.StyleValue(e)
		qui.UpdateStyle(e.imgWidget, func(rst *qui.Style) {
			rst.BorderSize = base.BorderSize
			rst.Border = base.Border
		})
		qui.UpdateStyle(e, func(bst *qui.Style) {
			bst.BorderSize = 0
			bst.Border = qui.Color{}
			bst.Background = qui.Color{}
			bst.Radius = 0
		})
		e.LayoutEngine = qui.FlowLayout{}
		e.syncChildren()
		return
	}

	// <canvas>: a replaced element whose pixels are produced by a caller-
	// supplied widget or paint callback (the engine's "draw it yourself"
	// hook in place of a JS 2D context). CSS width/height size the region
	// (HTML's 300×150 default otherwise); box decorations paint behind it.
	if e.isCanvas() {
		e.flowKids = nil
		applyBox(&e.Box, cs, e.isStyleRoot())
		e.ensureCanvas(cs)
		e.LayoutEngine = qui.FlowLayout{}
		e.syncChildren()
		return
	}

	// Form controls are backed by a real editing widget; CSS goes on it.
	if e.isControl() {
		e.flowKids = nil
		e.ensureBacking()
		if e.backing != nil {
			applyCommon(e.backing, cs)
			applyControlColors(e.backing, cs)
			// The El — not the backing — is the flex item in the parent's
			// layout, so flex-grow / width / margin must sit on the El; the
			// backing then fills the El's width as a flow block. Without this
			// a form control ignores flex-grow (staying at its intrinsic width).
			e.hoistControlLayout(cs)
		} else if e.tag == "select" {
			// No window yet (static render without opts.Window): show the
			// option list as a plain placeholder label.
			e.ensureLabel()
			e.textLabel.SetText(strings.Join(e.selectItems, " / "))
			applyTextStyle(e.textLabel, cs)
		}
		e.LayoutEngine = qui.FlowLayout{}
		if e.tag == "input" && e.attrs["type"] == "checkbox" && e.attrs["value"] != "" {
			// checkbox with a visible value label: centered row (the label
			// is a separate widget so it stays text-selectable).
			e.ensureLabel()
			e.textLabel.SetText(collapseText(e.attrs["value"]))
			applyTextStyle(e.textLabel, cs)
			qui.UpdateStyle(e.textLabel, func(lst *qui.Style) {
				lst.Margin = qui.Insets{}
				lst.Padding = qui.Insets{}
				lst.Background = qui.Color{}
			})
			e.LayoutEngine = qui.FlexLayout{Direction: qui.Horizontal, AlignItems: qui.AlignCenter, Gap: 6}
		}
		e.syncChildren()
		return
	}

	// <input type=submit|reset|button>: a push button, not an editable field.
	// The label comes from the `value` attribute (or a type default). It then
	// renders through the box/text path below (like <button>).
	if e.isButtonInput() {
		e.text = buttonInputLabel(e.attrs)
	} else if e.isFileInput() {
		e.text = e.fileInputLabel()
	} else if e.isColorInput() {
		// The swatch itself carries the color; no text label.
		e.text = ""
	}

	// Table box types. A <table> builds a grid of its cells (table.go);
	// structural row / row-group elements render no box (their cells are
	// hoisted onto the table's grid).
	if cs.Display == "table" {
		e.buildTable(cs)
		return
	}
	if cs.Display == "table-row" || cs.Display == "table-row-group" ||
		cs.Display == "table-column" || cs.Display == "table-column-group" {
		e.flowKids = nil
		e.setContainerChildren(nil)
		return
	}

	// Content layout from computed display.
	var contentLayout qui.Layout
	if cs.Display == "grid" {
		contentLayout = gridLayout(cs, len(e.elementKids))
	} else {
		contentLayout = chooseLayout(cs)
	}

	// Box decorations (background / border / radius / shadow / gradient /
	// transform / overflow-clip / relative offset / hover-focus-active).
	applyBox(&e.Box, cs, e.isStyleRoot())

	// A text-only state change (e.g. `a:hover{color}`) carries no box
	// decoration, so applyBox left the matching Box.Hover/Focus/Active nil
	// and Box.Handle wouldn't repaint on the transition. Install an empty
	// sentinel style: it overlays nothing (paintDecorations no-ops on zero
	// fields) but makes Box invalidate on the state change so El.Draw can
	// re-run applyStateText with the new color.
	if e.textStateActive {
		if cs.Hover != nil && e.Box.Hover == nil {
			e.Box.Hover = &qui.Style{}
		}
		if cs.Focus != nil && e.Box.Focus == nil {
			e.Box.Focus = &qui.Style{}
		}
		if cs.Active != nil && e.Box.Active == nil {
			e.Box.Active = &qui.Style{}
		}
	}

	// Table row striping: a cell with no background of its own adopts its
	// <tr>'s background (the restyle walk styles the tr before its cells, so
	// its computed style is ready). This is how `tr:nth-child(even){...}`
	// paints a continuous row band under the flattened grid.
	if (e.tag == "td" || e.tag == "th") && !cs.HasBackground &&
		e.elParent != nil && e.elParent.lastCS != nil && e.elParent.lastCS.HasBackground {
		qui.UpdateStyle(e, func(style *qui.Style) {
			style.Background = e.elParent.lastCS.Background
		})
	}

	// border-collapse: rewrite a cell's borders to single shared lines.
	e.applyTableCollapse(cs)

	// background-image: url(...) — a stretched bitmap fill (gradient wins).
	e.applyBackgroundImage(cs)

	// <input type=color>: reflect the picked color. Two looks:
	//   - plain swatch (no icon): the whole background IS the color, the
	//     classic color-well.
	//   - Google-Docs/Sheets style (an icon set via SetIcon): the icon keeps
	//     its own neutral box and the picked color shows as a thick bottom
	//     bar under it, so the control reads as "text color" / "fill color"
	//     without hovering for the tooltip.
	// A default swatch size is applied only when CSS didn't size it.
	if e.isColorInput() {
		c, ok := parseColor(e.currentColor())
		if e.iconSrc != nil {
			// Icon variant: neutral box, color as a bottom bar. Clear any
			// swatch fill a prior (icon-less) pass may have written so the
			// look is idempotent across restyles.
			qui.UpdateStyle(e, func(st *qui.Style) {
				st.Background = qui.Color{}
				st.BorderSize = 0
				st.Border = qui.Color{}
				if ok {
					st.BorderWidths = qui.Insets{Bottom: 3}
					st.BorderColors = qui.SideColors{Bottom: c}
				}
			})
		} else {
			qui.UpdateStyle(e, func(st *qui.Style) {
				if ok {
					st.Background = c
				}
				if !cs.HasWidth && cs.WidthPct == 0 {
					st.Width = 48
				}
				if !cs.HasHeight && cs.HeightPct == 0 {
					st.Height = 26
				}
			})
		}
	}

	// Grid cell placement: when this element is a child of a display:grid box,
	// resolve its grid-column / grid-row / grid-area into explicit placement.
	if e.elParent != nil && e.elParent.lastCS != nil && e.elParent.lastCS.Display == "grid" {
		e.applyGridItem(cs, e.elParent.lastCS)
	}

	// ::before / ::after generated content (resolved from pseudo-element rules).
	e.pbText, e.pbCS, e.paText, e.paCS = "", nil, "", nil
	if e.engine != nil && e.engine.sheet != nil && e.node != nil && !e.isTextSeg() {
		if pcs, txt := pseudoStyle(e.node, e.engine.sheet, "before", cs); txt != "" {
			e.pbText, e.pbCS = txt, pcs
		}
		if pcs, txt := pseudoStyle(e.node, e.engine.sheet, "after", cs); txt != "" {
			e.paText, e.paCS = txt, pcs
		}
	}

	// <label for=…>: clicking the label focuses/toggles the referenced control.
	if e.tag == "label" {
		if target := strings.TrimSpace(e.attrs["for"]); target != "" {
			e.onClick = func() {
				if t := e.findByID(target); t != nil {
					t.activateFromLabel()
				}
			}
		}
	}

	// A <button> (or push-button <input> / file picker) without author-
	// supplied :hover / :active shows the native darken-under-cursor / press
	// feedback (parity with the widget default it used to be backed by).
	// EXCLUDES the color swatch: its background IS the picked value, so a
	// hover/active darken (or focus overlay) would misrepresent the color —
	// only its border may react (below).
	if e.tag == "button" || e.isButtonInput() || e.isFileInput() {
		h, a := defaultButtonStates(cs)
		if e.Box.Hover == nil {
			e.Box.Hover = h
		}
		if e.Box.Active == nil {
			e.Box.Active = a
		}
	}
	// <input type=color>: hover/press feedback is BORDER-ONLY (background
	// stays the picked color; the overlay helper leaves bg untouched when a
	// state's Background.A == 0). The click ACTION (open palette) is handled
	// in runBuiltinClick at dispatch, NOT wired into e.onClick — see there.
	if e.isColorInput() {
		if e.Box.Hover == nil {
			e.Box.Hover = &qui.Style{Border: qui.Color{A: 0.55}, BorderSize: 2}
		}
		if e.Box.Active == nil {
			e.Box.Active = &qui.Style{Border: qui.Color{A: 0.75}, BorderSize: 2}
		}
	}

	// overflow:auto/scroll on an element-child container turns it into a
	// scroll host: a ScrollView (grown to fill) holds an inner box carrying
	// the real layout + children, while this element keeps its decorations
	// and fixed/max height and clips. Text leaves never scroll.
	e.overflowScroll = cs.OverflowScroll && e.text == ""
	if e.overflowScroll {
		e.flowKids = nil
		e.ensureScroll()
		// ScrollView paints an opaque viewport (theme Surface when its own
		// Style().Background is zero-alpha). Match it to THIS element's
		// background so the viewport is seamless with the element instead of
		// showing a differently-colored inset card inside the padding.
		qui.UpdateStyle(e.scrollView, func(style *qui.Style) {
			if cs.HasBackground {
				style.Background = cs.Background
			} else {
				style.Background = qui.Color{}
			}
		})
		e.scrollInner.LayoutEngine = contentLayout
		if cs.OverflowScrollX {
			e.scrollView.Orientation = widgets.ScrollHorizontal
		} else {
			e.scrollView.Orientation = widgets.ScrollVertical
		}
		// Move padding from the outer element to the inner content box so the
		// ScrollView (and its scrollbar) reaches the element's edge — web
		// behaviour: the page scrollbar hugs the window, padding insets the
		// content, not the bar. applyBox set the element's padding above.
		outerPadding := qui.StyleValue(e).Padding
		qui.UpdateStyle(e.scrollInner, func(style *qui.Style) { style.Padding = outerPadding })
		qui.UpdateStyle(e, func(style *qui.Style) { style.Padding = qui.Insets{} })
		e.Box.ClipChildren = true
		e.LayoutEngine = qui.FlexLayout{Direction: qui.Vertical}
	} else {
		e.LayoutEngine = contentLayout
		e.flowKids = e.buildFlow(cs)
		// ::before/::after on a leaf/inline element: fold [before][text][after]
		// into one inline run. (Elements with block children only get the
		// generated content when buildFlow already produced inline runs.)
		if e.flowKids == nil && (e.pbText != "" || e.paText != "") &&
			e.iconSrc == nil && e.tag != "img" {
			e.flowKids = e.buildPseudoLeaf(cs)
		}
		switch {
		case e.flowKids != nil:
			// Mixed inline content: anonymous inline runs (InlineBoxes)
			// interleaved with block children (see syncChildren).
			e.LayoutEngine = qui.FlowLayout{}
		case e.tag == "img" && e.iconSrc == nil:
			e.ensureImg(cs)
			if e.imgWidget != nil {
				applyCommon(e.imgWidget, cs)
			}
		case e.iconSrc != nil:
			// Icon leaf: an svgImage sized from CSS width/height (default 20px
			// square) tinted with the computed text color (icon glyphs are
			// monochrome and should adopt the surrounding color).
			w, h := float32(20), float32(20)
			if cs.HasWidth {
				w = cs.Width
			}
			if cs.HasHeight {
				h = cs.Height
			}
			if e.iconImg == nil {
				e.iconImg = newSVGImage(e.iconSrc, w, h, cs.Color)
			} else {
				e.iconImg.src = e.iconSrc
				e.iconImg.w, e.iconImg.h = w, h
				e.iconImg.tint = cs.Color
			}
		case e.text != "":
			// Text content (leaf, or leading text next to element children):
			// typography on the internal label; box model stays on the box
			// (mirror of build.go's visual-box path so padding isn't doubled).
			e.ensureLabel()
			e.textLabel.SetText(e.text)
			applyTextStyle(e.textLabel, cs)
			st := e.textLabel.Style()
			st.Margin = qui.Insets{}
			st.Padding = qui.Insets{}
			st.Background = qui.Color{}
		}

		// Browser parity: a push-button control centers its single-leaf
		// content (a text label OR an icon) on both axes. The default
		// FlowLayout would flow it to the top-left, so any padding (e.g. the
		// UA button padding) visibly shifts it — and two buttons with
		// different padding then fail to share a baseline. Applies to icon
		// buttons too (no display:flex needed on the class). Author CSS that
		// opts into display:flex/grid keeps its own alignment.
		if e.flowKids == nil &&
			(e.tag == "button" || e.isButtonInput() || e.isFileInput()) &&
			cs.Display != "flex" && cs.Display != "grid" {
			e.LayoutEngine = qui.FlexLayout{
				Direction:  qui.Horizontal,
				Justify:    qui.JustifyCenter,
				AlignItems: qui.AlignCenter,
			}
		}
	}

	// Text selectability. Two sources make text unselectable:
	//
	//   - CSS `user-select: none` (inherited) — the author's call.
	//   - Control surfaces: a <label for=…> whose click toggles/focuses the
	//     target, and push-button inputs. A drag on those must not turn the
	//     caption into selectable prose right after it toggled a checkbox.
	//     The click still bubbles to onClick.
	//
	// Assigned in BOTH directions: Els are reused across restyles, so a
	// class change that drops `user-select: none` has to hand selection back.
	selectable := !cs.NoSelect && !e.isControlSurface()
	if e.textLabel != nil {
		e.textLabel.Selectable = selectable
	}
	for _, ib := range e.inlinePool {
		ib.Selectable = selectable
	}

	// List-item marker: an li directly inside a ul/ol renders as a
	// [marker | content] row (hanging indent) unless list-style-type says
	// none. The row engine goes on the element; the real content layout
	// moves to the liContent host (see syncChildren).
	e.marker = ""
	e.listTag = ""
	if e.tag == "li" && !e.overflowScroll {
		if m := e.listMarker(cs); m != "" {
			e.marker = m
			if e.elParent != nil {
				e.listTag = e.elParent.tag // "ol" / "ul" for clipboard rebuild
			}
			if shape, ok := markerShapeFor(m); ok {
				// disc / circle / square draw as geometry — the bullet
				// codepoints are full-width in a CJK primary font, which is
				// qui's default. See listmarker.go.
				if e.markerShape == nil {
					e.markerShape = newListMarker(shape, fontFrom(cs), cs.Color)
				} else {
					e.markerShape.set(shape, fontFrom(cs), cs.Color)
				}
			} else {
				if e.markerLabel == nil {
					e.markerLabel = widgets.NewLabel(m)
				}
				e.markerLabel.SetText(m)
				applyTextStyle(e.markerLabel, cs)
				// A list marker isn't user-selectable (browser behavior) — keep
				// it out of drag-selection and clipboard; the real <ol>/<ul>
				// regenerates its own markers on paste.
				e.markerLabel.Selectable = false
				e.markerLabel.Paragraph.Wrap = false
				mst := e.markerLabel.Style()
				mst.Margin = qui.Insets{}
				mst.Padding = qui.Insets{}
				mst.Background = qui.Color{}
			}
			if e.liContent == nil {
				e.liContent = widgets.NewBox(qui.FlowLayout{})
			}
			e.liContent.LayoutEngine = e.LayoutEngine
			cst := e.liContent.Style()
			cst.Grow = 1
			cst.Basis = 0
			// The content host is an ANONYMOUS box — it must contribute no
			// box of its own. widgets.DefaultStyle() ships Padding{4,6,4,6},
			// which otherwise pushed the content 4px below the marker
			// baseline and 6px past the row gap.
			cst.Padding = qui.Insets{}
			cst.Margin = qui.Insets{}
			cst.Background = qui.Color{}
			e.LayoutEngine = qui.FlexLayout{Direction: qui.Horizontal, AlignItems: qui.AlignStart, Gap: 6}
		}
	}

	e.syncChildren()

	// A focusable element (:focus styling) is an interactive control —
	// its text must not grab click-focus away from the element itself.
	// This must run AFTER syncChildren so the label is reachable (applyBox
	// runs before the child list exists on the live path).
	if e.Focus != nil {
		disableTextSelection(e)
	}
}

// hoistControlLayout moves the CSS that governs how a form control
// participates in its PARENT's layout from the backing widget (where
// applyCommon put it) onto the El, which is the actual flex/flow child.
// Width, min/max-width, horizontal flex (grow/shrink/basis/order/align-self)
// and margins become the El's; the backing keeps only its own field chrome
// (padding/border/background/font/height) and fills the El's width as a flow
// block. This makes `flex-grow`, `width`, and `margin` on an <input>/<select>/
// <textarea> behave like they do on any block — previously they were inert
// because the backing's parent is the El's own (non-flex) flow layout.
func (e *El) hoistControlLayout(cs *ComputedStyle) {
	est := e.Style()
	bst := e.backing.Style()
	extraH, _ := cs.boxSizingExtra()

	// --- outer width: El owns it; backing fills it (clear on backing) ---
	est.Width = 0
	if cs.HasWidth {
		est.Width = cs.Width + extraH
	}
	est.WidthPct = cs.WidthPct
	est.MinWidth, est.MaxWidth = 0, 0
	if cs.MinWidth > 0 {
		est.MinWidth = cs.MinWidth + extraH
	}
	if cs.MaxWidth > 0 {
		est.MaxWidth = cs.MaxWidth + extraH
	}
	est.MinWidthPct, est.MaxWidthPct = cs.MinWidthPct, cs.MaxWidthPct
	bst.Width, bst.WidthPct = 0, 0
	bst.MinWidth, bst.MaxWidth = 0, 0
	bst.MinWidthPct, bst.MaxWidthPct = 0, 0

	// --- margins are outer box → El (clear on backing) ---
	est.Margin = cs.Margin
	est.MarginLeftAuto, est.MarginRightAuto = cs.MarginLeftAuto, cs.MarginRightAuto
	est.MarginTopAuto, est.MarginBottomAuto = cs.MarginTopAuto, cs.MarginBottomAuto
	bst.Margin = qui.Insets{}
	bst.MarginLeftAuto, bst.MarginRightAuto = false, false
	bst.MarginTopAuto, bst.MarginBottomAuto = false, false

	// --- flex participation → El (backing no longer grows on its own) ---
	est.Grow, est.Basis = 0, 0
	if cs.FlexGrow > 0 {
		est.Grow = cs.FlexGrow
		est.Basis = 0
	}
	if cs.HasFlexBasis {
		est.Basis = cs.FlexBasis
	}
	est.AlignSelf = cs.AlignSelf
	qui.UpdateFlexItem(e, func(f *qui.FlexItem) {
		f.Order = cs.Order
		f.NoShrink = cs.HasFlexShrink && cs.FlexShrink == 0
		if cs.HasFlexShrink && cs.FlexShrink > 0 {
			f.Shrink = cs.FlexShrink
		} else {
			f.Shrink = 0
		}
	})
	bst.Grow, bst.Shrink, bst.Basis = 0, 0, 0
	bst.AlignSelf = qui.AlignDefault
	qui.UpdateFlexItem(e.backing, func(f *qui.FlexItem) {
		f.Order, f.NoShrink, f.Shrink = 0, false, 0
	})
}

// buildPseudoLeaf assembles a leaf element's ::before / text / ::after into a
// single inline run (one InlineBox), reusing the inline pool for restyle
// stability. Used when the element has generated content but no element children.
func (e *El) buildPseudoLeaf(cs *ComputedStyle) []qui.Widget {
	var ib *widgets.InlineBox
	if len(e.inlinePool) > 0 {
		ib = e.inlinePool[0]
		ib.Clear()
	} else {
		ib = widgets.NewInlineBox()
		e.inlinePool = append(e.inlinePool, ib)
	}
	ib.Wrap = !cs.NoWrap
	ib.Align = cs.TextAlign
	ib.PreserveWhitespace = cs.PreserveWS
	ib.BreakLongWords = cs.BreakWord
	ib.LineHeightScale = 0
	if cs.LineHeight > 0 {
		ib.LineHeightScale = cs.LineHeight
	}
	st := ib.Style()
	st.Font = fontFrom(cs)
	st.Margin = qui.Insets{}
	st.Padding = qui.Insets{}
	st.Background = qui.Color{}
	if e.pbText != "" {
		ib.AddTextPaint(e.pbText, fontFrom(e.pbCS), e.pbCS.Color, e.pbCS.decoration(), e.pbCS.decorationPaint(), "")
	}
	if collapseText(e.text) != "" {
		ib.AddTextPaint(e.text, fontFrom(cs), cs.Color, cs.decoration(), cs.decorationPaint(), "")
	}
	if e.paText != "" {
		ib.AddTextPaint(e.paText, fontFrom(e.paCS), e.paCS.Color, e.paCS.decoration(), e.paCS.decorationPaint(), "")
	}
	return []qui.Widget{ib}
}

func (e *El) ensureLabel() {
	if e.textLabel == nil {
		e.textLabel = widgets.NewLabel(e.text)
	}
}

// --- inline formatting contexts (live path) ---

// buildFlow arranges this element's content when it mixes inline-level
// and block-level children: consecutive inline-level children (leading
// text, anonymous text segments, foldable text-like elements, atomic
// inline boxes) group into anonymous runs — each an InlineBox with
// word-level wrap and shared baselines — and block children stack
// between the runs. Returns the arranged child list, or nil when the
// plain block path should be used (no text folding anywhere, or a
// flex/grid container whose children must stay separate items).
//
// Restyle re-runs this per pass, so the decision must be cheap for the
// common all-block container: the tag pre-filter rejects it without
// computing any child style.
func (e *El) buildFlow(cs *ComputedStyle) []qui.Widget {
	if len(e.elementKids) == 0 || cs.Display == "flex" || cs.Display == "grid" {
		return nil
	}
	hasFoldCandidate := e.text != ""
	for _, k := range e.elementKids {
		if ke, ok := k.(*El); ok && (ke.isTextSeg() || textLikeInline[ke.tag]) {
			hasFoldCandidate = true
			break
		}
	}
	if !hasFoldCandidate {
		return nil
	}

	// One style computation per child; classify each as foldable text,
	// atomic inline, or block.
	const (
		kindFold = iota
		kindAtomic
		kindBlock
	)
	kinds := make([]int, len(e.elementKids))
	styles := make([]*ComputedStyle, len(e.elementKids))
	anyFold := e.text != ""
	for i, k := range e.elementKids {
		ke, ok := k.(*El)
		if !ok {
			kinds[i] = kindAtomic // non-El widget rides on the line
			continue
		}
		kcs := e.childStyle(ke, cs)
		styles[i] = kcs
		switch {
		case ke.isTextSeg():
			kinds[i] = kindFold
			if collapseText(ke.text) != "" {
				anyFold = true
			}
		case kcs != nil && strings.HasPrefix(kcs.Display, "inline"):
			if foldableInline(ke, kcs) {
				kinds[i] = kindFold
				anyFold = true
			} else {
				kinds[i] = kindAtomic
			}
		default:
			kinds[i] = kindBlock
		}
	}
	if !anyFold {
		return nil
	}

	// Assemble: runs of fold/atomic children become InlineBoxes (reused
	// from the pool so their tree position is stable across restyles);
	// block children pass through between the runs.
	var out []qui.Widget
	poolUsed := 0
	var run []int
	pendingLead := e.text != "" // element's own text precedes all children
	flushRun := func() {
		leading := pendingLead
		if len(run) == 0 && (!leading || collapseText(e.text) == "") {
			return
		}
		pendingLead = false
		defer func() { run = run[:0] }()
		// Meaningful-content check mirrors the one-shot path: a run of
		// only inter-block whitespace produces nothing; a run whose only
		// content is one atomic box is that box directly (no wrapper).
		// Foldable ELEMENTS always count (their text may be nested).
		meaningfulText := leading && collapseText(e.text) != ""
		var atomics []int
		for _, i := range run {
			ke, _ := e.elementKids[i].(*El)
			if kinds[i] == kindAtomic {
				atomics = append(atomics, i)
				continue
			}
			if ke != nil && (!ke.isTextSeg() || collapseText(ke.text) != "") {
				meaningfulText = true
			}
		}
		if !meaningfulText && len(atomics) == 0 {
			return
		}
		if !meaningfulText && len(atomics) == 1 {
			out = append(out, e.elementKids[atomics[0]])
			return
		}
		var ib *widgets.InlineBox
		if poolUsed < len(e.inlinePool) {
			ib = e.inlinePool[poolUsed]
			ib.Clear()
		} else {
			ib = widgets.NewInlineBox()
			e.inlinePool = append(e.inlinePool, ib)
		}
		poolUsed++
		ib.Wrap = !cs.NoWrap
		ib.Align = cs.TextAlign
		ib.PreserveWhitespace = cs.PreserveWS
		ib.BreakLongWords = cs.BreakWord
		ib.LineHeightScale = 0
		if cs.LineHeight > 0 {
			ib.LineHeightScale = cs.LineHeight
		}
		// Box model stays on the element's Box (applyBox already ran);
		// the InlineBox carries only typography.
		st := ib.Style()
		st.Font = fontFrom(cs)
		st.Margin = qui.Insets{}
		st.Padding = qui.Insets{}
		st.Background = qui.Color{}
		if leading {
			ib.AddTextPaint(e.text, fontFrom(cs), cs.Color, cs.decoration(), cs.decorationPaint(), "")
		}
		for _, i := range run {
			ke, ok := e.elementKids[i].(*El)
			if !ok {
				ib.AddBox(e.elementKids[i], qui.InlineBaseline)
				continue
			}
			e.appendInlineChild(ib, ke, styles[i], cs, "")
		}
		out = append(out, ib)
	}
	for i := range e.elementKids {
		if kinds[i] == kindBlock {
			flushRun()
			out = append(out, e.elementKids[i])
			continue
		}
		run = append(run, i)
	}
	flushRun()
	return out
}

// renderableKids filters out anonymous whitespace-only text segments —
// meaningful only as word gaps inside an inline run, never as standalone
// children.
func renderableKids(kids []qui.Widget) []qui.Widget {
	drop := 0
	for _, k := range kids {
		if ke, ok := k.(*El); ok && ke.isTextSeg() && collapseText(ke.text) == "" {
			drop++
		}
	}
	if drop == 0 {
		return kids
	}
	out := make([]qui.Widget, 0, len(kids)-drop)
	for _, k := range kids {
		if ke, ok := k.(*El); ok && ke.isTextSeg() && collapseText(ke.text) == "" {
			continue
		}
		out = append(out, k)
	}
	return out
}

// foldableInline reports whether a child element can fold into the parent's
// inline run as styled text spans. Anything that needs to stay a REAL
// widget — its own hit-testing (handlers, drag), addressability (#id),
// painted box decorations (badge backgrounds/borders), controls, icons,
// or a non-inline computed display — is kept as an atomic inline box
// instead, so behavior never silently degrades.
func foldableInline(c *El, cs *ComputedStyle) bool {
	if c.isTextSeg() {
		return true // anonymous text always folds
	}
	if !textLikeInline[c.tag] || c.isControl() || c.iconSrc != nil {
		return false
	}
	// A <label for=…> stays a real widget so its click can focus/toggle the
	// associated control (the onClick is wired in the label's own applyComputed,
	// which runs after this fold decision).
	if c.tag == "label" && strings.TrimSpace(c.attrs["for"]) != "" {
		return false
	}
	// Anything that needs to receive events on its own must stay a real
	// widget: folded text has no widget to dispatch to.
	if c.onClick != nil || c.onClickMods != nil || c.onContextMenu != nil ||
		c.draggable || c.dragHandle || c.onDrop != nil || c.onDragOver != nil ||
		c.onMouseEnter != nil || c.onMouseLeave != nil || c.onDoubleClick != nil ||
		c.onWheel != nil || c.onKeyDown != nil || c.onKeyUp != nil ||
		c.onFocus != nil || c.onBlur != nil {
		return false
	}
	if c.attrs["id"] != "" {
		return false
	}
	if cs != nil {
		if cs.Display != "" && cs.Display != "inline" {
			return false
		}
		if cs.isVisualBox() {
			return false
		}
		// A text-only :hover/:focus/:active variant (e.g. `a:hover{color}`)
		// needs the element to stay a real widget so El.Draw can recolor it
		// per interactive state — a folded text span can't track hover.
		if cs.textStateDiffers() {
			return false
		}
	}
	return true
}

// childStyle computes a child's style against this element's stylesheet.
// The engine walk recomputes the same values when it descends into the
// child — deterministic, slightly redundant; subtree-scoped restyle can
// remove the recompute later.
func (e *El) childStyle(c *El, parent *ComputedStyle) *ComputedStyle {
	if c.isTextSeg() {
		return parent // text segments match no selectors; pure inheritance
	}
	if e.engine == nil || e.engine.sheet == nil {
		return parent
	}
	return computeNode(c.node, e.engine.sheet, parent)
}

// appendInlineChild folds one child into the inline box: spans for a
// foldable inline element (recursing into nested inline markup,
// inheriting an enclosing <a>'s href), an atomic box otherwise.
// Block-level descendants are break-wrapped onto their own lines
// (anonymous-block approximation).
func (e *El) appendInlineChild(ib *widgets.InlineBox, ke *El, kcs, parent *ComputedStyle, href string) {
	if ke.tag == "br" {
		ib.AddBreak()
		return
	}
	if ke.isTextSeg() {
		if kcs == nil {
			kcs = parent
		}
		t := ke.text
		if kcs == nil || !kcs.PreserveWS {
			t = collapseInline(t)
		}
		if t != "" {
			ib.AddTextPaint(t, fontFrom(kcs), kcs.Color, kcs.decoration(), kcs.decorationPaint(), href)
		}
		return
	}
	if !foldableInline(ke, kcs) {
		if kcs != nil && (kcs.Display == "block" || kcs.Display == "flex" || kcs.Display == "grid") {
			ib.AddBreak()
			ib.AddBox(ke, inlineVAlignOf(kcs))
			ib.AddBreak()
		} else {
			ib.AddBox(ke, inlineVAlignOf(kcs))
		}
		return
	}
	if ke.tag == "a" {
		if v := ke.attrs["href"]; v != "" {
			href = v
		}
	}
	if kcs == nil {
		kcs = parent
	}
	if ke.text != "" {
		ib.AddTextPaint(ke.text, fontFrom(kcs), kcs.Color, kcs.decoration(), kcs.decorationPaint(), href)
	}
	for _, k := range ke.elementKids {
		if kke, ok := k.(*El); ok {
			e.appendInlineChild(ib, kke, e.childStyle(kke, kcs), kcs, href)
		} else {
			ib.AddBox(k, qui.InlineBaseline)
		}
	}
}

// isCanvas reports whether this is a <canvas> element — a replaced leaf
// whose content is supplied by the caller (SetCanvasDraw / SetCanvas).
func (e *El) isCanvas() bool { return e.tag == "canvas" }

// canvasSize resolves the canvas region's pixel size from CSS, defaulting
// to HTML's intrinsic 300×150 canvas geometry when unspecified.
func (e *El) canvasSize(cs *ComputedStyle) (w, h float32) {
	w, h = 300, 150
	if cs.HasWidth {
		w = cs.Width
	}
	if cs.HasHeight {
		h = cs.Height
	}
	return w, h
}

// ensureCanvas (re)builds the widget backing a <canvas> leaf: the caller's
// widget (SetCanvas), a canvasLeaf wrapping the caller's paint callback
// (SetCanvasDraw), or a light placeholder when neither is wired. Reuses an
// existing canvasLeaf across restyles so its tree position stays stable.
func (e *El) ensureCanvas(cs *ComputedStyle) {
	w, h := e.canvasSize(cs)
	switch {
	case e.canvasUser != nil:
		e.canvasWidget = e.canvasUser
		if st := e.canvasWidget.Style(); st != nil {
			if cs.HasWidth {
				st.Width = w
			}
			if cs.HasHeight {
				st.Height = h
			}
		}
	case e.canvasDraw != nil:
		if cl, ok := e.canvasWidget.(*canvasLeaf); ok && !cl.placeholder {
			cl.draw, cl.w, cl.h = e.canvasDraw, w, h
		} else {
			e.canvasWidget = newCanvasLeaf(e.canvasDraw, w, h)
		}
	default:
		if cl, ok := e.canvasWidget.(*canvasLeaf); ok && cl.placeholder {
			cl.w, cl.h = w, h
		} else {
			cl := newCanvasLeaf(canvasPlaceholderDraw, w, h)
			cl.placeholder = true
			e.canvasWidget = cl
		}
	}
}

// ensureImg loads (or reloads on src change) the widget backing an <img>
// element: an svgImage for .svg sources (tinted with the text color), a
// raster widgets.Image otherwise, or a light placeholder box when the
// source can't be loaded. Relative paths resolve against the engine's
// BaseDir option.
func (e *El) ensureImg(cs *ComputedStyle) {
	src := e.attrs["src"]
	if e.imgWidget != nil && e.imgSrc == src {
		if si, ok := e.imgWidget.(*svgImage); ok {
			// Keep the tint tracking the computed color.
			si.tint = cs.Color
			if cs.HasWidth {
				si.w = cs.Width
			}
			if cs.HasHeight {
				si.h = cs.Height
			}
		}
		return
	}
	e.imgSrc = src
	e.imgWidget = nil
	baseDir := ""
	if e.engine != nil {
		baseDir = e.engine.opts.BaseDir
	}
	if strings.HasSuffix(strings.ToLower(src), ".svg") {
		path := src
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, src)
		}
		if doc, err := svg.ParseFile(path); err == nil {
			w, h := float32(20), float32(20)
			if cs.HasWidth {
				w = cs.Width
			}
			if cs.HasHeight {
				h = cs.Height
			}
			e.imgWidget = newSVGImage(doc, w, h, cs.Color)
			return
		}
	}
	if img := loadRasterImage(src, baseDir); img != nil {
		iw := widgets.NewImage(img)
		switch strings.ToLower(strings.TrimSpace(cs.raw["object-fit"])) {
		case "cover":
			iw.Fit = qui.ImageCover
		case "contain":
			iw.Fit = qui.ImageContain
		}
		e.imgWidget = iw
		return
	}
	ph := widgets.NewBox(qui.FlowLayout{})
	ph.Style().Background = qui.Color{R: 0.9, G: 0.9, B: 0.9, A: 1}
	e.imgWidget = ph
}

// markerWidget returns the widget that renders this li's marker — the
// geometry bullet for disc/circle/square, the Label for numeric markers — or
// nil when the item has no marker at all.
func (e *El) markerWidget() qui.Widget {
	if e.marker == "" {
		return nil
	}
	if _, isBullet := markerShapeFor(e.marker); isBullet {
		if e.markerShape == nil {
			return nil
		}
		return e.markerShape
	}
	if e.markerLabel == nil {
		return nil
	}
	return e.markerLabel
}

// listMarker returns the bullet / number string for this li, honoring
// list-style-type on the li or its parent list. Empty means no marker
// (list-style-type none, or the li isn't directly inside a ul/ol).
//
// The string stays the marker's semantic value (clipboard rebuild, AX) even
// when a bullet is DRAWN rather than typeset — see markerWidget.
func (e *El) listMarker(cs *ComputedStyle) string {
	p := e.elParent
	if p == nil || (p.tag != "ul" && p.tag != "ol") {
		return ""
	}
	lst := firstNonEmpty(cs.raw["list-style-type"], listStyleFromShorthand(cs.raw["list-style"]))
	if lst == "" && p.lastCS != nil {
		lst = firstNonEmpty(p.lastCS.raw["list-style-type"], listStyleFromShorthand(p.lastCS.raw["list-style"]))
	}
	lst = strings.ToLower(strings.TrimSpace(lst))
	if lst == "none" {
		return ""
	}
	if p.tag == "ol" {
		idx := 1
		for _, k := range p.elementKids {
			ke, ok := k.(*El)
			if !ok {
				continue
			}
			if ke == e {
				break
			}
			if ke.tag == "li" {
				idx++
			}
		}
		return strconv.Itoa(idx) + "."
	}
	switch lst {
	case "circle":
		return "◦"
	case "square":
		return "▪"
	default: // disc
		return "•"
	}
}

func (e *El) ensureScroll() {
	if e.scrollView != nil {
		return
	}
	e.scrollInner = widgets.NewBox(qui.FlowLayout{})
	sv := widgets.NewScrollView()
	sv.Style().Background = qui.Color{} // transparent viewport
	st := sv.Style()
	st.Grow = 1
	st.Basis = 0
	e.scrollView = sv
}

// attachInto wires kids as children of an arbitrary container box (used for
// the scroll host's inner box), mirroring setContainerChildren.
func (e *El) attachInto(box *widgets.Box, kids []qui.Widget) {
	box.SetChildren(kids...)
}

// syncChildren rebuilds the Container child list from the current content:
// the control widget, the arranged inline runs, the element children, or
// the text label — whichever applies. The pre-restyle fallback for
// text+children (leading label) covers the frame between a mutation and
// the coalesced restyle that assembles the inline runs.
func (e *El) syncChildren() {
	var kids []qui.Widget
	switch {
	case e.isControl() && e.backing != nil:
		kids = []qui.Widget{e.backing}
		if e.tag == "input" && e.attrs["type"] == "checkbox" && e.attrs["value"] != "" && e.textLabel != nil {
			kids = append(kids, e.textLabel)
		}
	case e.isControl() && e.tag == "select" && e.textLabel != nil:
		kids = []qui.Widget{e.textLabel} // windowless placeholder
	case e.overflowScroll && e.scrollView != nil:
		// Element children live in the inner box; the ScrollView (this
		// element's only child) scrolls it.
		e.attachInto(e.scrollInner, renderableKids(e.elementKids))
		e.updateScrollContent()
		kids = []qui.Widget{e.scrollView}
	case len(e.flowKids) > 0:
		kids = e.flowKids
	case e.isCanvas() && e.canvasWidget != nil:
		kids = []qui.Widget{e.canvasWidget}
	case (e.tag == "hr" || e.tag == "img") && e.imgWidget != nil && e.iconSrc == nil:
		kids = []qui.Widget{e.imgWidget}
	case len(e.elementKids) > 0:
		// Outside an inline run (flex / grid / block stacking), anonymous
		// whitespace-only text segments render nothing — but they'd still
		// consume flex gaps as phantom items. Drop them.
		kids = renderableKids(e.elementKids)
		if e.text != "" {
			e.ensureLabel()
			kids = append([]qui.Widget{e.textLabel}, kids...)
		}
	case e.iconSrc != nil && e.iconImg != nil:
		kids = []qui.Widget{e.iconImg}
	case e.text != "":
		e.ensureLabel()
		kids = []qui.Widget{e.textLabel}
	}
	// List-item marker: content moves into the liContent host so the
	// bullet/number column hangs to the left of (possibly wrapping) content.
	if mw := e.markerWidget(); mw != nil && e.liContent != nil {
		e.attachInto(e.liContent, kids)
		kids = []qui.Widget{mw, e.liContent}
	}
	e.setContainerChildren(kids)
}

// setContainerChildren attaches kids as this element's children, wiring
// parent pointers + window tree (mirror of reactive.SetContainerChildren,
// duplicated here to keep htmlcss free of a reactive import).
func (e *El) setContainerChildren(kids []qui.Widget) {
	e.SetChildren(kids...)
}

// --- events + accessibility ---

// contentWidth returns this element's inner content width (bounds minus
// padding), or 0 when the element hasn't been laid out yet.
func (e *El) contentWidth() float32 {
	// The scroll host's padding lives on the inner box (see applyComputed),
	// so the ScrollView fills the element's full width; the inner box's own
	// padding then insets its children.
	cw := e.Bounds().W - e.Style().Padding.Horizontal()
	if cw < 0 {
		return 0
	}
	return cw
}

// updateScrollContent (re)measures the inner box at the real content width
// and hands the ScrollView an EXPLICIT content size — its zero-size
// auto-measure passes H:0, which collapses a flex column to just its gaps.
// Called on content changes (syncChildren) and from every Layout pass.
// Before the first layout, width is 0; a zero content size wires the
// content and Layout fills in the real size on the same frame.
func (e *El) updateScrollContent() { e.updateScrollContentAt(e.contentWidth()) }

// updateScrollContentAt is updateScrollContent against an explicit content
// width — Layout passes the width it is being GIVEN, so a resize refreshes
// the scroll range in the same pass rather than one frame later.
func (e *El) updateScrollContentAt(cw float32) {
	if cw <= 0 {
		if e.scrollView.Content != e.scrollInner {
			e.scrollView.SetContent(e.scrollInner, qui.Size{})
		}
		return
	}
	e.lastScrollW = cw
	nat := e.scrollInner.Measure(qui.Size{W: cw, H: 1 << 14})
	if nat != e.scrollView.ContentSize || e.scrollView.Content != e.scrollInner {
		e.scrollView.SetContent(e.scrollInner, nat)
	}
}

// Measure reports zero size for a display:none element so it neither
// paints nor reserves flow space; everything else measures as a Box.
func (e *El) Measure(available qui.Size) qui.Size {
	if e.displayNone {
		return qui.Size{}
	}
	return e.Box.Measure(available)
}

// triggerMask returns one bit per (trigger, state) pair that is currently
// active — 3 bits per trigger: hover, focus-within, press. The mask keys
// the ancestorVariants cache.
func (e *El) triggerMask() uint32 {
	var m uint32
	for i, t := range e.stateTriggers {
		if t.hover && t.el.Hovering() {
			m |= 1 << (uint(i) * 3)
		}
		if t.focus && t.el.focusWithin() {
			m |= 1 << (uint(i)*3 + 1)
		}
		if t.active && t.el.Pressed() {
			m |= 1 << (uint(i)*3 + 2)
		}
	}
	return m
}

// focusWithin reports whether keyboard focus rests on this element or any
// widget inside its subtree. qui normalizes :focus-within to :focus, so an
// ancestor-:focus trigger fires when e.g. an <input> inside the row is
// focused — the canonical `.row:focus-within .del` reveal.
func (e *El) focusWithin() bool {
	if e.Focused() {
		return true
	}
	win := e.Window()
	if win == nil {
		return false
	}
	for w := win.Focused(); w != nil; w = w.Parent() {
		if w == qui.Widget(e) {
			return true
		}
	}
	return false
}

// ancestorStateVariant resolves the computed variant for the CURRENT set of
// active triggers, or nil when none is active. Each distinct combination
// runs the cascade once with exactly those elements' state pseudos enabled
// (selectorState.hoverNodes/…), so the applied style is precisely the rules
// those triggers activate — hovering `.outer` never applies a `.row:hover`
// payload. Cached per mask until the next restyle.
func (e *El) ancestorStateVariant() *ancestorVariant {
	if len(e.stateTriggers) == 0 || e.lastCS == nil || e.engine == nil || e.engine.sheet == nil {
		return nil
	}
	mask := e.triggerMask()
	if mask == 0 {
		return nil
	}
	if v, ok := e.ancestorVariants[mask]; ok {
		return v
	}
	var st selectorState
	for i, t := range e.stateTriggers {
		if mask&(1<<(uint(i)*3)) != 0 {
			st.hoverNodes = append(st.hoverNodes, t.el.node)
		}
		if mask&(1<<(uint(i)*3+1)) != 0 {
			st.focusNodes = append(st.focusNodes, t.el.node)
		}
		if mask&(1<<(uint(i)*3+2)) != 0 {
			st.activeNodes = append(st.activeNodes, t.el.node)
		}
	}
	var parentCS *ComputedStyle
	if e.elParent != nil {
		parentCS = e.elParent.lastCS
	}
	cs := interpret(e.node, mergedDecls(e.node, e.engine.sheet, st), parentCS)
	v := &ancestorVariant{cs: cs, box: stateBoxStyle(e.lastCS, cs)}
	if e.ancestorVariants == nil {
		e.ancestorVariants = map[uint32]*ancestorVariant{}
	}
	e.ancestorVariants[mask] = v
	return v
}

// effectiveHidden resolves visibility for the current interactive state: the
// resting cs.Hidden, overridden by the ancestor-state variant while one of
// its triggers is active and by the self-:hover variant while this element
// is hovered. Each override applies only when it actually differs from the
// resting value, so a no-op variant never re-hides an element another state
// just revealed. v is the caller's ancestorStateVariant() (Draw resolves it
// once per frame).
func (e *El) effectiveHidden(v *ancestorVariant) bool {
	hidden := e.hidden
	cs := e.lastCS
	if cs == nil {
		return hidden
	}
	if v != nil && v.cs.Hidden != cs.Hidden {
		hidden = v.cs.Hidden
	}
	if cs.Hover != nil && cs.Hover.Hidden != cs.Hidden && e.Hovering() {
		hidden = cs.Hover.Hidden
	}
	return hidden
}

// VisibilityHidden exposes the element's current CSS visibility state to
// qui's shared hit-test, focus, and accessibility walks. Unlike display:none
// this preserves the layout box while removing the subtree from interaction.
func (e *El) VisibilityHidden() bool {
	if e == nil {
		return true
	}
	return e.effectiveHidden(e.ancestorStateVariant())
}

// ancestorStateSensitive reports whether an ancestor-state UNION variant
// changes anything the draw path can apply — visibility, text color /
// decoration, or a box decoration.
func ancestorStateSensitive(base, v *ComputedStyle) bool {
	return v != nil && (v.Hidden != base.Hidden ||
		v.Color != base.Color || v.Underline != base.Underline || v.LineThrough != base.LineThrough ||
		boxDecorationsDiffer(base, v))
}

// linkAncestorState rebuilds this element's trigger set: the specific
// elements whose :hover / :focus(-within) / :active activates one of its
// ancestor-state rules. Candidates are the ancestors plus — when the sheet
// has sibling-combinator state rules — the preceding siblings of the element
// and of each ancestor. Each trigger is registered with the engine so its
// state boundaries (El.Handle, the window focus listener) invalidate this
// element; without that the swap has nothing to repaint on and would only
// surface on the next unrelated repaint.
func (e *El) linkAncestorState(cs *ComputedStyle) {
	if len(e.stateTriggers) > 0 && e.engine != nil {
		e.engine.removeStateDep(e)
	}
	e.stateTriggers = e.stateTriggers[:0]
	e.ancestorVariants = nil
	sensH := ancestorStateSensitive(cs, cs.AncestorHover)
	sensF := ancestorStateSensitive(cs, cs.AncestorFocus)
	sensA := ancestorStateSensitive(cs, cs.AncestorActive)
	if (!sensH && !sensF && !sensA) || e.engine == nil || e.engine.sheet == nil || e.node == nil {
		return
	}
	sheet := e.engine.sheet
	for _, cand := range e.stateTriggerCandidates(sheet.hasSiblingStateSel) {
		if cand.node == nil {
			continue
		}
		h, f, a := sheet.stateTriggersFor(e.node, cand.node, sensH, sensF, sensA)
		if !h && !f && !a {
			continue
		}
		e.stateTriggers = append(e.stateTriggers, stateTrigger{el: cand, hover: h, focus: f, active: a})
		e.engine.addStateDep(cand, e, f)
		if len(e.stateTriggers) == maxStateTriggers {
			break
		}
	}
}

// stateTriggerCandidates lists the elements that could trigger an
// ancestor-state rule on e: every ancestor and — when the sheet has
// sibling-combinator state selectors — the preceding element siblings of e
// and of each ancestor (a `.a:hover ~ .b .c` trigger is a sibling of an
// ancestor). Following siblings can slip in when the element list is in an
// unexpected state; they are harmless (no selector can be triggered by one,
// so stateTriggersFor filters them out).
func (e *El) stateTriggerCandidates(includeSiblings bool) []*El {
	var out []*El
	for x := e; x != nil; x = x.elParent {
		if x != e {
			out = append(out, x)
		}
		if includeSiblings && x.elParent != nil {
			for _, k := range x.elParent.elementKids {
				ke, ok := k.(*El)
				if !ok {
					continue
				}
				if ke == x {
					break
				}
				if !ke.isTextSeg() {
					out = append(out, ke)
				}
			}
		}
	}
	return out
}

// notifyStateDeps invalidates the elements whose ancestor-state styling
// depends on THIS element, so a hover / press boundary on a trigger
// repaints its dependents even when they lie outside the trigger's own
// rect (sibling reveals). Focus changes are handled by the engine's window
// focus listener instead (focus moves without any event on the trigger).
func (e *El) notifyStateDeps() {
	if e.engine == nil {
		return
	}
	for dep := range e.engine.stateDeps[e] {
		dep.Invalidate()
	}
}

// Draw skips a display:none element entirely, and — for an element whose
// text appearance changes under :hover/:focus/:active — swaps the child
// label/marker/icon color+decoration to the current state before painting.
func (e *El) Draw(canvas qui.Canvas) {
	if e.displayNone {
		return
	}
	// Resolve the ancestor-state variant for the current trigger set once
	// per frame; it drives visibility, the box overlay and the text swap.
	v := e.ancestorStateVariant()
	if e.effectiveHidden(v) {
		return
	}
	// Push the trigger-scoped box override onto the Box so paintDecorations
	// picks it up this frame (and drops it when no trigger is active).
	if v != nil {
		e.Box.AncestorHover = v.box
	}
	e.Box.SetAncestorHovered(v != nil && v.box != nil)
	if e.textStateActive {
		e.applyStateText(v)
	}
	e.Box.Draw(canvas)
	// Outline: a ring just outside the border box (no layout effect).
	if e.lastCS != nil && e.lastCS.HasOutline && e.lastCS.OutlineWidth > 0 {
		b := e.Bounds()
		w := e.lastCS.OutlineWidth
		ring := qui.Rect{X: b.X - w, Y: b.Y - w, W: b.W + 2*w, H: b.H + 2*w}
		if r := e.Style().Radius; r > 0 {
			canvas.StrokeRoundedRect(ring, r+w, e.lastCS.OutlineColor, w)
		} else {
			canvas.StrokeRect(ring, e.lastCS.OutlineColor, w)
		}
	}
}

// applyStateText resolves the text color + decoration for the box's current
// interactive state (layering resting → ancestor-state → hover → focus →
// active, each overriding only the fields it actually changes, mirroring
// Box's box-decoration overlay) and pushes it onto the child label / list
// marker / icon. Cheap per-frame field writes; no relayout (color/decoration
// don't affect metrics). Font-weight/size state changes are intentionally
// not applied here — they would reflow and need a restyle instead. av is the
// caller's ancestorStateVariant() (Draw resolves it once per frame).
func (e *El) applyStateText(av *ancestorVariant) {
	cs := e.lastCS
	if cs == nil {
		return
	}
	color, under, strike := cs.Color, cs.Underline, cs.LineThrough
	pick := func(v *ComputedStyle) {
		if v == nil {
			return
		}
		if v.Color != cs.Color {
			color = v.Color
		}
		if v.Underline != cs.Underline {
			under = v.Underline
		}
		if v.LineThrough != cs.LineThrough {
			strike = v.LineThrough
		}
	}
	if av != nil {
		pick(av.cs)
	}
	if e.Hovering() {
		pick(cs.Hover)
	}
	if e.Focused() {
		pick(cs.Focus)
	}
	if e.Pressed() {
		pick(cs.Active)
	}
	var dec qui.TextDecoration
	if under {
		dec |= qui.DecorationUnderline
	}
	if strike {
		dec |= qui.DecorationLineThrough
	}
	if e.textLabel != nil {
		e.textLabel.Style().Foreground = color
		e.textLabel.Paragraph.Decoration = dec
	}
	if e.markerLabel != nil {
		e.markerLabel.Style().Foreground = color
	}
	if e.markerShape != nil {
		e.markerShape.color = color
	}
	if e.iconImg != nil {
		e.iconImg.tint = color
	}
	if si, ok := e.imgWidget.(*svgImage); ok {
		si.tint = color
	}
}

// Layout lays out the element. For a scroll host it first re-measures the
// inner content at the width it is being given, so the ScrollView's scroll
// range tracks the real, wrap-resolved content size — a descendant can
// change size WITHOUT a width change or a child-list change (a restyled
// height, longer text, a swapped image), and nothing else would notice.
// updateScrollContent only calls SetContent when the natural size actually
// differs, so the resulting InvalidateLayout can't loop.
func (e *El) Layout(rect qui.Rect) {
	if e.overflowScroll && e.scrollView != nil {
		e.updateScrollContentAt(rect.W - e.Style().Padding.Horizontal())
	}
	e.Box.Layout(rect)
	e.maybeAutofocus()
}

// maybeAutofocus honors the `autofocus` attribute once: the first layout
// with a window (the element is mounted — in a portal that is the mount
// itself) posts the focus move, so it runs after the mount's own focus
// handling instead of fighting it mid-layout.
func (e *El) maybeAutofocus() {
	if e.autofocused {
		return
	}
	if _, ok := e.Attr("autofocus"); !ok {
		return
	}
	win := e.Window()
	if win == nil {
		return
	}
	e.autofocused = true
	win.PostJob(func() { e.RequestFocus() })
}

// beginDrag lifts the element: it dims and starts following the cursor, and
// the window cursor switches to a hand so the gesture reads as a drag.
// dragGhost is the element the drag feedback (dim + follow) is applied to.
// Normally the drag source itself; for a drag handle it's the handle's
// parent row, so grabbing a small handle still lifts the whole row.
func (e *El) dragGhost() *El {
	if e.dragHandle {
		if p, ok := e.Parent().(*El); ok && p != nil {
			return p
		}
	}
	return e
}

func (e *El) beginDrag(de qui.DragEvent) {
	e.dragging = true
	e.dragStart = qui.Point{X: de.X, Y: de.Y}
	e.savedVisualTransform = e.dragGhost().VisualTransform
	e.liftAncestors(1)
	if w := e.Window(); w != nil {
		w.SetCursor(qui.CursorHand)
		w.Invalidate()
	}
}

// liftAncestors adds delta to the drag-lift count of every El above the drag
// ghost, so the whole chain from the ghost to the root paints last at its own
// level. Without it a ghost nested one box deep (a row wrapped with its drop
// indicators, a cell in a grid item) disappears UNDER the next wrapper as
// soon as it is dragged over it: z-index only ever orders siblings.
func (e *El) liftAncestors(delta int) {
	for p := e.dragGhost().Parent(); p != nil; p = p.Parent() {
		if pe, ok := p.(*El); ok {
			pe.dragLift += delta
		}
	}
}

// updateDrag moves the lifted element with the pointer via a paint-only
// VisualTransform — no relayout and, crucially, no shift of the hit-test
// bounds, so the element under the cursor (the real drop target) resolves
// correctly instead of the dragged source. A horizontal drag tracks the X
// delta (tabs), the default vertical drag tracks Y (lists), and a free drag
// tracks both (wrapped grids).
func (e *El) updateDrag(de qui.DragEvent) {
	if !e.dragging {
		return
	}
	t := &widgets.BoxTransform{SX: 1, SY: 1}
	switch e.dragAxis {
	case DragAxisHorizontal:
		t.TX = de.X - e.dragStart.X
	case DragAxisFree:
		t.TX = de.X - e.dragStart.X
		t.TY = de.Y - e.dragStart.Y
	default:
		t.TY = de.Y - e.dragStart.Y
	}
	e.dragGhost().VisualTransform = t
	if w := e.Window(); w != nil {
		w.Invalidate() // the element moved; repaint the whole window for this frame
	}
}

// endDrag restores the ghost row's pre-drag transform/opacity and cursor.
// Fires before the drop is dispatched, so the source is back to normal by
// the time the list reorders.
func (e *El) endDrag() {
	if !e.dragging {
		return
	}
	e.dragging = false
	e.dragGhost().VisualTransform = e.savedVisualTransform
	e.savedVisualTransform = nil
	e.liftAncestors(-1)
	if w := e.Window(); w != nil {
		w.SetCursor(qui.CursorDefault)
		w.Invalidate()
	}
	if e.onDragEnd != nil {
		e.onDragEnd()
	}
}

// dragSourceKey returns the DragKey of the element being dragged, or "".
func dragSourceKey(de qui.DragEvent) string {
	if src, ok := de.Source.(*El); ok {
		return src.dragKey
	}
	return ""
}

// appRegionFor resolves the app-region that governs a press whose target is
// t, from this element's point of view.
//
// The value on the DEEPEST element wins, not this one's: app-region inherits
// during the cascade, so a `no-drag` tab inside a `drag` strip already
// reports no-drag on itself and on its own children. Walking down from the
// event target means the strip (which receives the press by bubbling) honors
// that carve-out without having to know which of its descendants declared
// it. Returns "" when nothing in the chain is a drag region.
func (e *El) appRegionFor(t qui.Widget) string {
	for w := t; w != nil && w != qui.Widget(e); w = w.Parent() {
		if el, ok := w.(*El); ok && el.lastCS != nil && el.lastCS.AppRegion != "" {
			return el.lastCS.AppRegion
		}
	}
	if e.lastCS != nil {
		return e.lastCS.AppRegion
	}
	return ""
}

// Handle adds click + drop dispatch on top of Box's hover/press tracking.
func (e *El) Handle(event qui.Event) bool {
	if de, ok := event.(qui.DragEvent); ok {
		switch de.Type() {
		case qui.EventDragStart:
			e.beginDrag(de)
			return true
		case qui.EventDragMove:
			e.updateDrag(de)
			return true
		case qui.EventDragOver:
			if e.onDragOver != nil {
				b := e.Bounds()
				var after bool
				if e.dragAxis == DragAxisVertical {
					after = b.H > 0 && de.Y > b.Y+b.H/2
				} else {
					// Horizontal strips AND wrapped grids both insert in
					// reading order, so the side is the horizontal midpoint.
					after = b.W > 0 && de.X > b.X+b.W/2
				}
				e.onDragOver(dragSourceKey(de), after)
				return true
			}
		case qui.EventDragEnd:
			e.endDrag()
			return true
		case qui.EventDrop:
			if e.onDrop != nil {
				e.onDrop(dragSourceKey(de))
				return true
			}
		}
		return false
	}
	handled := e.Box.Handle(event)
	if ke, ok := event.(qui.KeyEvent); ok {
		// The autocomplete popup gets first refusal — during CAPTURE too, so
		// an Enter meant for the suggestion list never reaches the field and
		// becomes a form submit (see El.handleSuggestKey).
		if e.handleSuggestKey(ke) {
			return true
		}
		// Keys arrive on the focused element and bubble to its ancestors, so
		// both "this element has focus" and "a descendant does" land here.
		// A disabled element eats its keys without acting, like its clicks.
		if !e.Enabled() {
			return handled
		}
		// Author key handlers act on target / bubble only — DOM semantics,
		// as for clicks below. Run during capture, an ancestor's handler
		// would beat the focused element to its own keys: a dialog's
		// Enter-to-confirm firing before the focused <textarea> inserts
		// its newline.
		if ke.Phase() == qui.PhaseCapture {
			return handled
		}
		switch ke.Type() {
		case qui.EventKeyDown:
			if e.onKeyDown != nil && e.onKeyDown(ke) {
				return true
			}
			// A focused push button presses on Enter / Space, after the
			// author's keydown handler had its chance (DOM order).
			if (ke.Key == qui.KeyEnter || ke.Key == qui.KeySpace) &&
				e.isPushButton() && ke.Target() == qui.Widget(e) {
				return e.pressFromKeyboard(ke.Mods)
			}
		case qui.EventKeyUp:
			if e.onKeyUp != nil && e.onKeyUp(ke) {
				return true
			}
		}
		return handled
	}
	me, ok := event.(qui.MouseEvent)
	if !ok {
		return handled
	}
	// State boundaries on a trigger element repaint its dependents (the
	// elements whose ancestor-state styling this element's hover / press
	// drives) — see notifyStateDeps. Box.Handle above already updated the
	// hover / pressed flags for this event.
	switch me.Type() {
	case qui.EventMouseEnter, qui.EventMouseLeave, qui.EventMouseDown, qui.EventMouseUp:
		e.notifyStateDeps()
	}
	// A disabled element consumes its pointer events without acting: no
	// author handler, no built-in control behavior. Hover/press bookkeeping
	// above still ran, so :hover styling is unaffected.
	if !e.Enabled() {
		return handled
	}
	// Pointer transitions are synthesized per widget with no bubbling, so
	// they fire regardless of phase and never compete with a descendant.
	switch me.Type() {
	case qui.EventMouseEnter:
		if e.onMouseEnter != nil {
			e.onMouseEnter()
		}
	case qui.EventMouseLeave:
		if e.onMouseLeave != nil {
			e.onMouseLeave()
		}
	case qui.EventScroll:
		// Wheel handlers run innermost-first (bubble phase) and only consume
		// the event when they say so — otherwise the enclosing ScrollView
		// keeps scrolling, matching a DOM handler that skips preventDefault.
		if e.onWheel != nil && me.Phase() != qui.PhaseCapture {
			if e.onWheel(me.DeltaX, me.DeltaY) {
				return true
			}
		}
	}
	// Everything below is an ACTION (click, context menu, window drag, link
	// navigation), and actions belong to the innermost element that wants
	// them — DOM semantics. Dispatch reaches ancestors during the capture
	// phase too, and acting there would both fire the outer handler first
	// and swallow the release on the way down: a clickable row containing a
	// ✕ button would activate the row and never close it. The hover / press
	// bookkeeping above still runs in every phase, so `.row:active` styling
	// from a press on a child is unaffected.
	if me.Phase() == qui.PhaseCapture {
		return handled
	}
	if e.onContextMenu != nil && me.Type() == qui.EventMouseDown && me.Button == qui.MouseButtonRight {
		// Event coordinates arrive in this element's own space; the handler
		// anchors a window-level overlay, so map back out (a no-op outside
		// scroll containers / transformed subtrees).
		p := qui.LocalPointToWindow(e, qui.Point{X: me.X, Y: me.Y})
		e.onContextMenu(p.X, p.Y)
		return true
	}
	// A press inside an `app-region: drag` area moves the OS window (the
	// browser title-bar gesture). Checked here — after the capture phase and
	// after backing controls have had the press — so a button or text field
	// in the strip still wins; see appRegionFor for the no-drag carve-out.
	if me.Type() == qui.EventMouseDown && me.Button == qui.MouseButtonLeft &&
		me.Phase() != qui.PhaseCapture && e.appRegionFor(me.Target()) == "drag" {
		if win := e.Window(); win != nil && win.BeginWindowDrag() {
			return true
		}
	}
	// A release that ended a drag is not a click: reordering a row by
	// dropping it must not also fire the row's click handler (in the grid
	// overview that meant every reorder closed the view).
	if me.AfterDrag {
		return handled
	}
	if me.Type() == qui.EventMouseUp && me.Button == qui.MouseButtonLeft {
		acted := false
		// Double click: two releases close together in time and space. Timed
		// off the event's own timestamp (not the wall clock) so tests and
		// replayed recordings behave like live input. Detected BEFORE the
		// click handlers run (the release has to be recorded either way) but
		// dispatched AFTER them, because the DOM order is click, click,
		// dblclick — a handler pair that logs both must see it that way.
		isDouble := e.isDoubleClick(me)
		if e.onClick != nil {
			e.onClick()
			acted = true
		}
		if isDouble && e.onDoubleClick != nil {
			e.onDoubleClick()
			acted = true
		}
		if e.onClickMods != nil {
			e.onClickMods(me.Mods)
			acted = true
		}
		// Built-in control behavior (color palette / file dialog / form
		// submit) runs alongside any author handler, and — crucially —
		// independently of e.onClick, which the reconciler overwrites on
		// every re-render.
		if e.runBuiltinClick() {
			acted = true
		}
		if acted {
			return true
		}
	}
	// Hyperlink navigation (anchor semantics without a reactive handler):
	// a click on the element itself (<a href>) or on a folded link span
	// inside one of its inline runs opens the target with the system
	// handler — matching widgets.Anchor.
	if me.Type() == qui.EventMouseUp && me.Button == qui.MouseButtonLeft {
		if e.tag == "a" {
			if href := e.attrs["href"]; href != "" {
				_ = qui.OpenURL(href)
				return true
			}
		}
		for _, ib := range e.inlinePool {
			if href, ok := ib.LinkAt(qui.Point{X: me.X, Y: me.Y}); ok && href != "" {
				_ = qui.OpenURL(href)
				return true
			}
		}
	}
	return handled
}

// doubleClickWindow / doubleClickSlop are the gesture thresholds: the
// platform double-click interval most systems ship, and a few pixels of
// hand tremor.
const (
	doubleClickWindow = 400 * time.Millisecond
	doubleClickSlop   = 5
)

// isDoubleClick reports whether this left release completes a double click,
// and records the release either way so the next one can be compared. A
// release outside the time or distance window starts a fresh gesture.
func (e *El) isDoubleClick(me qui.MouseEvent) bool {
	prevAt, prevX, prevY := e.lastClickAt, e.lastClickX, e.lastClickY
	e.lastClickAt, e.lastClickX, e.lastClickY = me.When, me.X, me.Y
	if prevAt.IsZero() || me.When.Sub(prevAt) > doubleClickWindow {
		return false
	}
	if absf(me.X-prevX) > doubleClickSlop || absf(me.Y-prevY) > doubleClickSlop {
		return false
	}
	// Consume the pair so a third click starts over rather than reading as
	// a second double click.
	e.lastClickAt = time.Time{}
	return true
}

// InlineBaseline reports this element's first-line text baseline (distance
// from its top) so that when it rides in an inline run as an atomic box — a
// styled link kept as its own box for :hover recoloring, say — the IFC aligns
// its text baseline with the surrounding text instead of dropping its bottom
// edge onto the baseline (which left it sitting visibly high). Returns 0 for
// elements without a resolved style, letting the IFC fall back to bottom-on-
// baseline (correct for image / block-ish boxes). Satisfies
// qui.InlineBaselineProvider.
func (e *El) InlineBaseline() float32 {
	cs := e.lastCS
	if cs == nil {
		return 0
	}
	f := fontFrom(cs)
	m := qui.GetFontFaceFor(f).Metrics()
	// Un-Ceil'd, matching the layout engine's inline metrics — rounding each
	// half up here would predict a baseline the engine no longer paints at.
	ascent := float32(m.Ascent) / 64
	descent := float32(m.Descent) / 64
	// Half-leading above the first line, matching the internal Label's
	// top-aligned text layout (line height distributes leading symmetrically).
	// The line-box height is asked of the engine (memoized) with the same
	// LineHeightScale applyTextStyle hands the Label, rather than re-derived
	// here — a local copy of the formula is exactly what drifted when the
	// engine's line-height semantics changed.
	lineH := qui.BuildTextLayout("", f, qui.TextLayoutOptions{LineHeightScale: cs.LineHeight}).LineHeight
	half := (lineH - (ascent + descent)) / 2
	if half < 0 {
		half = 0
	}
	st := e.Style()
	borderTop := st.BorderSize
	if st.BorderWidths.Top > borderTop {
		borderTop = st.BorderWidths.Top
	}
	return st.Padding.Top + borderTop + half + ascent
}

// ClipboardTableCell reports this cell's table membership so the window's
// copy path can rebuild a real <table> (qui.ClipboardTableCell) — pasting
// into Google Docs / Word / Sheets as a genuine table. The table identity
// is the enclosing <table> El; grid coordinates come from the placement
// buildTable assigned. Non-cells return a nil table.
func (e *El) ClipboardTableCell() (table interface{}, row, col, colSpan, rowSpan int, header bool) {
	if e.tag != "td" && e.tag != "th" {
		return nil, 0, 0, 0, 0, false
	}
	t := e.enclosingTable()
	if t == nil {
		return nil, 0, 0, 0, 0, false
	}
	gi := e.GridItemValue()
	cs, rs := gi.ColSpan, gi.RowSpan
	if cs < 1 {
		cs = 1
	}
	if rs < 1 {
		rs = 1
	}
	// A top <caption> occupies grid row 0 and shifts data cells down; report
	// the data-relative row so clipboard table reconstruction stays correct.
	row = gi.Row - t.captionRowOffset
	if row < 0 {
		row = 0
	}
	return t, row, gi.Col, cs, rs, e.tag == "th"
}

// applyBackgroundImage wires a CSS background-image: url(...) as a stretched
// bitmap shader on the box. A gradient (if any) takes precedence. The decoded
// image is cached by URL so restyles don't reload it.
func (e *El) applyBackgroundImage(cs *ComputedStyle) {
	if cs.Gradient != nil || cs.BackgroundImageURL == "" {
		e.bgImg = nil
		e.bgImgSrc = ""
		return
	}
	if e.bgImg == nil || e.bgImgSrc != cs.BackgroundImageURL {
		baseDir := ""
		if e.engine != nil {
			baseDir = e.engine.opts.BaseDir
		}
		e.bgImg = loadRasterImage(cs.BackgroundImageURL, baseDir)
		e.bgImgSrc = cs.BackgroundImageURL
	}
	if e.bgImg != nil {
		img := e.bgImg
		fit := qui.ImageStretch
		switch strings.ToLower(strings.TrimSpace(cs.raw["background-size"])) {
		case "cover":
			fit = qui.ImageCover
		case "contain":
			fit = qui.ImageContain
		}
		e.Box.BackgroundShader = func(b qui.Rect) qui.Shader { return qui.ImageShader(img, b, fit) }
	}
}

// dragZIndex lifts a dragging element above all normal siblings so its
// content isn't occluded by the elements it's dragged over.
const dragZIndex = 1 << 30

// ZIndex reports the element's CSS z-index so the container paints siblings in
// stacking order (qui.ZIndexed). While this element is the drag source — or
// merely CONTAINS one — it reports dragZIndex so the lifted element paints on
// top of its siblings and of the subtrees it is dragged over. 0 when unset.
func (e *El) ZIndex() int {
	if e.dragging || e.dragLift > 0 {
		return dragZIndex
	}
	if e.lastCS == nil {
		return 0
	}
	return e.lastCS.ZIndex
}

// findByID walks the engine's element tree for the first element with the given
// id (used by <label for=…>). Returns nil when absent or no engine root.
func (e *El) findByID(id string) *El {
	if e.engine == nil || e.engine.root == nil {
		return nil
	}
	var found *El
	var walk func(*El)
	walk = func(x *El) {
		if found != nil {
			return
		}
		if x.attrs["id"] == id {
			found = x
			return
		}
		for _, k := range x.elementKids {
			if ke, ok := k.(*El); ok {
				walk(ke)
			}
		}
	}
	walk(e.engine.root)
	return found
}

// activateFromLabel is invoked when an associated <label> is clicked: it
// toggles a checkbox, selects a radio, or focuses a text control.
// pressFromKeyboard runs a push button's click behavior for an Enter /
// Space press: the author handlers, then the built-in form behavior —
// what a mouse click on it runs. Reports whether anything acted.
func (e *El) pressFromKeyboard(mods qui.Modifiers) bool {
	acted := false
	if e.onClick != nil {
		e.onClick()
		acted = true
	}
	if e.onClickMods != nil {
		e.onClickMods(mods)
		acted = true
	}
	if e.runBuiltinClick() {
		acted = true
	}
	return acted
}

// runBuiltinClick performs the built-in click behavior of a form control —
// opening the color palette, opening the file dialog, or submitting/resetting
// the enclosing form — and reports whether it acted. It is invoked DIRECTLY
// from event dispatch rather than wired into e.onClick, because the reactive
// reconciler resets e.onClick to the author handler on every prop update: a
// behavior wrapped into onClick would be silently dropped after the first
// re-render (the bug where the color picker went dead once the selection — and
// thus the swatch's value prop — changed).
func (e *El) runBuiltinClick() bool {
	switch {
	case e.isColorInput():
		e.openColorPopup()
		return true
	case e.isFileInput():
		e.openFileDialog()
		return true
	case e.tag == "button" || e.isButtonInput():
		t := e.attrs["type"]
		isSubmit := (e.tag == "button" && (t == "submit" || t == "")) ||
			(e.tag == "input" && t == "submit")
		isReset := e.tag == "input" && t == "reset"
		if (isSubmit || isReset) && e.enclosingForm() != nil {
			if isReset {
				e.resetForm()
			} else {
				e.maybeSubmitForm()
			}
			return true
		}
	}
	return false
}

func (e *El) activateFromLabel() {
	e.ensureBacking()
	switch w := e.backing.(type) {
	case *widgets.CheckBox:
		w.Checked = !w.Checked
		if w.OnChange != nil {
			w.OnChange(w.Checked)
		}
		w.Invalidate()
	case *widgets.RadioButton:
		if e.radioGrp != nil {
			e.radioGrp.Select(w)
		}
		w.Invalidate()
	case interface{ SetFocused(bool) }:
		w.SetFocused(true)
	}
}

// enclosingTable walks the element-parent chain to the nearest <table>.
func (e *El) enclosingTable() *El {
	for p := e.elParent; p != nil; p = p.elParent {
		if p.tag == "table" {
			return p
		}
	}
	return nil
}

// ClipboardListItem reports this element's list context so the window's
// copy path can rebuild a real <ol>/<ul> (qui.ClipboardListItem). Only a
// marked <li> qualifies; everything else returns an empty list tag.
func (e *El) ClipboardListItem() (listTag, marker string) {
	if e.tag == "li" && e.marker != "" {
		return e.listTag, e.marker
	}
	return "", ""
}

// clipboardBlockTags is the set of semantic block tags worth preserving
// in a clipboard copy — a pasted heading stays a heading, a quote a
// quote. div/section/… stay generic (the window wraps them in <div>).
var clipboardBlockTags = map[string]bool{
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"p": true, "blockquote": true, "pre": true, "figcaption": true,
}

// ClipboardBlockTag reports this element's semantic block tag so the
// window's copy path wraps its text in the real tag instead of a <div>
// (qui.ClipboardBlock).
func (e *El) ClipboardBlockTag() string {
	if clipboardBlockTags[e.tag] {
		return e.tag
	}
	return ""
}

// InlineAtom lets this element contribute its content to an enclosing
// InlineBox's selection + clipboard copy when it rides the line as an
// atomic inline box (inline-block badge, inline <img>). Implements
// widgets.InlineAtomContent. Elements with no copyable content return "".
func (e *El) InlineAtom() (plain string, spans []qui.TextSpan) {
	if e.tag == "img" {
		src := e.attrs["src"]
		if src == "" {
			return "", nil
		}
		alt := e.attrs["alt"]
		// Occupy one selectable slot; a plain-text paste shows the alt (or a
		// single space), the HTML flavor carries the picture as <img src>.
		p := alt
		if p == "" {
			p = " "
		}
		return p, []qui.TextSpan{{Text: alt, Image: e.clipboardImageSrc(src)}}
	}
	t := collapseText(e.text)
	if t == "" {
		t = strings.TrimSpace(e.AccessibleName())
	}
	if t == "" {
		return "", nil
	}
	span := qui.TextSpan{Text: t}
	if e.lastCS != nil {
		f := fontFrom(e.lastCS)
		c := e.lastCS.Color
		span.Font = &f
		span.Color = &c
	}
	return t, []qui.TextSpan{span}
}

// clipboardImageSrc resolves an <img> src into a form that survives being
// pasted into another app. A remote (http/https) or already-inline (data:)
// src is kept verbatim. A LOCAL src (relative to BaseDir, or absolute) is
// inlined as a base64 data URI, so the picture travels with the clipboard
// instead of pointing at a path only this app can find. SVG is rasterized
// to PNG first — editors like Google Docs reject image/svg+xml — using the
// element's displayed size + tint. On any error the original src is
// returned unchanged.
func (e *El) clipboardImageSrc(src string) string {
	low := strings.ToLower(src)
	if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "data:") {
		return src
	}
	path := src
	if !filepath.IsAbs(path) {
		baseDir := ""
		if e.engine != nil {
			baseDir = e.engine.opts.BaseDir
		}
		path = filepath.Join(baseDir, src)
	}
	if strings.HasSuffix(low, ".svg") {
		if doc, err := svg.ParseFile(path); err == nil {
			w, h, tint := e.imgClipboardSizeTint()
			if uri := svgToPNGDataURI(doc, w, h, tint); uri != "" {
				return uri
			}
		}
		return src
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return src
	}
	mime := imageMIME(path)
	if mime == "" {
		return src
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// imgClipboardSizeTint returns the pixel size + tint to rasterize an <img>
// SVG at for the clipboard — the element's CSS width/height (default 20)
// at 2× for crispness, tinted with the computed text color (matching how
// qui paints monochrome icons on screen).
func (e *El) imgClipboardSizeTint() (w, h int, tint qui.Color) {
	fw, fh := float32(20), float32(20)
	if e.lastCS != nil {
		if e.lastCS.HasWidth && e.lastCS.Width > 0 {
			fw = e.lastCS.Width
		}
		if e.lastCS.HasHeight && e.lastCS.Height > 0 {
			fh = e.lastCS.Height
		}
		tint = e.lastCS.Color
	}
	const scale = 2
	return int(fw * scale), int(fh * scale), tint
}

// svgToPNGDataURI rasterizes an SVG to a PNG data URI. Rasterize returns a
// premultiplied-alpha image; PNG uses straight alpha, so un-premultiply
// first or anti-aliased edges darken.
func svgToPNGDataURI(doc *svg.Document, w, h int, tint qui.Color) string {
	img := doc.Rasterize(w, h, tint)
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, unpremultiplyRGBA(rgba)); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// unpremultiplyRGBA converts a premultiplied-alpha image to straight alpha
// (what PNG expects), leaving fully-opaque / fully-transparent pixels
// untouched.
func unpremultiplyRGBA(src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	copy(out.Pix, src.Pix)
	for i := 0; i+3 < len(out.Pix); i += 4 {
		a := out.Pix[i+3]
		if a == 0 || a == 255 {
			continue
		}
		out.Pix[i] = uint8(uint16(out.Pix[i]) * 255 / uint16(a))
		out.Pix[i+1] = uint8(uint16(out.Pix[i+1]) * 255 / uint16(a))
		out.Pix[i+2] = uint8(uint16(out.Pix[i+2]) * 255 / uint16(a))
	}
	return out
}

// imageMIME maps a file extension to an image MIME type for data URIs.
// (SVG is intentionally excluded — it's rasterized to PNG instead.)
func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// Role reports the accessibility role derived from the tag, so the agent /
// AX surface addresses live elements the same way it does one-shot ones.
func (e *El) Role() string {
	switch e.tag {
	case "button":
		return qui.RoleButton
	case "a":
		return qui.RoleLink
	case "textarea":
		return qui.RoleTextbox
	case "input":
		// The input TYPE is the role: a checkbox that reports "textbox" is
		// unaddressable as one ([role=checkbox] matches nothing) and lies
		// about what a click will do.
		inputType, _ := e.readAttr("type")
		switch inputType {
		case "checkbox":
			return qui.RoleCheckbox
		case "radio":
			return qui.RoleRadio
		case "range":
			return qui.RoleSlider
		}
		return qui.RoleTextbox
	case "img", "canvas":
		return qui.RoleImage
	case "ul", "ol":
		return qui.RoleListbox
	case "li":
		return qui.RoleListitem
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return qui.RoleHeading
	default:
		return e.tag
	}
}

// AccessibleName returns the element's accessible name, following the
// HTML/ARIA precedence an agent expects:
//
//  1. `aria-label` — the only option for icon-only controls, which have no
//     text content at all. Without this an icon toolbar is invisible to
//     selectors: every button reports name "" and can only be reached by
//     positional index, which silently breaks when the toolbar is reordered.
//  2. text content (including spans folded into an inline run)
//  3. `title` — already used for the hover tooltip, so it is a reasonable
//     last resort rather than a separate concept.
func (e *El) AccessibleName() string {
	if label, _ := e.readAttr("aria-label"); strings.TrimSpace(label) != "" {
		return strings.TrimSpace(label)
	}
	if n := e.textualName(); n != "" {
		return n
	}
	title, _ := e.readAttr("title")
	return strings.TrimSpace(title)
}

// AccessibleState maps the ARIA state attributes onto qui's state bitmask.
//
// Toggle buttons in a CSS-driven UI usually carry their "on" look in a class
// (`.active`), which is invisible to an agent: it can click the button but
// not read back whether the thing is now on. `aria-pressed` / `aria-checked`
// make that assertable. `disabled` is included because a button El has no
// backing control to receive SetEnabled — without this a greyed-out toolbar
// button reports as perfectly clickable.
func (e *El) AccessibleState() qui.AccessibleState {
	var s qui.AccessibleState
	attrs := e.attrSnapshot()
	if attrs["aria-pressed"] == "true" {
		s |= qui.AXStatePressed
	}
	if attrs["aria-checked"] == "true" {
		s |= qui.AXStateChecked
	}
	if attrs["aria-expanded"] == "true" {
		s |= qui.AXStateExpanded
	}
	if _, off := attrs["disabled"]; off {
		s |= qui.AXStateDisabled
	}
	return s
}

// AccessibleShortcut exposes `aria-keyshortcuts` so keyboard accelerators
// declared in markup reach the AX tree (qui.Shortcutted).
func (e *El) AccessibleShortcut() string {
	v, _ := e.readAttr("aria-keyshortcuts")
	return strings.TrimSpace(v)
}

// AccessibleHasPopup exposes `aria-haspopup` (qui.PopupOwner): a control
// that opens a menu / dropdown when activated. Any value other than the
// explicit "false" counts, matching ARIA.
func (e *El) AccessibleHasPopup() bool {
	if v, _ := e.readAttr("aria-haspopup"); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v) != "false"
	}
	// A <select> always opens a list; the flag belongs on the element an
	// agent addresses, not only on the backing widget buried inside it.
	return e.tag == "select"
}

func (e *El) textualName() string {
	if len(e.flowKids) > 0 {
		var sb strings.Builder
		for _, k := range e.flowKids {
			if ib, ok := k.(*widgets.InlineBox); ok {
				sb.WriteString(ib.Text())
			}
		}
		return sb.String()
	}
	if e.isTextSeg() {
		return collapseText(e.text)
	}
	return e.text
}
