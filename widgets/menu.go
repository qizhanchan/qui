package widgets

import (
	. "github.com/qizhanchan/qui"
)

// Menus are built on top of Popup — both ContextMenu and MenuBar
// dropdowns are just non-modal popups containing a vertical list of
// clickable items. The helpers below construct those popups with
// consistent styling, keyboard navigation, and auto-close-on-select.
//
// Visual defaults: a raised container with a small corner radius and a
// level-2 shadow; 48 px rows with a body-large label; hover and pressed
// rendered as a state layer over the row (8% / 12%); checked and
// radio-selected rows carry a neutral tint of the row's own text color.
// Every one of those colors is overridable through MenuColors.

// MenuItem is a single clickable entry in a menu.
//
// A single MenuItem is one of five things depending on which optional
// fields are set:
//   - Separator:  {Separator: true}                  — draws a divider, not selectable
//   - Submenu:    {Label:"…", Submenu: []*MenuItem}  — opens a nested popup on hover/click
//   - Checkable:  {Label:"…", Checkable: true}       — left gutter draws a ✓ when Checked
//   - Plain:      {Label:"…", OnClick: fn}           — fires OnClick, then closes the chain
//   - Panel:      {Panel: func(close) Widget}        — caller-supplied content as the row
//
// `Selected` is orthogonal to `Checkable`/`Checked`: it marks the row
// as "current value" for radio-style menus (select dropdowns, picked
// option in a chooser). The row gets the selected-row tint but no ✓
// glyph. Use it when one item out of N is the active choice.
type MenuItem struct {
	Label     string
	Shortcut  string // displayed on the right (also harvested by MenuBar.AcceleratorRegistry)
	OnClick   func()
	Disabled  bool
	Separator bool
	Checkable bool
	Checked   bool
	Selected  bool // current value in a radio-style chooser; bg tint, no ✓
	Submenu   []*MenuItem
	// Panel replaces the label row with caller-supplied content — the
	// shape a menu needs when a command is picked by pointing at a
	// picture rather than by reading a label (a table-size grid, a colour
	// swatch sheet). The hosted widget owns its own hover/click handling;
	// the row itself paints no state layer and never activates, and
	// keyboard navigation skips it. Call the passed `close` after acting
	// to dismiss the whole popup chain, exactly as a plain row's OnClick
	// does. Label / Shortcut / Checkable are ignored on a Panel row.
	Panel func(close func()) Widget
	// RadioGroup tags items that behave like radio buttons within a
	// parent menu: picking one visually "unchecks" the others. Zero
	// means "not part of a group". The caller must coordinate the
	// Checked fields — this package only reflects the current state.
	RadioGroup int
}

// menuMinWidth is the 112 px floor every menu-item's Measure reports,
// so short-label menus ("OK" / "Cancel") don't collapse into a sliver.
const menuMinWidth float32 = 112

// menuShadowMargin bounds how far the level-2 elevation halo can
// extend past the menu surface. Level-2 ambient is Y=2, Blur=6,
// Spread=2 → ~10 px reach; 12 px matches the conservative box-blur cap
// that Button uses (buttonShadowMargin) and lets the same margin
// constant cover any future bump to level-3 if menu elevation rises.
// Used by menuListView.OverlayHaloRect so the Popup invalidates the
// halo region on show + close.
const menuShadowMargin float32 = 12

// MenuColors carries the palette a menu surface paints with. Zero-A
// on any field means "keep whatever the widget had at construction"
// — set the ones you want to override and pass to a menu factory /
// MenuPanel constructor.
//
// themedMenuColors() derives a palette from the current theme (the
// default for live popups); the offscreen MenuPanel building block
// defaults to a plain HTML dropdown look (white surface, gray hover,
// black text).
type MenuColors struct {
	Background      Color
	Foreground      Color // list-item-label-text-color
	ForegroundMuted Color // supporting-text / shortcut / disabled tail
	Divider         Color
	SelectedBg      Color
	SelectedFg      Color
	StateLayer      Color // hover/pressed overlay tint
}

// Default menu typography + row height. Every menu popup (context menus,
// menu-bar dropdowns, the sheet-list-style anchored menus, and the Select
// dropdown) reads these, so an app makes ALL its menus compact once at
// startup via SetMenuMetrics — mirroring how jetbrainsmono.Use() swaps
// the global default font. Defaults are body-large (16px) labels,
// label (14px) shortcuts, 48px rows.
var (
	menuLabelRole    = TextBodyLarge
	menuShortcutRole = TextLabel
	menuItemHeight   = float32(48)
)

// SetMenuMetrics overrides the default menu typography + row height for
// every popup created afterward. Pass itemHeight <= 0 to keep the current
// height. Intended to be called once at startup (not per-window state, so
// it is safe as a package-level default). Example — a compact,
// Google-Sheets-style menu matching a 14px menu bar:
//
//	widgets.SetMenuMetrics(qui.TextBody, qui.TextLabelSmall, 32)
func SetMenuMetrics(labelRole, shortcutRole TextRole, itemHeight float32) {
	menuLabelRole = labelRole
	menuShortcutRole = shortcutRole
	if itemHeight > 0 {
		menuItemHeight = itemHeight
	}
}

// menuStyle centralizes the visual tokens used by menu popups.
//
// Numeric tokens are plain layout constants. Color tokens are held as
// widget-side fields so palettes are plugged
// via MenuColors, not read out of CurrentTheme at Draw.
type menuStyle struct {
	bg            Color
	bgHover       Color // legacy hover bg — superseded by state-layer; kept for Select override
	fg            Color
	fgMuted       Color
	divider       Color
	border        Color // legacy border — off by default
	selectedBg    Color
	selectedFg    Color
	stateColor    Color    // hover/pressed state-layer base
	labelRole     TextRole // label text typescale (default TextBodyLarge)
	shortcutRole  TextRole // shortcut text typescale (default TextLabel)
	gutterW       float32
	submenuW      float32
	itemPadV      float32
	itemPadLR     float32
	leadingSpace  float32
	trailingSpace float32
	itemHeight    float32
	menuPadV      float32
	dividerInset  float32
	shortcutGap   float32
	radius        float32
	elevation     int // Theme.Elevation index for the popup shadow; 0 = flat

	// colorsPinned = true skips refreshColors so a caller-supplied or
	// theme-derived palette (Select dropdown, themedMenuStyle) survives a
	// theme swap without being overwritten by the plain defaults.
	colorsPinned bool

	// selectedColorsOverridden legacy flag preserved for Select path
	// that wants only the selected-row colors held.
	selectedColorsOverridden bool
}

