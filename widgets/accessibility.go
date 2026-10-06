package widgets

import (
	"strconv"

	. "github.com/qizhanchan/qui"
)

// Accessibility (a11y) overrides for every concrete widget. Each
// widget implements as little as it needs to surface its semantics
// to the agent layer:
//
//   - Role() string                — semantic role (a constant from
//                                    qui.Role* when one fits)
//   - AccessibleName() string      — human-readable label
//   - AccessibleValue() string     — current mutable value
//   - AccessibleState() AccessibleState
//                                  — extra state bits (checked,
//                                    expanded, pressed). Framework-
//                                    derived bits (focused, disabled,
//                                    hidden) are OR-ed in by the AX
//                                    walker, so widgets only need to
//                                    contribute their own.
//
// Widgets that match the default role (lowercased type name == role
// constant) don't override Role — Label, Slider, ScrollView,
// CheckBox, Select, TextArea, Switch, Popup, Dialog, Button.

// -------------------------------------------------------------------
// Button

func (b *Button) AccessibleName() string    { return b.DisplayText() }
func (b *Button) AccessibleNameKey() string { return b.TextKey() }
func (b *Button) AccessibleState() AccessibleState {
	var s AccessibleState
	if b.currentState()&StatePressed != 0 {
		s |= AXStatePressed
	}
	if b.currentState()&StateHover != 0 {
		s |= AXStateHovered
	}
	return s
}

// -------------------------------------------------------------------
// Label

func (l *Label) AccessibleName() string    { return l.Text() }
func (l *Label) AccessibleNameKey() string { return l.TextKey() }

// -------------------------------------------------------------------
// Input

// Input's name is its floating label when it has one (that is what a
// sighted user reads as the field's name), else its placeholder.
func (t *Input) Role() string { return RoleTextbox }
func (t *Input) AccessibleName() string {
	if l := t.DisplayLabel(); l != "" {
		return l
	}
	return t.DisplayPlaceholder()
}
func (t *Input) AccessibleNameKey() string {
	if t.Label != "" || t.LabelKey() != "" {
		return t.LabelKey()
	}
	return t.PlaceholderKey()
}
func (t *Input) AccessibleValue() string { return t.Text }

// -------------------------------------------------------------------
// TextArea

func (t *TextArea) AccessibleName() string    { return t.DisplayPlaceholder() }
func (t *TextArea) AccessibleNameKey() string { return t.PlaceholderKey() }
func (t *TextArea) AccessibleValue() string   { return t.Text }

// -------------------------------------------------------------------
// CheckBox

func (c *CheckBox) AccessibleName() string    { return c.DisplayLabel() }
func (c *CheckBox) AccessibleNameKey() string { return c.LabelKey() }
func (c *CheckBox) AccessibleValue() string {
	if c.Checked {
		return "true"
	}
	return "false"
}
func (c *CheckBox) AccessibleState() AccessibleState {
	if c.Checked {
		return AXStateChecked
	}
	return 0
}

// -------------------------------------------------------------------
// Switch (shares the CheckBox semantic)

func (s *Switch) AccessibleName() string    { return s.DisplayLabel() }
func (s *Switch) AccessibleNameKey() string { return s.LabelKey() }
func (s *Switch) AccessibleValue() string {
	if s.On {
		return "true"
	}
	return "false"
}
func (s *Switch) AccessibleState() AccessibleState {
	if s.On {
		return AXStateChecked
	}
	return 0
}

// -------------------------------------------------------------------
// Progress

func (p *Progress) Role() string {
	return "progressbar"
}
func (p *Progress) AccessibleValue() string {
	return formatFloat(p.Value)
}

// -------------------------------------------------------------------
// Slider

func (s *Slider) AccessibleValue() string {
	return formatFloat(s.Value)
}

// -------------------------------------------------------------------
// Select

