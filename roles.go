package qui

import "strings"

// Roles and introspection contracts for the accessibility tree and the
// agent layer. Widgets opt into the contracts by implementing the
// matching interface; the package-level helpers (WidgetRole, WidgetName,
// WidgetValue, WidgetState) collapse the optional surface into a
// single resolved value per widget.
//
// Why interfaces rather than fields on BaseWidget:
//   - Roles and names are usually derivable from existing widget state
//     (Button.Text, CheckBox.Label, Input.Placeholder). Storing
//     them on BaseWidget would duplicate that state.
//   - This pattern matches existing optional-capability interfaces in
//     the framework (Focusable, Tickable, IMEClient, Draggable,
//     Droppable). Consistency wins over a one-off field.

// Standard role strings. Widgets implementing Roled should return one
// of these where possible; novel widgets can return any non-empty
// string. The selector grammar matches by exact string equality, so
// stick to the constants if existing tooling should recognize the
// widget.
const (
	RoleButton    = "button"
	RoleTextbox   = "textbox"
	RoleTextarea  = "textarea"
	RoleLabel     = "label"
	RoleCheckbox  = "checkbox"
	RoleRadio     = "radio"
	RoleSlider    = "slider"
	RoleCombobox  = "combobox"
	RoleListbox   = "listbox"
	RoleListitem  = "listitem"
	RoleTable     = "table"
	RoleTableRow  = "row"
	RoleTableCell = "cell"
	// Header cells of a grid. Split by axis the way ARIA does, because a
	// grid's two header lanes are addressed for different reasons — a
	// column header selects/sorts a field, a row header selects a record —
	// and one shared "header" role would make those indistinguishable by
	// selector.
	RoleColumnHeader = "columnheader"
	RoleRowHeader    = "rowheader"
	RoleTabs         = "tabs"
	RoleTab          = "tab"
	RoleScrollview   = "scrollview"
	RoleMenu         = "menu"
	RoleMenuitem     = "menuitem"
	RoleDialog       = "dialog"
	RolePopup        = "popup"
	RoleTooltip      = "tooltip"
	RoleFrame        = "frame"
	RoleFiledrop     = "filedrop"
	RoleViewport3d   = "viewport3d"
	RoleVideo        = "video"
	RoleWebview      = "webview"
	RoleChart        = "chart"
	RoleContainer    = "container"
	RoleLink         = "link"
	RoleSeparator    = "separator"
	RoleImage        = "image"
	RoleHeading      = "heading"
	RoleGeneric      = "generic"
)

// Roled is implemented by widgets that declare an explicit semantic
// role distinct from their Go type name. Optional — widgets that
// don't implement it fall back to a lowercased type name.
type Roled interface {
	Role() string
}

// Named is implemented by widgets that expose a human-readable
// accessible name. Typically derived from a Text, Label, Title, or
// Placeholder field already present on the widget. Returning "" is
// allowed and means "unnamed".
type Named interface {
	AccessibleName() string
}

// NameKeyed is implemented by widgets whose accessible name comes from
// a message catalog, exposing the key it was translated from. Optional;
// widgets with literal captions do not implement it.
//
// The key is the locale-independent handle on a control. Selectors match
// it as [key=...], which is how an agent script or a CI smoke test
// addresses "the Save button" without knowing what language the app is
// currently rendering in.
type NameKeyed interface {
	AccessibleNameKey() string
}

// Valued is implemented by widgets that have a current "value" beyond
// their label — text widgets return their Text, sliders return their
// numeric value formatted, checkboxes return "true"/"false". Used by
// the accessibility tree to surface mutable state to agents without
// requiring widget-type-aware code on the consumer side.
type Valued interface {
	AccessibleValue() string
}

// AccessibleState is a bitmask of widget visual / interaction state
// surfaced to agents. It is a stable, framework-defined enumeration
// independent of widgets' internal State (which is per-widget and
// includes style-only flags).
type AccessibleState uint32

const (
	AXStateFocused AccessibleState = 1 << iota
	AXStateHovered
	AXStatePressed
	AXStateDisabled
	AXStateSelected
	AXStateChecked
	AXStateExpanded
	AXStateBusy
	AXStateHidden
)