// refreshColors reapplies the raw-HTML palette. Skipped when
// colorsPinned is true so caller-supplied / theme-derived palettes
// (Select dropdown, themedMenuStyle) survive a theme swap without being
// overwritten. The itemHeight is refreshed unconditionally.
func (st *menuStyle) refreshColors() {
	st.itemHeight = menuItemHeight
	if st.colorsPinned {
		return
	}
	*st = withPlainMenuColors(*st)
}

// ApplyColors overlays a caller palette on the menu style. Zero-A
// fields on the argument leave the corresponding widget field alone.
// Sets colorsPinned so subsequent theme toggles don't overwrite.
func (st *menuStyle) ApplyColors(c MenuColors) {
	if c.Background.A > 0 {
		st.bg = c.Background
	}
	if c.Foreground.A > 0 {
		st.fg = c.Foreground
	}
	if c.ForegroundMuted.A > 0 {
		st.fgMuted = c.ForegroundMuted
	}
	if c.Divider.A > 0 {
		st.divider = c.Divider
	}
	if c.SelectedBg.A > 0 {
		st.selectedBg = c.SelectedBg
	}
	if c.SelectedFg.A > 0 {
		st.selectedFg = c.SelectedFg
	}
	if c.StateLayer.A > 0 {
		st.stateColor = c.StateLayer
	}
	st.colorsPinned = true
}

// withPlainMenuColors returns st with its color fields reset to the
// plain HTML-dropdown palette (white surface, gray hover, black text).
// Non-color fields are preserved.
func withPlainMenuColors(st menuStyle) menuStyle {
	st.bg = Color{R: 1, G: 1, B: 1, A: 1}
	st.bgHover = Color{R: 0.90, G: 0.90, B: 0.90, A: 1}
	st.fg = Color{R: 0.10, G: 0.10, B: 0.10, A: 1}
	st.fgMuted = Color{R: 0.45, G: 0.45, B: 0.45, A: 1}
	st.divider = Color{R: 0.85, G: 0.85, B: 0.85, A: 1}
	st.stateColor = Color{R: 0.10, G: 0.10, B: 0.10, A: 1}
	if !st.selectedColorsOverridden {
		st.selectedBg = Color{R: 0.30, G: 0.55, B: 0.85, A: 1}
		st.selectedFg = Color{R: 1, G: 1, B: 1, A: 1}
	}
	return st
}

// defaultMenuStyle returns the plain (non-themed) menu style — the raw
// HTML dropdown look (white surface, gray hover, black text). Used by
// the offscreen MenuPanel building block; live popups go through
// themedMenuStyle so they follow the app's scheme.
func defaultMenuStyle() menuStyle {
	st := menuStyle{
		labelRole:     menuLabelRole,
		shortcutRole:  menuShortcutRole,
		border:        Color{}, // no border — parent decides shadow
		gutterW:       24,
		submenuW:      24,
		itemPadV:      0,
		itemPadLR:     0,
		leadingSpace:  12,
		trailingSpace: 12,
		itemHeight:    menuItemHeight,
		menuPadV:      8,
		dividerInset:  12,
		shortcutGap:   28,
		radius:        4,
	}
	return withPlainMenuColors(st)
}

// themedMenuColors builds a menu palette from the current theme — a
// raised surface, body text, and a hairline divider. The selected /
// checked row is a NEUTRAL tint (the text color blended into the
// surface) rather than an accent-derived fill: apps commonly retint the
// accent and surface but leave secondary roles at the baseline, so an
// accent-derived selection leaked a foreign hue into menus whose theme
// was otherwise recolored. Read fresh at open time so a live theme swap
// retints the next menu; shared by the context-menu popups and the
// Select dropdown. Callers wanting a designed selection color pass
// MenuColors through the *Styled entry points.
func themedMenuColors() MenuColors {
	t := CurrentTheme()
	return MenuColors{
		Background:      t.SurfaceRaised,
		Foreground:      t.Text,
		ForegroundMuted: t.TextMuted,
		Divider:         t.Border,
		SelectedBg:      LerpColor(t.SurfaceRaised, t.Text, 0.12),
		SelectedFg:      t.Text,
		StateLayer:      t.Text,
	}
}

// themedMenuStyle is defaultMenuStyle re-tinted from the current theme,
// with a level-2 popup shadow — the base for every live menu popup
// so menus, submenus, and menu-bar dropdowns follow the app's palette
// without any per-call palette. Explicit callers layer overrides on top
// via styledMenuStyle.
func themedMenuStyle() menuStyle {
	st := defaultMenuStyle()
	st.ApplyColors(themedMenuColors())
	st.elevation = 2
	return st
}

// MenuPanel builds the visual surface of a menu with the plain HTML
// palette — white surface, black text, subtle divider. Intended for
// golden-snapshot tests and offscreen rendering; production code goes
// through ShowContextMenu / ShowContextMenuForAnchor / MenuBar instead.
//
// The returned Widget is a *menuListView wrapped behind the Widget
// interface; submenu / OnClick / closeChain plumbing is hooked up but
// will no-op (clicks fire OnClick but the popup overlay nopops on
// "close" because the popup was never pushed).
//
// Reach for MenuPanelStyled to supply a specific palette + elevation.
func MenuPanel(items []*MenuItem) Widget {
	_, list := buildMenuPopup(nil, nil, items)
	return list
}

// MenuPanelStyled builds the same offscreen menu surface as MenuPanel
// but with a caller-supplied palette + elevation applied on top of the
// default (structural) tokens. Zero-A fields in colors leave the plain
// default alone; elevation < 0 keeps the default flat look.
//
// For anyone who needs a designed menu widget (a specific palette +
// elevation) outside a live window.
func MenuPanelStyled(items []*MenuItem, colors MenuColors, elevation int) Widget {
	_, list := buildMenuPopup(nil, nil, items)
	list.style.ApplyColors(colors)
	if elevation > 0 {
		list.style.elevation = elevation
	}
	return list
}

// ShowContextMenu opens a menu popup at (x, y) and returns the popup
// (so callers can force-close it). The popup auto-dismisses on
// outside click, Esc, or any selection. Keyboard navigation
// (Up/Down/Enter/Left/Right) is active once the popup is shown.
func ShowContextMenu(window *Window, x, y float32, items []*MenuItem) *Popup {
	return showContextMenuStyled(window, x, y, items, themedMenuStyle())
}