func (c *Select) Role() string { return RoleCombobox }
func (c *Select) AccessibleName() string {
	if l := c.DisplayLabel(); l != "" {
		return l
	}
	return c.DisplayPlaceholder()
}
func (c *Select) AccessibleNameKey() string {
	if c.Label != "" || c.LabelKey() != "" {
		return c.LabelKey()
	}
	return c.PlaceholderKey()
}
func (c *Select) AccessibleValue() string {
	c.refreshItems()
	if c.SelectedIdx < 0 || c.SelectedIdx >= len(c.Items) {
		return ""
	}
	return c.Items[c.SelectedIdx]
}

// AccessibleState surfaces the dropdown's open/closed state as
// expanded and the resting hover — so an agent can tell whether the
// popup is showing (and therefore whether option rows are clickable in
// the overlay layer) without a screenshot.
func (c *Select) AccessibleState() AccessibleState {
	var s AccessibleState
	if c.isOpen {
		s |= AXStateExpanded
	}
	if c.hovering {
		s |= AXStateHovered
	}
	return s
}

// AccessibleHasPopup marks the Select as a dropdown trigger.
func (c *Select) AccessibleHasPopup() bool { return true }

// AccessibleOptions lists the full choice set with the current selection
// flagged, so `/tree` (and `/dom`) show what can be picked and what is
// picked while the dropdown is closed. Pair with the Type action
// (Select.SetText) to select an option by label.
func (c *Select) AccessibleOptions() []AXOption {
	c.refreshItems()
	if len(c.Items) == 0 {
		return nil
	}
	opts := make([]AXOption, len(c.Items))
	for i, it := range c.Items {
		o := c.option(i)
		ax := AXOption{Label: it, Selected: i == c.SelectedIdx, Disabled: !c.itemEnabled(i), Group: o.Group}
		if o.Value != "" && o.Value != it {
			ax.Value = o.Value
		}
		opts[i] = ax
	}
	return opts
}

// -------------------------------------------------------------------
// RadioButton

func (r *RadioButton) Role() string              { return RoleRadio }
func (r *RadioButton) AccessibleName() string    { return r.DisplayLabel() }
func (r *RadioButton) AccessibleNameKey() string { return r.LabelKey() }
func (r *RadioButton) AccessibleState() AccessibleState {
	if r.group != nil && r.group.selected == r {
		return AXStateChecked
	}
	return 0
}

// -------------------------------------------------------------------
// ListView

func (lv *ListView) Role() string            { return RoleListbox }
func (lv *ListView) AccessibleValue() string { return lv.SelectedText() }

// -------------------------------------------------------------------
// TableView

func (tv *TableView) Role() string { return RoleTable }

// -------------------------------------------------------------------
// TableRow

func (tr *TableRow) Role() string { return RoleTableRow }

// -------------------------------------------------------------------
// TabView

func (tv *TabView) Role() string { return RoleTabs }
func (tv *TabView) AccessibleValue() string {
	if tv.SelectedIdx < 0 || tv.SelectedIdx >= len(tv.Tabs) {
		return ""
	}
	return tv.Tabs[tv.SelectedIdx].DisplayTitle()
}

// -------------------------------------------------------------------
// ScrollView

// ScrollView's default role ("scrollview") already matches the
// constant — no override needed. Provided here as a no-op marker so
// future contributors can see the contract was considered.

// -------------------------------------------------------------------
// FieldSet

func (g *FieldSet) Role() string              { return RoleFrame }
func (g *FieldSet) AccessibleName() string    { return g.Title() }
func (g *FieldSet) AccessibleNameKey() string { return g.TitleKey() }

// -------------------------------------------------------------------
// Popup, Dialog

// Popup's default role ("popup") matches the constant. No override.

func (d *Dialog) AccessibleName() string    { return d.DisplayTitle() }
func (d *Dialog) AccessibleNameKey() string { return d.TitleKey() }

// -------------------------------------------------------------------
// MenuBar

func (mb *MenuBar) Role() string { return RoleMenu }

// -------------------------------------------------------------------
// Menu rows + list

func (v *menuItemView) Role() string              { return RoleMenuitem }
func (v *menuItemView) AccessibleName() string    { return v.item.DisplayLabel() }
func (v *menuItemView) AccessibleNameKey() string { return v.item.LabelKey }

