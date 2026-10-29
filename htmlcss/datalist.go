package htmlcss

import (
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// <datalist> — the native autocomplete combobox.
//
// `<input list="cities">` + `<datalist id="cities"><option value="Paris">…`
// is HTML's own answer to "type-to-filter suggestions"; the clear/tag/remote
// behavior component libraries add on top of <select> is NOT native (see
// COVERAGE.md §4 "select 专项"). This file implements the native half:
//
//   - suggestions are read off the linked <datalist> and filtered as the user
//     types (case-insensitive substring, which is what Chrome does);
//   - the list renders in a non-modal Popup anchored under the field, so the
//     field KEEPS focus and typing continues uninterrupted;
//   - ↓/↑ move, Enter accepts, Esc closes, click accepts. Those keys are
//     intercepted during the CAPTURE phase (see El.handleSuggestKey) so the
//     field never sees an Enter that was meant for the popup — otherwise
//     accepting a suggestion would submit the enclosing form instead.
//
// The <datalist> element itself renders nothing (UA `display: none`).

// datalistSuggestions resolves the option list an `<input list=ID>` points
// at, by finding the `<datalist id=ID>` in the same document. An option's
// `value` wins over its text, matching how the browser fills the field.
// Returns nil when there is no list attribute or no such datalist.
func datalistSuggestions(n *Node) []string {
	if n == nil {
		return nil
	}
	id := strings.TrimSpace(n.AttrOr("list", ""))
	if id == "" {
		return nil
	}
	root := n
	for root.Parent != nil {
		root = root.Parent
	}
	dl := findDatalist(root, id)
	if dl == nil {
		return nil
	}
	var out []string
	for _, c := range dl.Children {
		if !c.isElement("option") {
			continue
		}
		if v, ok := c.Attr("value"); ok && strings.TrimSpace(v) != "" {
			out = append(out, v)
			continue
		}
		if label := collapseText(textContent(c)); label != "" {
			out = append(out, label)
		}
	}
	return out
}

func findDatalist(n *Node, id string) *Node {
	if n.isElement("datalist") && n.AttrOr("id", "") == id {
		return n
	}
	for _, c := range n.Children {
		if c.Type != ElementNode {
			continue
		}
		if found := findDatalist(c, id); found != nil {
			return found
		}
	}
	return nil
}

// SetSuggestions installs the autocomplete list for a text input directly —
// the programmatic equivalent of pointing `list` at a `<datalist>`. Pass nil
// to turn autocompletion off.
func (e *El) SetSuggestions(items []string) {
	e.suggestions = items
	if len(items) == 0 {
		e.closeSuggestions()
	}
}

// matchSuggestions filters the suggestion list against what has been typed.
// Empty input offers everything (a browser shows the full list when the field
// is empty and the user presses ↓). Matching is case-insensitive substring.
func matchSuggestions(items []string, typed string) []string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return items
	}
	var out []string
	for _, it := range items {
		if strings.Contains(strings.ToLower(it), typed) {
			out = append(out, it)
		}
	}
	return out
}

// suggestOpen reports whether the suggestion popup is currently showing.
func (e *El) suggestOpen() bool { return e.suggestPopup != nil }

// refreshSuggestions re-filters and re-shows the popup for the current text.
// Called from the field's change handler, so it tracks every keystroke,
// paste and IME commit. A filter that matches nothing closes the popup rather
// than showing an empty box.
func (e *El) refreshSuggestions(typed string) {
	if len(e.suggestions) == 0 || e.suggestApplying {
		return
	}
	matches := matchSuggestions(e.suggestions, typed)
	if len(matches) == 0 {
		e.closeSuggestions()
		return
	}
	e.suggestMatches = matches
	e.suggestIdx = -1 // nothing highlighted until the user arrows into the list
	e.suggestOff = 0  // a new filter starts at the top
	e.showSuggestions()
}

// showSuggestions (re)builds the popup so its rows reflect the current
// matches and highlight. Rebuilding wholesale keeps the row widgets' click
// closures honest about which suggestion they carry.
func (e *El) showSuggestions() {
	win := e.Window()
	if win == nil && e.engine != nil {
		win = e.engine.opts.Window
	}
	if win == nil || len(e.suggestMatches) == 0 {
		return
	}
	e.closeSuggestions()

	pal := e.popupPalette()
	highlight := qui.LerpColor(pal.Surface, pal.Text, 0.08)
	list := widgets.NewBox(qui.FlowLayout{})
	list.Style().Background = pal.Surface
	list.Style().Border = pal.Border
	list.Style().BorderSize = 1
	list.Style().Radius = 4
	list.Style().Padding = qui.Insets{Top: 4, Bottom: 4}
	// Anchor on the field itself (the backing widget's box, which is where
	// the user sees the caret) so the popup hangs off the right edge.
	// On-screen bounds, not raw Bounds(): inside an overflow:auto element the
	// field's retained bounds are in content coordinates, and the popup is a
	// window-level overlay.
	anchor := qui.InteractionBoundsOf(e)
	if in, ok := e.backing.(*widgets.Input); ok {
		if b := qui.InteractionBoundsOf(in); b.W > 0 {
			anchor = b
		}
	}

	var current qui.Widget
	for i, match := range e.suggestMatches {
		idx, value := i, match
		// Rows are flat list items, not buttons: rebuild States so the
		// default button chrome (border + radius + fill) is gone and only the
		// hover / highlight tint remains. Button's resting look comes from
		// States, so setting Style() alone would leave the chrome painted.
		row := widgets.NewButton(value, func() { e.applySuggestion(value) })
		flat := qui.Style{
			Foreground: pal.Text,
			Padding:    qui.Insets{Top: 5, Right: 10, Bottom: 5, Left: 10},
		}
		if idx == e.suggestIdx {
			flat.Background = highlight
			current = row
		}
		hover := flat
		hover.Background = highlight
		row.States = qui.StateStyle{Base: flat, Hover: &hover, Pressed: &hover}
		row.StateLayerColor = qui.Color{}
		row.AlignStart = true // Chrome's rows read left-aligned
		list.AddChild(row)
	}

	// Size it like a <select> dropdown: at least as wide as the field,
	// growing to the widest suggestion (up to the window), and as tall as
	// its rows on the roomier side of the field — the popup caps a longer
	// list at that height (MaxSize) and scrolls it. Box reports its natural
	// content width from Container.Measure, so the width rides on the
	// preferred size; FlowLayout then stretches every row to the full list
	// width. A field with no laid-out width yet (measure-only tree) hugs
	// the suggestions.
	winSize := win.Size()
	nat := qui.MeasureConstrained(list, qui.Size{W: winSize.W, H: 1 << 20})
	w := min(max(nat.W, anchor.W), winSize.W-2*suggestEdge)
	y, budget := suggestPlaceY(winSize.H, anchor.Y-suggestGap, anchor.Y+anchor.H+suggestGap, nat.H)
	list.SetPreferredSize(w, 0)

	popup := widgets.NewPopup(list)
	popup.MaxSize = qui.Size{W: w, H: budget}
	popup.ShowAt(win, max(min(anchor.X, winSize.W-w), 0), y)
	popup.OnClose = func() { e.suggestPopup = nil }
	e.suggestPopup = popup
	if popup.Scrolls() {
		// Keep the offset across rebuilds (each ↑/↓ rebuilds the list) and
		// bring the highlighted row into view.
		popup.ScrollTo(e.suggestOff)
		if current != nil {
			popup.ScrollChildIntoView(current)
		}
	}
}