// ShowContextMenuStyled is ShowContextMenu with a caller palette +
// elevation applied to the popup surface — submenus inherit the same
// style. Zero-A fields in colors resolve from the current theme (the
// base themedMenuStyle); elevation <= 0 keeps the themed level-2 shadow.
func ShowContextMenuStyled(window *Window, x, y float32, items []*MenuItem, colors MenuColors, elevation int) *Popup {
	return showContextMenuStyled(window, x, y, items, styledMenuStyle(colors, elevation))
}

// styledMenuStyle overlays a caller palette + elevation on the themed
// base, so a partial MenuColors (zero-A fields) still resolves the rest
// from the current theme rather than the plain default.
func styledMenuStyle(colors MenuColors, elevation int) menuStyle {
	st := themedMenuStyle()
	st.ApplyColors(colors)
	if elevation > 0 {
		st.elevation = elevation
	}
	return st
}

func showContextMenuStyled(window *Window, x, y float32, items []*MenuItem, st menuStyle) *Popup {
	if window == nil {
		return nil
	}
	popup, list := buildMenuPopupStyled(window, nil, nil, items, st)
	if popup == nil {
		return nil
	}
	// Measure the menu and nudge the anchor so the whole thing stays
	// inside the window. Native toolkits (NSMenu etc.) pop a separate OS
	// window that can spill past the host window's edge; qui overlays
	// render inside the GL window, so instead we flip the menu up when it
	// would overflow the bottom and clamp it horizontally. A right-click
	// near the bottom edge then shows the full menu growing upward,
	// matching the VSCode / GoLand result.
	win := window.Size()
	natural := list.Measure(Size{W: win.W, H: 1 << 20})
	top, budget := placeMenuY(win.H, y, y, natural.H)
	size := list.Measure(Size{W: win.W, H: budget}) // caps + wraps a scroll if needed
	x = clampMenuX(win.W, x, size.W)
	popup.showAtSize(window, x, top, size)
	window.SetFocus(list)
	return popup
}

// placeMenuY decides the vertical origin and height budget for a menu of
// natural height natH, given the trigger's top and bottom edges (equal for a
// cursor point). It prefers opening below the trigger, flips above when the
// menu wouldn't fit below, and — when the menu is taller than either side —
// opens on the roomier side capped to that space (the menuListView then
// scrolls). The returned span never crosses the trigger, so a menu can't cover
// its own trigger button or the bar it sits on (e.g. the all-sheets ☰ menu
// popping up must not hide the +/☰ buttons beneath it).
func placeMenuY(winH, triggerTop, triggerBottom, natH float32) (top, budget float32) {
	const edge = menuViewportMargin
	below := winH - triggerBottom - edge
	above := triggerTop - edge
	switch {
	case natH <= below:
		return triggerBottom, natH
	case natH <= above:
		return triggerTop - natH, natH
	case above >= below:
		return triggerTop - above, above // spans [edge, triggerTop]
	default:
		return triggerBottom, below // spans [triggerBottom, winH-edge]
	}
}

// clampMenuX shifts a menu of width w left to stay inside the window, then
// clamps to the left edge.
func clampMenuX(winW, x, w float32) float32 {
	if x+w > winW {
		x = winW - w
	}
	if x < 0 {
		x = 0
	}
	return x
}

// ShowContextMenuForAnchor opens a menu popup placed relative to an
// anchor rect — preferring just below the anchor, but flipping above
// when the menu wouldn't fit below the window. Returns the popup so
// callers can force-close it.
//
// This is the right entry point for dropdown-style triggers (Select,
// menubar buttons, "kebab" menus) where the anchor's geometry is known
// — ShowContextMenu can't auto-flip because it only has a point.
func ShowContextMenuForAnchor(window *Window, anchor Rect, items []*MenuItem) *Popup {
	return showContextMenuForAnchor(window, staticAnchor(anchor), items, 0, themedMenuStyle())
}

// ShowContextMenuForAnchorFunc is ShowContextMenuForAnchor with a LIVE
// anchor: anchorFn is re-evaluated whenever the menu re-places — notably
// on a window resize. A menu anchored to a widget that moves as the
// window resizes (e.g. a control pinned to a bottom bar) then follows its
// trigger instead of being stranded or dismissed. Pass a closure that
// reads the trigger's current bounds, e.g. func() Rect { return b.Bounds() }.
func ShowContextMenuForAnchorFunc(window *Window, anchorFn func() Rect, items []*MenuItem) *Popup {
	return showContextMenuForAnchor(window, anchorFn, items, 0, themedMenuStyle())
}

// ShowContextMenuForAnchorStyled is ShowContextMenuForAnchor with a
// caller palette + elevation applied to the popup surface — submenus
// inherit the same style (see ShowContextMenuStyled).
func ShowContextMenuForAnchorStyled(window *Window, anchor Rect, items []*MenuItem, colors MenuColors, elevation int) *Popup {
	return showContextMenuForAnchor(window, staticAnchor(anchor), items, 0, styledMenuStyle(colors, elevation))
}

// ShowContextMenuForAnchorFuncStyled is ShowContextMenuForAnchorFunc with
// a caller palette + elevation (see ShowContextMenuForAnchorFunc).
func ShowContextMenuForAnchorFuncStyled(window *Window, anchorFn func() Rect, items []*MenuItem, colors MenuColors, elevation int) *Popup {
	return showContextMenuForAnchor(window, anchorFn, items, 0, styledMenuStyle(colors, elevation))
}

func showContextMenuForAnchorWidth(window *Window, anchorFn func() Rect, items []*MenuItem, width float32) *Popup {
	return showContextMenuForAnchor(window, anchorFn, items, width, themedMenuStyle())
}

// staticAnchor adapts a fixed rect to the live-anchor func signature. On
// resize the menu re-places against the same rect (staying put) rather
// than vanishing — correct for triggers whose geometry doesn't move.
func staticAnchor(anchor Rect) func() Rect { return func() Rect { return anchor } }

func showContextMenuForAnchor(window *Window, anchorFn func() Rect, items []*MenuItem, width float32, st menuStyle) *Popup {
	if window == nil || anchorFn == nil {
		return nil
	}
	popup, list := buildMenuPopupStyled(window, nil, nil, items, st)
	if popup == nil {
		return nil
	}
	winSize := window.Size()
	measureSize := winSize
	if width > 0 {
		measureSize.W = width
	}
	size := list.Measure(measureSize)
	if width > 0 {
		size.W = width
	}
	return showAnchoredPopup(window, popup, list, anchorFn, size)
}