func (r *menuContentRow) Role() string           { return RoleMenuitem }
func (r *menuContentRow) AccessibleName() string { return r.item.DisplayLabel() }

// AccessibleState publishes the tick / radio selection. Disabled comes
// from the framework (newMenuItemView mirrors item.Disabled into
// SetEnabled), so it is deliberately not repeated here.
//
// A menu row that renders a ✓ but doesn't say so in the AX tree is not
// assertable: an agent can read that "Bold" exists but not whether the
// caret is currently bold, which is exactly the state a UI test needs.
func (v *menuItemView) AccessibleState() AccessibleState {
	var s AccessibleState
	if v.item.Checked {
		s |= AXStateChecked
	}
	if v.item.Selected {
		s |= AXStateSelected
	}
	return s
}

// AccessibleShortcut publishes the accelerator the row already displays.
func (v *menuItemView) AccessibleShortcut() string { return v.item.Shortcut }

// AccessibleHasPopup marks submenu parents, so a tree walker can descend
// without activating commands to find out.
func (v *menuItemView) AccessibleHasPopup() bool { return len(v.item.Submenu) > 0 }

func (v *menuListView) Role() string { return RoleMenu }

// -------------------------------------------------------------------
// Image

func (iw *Image) Role() string { return RoleImage }

// -------------------------------------------------------------------
// Box / Anchor / Rule — the generic HTML5 elements.

// Box is HTML's <div>: a generic grouping element with no implicit
// semantic role of its own.
func (b *Box) Role() string { return RoleGeneric }

// Anchor is HTML's <a>.
func (a *Anchor) Role() string { return RoleLink }

// AccessibleName returns the link text.
func (a *Anchor) AccessibleName() string { return a.Text() }

// AccessibleValue returns the navigation target.
func (a *Anchor) AccessibleValue() string { return a.Href }

// Rule is HTML's <hr>.
func (r *Rule) Role() string { return RoleSeparator }

// -------------------------------------------------------------------
// ScrollIntoViewable implementations.
//
// ScrollView is the canonical case: walk up from the child looking
// for ScrollView, then adjust scrollY so the child's bounds (in
// content-relative coordinates) lie inside the visible viewport.

// ScrollChildIntoView ensures child's vertical bounds fall inside
// the ScrollView's viewport.
func (s *ScrollView) ScrollChildIntoView(child Widget) {
	if s == nil || child == nil {
		return
	}
	// Both rects go through the interaction transform so they are in the
	// same (on-screen) space: the child's own Bounds are in CONTENT
	// coordinates now that scrolling is a transform, and either widget may
	// additionally sit under a transformed ancestor.
	cb := InteractionBoundsOf(child)
	vb := InteractionBoundsOf(s)
	if vb.IsEmpty() || cb.IsEmpty() {
		return
	}
	// Convert the child's on-screen top into a content offset:
	//
	//   contentY = cb.Y - (vb.Y - scrollY)
	//
	// The viewport spans [scrollY, scrollY + vb.H] in content
	// coordinates. We need contentY in that range.
	contentY := cb.Y - vb.Y + s.ScrollY()
	if contentY < s.ScrollY() {
		s.ScrollTo(contentY)
	} else if contentY+cb.H > s.ScrollY()+vb.H {
		s.ScrollTo(contentY + cb.H - vb.H)
	}
}

// ScrollChildIntoView ensures child (typically a row) is visible
// within the ListView's viewport.
func (lv *ListView) ScrollChildIntoView(child Widget) {
	if lv == nil || child == nil {
		return
	}
	cb := child.Bounds()
	vb := lv.Bounds()
	if vb.IsEmpty() || cb.IsEmpty() {
		return
	}
	// Same math as ScrollView.
	contentY := cb.Y - vb.Y + lv.scrollY
	if contentY < lv.scrollY {
		lv.ScrollTo(contentY)
	} else if contentY+cb.H > lv.scrollY+vb.H {
		lv.ScrollTo(contentY + cb.H - vb.H)
	}
}

// -------------------------------------------------------------------
// helpers

func formatFloat(v float32) string {
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}