// String formats the bitmask as a sorted "|"-joined list of state
// names. Useful for JSON / log output. Returns "" for the zero value.
func (s AccessibleState) String() string {
	if s == 0 {
		return ""
	}
	var parts []string
	if s&AXStateFocused != 0 {
		parts = append(parts, "focused")
	}
	if s&AXStateHovered != 0 {
		parts = append(parts, "hovered")
	}
	if s&AXStatePressed != 0 {
		parts = append(parts, "pressed")
	}
	if s&AXStateDisabled != 0 {
		parts = append(parts, "disabled")
	}
	if s&AXStateSelected != 0 {
		parts = append(parts, "selected")
	}
	if s&AXStateChecked != 0 {
		parts = append(parts, "checked")
	}
	if s&AXStateExpanded != 0 {
		parts = append(parts, "expanded")
	}
	if s&AXStateBusy != 0 {
		parts = append(parts, "busy")
	}
	if s&AXStateHidden != 0 {
		parts = append(parts, "hidden")
	}
	return strings.Join(parts, "|")
}

// AccessibleStateProvider is implemented by widgets that surface
// extra state bits beyond the framework-derived focused/disabled.
// The provider's return value is OR-ed with the framework defaults,
// so a widget never has to recompute focused/disabled itself.
type AccessibleStateProvider interface {
	AccessibleState() AccessibleState
}

// TextSink is implemented by widgets that accept programmatic text
// replacement. The agent layer's Type action goes through TextSink
// when present — that path bypasses the 500ms undo-coalesce timer
// and pushes ONE complete undo entry covering the substitution, so
// Cmd-Z reverses the whole agent-driven edit instead of one
// rune at a time.
//
// Implementations should:
//   - call breakCoalesce / equivalent so the agent's edit doesn't
//     merge with subsequent human typing,
//   - push one undo entry,
//   - fire OnChange / OnSubmit if appropriate,
//   - reset selection.
//
// `SetText(string)` is far too common a method name to be a reliable
// marker on its own — a Label, or an htmlcss element whose SetText
// replaces markup content, satisfies it structurally without honoring
// any of the above. So the Type action only takes this path for a
// widget that ALSO reports its text state (TextEditable) or its option
// set (Optioned): whatever an agent may write, it must be able to read
// back. Widgets that fail that test are typed into by dispatching
// characters through the focus route instead (see Window.Type), and a
// widget that can neither sink nor focus text yields ErrNotTextTarget
// rather than having some ancestor absorb the keystrokes.
type TextSink interface {
	SetText(string)
}

// TextChangeNotifier is implemented by editable widgets whose SetText is
// deliberately SILENT — a programmatic set must not re-enter the app's own
// change handler, or binding a value back from state would loop — but which
// can publish a user-equivalent change on demand.
//
// The Type action calls this right after the bulk SetText path so an
// agent-typed value reaches the app's OnChange exactly as typing would.
// Without it, every app that keeps its field value in its own state (which is
// every reactive dialog) sees the widget change and its state not — and an
// agent testing that app gets a false pass.
type TextChangeNotifier interface {
	NotifyTextChanged()
}

// TextTargetProvider is implemented by wrapper widgets that render text
// entry through a backing control widget — htmlcss's <input> / <textarea>
// / <select> elements are the canonical case: the element owns styling,
// layout and identity (`#email`), while a child Input / TextArea / Select
// owns the editing. Text actions retarget to TextTarget() so an agent can
// address the wrapper it can see in the tree and still drive the real
// editor. Return nil when there is nothing to retarget to.
type TextTargetProvider interface {
	TextTarget() Widget
}

// TextState is a snapshot of a text-editing widget's internal editing
// state. It exists to make input behavior debuggable through the AX /
// agent surface: caret position, selection range, in-flight IME
// composition and undo availability are otherwise invisible, so when an
// input "misbehaves" there is nothing to inspect. Surfaced on AXNode and
// on the htmlcss DOM node for <input>/<textarea>.
type TextState struct {
	Value    string `json:"value"`
	Caret    int    `json:"caret"`             // cursor position, in runes
	SelStart int    `json:"selStart"`          // selection anchor, -1 when no selection
	SelEnd   int    `json:"selEnd"`            // selection end, in runes
	Preedit  string `json:"preedit,omitempty"` // in-flight IME composition (uncommitted)
	CanUndo  bool   `json:"canUndo,omitempty"`
	CanRedo  bool   `json:"canRedo,omitempty"`
}