func showAnchoredPopup(window *Window, popup *Popup, focus Widget, anchorFn func() Rect, size Size) *Popup {
	if window == nil || popup == nil || anchorFn == nil {
		return nil
	}
	const gap float32 = 2
	lv, scrollable := focus.(*menuListView)

	// place computes flip-aware geometry from the CURRENT anchor + window
	// size and either shows the popup (first call) or moves it in place (on
	// resize). Re-evaluating anchorFn each time is what lets an anchored
	// menu follow a trigger that moves when the window resizes.
	place := func(firstShow bool) {
		anchor := anchorFn()
		winSize := window.Size()
		s := size
		var x, y float32
		if scrollable {
			// A scrollable menu list gets a height budget so a long dropdown
			// (e.g. a Select with dozens of options) caps and scrolls on the
			// roomier side instead of covering the anchor or running past the
			// window edges. Width (s.W, possibly a forced trigger-aligned
			// width) is preserved.
			natural := lv.Measure(Size{W: s.W, H: 1 << 20})
			top, budget := placeMenuY(winSize.H, anchor.Y-gap, anchor.Y+anchor.H+gap, natural.H)
			capped := lv.Measure(Size{W: s.W, H: budget})
			s.H = capped.H
			x = clampMenuX(winSize.W, anchor.X, s.W)
			y = top
		} else {
			y = anchor.Y + anchor.H + gap
			// Flip above when the menu would overflow below AND there's room
			// above. When neither side fits, prefer below so at least the top
			// of the menu is visible.
			if y+s.H > winSize.H && anchor.Y-s.H-gap >= 0 {
				y = anchor.Y - s.H - gap
			}
			x = clampMenuX(winSize.W, anchor.X, s.W)
		}
		if firstShow {
			popup.showAtSize(window, x, y, s)
			if focus != nil {
				window.SetFocus(focus)
			}
			return
		}
		popup.RelayoutAt(x, y, s)
	}

	place(true)
	// Follow the trigger across window resizes instead of dismissing.
	popup.SetResizeHandler(func(Size) {
		if popup.window != nil {
			place(false)
		}
	})
	return popup
}

// showMenuPopup is the shared opener for submenu popups. parent is the
// opening popup. st carries the OPENING menu's style so a styled menu's
// submenus inherit its palette instead of reverting to the plain
// default. When a leaf item in a submenu is selected, Close cascades up
// through parent chain so the whole stack dismisses.
// closeParent is the PARENT's own close-the-whole-chain callback. Passing
// the popup alone is not enough: Popup.Close knows nothing about its
// ancestors, so a submenu that only closed its immediate parent left every
// grandparent on screen. That was live in any app with a three-level menu —
// picking Format > Bullets & numbering > List options > Restart numbering
// ran the command and left the Format menu covering the document.
func showMenuPopup(window *Window, parent *Popup, closeParent func(), x, y float32, items []*MenuItem, st menuStyle) *Popup {
	popup, list := buildMenuPopupStyled(window, parent, closeParent, items, st)
	if popup == nil {
		return nil
	}
	popup.ShowAt(window, x, y)
	window.SetFocus(list)
	return popup
}

// buildMenuPopup constructs the popup + content for a menu but does NOT
// push it onto the overlay stack — the caller decides final placement
// (fixed (x, y) for ShowContextMenu / submenus, flip-aware placement
// for ShowContextMenuForAnchor) and then invokes popup.ShowAt.
func buildMenuPopup(window *Window, parent *Popup, items []*MenuItem) (*Popup, *menuListView) {
	return buildMenuPopupStyled(window, parent, nil, items, defaultMenuStyle())
}

// buildMenuPopupStyled is the styled variant used by callers that need
// to override style fields (currently Select: wider leadingSpace to
// align with its trigger text, distinct selectedBg). The provided style
// is treated as a starting point; gutter collapse for label-only menus
// still happens here.
func buildMenuPopupStyled(window *Window, parent *Popup, closeParent func(), items []*MenuItem, st menuStyle) (*Popup, *menuListView) {
	// Snapshot the incoming style BEFORE the gutter collapse below:
	// submenus inherit the palette but re-decide their own gutter from
	// their own items.
	subStyle := st

	// Reserve the gutter column only when at least one item is Checkable
	// — Selected (radio-style) items use bg tint alone, no ✓ glyph, so
	// they don't need the leading 24-px indent either.
	hasCheck := false
	for _, it := range items {
		if it.Checkable {
			hasCheck = true
			break
		}
	}
	if !hasCheck {
		st.gutterW = 0
	}

	var popup *Popup
	var list *menuListView
	var itemWidgets []Widget

	// closeChain dismisses this popup AND everything it was opened from.
	// It walks the chain through the parent's own closeChain rather than
	// calling parent.Close() once, which only ever unwound one level.
	closeChain := func() {
		if popup != nil {
			popup.Close()
		}
		if closeParent != nil {
			closeParent()
			return
		}
		if parent != nil {
			parent.Close()
		}
	}

	for i, item := range items {
		it := item // capture
		idx := i
		if it.Separator {
			itemWidgets = append(itemWidgets, newMenuSeparator(st))
			continue
		}
		if it.Panel != nil {
			// Exactly one widget per item, always: menuListView.activate
			// reaches a row by its ITEM index (content.ChildAt(focused)), so a
			// row that appended nothing — or two — would misdirect every
			// keyboard activation below it.
			itemWidgets = append(itemWidgets, newMenuPanelRow(it.Panel(closeChain), st))
			continue
		}
		itemWidgets = append(itemWidgets, newMenuItemWidget(it, st, func() {
			if it.Submenu != nil {
				// Open submenu anchored to this row's right edge.
				row := itemWidgets[idx].Bounds()
				showMenuPopup(window, popup, closeChain, row.X+row.W, row.Y, it.Submenu, subStyle)
				return
			}
			if it.Checkable {
				it.Checked = !it.Checked
			}
			if it.OnClick != nil {
				it.OnClick()
			}
			closeChain()
		}))
	}

	content := NewContainer(
		FlexLayout{Direction: Vertical, Gap: 0},
		itemWidgets...,
	)
	content.Style().Background = st.bg
	// Border is intentionally zero — the menu surface conveys its edge
	// via the shadow alone (painted by menuListView.Draw before content).
	content.Style().BorderSize = 0
	content.Style().Padding = Insets{Top: st.menuPadV, Right: 0, Bottom: st.menuPadV, Left: 0}
	content.Style().Radius = st.radius

	list = newMenuListView(content, items, closeChain, st)
	popup = NewPopup(list)
	return popup, list
}