const (
	suggestGap  = 4 // between the field and the list
	suggestEdge = 8 // the list keeps this far from the window edges
)

// suggestPlaceY places a list of natural height natH next to a field
// spanning [top, bottom] in a window winH tall, as a <select> dropdown
// does: below when it fits, else above, else on the roomier side capped
// to the space there (budget < natH: the list scrolls).
func suggestPlaceY(winH, top, bottom, natH float32) (y, budget float32) {
	below := winH - bottom - suggestEdge
	above := top - suggestEdge
	switch {
	case natH <= below:
		return bottom, natH
	case natH <= above:
		return top - natH, natH
	case above > below:
		return top - above, above
	default:
		return bottom, max(below, 0)
	}
}

// closeSuggestions dismisses the popup if it is showing.
func (e *El) closeSuggestions() {
	if e.suggestPopup == nil {
		return
	}
	p := e.suggestPopup
	e.suggestPopup = nil
	e.suggestOff = p.ScrollOffset()
	p.OnClose = nil
	p.Close()
}

// applySuggestion fills the field with a suggestion and closes the list.
//
// SetInputValue is silent (programmatic sets must not re-enter the app's
// handler), so an accepted suggestion — a user action — publishes the
// change explicitly, exactly once.
//
// suggestApplying suppresses only the re-filter that the change wrapper
// triggers, which would otherwise reopen the popup on the committed value.
func (e *El) applySuggestion(value string) {
	e.suggestApplying = true
	e.SetInputValue(value)
	if in, ok := e.backing.(*widgets.Input); ok {
		in.NotifyTextChanged()
	}
	e.suggestApplying = false
	e.closeSuggestions()
}

// handleSuggestKey gives the popup first refusal on the navigation keys.
// Reports whether the key was consumed.
//
// Runs for every dispatch phase, and the CAPTURE phase is the important one:
// El is an ancestor of the backing field, so capture reaches it first and an
// Enter meant for the popup never becomes a form submit.
func (e *El) handleSuggestKey(ke qui.KeyEvent) bool {
	if ke.Type() != qui.EventKeyDown {
		return false
	}
	// ↓ with a closed list opens it (browser behavior), provided the field
	// has suggestions to offer.
	if !e.suggestOpen() {
		if ke.Key == qui.KeyDown && len(e.suggestions) > 0 && e.inputHasFocus() {
			e.refreshSuggestions(e.inputText())
			return e.suggestOpen()
		}
		return false
	}
	switch ke.Key {
	case qui.KeyDown:
		e.moveSuggestion(+1)
		return true
	case qui.KeyUp:
		e.moveSuggestion(-1)
		return true
	case qui.KeyEnter:
		if e.suggestIdx >= 0 && e.suggestIdx < len(e.suggestMatches) {
			e.applySuggestion(e.suggestMatches[e.suggestIdx])
			return true
		}
		// Nothing highlighted: close and let the field handle Enter (submit).
		e.closeSuggestions()
		return false
	case qui.KeyEscape:
		e.closeSuggestions()
		return true
	}
	return false
}

// moveSuggestion walks the highlight, wrapping at both ends, and rebuilds the
// popup so the new row is the one tinted.
func (e *El) moveSuggestion(delta int) {
	n := len(e.suggestMatches)
	if n == 0 {
		return
	}
	next := e.suggestIdx + delta
	switch {
	case next < 0:
		next = n - 1
	case next >= n:
		next = 0
	}
	e.suggestIdx = next
	e.showSuggestions()
}

// inputText / inputHasFocus read the backing field, which is where a text
// input's value and focus actually live.
func (e *El) inputText() string {
	if in, ok := e.backing.(*widgets.Input); ok {
		return in.Text
	}
	return ""
}

func (e *El) inputHasFocus() bool {
	if in, ok := e.backing.(*widgets.Input); ok {
		return in.Focused()
	}
	return false
}