// TextEditable is implemented by text-editing widgets (Input, TextArea)
// that expose their editing state for introspection. Optional, following
// the same opt-in pattern as Valued / Named.
type TextEditable interface {
	TextState() TextState
}

// WidgetTextState returns w's editing state and true when w implements
// TextEditable, or the zero state and false otherwise.
func WidgetTextState(w Widget) (TextState, bool) {
	if w == nil {
		return TextState{}, false
	}
	if te, ok := w.(TextEditable); ok {
		return te.TextState(), true
	}
	return TextState{}, false
}

// AXOption is one selectable choice in a combobox / listbox. It is
// surfaced on AXNode.Options (and the htmlcss DOM node) so an agent can
// read the full choice set and the current selection WITHOUT opening the
// dropdown — the option list is otherwise invisible until the popup is
// shown, which made a Select awkward to drive from the agent surface.
type AXOption struct {
	Label    string `json:"label"`
	Selected bool   `json:"selected,omitempty"`
}

// Optioned is implemented by widgets presenting a fixed set of choices
// (Select today; any future listbox / combobox). Optional, following the
// same opt-in pattern as Valued / TextEditable. Pairs with TextSink: a
// widget that lists its options here should accept SetText(label) to pick
// one, so an agent can inspect-then-select in two obvious steps.
type Optioned interface {
	AccessibleOptions() []AXOption
}

// WidgetOptions returns w's option set when it implements Optioned, else
// nil.
func WidgetOptions(w Widget) []AXOption {
	if w == nil {
		return nil
	}
	if o, ok := w.(Optioned); ok {
		return o.AccessibleOptions()
	}
	return nil
}

// AXChild describes one interactive element that a self-drawing widget
// renders itself, so it can be published to the AX tree as a node of its own.
// Bounds are in the same window coordinate space as Widget.Bounds().
type AXChild struct {
	ID     string          // optional stable id, addressable as #id
	Role   string          // see the Role* constants
	Name   string          // accessible name
	Value  string          // accessible value
	State  AccessibleState // checked / selected / disabled / …
	Bounds Rect
}

// AccessibleChildProvider is implemented by widgets that paint their own
// interactive content instead of composing child widgets — a document view
// with checklist boxes, a spreadsheet grid, a chart with data points, a
// canvas of shapes.
//
// Without it, everything such a widget draws is invisible to the AX tree and
// unreachable by selector: the widget is one opaque rectangle. The only way
// to drive an element inside it is to hardcode window coordinates, which
// silently break the moment layout, zoom, or content shifts — the failure
// mode is a click that lands on empty space and an assertion that quietly
// passes because nothing changed.
//
// Published children are addressable like any other node
// (`[role=checkbox][name="Buy milk"]`). Actions targeting one dispatch a real
// event at the CHILD's bounds — coordinates come from the widget that drew
// it, not from the caller.
//
// Implementations should return only what is currently laid out and visible;
// the walk runs on every AX snapshot, so keep it allocation-light.
type AccessibleChildProvider interface {
	AccessibleChildren() []AXChild
}

// WidgetAccessibleChildren returns w's self-drawn AX children, or nil.
func WidgetAccessibleChildren(w Widget) []AXChild {
	if w == nil {
		return nil
	}
	if p, ok := w.(AccessibleChildProvider); ok {
		return p.AccessibleChildren()
	}
	return nil
}

// Shortcutted is implemented by widgets that own a keyboard accelerator
// worth publishing to the AX tree — menu items, and anything else whose
// affordance is "there is a key for this". Mirrors ARIA's
// `aria-keyshortcuts`.
//
// Publishing it matters for more than documentation: without it an agent
// can read WHAT commands exist but not HOW to reach them by keyboard, so
// shortcut coverage can't be diffed against a reference app.
//
// The string is the display form the widget already shows the user
// ("⌘⇧S", "Ctrl+A") — no normalization is imposed, since the accelerator
// registry and the on-screen label use the same text.
type Shortcutted interface {
	AccessibleShortcut() string
}

// WidgetAccessibleShortcut returns w's accelerator when it implements
// Shortcutted, else "".
func WidgetAccessibleShortcut(w Widget) string {
	if w == nil {
		return ""
	}
	if s, ok := w.(Shortcutted); ok {
		return s.AccessibleShortcut()
	}
	return ""
}