// newMenuItemWidget builds a list-item row for a menu entry. The
// returned widget implements menuActivatable so the list view's keyboard
// nav can fire activation without poking into private structure.
//
// onSelect runs after the item's Submenu / Checked / OnClick semantics
// have resolved — it's the closer of the popup chain.
func newMenuItemWidget(item *MenuItem, st menuStyle, onSelect func()) *menuItemView {
	return newMenuItemView(item, st, onSelect)
}

// menuItemView is the row widget for a single menu entry.
//
// Layout columns from leading edge:
//   - leadingSpace (12) gap
//   - gutterW (24) reserved when the parent menu has any Checkable item;
//     populated with the ✓ check mark when Checked
//   - label-text (TextBodyLarge, in the row's foreground — or the
//     selected foreground when selected via Checked)
//   - shortcutGap, then shortcut text (TextLabel, muted foreground)
//   - submenuW (24) reserved when the item has a Submenu; populated with
//     the ▸ submenu chevron
//   - trailingSpace (12) gap
//
// Vertical: itemHeight = 48 — the compact one-line row height menus
// render at, rather than the roomier 56 px list default.
//
// State painting order: selected-bg → hover/pressed state-layer → ripple
// → content. Disabled rows skip state-layer / ripple and dim text / icon
// to disabled-label-text-opacity (0.38).
type menuItemView struct {
	BaseWidget
	item     *MenuItem
	style    menuStyle
	hovering bool
	pressed  bool
	onSelect func()
}

func newMenuItemView(item *MenuItem, st menuStyle, onSelect func()) *menuItemView {
	v := &menuItemView{
		BaseWidget: NewBaseWidget(),
		item:       item,
		style:      st,
		onSelect:   onSelect,
	}
	v.SetSelf(v)
	v.SetEnabled(!item.Disabled)
	return v
}

func (v *menuItemView) labelFont() Font    { return ThemeFont(v.style.labelRole) }
func (v *menuItemView) shortcutFont() Font { return ThemeFont(v.style.shortcutRole) }

func (v *menuItemView) Measure(available Size) Size {
	labelW, _ := TextMetrics(v.item.Label, v.labelFont())
	w := v.style.leadingSpace + v.style.gutterW + labelW + v.style.trailingSpace
	if v.item.Shortcut != "" {
		sw, _ := TextMetrics(v.item.Shortcut, v.shortcutFont())
		w += v.style.shortcutGap + sw
	}
	if v.item.Submenu != nil {
		w += v.style.submenuW
	}
	// Pad to the menu min-width so single-item menus don't collapse.
	if w < menuMinWidth {
		w = menuMinWidth
	}
	// Deliberately ignore available.W — the row reports its NATURAL
	// width (label + gutters + chevron + shortcut). Letting the parent
	// FlexLayout's cross-axis stretch handle expansion to the panel's
	// actual width keeps the menu panel sized to its content rather
	// than the full available space (otherwise the panel inside a wide
	// horizontal flex would balloon to fill the whole row).
	return Size{W: w, H: v.style.itemHeight}
}

func (v *menuItemView) HitTest(p Point) Widget {
	if v.Bounds().Contains(p) {
		return v
	}
	return nil
}

func (v *menuItemView) Handle(event Event) bool {
	e, ok := event.(MouseEvent)
	if !ok {
		return false
	}
	if !v.Enabled() {
		return false
	}
	switch e.Type() {
	case EventMouseEnter:
		v.hovering = true
		v.Invalidate()
		return false
	case EventMouseLeave:
		v.hovering = false
		v.Invalidate()
		return false
	case EventMouseDown:
		if e.Button == MouseButtonLeft && v.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
			v.pressed = true
			v.Invalidate()
			v.activate()
			return true
		}
	case EventMouseUp:
		if v.pressed {
			v.pressed = false
			v.Invalidate()
		}
	}
	return false
}

func (v *menuItemView) activate() {
	if v.onSelect != nil {
		v.onSelect()
	}
}

func (v *menuItemView) Draw(canvas Canvas) {
	b := v.Bounds()
	// Refresh theme-derived color tokens each frame so MenuPanel —
	// built once and held in the tree — picks up SetTheme transitions
	// (a retinted palette) without rebuild. Structural fields
	// (gutterW, itemHeight, leadingSpace, …) stay as captured at
	// construction so any Select-style structural overrides hold.
	v.style.refreshColors()
	st := v.style
	// "selected" controls bg tint + text color override. Two paths feed
	// into it: an explicit Selected flag (radio-style "current value")
	// or Checkable+Checked (toggleable item that's currently on). The
	// ✓ glyph in the leading gutter, in contrast, only paints for the
	// Checkable path — Selected items get the tint alone.
	selected := v.item.Selected || (v.item.Checkable && v.item.Checked)
	showCheckGlyph := v.item.Checkable && v.item.Checked

	// Selected row: paint secondary-container backdrop. Otherwise the
	// menu's surface-container shows through (the surrounding Container
	// already filled it, so nothing to do for the unselected case).
	if selected {
		canvas.FillRect(b, st.selectedBg)
	}

	// State-layer (hover/pressed). Enabled rows only; pressed beats hover.
	// Disabled rows skip the layer entirely — menus don't focus disabled
	// items, so there is nothing to give feedback for.
	if v.Enabled() {
		if v.pressed {
			DrawStateLayer(canvas, b, 0, st.stateColor, CurrentTheme().PressedOpacity)
		} else if v.hovering {
			DrawStateLayer(canvas, b, 0, st.stateColor, CurrentTheme().HoverOpacity)
		}
	}

	// Text colors honor selected + disabled multiplicatively.
	labelColor := st.fg
	if selected {
		labelColor = st.selectedFg
	}
	shortcutColor := st.fgMuted
	if selected {
		shortcutColor = st.selectedFg
	}
	if !v.Enabled() {
		// Disabled label text sits at 38% of full contrast.
		// Lerping toward the row's bg (instead of reducing the source
		// color's alpha) produces an opaque color that matches CSS
		// `opacity: 0.38`. Reducing alpha alone goes through DrawText's
		// image/draw path which expects pre-multiplied colors and ends
		// up at the wrong shade (qui's Color is non-premultiplied — see
		// blendPixel in renderer_gl.go).
		bg := st.bg
		if selected {
			bg = st.selectedBg
		}
		labelColor = LerpColor(bg, labelColor, 0.38)
		shortcutColor = LerpColor(bg, shortcutColor, 0.38)
	}

	// Layout the row left-to-right.
	cursor := b.X + st.leadingSpace

	if st.gutterW > 0 {
		gutter := Rect{X: cursor, Y: b.Y, W: st.gutterW, H: b.H}
		if showCheckGlyph {
			iconColor := labelColor
			drawMenuCheck(canvas, gutter, 18, iconColor)
		}
		cursor += st.gutterW
	}

	font := v.labelFont()
	_, textH := TextMetrics(v.item.Label, font)
	labelRect := Rect{
		X: cursor,
		Y: b.Y + (b.H-textH)/2,
		W: b.W, // overruns are clipped by the popup's outer clip
		H: textH,
	}
	canvas.DrawText(v.item.Label, labelRect, labelColor, font)

	if v.item.Shortcut != "" || v.item.Submenu != nil {
		// Right-align trailing content (shortcut text first, then chevron).
		rightCursor := b.X + b.W - st.trailingSpace
		if v.item.Submenu != nil {
			chev := Rect{X: rightCursor - st.submenuW, Y: b.Y, W: st.submenuW, H: b.H}
			drawMenuChevron(canvas, chev, 18, labelColor)
			rightCursor -= st.submenuW
		}
		if v.item.Shortcut != "" {
			sfont := v.shortcutFont()
			sw, sh := TextMetrics(v.item.Shortcut, sfont)
			scRect := Rect{
				X: rightCursor - sw,
				Y: b.Y + (b.H-sh)/2,
				W: sw,
				H: sh,
			}
			canvas.DrawText(v.item.Shortcut, scRect, shortcutColor, sfont)
		}
	}
}

// menuGlyphBox centers a size×size square inside `slot` — the drawing
// box both menu glyphs (✓ and ▸) anchor their polylines to.
func menuGlyphBox(slot Rect, size float32) Rect {
	return Rect{
		X: slot.X + (slot.W-size)/2,
		Y: slot.Y + (slot.H-size)/2,
		W: size,
		H: size,
	}
}

// drawMenuCheck strokes the checked-item mark: a three-point polyline on
// an 18-unit grid, the same anchors widgets.CheckBox uses, so a checked
// menu row and a checked box read as the same mark at any scale. Drawn
// geometrically rather than from an icon font/SVG so the widgets package
// carries no icon-set dependency.
func drawMenuCheck(canvas Canvas, slot Rect, size float32, color Color) {
	box := menuGlyphBox(slot, size)
	s := size / 18
	canvas.DrawPolyline([]Point{
		{X: box.X + 3.0*s, Y: box.Y + 9.5*s},
		{X: box.X + 7.5*s, Y: box.Y + 13.5*s},
		{X: box.X + 14.5*s, Y: box.Y + 6.0*s},
	}, color, 2)
}

// drawMenuChevron strokes the submenu arrow: a two-segment polyline
// pointing right, on the same 18-unit grid as drawMenuCheck.
func drawMenuChevron(canvas Canvas, slot Rect, size float32, color Color) {
	box := menuGlyphBox(slot, size)
	s := size / 18
	canvas.DrawPolyline([]Point{
		{X: box.X + 7.0*s, Y: box.Y + 4.5*s},
		{X: box.X + 12.0*s, Y: box.Y + 9.0*s},
		{X: box.X + 7.0*s, Y: box.Y + 13.5*s},
	}, color, 1.5)
}

// newMenuPanelRow wraps MenuItem.Panel content in a menu row: the menu's
// own vertical padding, the label column's leading inset, and nothing else.
// A plain Container is deliberate — it implements neither menuActivatable
// nor any hover painting, so the hosted content is the only thing that
// reacts to the pointer. A nil child yields an empty row rather than a
// hole in the item/widget index alignment.
func newMenuPanelRow(child Widget, st menuStyle) Widget {
	var children []Widget
	if child != nil {
		children = append(children, child)
	}
	row := NewContainer(FlexLayout{Direction: Vertical}, children...)
	row.Style().Padding = Insets{
		Top: st.menuPadV, Right: st.leadingSpace,
		Bottom: st.menuPadV, Left: st.leadingSpace,
	}
	return row
}

// newMenuSeparator returns a thin horizontal divider widget that does
// not participate in focus or click handling. Drawn at 1 px in
// the divider color with a 12-px horizontal inset. The row reserves a
// small vertical band (8 px)
// so consecutive separators or items don't visually collide.
func newMenuSeparator(st menuStyle) Widget {
	return newMenuSeparatorView(st)
}

type menuSeparatorView struct {
	BaseWidget
	style menuStyle
}

func newMenuSeparatorView(st menuStyle) *menuSeparatorView {
	v := &menuSeparatorView{
		BaseWidget: NewBaseWidget(),
		style:      st,
	}
	v.SetSelf(v)
	return v
}

func (v *menuSeparatorView) Measure(available Size) Size {
	// Separator reports the menu min-width as its NATURAL width and
	// relies on FlexLayout's vertical cross-axis stretch to widen it to
	// the panel's actual width during Layout. Returning `available.W`
	// here used to bleed the menu panel's measured width up to whatever
	// space the parent flex offered (e.g. the showcase's full row),
	// because Container's vertical-flex crossMax took max across all
	// children including this one.
	return Size{W: menuMinWidth, H: 9} // 4 px gap + 1 px line + 4 px gap
}

func (v *menuSeparatorView) Draw(canvas Canvas) {
	b := v.Bounds()
	// Refresh divider color from the current theme so a light/dark
	// or seed swap retints the rule line in step with the menu rows.
	v.style.refreshColors()
	line := Rect{
		X: b.X + v.style.dividerInset,
		Y: b.Y + (b.H-1)/2,
		W: b.W - 2*v.style.dividerInset,
		H: 1,
	}
	if line.W < 0 {
		line.W = 0
	}
	canvas.FillRect(line, v.style.divider)
}

// menuListView wraps a menu's vertical item container with keyboard
// navigation. Up/Down moves the focused index, Enter invokes the
// focused item's OnClick (or opens its submenu), Esc closes the
// popup, Left closes a submenu, Right opens one.
type menuListView struct {
	BaseWidget
	content    *Container
	scroll     *ScrollView // non-nil when the list is taller than the window
	items      []*MenuItem
	focused    int
	closeChain func()
	style      menuStyle
}