// PopupOwner is implemented by widgets that open a submenu / dropdown /
// popover when activated. Mirrors ARIA's `aria-haspopup`.
//
// This is what lets a tree walker discover nested structure SAFELY: with
// no such marker, the only way to learn whether a menu item has a submenu
// is to activate it — and activating a plain item runs its command. An
// agent enumerating a menu tree must be able to tell "opens more UI" from
// "does something" before it clicks.
type PopupOwner interface {
	AccessibleHasPopup() bool
}

// WidgetAccessibleHasPopup reports whether w opens a popup, per PopupOwner.
func WidgetAccessibleHasPopup(w Widget) bool {
	if w == nil {
		return false
	}
	if p, ok := w.(PopupOwner); ok {
		return p.AccessibleHasPopup()
	}
	return false
}

// ScrollIntoViewable is implemented by scrolling containers that
// can ensure a particular descendant widget is visible. The agent
// layer calls ScrollChildIntoView before Click / Hover / Focus so
// an offscreen target becomes hit-testable.
//
// Implementations are expected to:
//   - walk up from child looking for child's row / offset,
//   - clamp scroll position so child.Bounds() lies inside the
//     scrolling container's content area,
//   - leave focus / selection state unchanged.
type ScrollIntoViewable interface {
	ScrollChildIntoView(child Widget)
}

// WidgetRole returns the semantic role for w. Prefers an explicit
// Roled implementation; otherwise falls back to the lowercased type
// name with the package prefix stripped (e.g. *widgets.Button →
// "button").
func WidgetRole(w Widget) string {
	if w == nil {
		return ""
	}
	if r, ok := w.(Roled); ok {
		if role := r.Role(); role != "" {
			return role
		}
	}
	name := widgetTypeName(w)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.ToLower(name)
}

// WidgetName returns the accessible name of w, or "" if it has none.
func WidgetName(w Widget) string {
	if w == nil {
		return ""
	}
	if o, ok := w.(interface{ accessibleNameOverride() string }); ok {
		if name := o.accessibleNameOverride(); name != "" {
			return name
		}
	}
	if n, ok := w.(Named); ok {
		return n.AccessibleName()
	}
	return ""
}

// WidgetNameKey returns the message key behind w's accessible name, or
// "" when the name is a literal. See NameKeyed.
func WidgetNameKey(w Widget) string {
	if w == nil {
		return ""
	}
	if o, ok := w.(interface{ accessibleNameOverride() string }); ok && o.accessibleNameOverride() != "" {
		return "" // an explicit name is a literal
	}
	if n, ok := w.(NameKeyed); ok {
		return n.AccessibleNameKey()
	}
	return ""
}

// WidgetValue returns the accessible value of w, or "" if it has
// none.
func WidgetValue(w Widget) string {
	if w == nil {
		return ""
	}
	if v, ok := w.(Valued); ok {
		return v.AccessibleValue()
	}
	return ""
}

// WidgetID returns the developer-assigned ID for w, or "" if w
// doesn't carry an ID (e.g. a custom widget that doesn't embed
// BaseWidget). The fallback path is for diagnostic robustness — most
// widgets in the framework embed BaseWidget transitively.
func WidgetID(w Widget) string {
	if w == nil {
		return ""
	}
	type idGetter interface{ ID() string }
	if ig, ok := w.(idGetter); ok {
		return ig.ID()
	}
	return ""
}

// WidgetAccessibleState assembles the framework-derived state bits
// (focused, disabled, hidden) and ORs in any provider-supplied bits.
// Focused is resolved via the owning Window's currently-focused
// widget; pass a nil window to skip that check. Hidden means the
// widget has empty bounds or is hidden/collapsed itself or through an
// ancestor.
func WidgetAccessibleState(w Widget, focused Widget) AccessibleState {
	if w == nil {
		return 0
	}
	var s AccessibleState
	hidden := InteractionBoundsOf(w).IsEmpty() || isWidgetEffectivelyHidden(w)
	if focused != nil && focused == w && !hidden {
		s |= AXStateFocused
	}
	if !w.Enabled() {
		s |= AXStateDisabled
	}
	if hidden {
		s |= AXStateHidden
	}
	if p, ok := w.(AccessibleStateProvider); ok {
		s |= p.AccessibleState()
	}
	return s
}