// menuViewportMargin is the space kept clear above and below a menu that has
// grown tall enough to scroll, so it never runs flush to the window edges.
const menuViewportMargin = 8

func newMenuListView(content *Container, items []*MenuItem, closeChain func(), st menuStyle) *menuListView {
	lv := &menuListView{
		BaseWidget: NewBaseWidget(),
		content:    content,
		items:      items,
		focused:    firstSelectable(items),
		closeChain: closeChain,
		style:      st,
	}
	lv.SetSelf(lv)
	content.SetParent(lv)
	return lv
}

// firstSelectable picks the first non-separator, non-disabled item so
// keyboard focus lands somewhere reasonable when the menu opens. Panel
// rows are skipped like separators: their content handles its own input,
// so "activating the row" means nothing.
func firstSelectable(items []*MenuItem) int {
	for i, it := range items {
		if it.Separator || it.Disabled || it.Panel != nil {
			continue
		}
		return i
	}
	return -1
}

func (lv *menuListView) Focusable() bool   { return true }
func (lv *menuListView) SetFocused(f bool) {}

// ReleaseChildForTransfer agrees to the ONE transfer this view ever makes:
// handing its content to its own scroll wrapper when the list outgrows the
// window (see Measure).
//
// Without it AdoptWidgetTree refuses the move — a parent that has not agreed
// never has a child taken from it — SetContent leaves the ScrollView empty,
// and the menu draws as a blank panel with a scrollbar. Every menu longer
// than the window did exactly that.
func (lv *menuListView) ReleaseChildForTransfer(child Widget) bool {
	return child == Widget(lv.content)
}

func (lv *menuListView) ChildList() []Widget {
	if lv.scroll != nil {
		return []Widget{lv.scroll}
	}
	if lv.content == nil {
		return nil
	}
	return []Widget{lv.content}
}

// Measure returns the content's natural size, but caps the height to the
// window (minus a small margin) when the item list is taller than that —
// wrapping the content in a ScrollView so a long menu (e.g. a workbook with
// dozens of sheets) scrolls instead of overflowing off both edges.
func (lv *menuListView) Measure(available Size) Size {
	if lv.content == nil {
		return Size{}
	}
	// Measure with an unbounded height so the list reports its NATURAL size
	// (item count × itemHeight). Measuring against `available` would return a
	// height already shrunk to fit the window — the FlexLayout would squeeze
	// items to slivers instead of overflowing, and the overflow would go
	// undetected here.
	//
	// available.H is the height BUDGET the caller allotted (the space above or
	// below the trigger, not the whole window — see placeMenuY). When the list
	// is taller than the budget it wraps in a ScrollView capped to it.
	nat := lv.content.Measure(Size{W: available.W, H: 1 << 20})
	if available.H > 0 && nat.H > available.H {
		if lv.scroll == nil {
			lv.scroll = NewScrollView()
			lv.scroll.SetParent(lv)
		}
		lv.scroll.SetContent(lv.content, nat)
		return Size{W: nat.W, H: available.H}
	}
	if lv.scroll != nil {
		// The list now fits — drop the scroll wrapper and re-parent content.
		lv.scroll = nil
		lv.content.SetParent(lv)
	}
	return nat
}

func (lv *menuListView) Layout(rect Rect) {
	lv.BaseWidget.Layout(rect)
	if lv.scroll != nil {
		lv.scroll.Layout(rect)
	} else if lv.content != nil {
		lv.content.Layout(rect)
	}
}

func (lv *menuListView) HitTest(p Point) Widget {
	if lv.scroll != nil {
		if hit := lv.scroll.HitTest(p); hit != nil {
			return hit
		}
		if lv.Bounds().Contains(p) {
			return lv
		}
		return nil
	}
	if lv.content == nil {
		return nil
	}
	if hit := lv.content.HitTest(p); hit != nil {
		return hit
	}
	if lv.Bounds().Contains(p) {
		return lv
	}
	return nil
}

func (lv *menuListView) Draw(canvas Canvas) {
	// Paint level-2 elevation behind the menu surface. The content
	// Container fills its rounded-rect bg on top.
	//
	// Refresh the palette in case the caller-supplied MenuColors path
	// unpinned or the plain-defaults path wants a refresh.
	// A pinned palette skips the color reset; itemHeight always
	// refreshes.
	lv.style.refreshColors()
	if lv.content == nil {
		return
	}
	if lv.style.elevation > 0 {
		DrawElevation(canvas, lv.Bounds(), lv.style.radius, lv.style.elevation)
	}
	lv.content.Style().Background = lv.style.bg
	if lv.scroll != nil {
		// The ScrollView clips + scrolls the content; give its viewport the
		// same surface color so the menu reads as one panel.
		lv.scroll.Style().Background = lv.style.bg
		lv.scroll.Draw(canvas)
		return
	}
	lv.content.Draw(canvas)
}

// OverlayHaloRect tells Popup the dirty region it must invalidate on
// show + close so the level-2 shadow halo paints + unpaints cleanly.
// Without this, opening the menu upward leaves a gray rim on the
// trigger button area after close, because the framework's default
// overlay invalidation only covers Bounds and the halo bleeds out.
func (lv *menuListView) OverlayHaloRect() Rect {
	return lv.PaintBounds()
}

// PaintBounds satisfies qui.PaintBounder — Container uses it instead
// of Bounds() when deciding whether to skip this child during a
// dirty-region paint. Returning the bounds+shadow halo here is what
// keeps the inline MenuPanel's level-2 shadow visible when a neighbor
// widget (e.g. a Button) invalidates a region that only intersects
// our halo, not our layout box.
func (lv *menuListView) PaintBounds() Rect {
	b := lv.Bounds()
	m := menuShadowMargin
	return Rect{X: b.X - m, Y: b.Y - m, W: b.W + 2*m, H: b.H + 2*m}
}

func (lv *menuListView) Handle(event Event) bool {
	ke, ok := event.(KeyEvent)
	if !ok || ke.Type() != EventKeyDown {
		return false
	}
	switch ke.Key {
	case KeyUp:
		lv.focused = lv.move(-1)
		return true
	case KeyDown:
		lv.focused = lv.move(+1)
		return true
	case KeyEnter, KeySpace:
		lv.activate()
		return true
	case KeyEscape:
		if lv.closeChain != nil {
			lv.closeChain()
		}
		return true
	case KeyRight:
		if lv.focused >= 0 && lv.focused < len(lv.items) {
			it := lv.items[lv.focused]
			if it.Submenu != nil {
				lv.activate()
				return true
			}
		}
	case KeyLeft:
		// For submenus, closing the chain should close this submenu but
		// leave the parent open. The closeChain callback bound in
		// showMenuPopup also walks up to close ancestors — submenu
		// close should happen through Popup.Close instead. Close the
		// immediate popup by calling its Close directly; the chain
		// resolves to closeChain which clears everything. For now this
		// also clears parents; refinement can wire a "close-self-only"
		// callback later if multi-level submenus become common.
		if lv.closeChain != nil {
			lv.closeChain()
		}
		return true
	}
	return false
}

// activate fires the focused item. Mirrors the menuItemView.Handle
// MouseDown path so submenu / OnClick / checkable semantics remain
// consistent across click and keyboard activation.
func (lv *menuListView) activate() {
	if lv.focused < 0 || lv.focused >= len(lv.items) {
		return
	}
	if lv.focused >= lv.content.ChildCount() {
		return
	}
	child := lv.content.ChildAt(lv.focused)
	if item, ok := child.(menuActivatable); ok {
		item.activate()
		return
	}
	if btn, ok := child.(*Button); ok && btn.OnClick != nil {
		btn.OnClick()
	}
}

type menuActivatable interface {
	activate()
}

// move steps the focused index by delta, skipping separators, panel rows
// and disabled entries. Wraps around at the ends.
func (lv *menuListView) move(delta int) int {
	if len(lv.items) == 0 {
		return -1
	}
	cur := lv.focused
	if cur < 0 {
		cur = 0
	}
	n := len(lv.items)
	for k := 0; k < n; k++ {
		cur = (cur + delta + n) % n
		it := lv.items[cur]
		if it.Separator || it.Disabled || it.Panel != nil {
			continue
		}
		return cur
	}
	return lv.focused
}

// ----------------------------------------------------------------------
// MenuBar

// MenuBar is the horizontal strip of top-level menu triggers typically
// rendered at the top of a main window ("File", "Edit", "View", ...).
// Each trigger opens a ContextMenu dropdown when clicked.
//
// Usage:
//
//	mb := widgets.NewMenuBar(window)
//	mb.AddMenu("File", []*widgets.MenuItem{
//	    {Label: "New",  Shortcut: "Cmd+N", OnClick: newFile},
//	    {Label: "Open", Shortcut: "Cmd+O", OnClick: openFile},
//	})
//	mb.AddMenu("Edit", ...)
//	window.SetAcceleratorRegistry(mb.AcceleratorRegistry())
type MenuBar struct {
	Container
	window *Window
	menus  []menuEntry
}

type menuEntry struct {
	Label string
	Items []*MenuItem
	// trigger is the Button that opens this menu's dropdown.
	trigger *Button
}

// NewMenuBar creates an empty menu bar attached to the given window.
// The window reference is needed to push dropdown popups onto its
// overlay stack when a menu trigger is clicked. Plain surface + text
// colors — apps that want a specific palette set mb.Style() after
// construction. The dropdown it opens follows the current theme (see
// ShowContextMenu).
func NewMenuBar(window *Window) *MenuBar {
	mb := &MenuBar{window: window}
	mb.BaseWidget = NewBaseWidget()
	mb.LayoutEngine = FlexLayout{Direction: Horizontal, Gap: 2}
	mb.SetSelf(mb)
	mb.Style().Background = Color{R: 0.95, G: 0.95, B: 0.95, A: 1}
	mb.Style().Foreground = Color{R: 0.10, G: 0.10, B: 0.10, A: 1}
	mb.Style().Padding = Insets{Top: 2, Right: 4, Bottom: 2, Left: 4}
	return mb
}

// AddMenu appends a top-level menu. The caller-supplied items drive
// the dropdown shown when the menu's trigger is clicked. The trigger
// button is a plain widgets.NewButton — retint via
// mb.Menus()[i].Trigger().States for a designed look.
func (mb *MenuBar) AddMenu(label string, items []*MenuItem) {
	entry := menuEntry{Label: label, Items: items}
	idx := len(mb.menus)
	entry.trigger = NewButton(label, func() {
		mb.showDropdown(idx)
	})
	// Flatten the trigger so it reads as a menu-bar entry (no border,
	// tight padding). Colors inherit mb.Style().
	entry.trigger.States.Base.BorderSize = 0
	entry.trigger.States.Base.Radius = 2
	entry.trigger.States.Base.Padding = Insets{Top: 4, Right: 10, Bottom: 4, Left: 10}
	entry.trigger.States.Base.Background = mb.Style().Background
	entry.trigger.States.Base.Foreground = mb.Style().Foreground

	mb.menus = append(mb.menus, entry)
	mb.AddChild(entry.trigger)
}

// showDropdown opens the submenu for the menu at the given index,
// anchored just below the trigger button.
func (mb *MenuBar) showDropdown(idx int) {
	if mb.window == nil || idx < 0 || idx >= len(mb.menus) {
		return
	}
	entry := mb.menus[idx]
	anchor := InteractionBoundsOf(entry.trigger)
	ShowContextMenu(mb.window, anchor.X, anchor.Y+anchor.H+2, entry.Items)
}

// AcceleratorRegistry walks every menu item (including submenus) and
// builds a fresh registry binding each item's Shortcut to its OnClick.
// Attach the returned registry with Window.SetAcceleratorRegistry.
//
// Separators and items without a Shortcut or OnClick are skipped.
// Checkable items get wrapped so firing the accelerator flips Checked
// (matching what a click would do); the caller still needs to redraw
// the menu if they want the tick mark to update without a fresh open.
func (mb *MenuBar) AcceleratorRegistry() *AcceleratorRegistry {
	reg := NewAcceleratorRegistry()
	for _, m := range mb.menus {
		registerItems(reg, m.Items)
	}
	return reg
}

func registerItems(reg *AcceleratorRegistry, items []*MenuItem) {
	for _, it := range items {
		if it.Separator {
			continue
		}
		if it.Submenu != nil {
			registerItems(reg, it.Submenu)
			continue
		}
		if it.Shortcut == "" || it.OnClick == nil {
			continue
		}
		item := it // capture
		fn := func() {
			if item.Checkable {
				item.Checked = !item.Checked
			}
			item.OnClick()
		}
		// Best-effort registration: swallow parse errors so one bad
		// shortcut doesn't break the whole menu.
		_ = reg.Register(item.Shortcut, fn)
	}
}
