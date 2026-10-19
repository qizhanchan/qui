package widgets

import (
	"runtime"
	"strings"
	"time"

	. "github.com/qizhanchan/qui"
)

// Menus are built on top of Popup — both ContextMenu and MenuBar
// dropdowns are just non-modal popups containing a vertical list of
// clickable items. The helpers below construct those popups with
// consistent styling, keyboard navigation, and auto-close-on-select.
//
// Visual defaults (live popups) are a plain desktop menu in the theme's
// colors: a SurfaceOverlay panel with RadiusSmall corners and a soft
// 0 2px 6px 2px shadow, 6 px vertical padding, 32 px rows with a 14 px
// label starting 38 px in (an 18 px icon / check column sits 12 px from
// the left), right-aligned 14 px medium shortcuts in TextMuted, a filled
// submenu arrow, a HoverOpacity hover fill and 17 px separator blocks with a
// full-width hairline. The tokens live in Theme.Popup (see PopupTokens);
// colors are also overridable per menu through MenuColors.

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
// option in a chooser). Live menus mark it with the ✓
// in the icon column; the plain MenuPanel look tints the row instead.
// Use it when one item out of N is the active choice.
type MenuItem struct {
	Label    string
	Shortcut string // displayed on the right (also harvested by MenuBar.AcceleratorRegistry)
	// Badge is a short caption drawn as a small pill on the row's right,
	// before the shortcut / submenu arrow — Google's "New" marker on a
	// freshly added command (11 px medium, blue on light blue; see
	// Theme.Popup.MenuBadgeText / MenuBadgeSurface). Empty = none.
	Badge     string
	BadgeKey  string // message key resolving Badge
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
	// ID is a stable identifier for the row widget (qui-agent `#id`
	// selectors, tests); it doesn't change with the language.
	ID string
	// LabelKey makes the label come from the message catalog, resolved
	// each time the row is measured / drawn; Label is the fallback.
	LabelKey string
	// Icon is drawn in a leading icon column (reserved for the whole menu
	// when any row has one), tinted with the row's text color.
	Icon VectorSource
	// Content replaces the label/shortcut with caller-built content while
	// keeping normal row behavior — hover and keyboard highlight, click
	// and Enter to pick, OnClick then close. (Panel, by contrast, hands the
	// row over entirely.)
	Content func() Widget
	// Foreground, when it has alpha, overrides the row's text color (label,
	// icon, check glyph). Lets a menu tint rows individually — e.g. a
	// method chooser colouring GET green and DELETE red. A selected row's
	// backdrop tint still paints; disabled rows still dim it.
	Foreground Color
}

// DisplayLabel is the label the row shows: LabelKey resolved through the
// catalog, falling back to Label.
func (it *MenuItem) DisplayLabel() string {
	if it.LabelKey == "" {
		return it.Label
	}
	return TranslateOr("", it.LabelKey, it.Label, nil)
}

// DisplayBadge is the localized badge text.
func (it *MenuItem) DisplayBadge() string {
	if it.BadgeKey == "" {
		return it.Badge
	}
	return TOr(it.BadgeKey, it.Badge)
}

// displayShortcut renders the portable accelerator spelling in the
// platform's own notation. The registry still binds the raw Shortcut.
func displayShortcut(s string) string {
	if s == "" {
		return ""
	}
	if runtime.GOOS != "darwin" {
		return strings.ReplaceAll(s, "CmdOrCtrl", "Ctrl")
	}
	r := strings.NewReplacer(
		"CmdOrCtrl", "⌘",
		"Cmd", "⌘",
		"Ctrl", "⌃",
		"Shift", "⇧",
		"Alt", "⌥",
		"Option", "⌥",
		"+", "",
	)
	return r.Replace(s)
}

// MenuActivatable is implemented by a menu row the keyboard can pick: when
// it is highlighted, Enter / Space calls ActivateMenuRow. Built-in rows
// implement it; so can the widget a MenuItem.Panel returns, which then
// joins arrow-key navigation instead of being skipped.
type MenuActivatable interface {
	ActivateMenuRow()
}

// MenuHighlighter is implemented by a row that paints the keyboard
// highlight itself. Panel content implementing MenuActivatable usually
// wants it too.
type MenuHighlighter interface {
	SetMenuHighlighted(bool)
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

	// Hover, when set, is a solid fill for the hovered / keyboard-
	// highlighted row (instead of the faint StateLayer overlay), with
	// HoverFg as that row's text colour — the JetBrains / macOS menu
	// highlight. HoverInset insets the fill from the panel's left and
	// right edges and HoverRadius rounds it, so it reads as a pill
	// rather than a full-bleed band.
	Hover       Color
	HoverFg     Color
	HoverInset  float32
	HoverRadius float32
	// SeparatorHeight overrides a separator row's height (the rule sits
	// in its middle); zero keeps the style's default.
	SeparatorHeight float32
	// Radius overrides the panel's corner radius; Border (with
	// BorderWidth, default 1) strokes the panel's edge — the JetBrains
	// popup outline. Zero keeps the theme's radius and no outline.
	Radius      float32
	Border      Color
	BorderWidth float32
}

// Default menu typography + row height. Every menu popup (context menus,
// menu-bar dropdowns, the sheet-list-style anchored menus, and the Select
// dropdown) reads these, so an app retunes ALL its menus once at startup
// via SetMenuMetrics — mirroring how jetbrainsmono.Use() swaps the global
// default font. Defaults are a compact desktop menu: body (14px) labels,
// label (14px medium) shortcuts, 32px rows.
var (
	menuLabelRole    = TextBody
	menuShortcutRole = TextLabel
	menuItemHeight   = float32(32)
)

// SetMenuMetrics overrides the default menu typography + row height for
// every popup created afterward. Pass itemHeight <= 0 to keep the current
// height. Intended to be called once at startup (not per-window state, so
// it is safe as a package-level default). Example — a roomier list-menu
// look (16px labels, 48px rows):
//
//	widgets.SetMenuMetrics(qui.TextBodyLarge, qui.TextLabel, 48)
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
	elevation     int // Theme.Elevation index for the popup shadow; 0 = shadow (below) or flat
	iconW         float32

	// shadow is a single drop shadow painted when elevation is 0 (the
	// stock 0 2px 6px 2px). Zero = flat.
	shadow ElevationSpec
	// corners, when set, overrides radius per corner for the panel fill —
	// a menu-bar dropdown's square top-left corner joining its trigger.
	corners CornerRadii
	// minWidth floors the panel width (Theme.Popup.MenuMinWidth for top-level menus).
	minWidth float32
	// labelInset > 0 switches rows to the column layout: the
	// label always starts labelInset px in, and the check mark or icon is
	// drawn iconSize px square, iconInset px from the row's left edge.
	// Zero keeps the collapsing gutter/icon columns (Select, MenuPanel).
	labelInset float32
	iconInset  float32
	iconSize   float32
	// hoverColor, when it has alpha, is the hovered / highlighted row fill,
	// painted as-is (instead of stateColor at Theme.HoverOpacity).
	hoverColor Color
	// hoverFg (when it has alpha) is the hovered row's text colour;
	// hoverInset / hoverRadius shape the hover fill (see MenuColors).
	hoverFg     Color
	hoverInset  float32
	hoverRadius float32
	// borderW is the outline width when border has alpha.
	borderW float32
	// sepHeight is a separator row's height (the line sits in its middle);
	// zero = 9.
	sepHeight float32
	// filledArrow draws the submenu indicator as a small solid ► (the
	// glyph) instead of a stroked chevron.
	filledArrow bool

	// navigate, set by a MenuBar, switches to the previous (-1) or next
	// (+1) top-level menu: Left in a top-level dropdown, Right on a row
	// without a submenu. Inherited by submenus.
	navigate func(dir int)

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
	if c.Hover.A > 0 {
		st.hoverColor = c.Hover
	}
	if c.HoverFg.A > 0 {
		st.hoverFg = c.HoverFg
	}
	if c.HoverInset > 0 {
		st.hoverInset = c.HoverInset
	}
	if c.HoverRadius > 0 {
		st.hoverRadius = c.HoverRadius
	}
	if c.SeparatorHeight > 0 {
		st.sepHeight = c.SeparatorHeight
	}
	if c.Radius > 0 {
		st.radius = c.Radius
	}
	if c.Border.A > 0 {
		st.border = c.Border
		st.borderW = c.BorderWidth
		if st.borderW <= 0 {
			st.borderW = 1
		}
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
		st.selectedBg = baselineBlue
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

// themedMenuColors builds a menu palette from the current theme's popup
// tokens (Theme.Popup, resolved through Theme.ResolvedPopup) — a white
// panel, near-black text, 38% shortcuts and a hairline divider. The
// selected row is a NEUTRAL tint rather than an accent-derived fill: apps
// commonly retint the accent and surface but leave secondary roles at the
// baseline, so an accent-derived selection leaked a foreign hue into menus
// whose theme was otherwise recolored. Read fresh at open time so a live
// theme swap retints the next menu; shared by the context-menu popups and
// the Select dropdown. Callers wanting a designed selection color pass
// MenuColors through the *Styled entry points.
func themedMenuColors() MenuColors {
	p := CurrentTheme().ResolvedPopup()
	return MenuColors{
		Background:      p.MenuSurface,
		Foreground:      p.MenuText,
		ForegroundMuted: p.MenuTextMuted,
		Divider:         p.MenuDivider,
		SelectedBg:      p.MenuSelected,
		SelectedFg:      p.MenuText,
		StateLayer:      p.MenuText,
	}
}

// themedMenuStyle is the base for every live menu popup — menus,
// submenus, context menus and menu-bar dropdowns: the stock menu geometry
// (see the package comment) in the current theme's popup tokens.
// Explicit callers layer overrides on top via styledMenuStyle.
func themedMenuStyle() menuStyle {
	st := defaultMenuStyle()
	st.ApplyColors(themedMenuColors())
	p := CurrentTheme().ResolvedPopup()
	st.leadingSpace = 12
	st.trailingSpace = 15
	st.shortcutGap = 24
	st.submenuW = 24
	st.menuPadV = 6
	st.dividerInset = 0
	st.sepHeight = 17
	st.radius = p.MenuRadius
	st.elevation = 0
	st.shadow = p.MenuShadow
	st.hoverColor = p.MenuHover
	st.labelInset = 38
	st.iconInset = 12
	st.iconSize = 18
	st.filledArrow = true
	return st
}

// topLevelMenuStyle is themedMenuStyle with the top-level panel floor
// (Theme.Popup.MenuMinWidth) — context menus and menu-bar dropdowns.
// Submenus and anchored dropdowns keep their natural width.
func topLevelMenuStyle() menuStyle {
	st := themedMenuStyle()
	st.minWidth = CurrentTheme().ResolvedPopup().MenuMinWidth
	return st
}

// paintHover fills a hovered row with the solid hover colour, inset and
// rounded per the style.
func (st *menuStyle) paintHover(canvas Canvas, b Rect) {
	r := Rect{X: b.X + st.hoverInset, Y: b.Y, W: b.W - 2*st.hoverInset, H: b.H}
	if st.hoverRadius > 0 {
		canvas.FillRoundedRect(r, st.hoverRadius, st.hoverColor)
		return
	}
	canvas.FillRect(r, st.hoverColor)
}

// flattenOver returns c composited over an opaque bg as an opaque color.
// Text is drawn opaque (see menuItemView.Draw): a translucent token such
// as the 38% shortcut grey is resolved against the row surface here.
func flattenOver(c, bg Color) Color {
	if c.A >= 1 || bg.A <= 0 {
		return c
	}
	solid := c
	solid.A = 1
	return LerpColor(bg, solid, c.A)
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
	// Build rows AFTER applying the palette: rows and dividers copy the
	// style at construction, so post-applying colors to the list left
	// foreground / selected / hover / divider colors at the defaults.
	st := styledMenuStyle(colors, elevation)
	_, list := buildMenuPopupStyled(nil, nil, nil, items, st)
	return list
}

// ShowContextMenu opens a menu popup at (x, y) and returns the popup
// (so callers can force-close it). The popup auto-dismisses on
// outside click, Esc, or any selection. Keyboard navigation
// (Up/Down/Enter/Left/Right) is active once the popup is shown.
func ShowContextMenu(window *Window, x, y float32, items []*MenuItem) *Popup {
	return showContextMenuStyled(window, x, y, items, topLevelMenuStyle())
}

// ShowContextMenuStyled is ShowContextMenu with a caller palette +
// elevation applied to the popup surface — submenus inherit the same
// style. Zero-A fields in colors resolve from the current theme (the
// base themedMenuStyle); elevation <= 0 keeps the themed level-2 shadow.
func ShowContextMenuStyled(window *Window, x, y float32, items []*MenuItem, colors MenuColors, elevation int) *Popup {
	st := styledMenuStyle(colors, elevation)
	st.minWidth = CurrentTheme().ResolvedPopup().MenuMinWidth
	return showContextMenuStyled(window, x, y, items, st)
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
	case triggerTop == triggerBottom && natH <= winH-2*edge:
		// A point (context) menu has no trigger to keep clear: rather
		// than scroll, slide it up until it fits, as a browser does.
		return winH - edge - natH, natH
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
	return showAnchoredPopupGap(window, popup, focus, anchorFn, size, 2, false, true)
}

// ShowMenuBarDropdown opens items as a menu-bar dropdown under the trigger
// whose live bounds anchorFn returns: flush against the trigger's bottom
// edge, at least Theme.Popup.MenuMinWidth wide, with a square top-left
// corner so panel and (open) trigger read as one shape — the column-layout
// editors' File / Edit / … menus. It flips above, all corners round, when
// there's no room below. Returns the popup so callers can force-close it.
func ShowMenuBarDropdown(window *Window, anchorFn func() Rect, items []*MenuItem) *Popup {
	return showMenuBarDropdown(window, anchorFn, items, topLevelMenuStyle())
}

func showMenuBarDropdown(window *Window, anchorFn func() Rect, items []*MenuItem, st menuStyle) *Popup {
	if window == nil || anchorFn == nil {
		return nil
	}
	if st.minWidth == 0 {
		st.minWidth = CurrentTheme().ResolvedPopup().MenuMinWidth
	}
	popup, list := buildMenuPopupStyled(window, nil, nil, items, st)
	if popup == nil {
		return nil
	}
	size := list.Measure(window.Size())
	return showAnchoredPopupGap(window, popup, list, anchorFn, size, 0, true, true)
}

// showAnchoredPopupGap is showAnchoredPopup with the anchor gap spelled
// out. menuBar squares the panel's top-left corner while it hangs below
// the anchor (see ShowMenuBarDropdown).
// takeFocus false leaves keyboard focus on the trigger (a <select> keeps it
// while its list is open, and forwards the list's keys — see Select).
func showAnchoredPopupGap(window *Window, popup *Popup, focus Widget, anchorFn func() Rect, size Size, gap float32, menuBar, takeFocus bool) *Popup {
	if window == nil || popup == nil || anchorFn == nil {
		return nil
	}
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
		if menuBar && lv != nil {
			if y >= anchor.Y+anchor.H && x == anchor.X {
				r := lv.style.radius
				lv.style.corners = CornerRadii{TL: 0, TR: r, BR: r, BL: r}
			} else {
				lv.style.corners = CornerRadii{}
			}
		}
		if firstShow {
			popup.showAtSize(window, x, y, s)
			if focus != nil && takeFocus {
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
	// A submenu that would run off the right edge opens to the left of its
	// parent instead of being slid back over the parent panel.
	size := MeasureConstrained(list, window.Size())
	if size.W > 0 && x+size.W > window.Size().W && x-size.W >= 0 {
		x -= size.W
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
	// A submenu is never a top-level panel: natural width, all corners round.
	subStyle.minWidth = 0
	subStyle.corners = CornerRadii{}

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
	st.iconW = 0
	for _, it := range items {
		if it.Icon != nil {
			st.iconW = 24
			break
		}
	}
	if st.labelInset > 0 {
		// column layout: one fixed icon / check column, label at
		// labelInset whether or not any row has an icon.
		st.gutterW, st.iconW = 0, 0
	}

	var popup *Popup
	var list *menuListView
	var itemWidgets []Widget
	// rows parallels items: the widget keyboard navigation drives (the
	// panel's own content for a Panel row).
	rows := make([]Widget, 0, len(items))

	// openSub / openSubIdx track the submenu this menu currently shows
	// (opened by click, Right or hover) so another row can replace it.
	var openSub *Popup
	openSubIdx := -1
	// Aim tolerance: aimFrom is where the pointer last was on the row whose
	// submenu is open; pointer / movedAt are its latest position and the time
	// it last moved. A pointer crossing a sibling row on its way to the
	// submenu keeps it open (see menuAiming).
	var aimFrom, pointer Point
	var aimSet bool
	var movedAt time.Time
	closeOpenSub := func() {
		if openSub != nil {
			sub := openSub
			openSub, openSubIdx = nil, -1
			aimSet = false
			sub.Close()
		}
	}
	// hovered is the row under the pointer (-1: none — the pointer is in
	// a submenu or outside); hoverGen invalidates superseded timers.
	hovered := -1
	hoverGen := 0

	// closeChain dismisses this popup AND everything it was opened from.
	// It walks the chain through the parent's own closeChain rather than
	// calling parent.Close() once, which only ever unwound one level.
	closeChain := func() {
		closeOpenSub()
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
			sep := newMenuSeparator(st)
			itemWidgets = append(itemWidgets, sep)
			rows = append(rows, sep)
			continue
		}
		if it.Panel != nil {
			// Exactly one widget per item, always: rows / itemWidgets are
			// indexed by ITEM index, so a row that appended nothing — or two
			// — would misdirect every keyboard activation below it.
			child := it.Panel(closeChain)
			itemWidgets = append(itemWidgets, newMenuPanelRow(it, child, st))
			rows = append(rows, child)
			continue
		}
		onSelect := func() {
			if it.Submenu != nil {
				if openSub != nil && openSub.IsShown() && openSubIdx == idx {
					return // already showing this row's submenu
				}
				closeOpenSub()
				// Open submenu anchored to this row's right edge. In the
				// column layout the panel rises by its own padding so its
				// first row lines up with the row that opened it.
				// InteractionBoundsOf maps through a scrolling menu's
				// transform; Bounds() is content-space and opened submenus
				// at the wrong Y after a scroll.
				row := InteractionBoundsOf(itemWidgets[idx])
				y := row.Y
				if subStyle.labelInset > 0 {
					y -= subStyle.menuPadV
				}
				// Open off the PANEL's right edge, not the row's: a scrolling
				// menu's rows stop short of its scrollbar.
				right := row.X + row.W
				if list != nil {
					if lb := list.Bounds(); lb.W > 0 {
						right = lb.X + lb.W
					}
				}
				sub := showMenuPopup(window, popup, closeChain, right, y, it.Submenu, subStyle)
				if sub == nil {
					return
				}
				openSub, openSubIdx = sub, idx
				if hovered == idx {
					aimFrom, aimSet = pointer, true
				}
				// The pointer may travel back over this menu while the
				// submenu is up: route it to these rows (hover switches /
				// closes the submenu, a click picks the row).
				parentPopup := popup
				sub.passThrough = func(pt Point) Widget {
					if parentPopup == nil || !parentPopup.IsShown() {
						return nil
					}
					if parentPopup.Bounds().Contains(pt) {
						return parentPopup.HitTest(pt)
					}
					if parentPopup.passThrough != nil {
						return parentPopup.passThrough(pt)
					}
					return nil
				}
				v, isItem := itemWidgets[idx].(*menuItemView)
				// The opening row keeps its highlight while its submenu is up.
				if isItem {
					v.setSubmenuOpen(true)
				}
				prev := sub.OnClose
				sub.OnClose = func() {
					if prev != nil {
						prev()
					}
					if isItem {
						v.setSubmenuOpen(false)
					}
					if openSub == sub {
						openSub, openSubIdx = nil, -1
					}
				}
				return
			}
			closeOpenSub()
			if it.Checkable {
				it.Checked = !it.Checked
			}
			if it.OnClick != nil {
				it.OnClick()
			}
			closeChain()
		}
		// Google-style hover: resting on a row for menuHoverDelay opens
		// its submenu, or closes a sibling's. Moving on to the submenu
		// before the delay leaves hovered at -1, which keeps it open.
		var row Widget
		onMove := func(p Point) {
			// Events arrive in the row's own space (a scrolling menu is a
			// transform away from window space); the aim test compares
			// against overlay bounds, so map out.
			if row != nil {
				p = LocalPointToWindow(row, p)
			}
			pointer, movedAt = p, time.Now()
			if openSub != nil && openSubIdx == idx {
				aimFrom, aimSet = p, true
			}
		}
		onHover := func(in bool) {
			if !in {
				if hovered == idx {
					hovered = -1
				}
				return
			}
			hovered = idx
			hoverGen++
			gen := hoverGen
			var settle func()
			settle = func() {
				if gen != hoverGen || hovered != idx || popup == nil || !popup.IsShown() {
					return
				}
				if openSub != nil && openSub.IsShown() && openSubIdx == idx {
					return
				}
				// A pointer still travelling toward the open submenu across
				// this sibling row is not choosing the row: wait for it to
				// arrive or come to rest.
				if MenuHoverDelay > 0 && openSub != nil && aimSet &&
					time.Since(movedAt) < MenuHoverDelay &&
					menuAiming(aimFrom, pointer, openSub.Bounds()) {
					afterMenuHoverDelay(window, settle)
					return
				}
				if it.Submenu != nil && !it.Disabled {
					onSelect() // closes any sibling submenu, opens this one
					return
				}
				closeOpenSub()
			}
			afterMenuHoverDelay(window, settle)
		}
		if it.Content != nil {
			cr := newMenuContentRow(it, st, onSelect)
			cr.onHover, cr.onMove = onHover, onMove
			row = cr
		} else {
			iv := newMenuItemWidget(it, st, onSelect)
			iv.onHover, iv.onMove = onHover, onMove
			row = iv
		}
		itemWidgets = append(itemWidgets, row)
		rows = append(rows, row)
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
	if !st.corners.IsZero() {
		// Container paints a uniform radius only; menuListView.Draw fills
		// the per-corner panel itself.
		content.Style().Background = Color{}
	}

	list = newMenuListView(content, items, closeChain, st)
	list.rows = rows
	list.focused = list.moveFrom(-1, +1)
	if list.focused >= 0 && !list.selectable(list.focused) {
		list.focused = -1
	}
	list.isSub = parent != nil
	popup = NewPopup(list)
	// A menu that closes takes its open submenu with it (callers chain
	// their own OnClose after this one).
	popup.OnClose = closeOpenSub
	list.closeSelf = popup.Close
	return popup, list
}

// MenuHoverDelay is how long the pointer rests on a menu row before its
// submenu opens (or a sibling's submenu closes) — Google's menus use
// ~218 ms. Zero acts at once (tests).
var MenuHoverDelay = 218 * time.Millisecond

// afterMenuHoverDelay runs fn on the UI thread once MenuHoverDelay has
// elapsed.
func afterMenuHoverDelay(w *Window, fn func()) {
	if MenuHoverDelay <= 0 || w == nil {
		fn()
		return
	}
	time.AfterFunc(MenuHoverDelay, func() { w.PostJob(fn) })
}

// menuAiming reports whether a pointer at cur, which left the row that
// opened a submenu at from, is on its way to that submenu: cur lies inside
// the triangle from the departure point to the submenu panel's near edge
// (the classic cascading-menu "aim" test). Moving across a sibling row
// inside that triangle must not switch submenus.
func menuAiming(from, cur Point, sub Rect) bool {
	edgeX := sub.X
	if sub.X+sub.W/2 < from.X {
		edgeX = sub.X + sub.W // the submenu opened to the left
	}
	a, b := Point{X: edgeX, Y: sub.Y}, Point{X: edgeX, Y: sub.Y + sub.H}
	cross := func(p, q, r Point) float32 { return (q.X-p.X)*(r.Y-p.Y) - (q.Y-p.Y)*(r.X-p.X) }
	d1, d2, d3 := cross(from, a, cur), cross(a, b, cur), cross(b, from, cur)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// notifyMenuHover reports a row's pointer enter / leave to onHover and its
// position (on enter and every move) to onMove — either may be nil.
func notifyMenuHover(onHover func(bool), onMove func(Point), e MouseEvent) {
	switch e.Type() {
	case EventMouseEnter:
		if onMove != nil {
			onMove(Point{X: e.X, Y: e.Y})
		}
		if onHover != nil {
			onHover(true)
		}
	case EventMouseLeave:
		if onHover != nil {
			onHover(false)
		}
	case EventMouseMove:
		if onMove != nil {
			onMove(Point{X: e.X, Y: e.Y})
		}
	}
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
	item        *MenuItem
	style       menuStyle
	hovering    bool
	highlighted bool // keyboard highlight
	pressed     bool
	subOpen     bool // this row's submenu is showing
	onSelect    func()
	// onHover reports pointer enter (true) / leave (false) — the hover
	// open / close of submenus. Fires for disabled rows too; onMove reports
	// the pointer position on enter and every move.
	onHover func(in bool)
	onMove  func(p Point)
}

func (v *menuItemView) setSubmenuOpen(on bool) {
	if v.subOpen != on {
		v.subOpen = on
		v.Invalidate()
	}
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
	if item.ID != "" {
		v.SetID(item.ID)
	}
	return v
}

// SetMenuHighlighted paints (or clears) the keyboard highlight.
func (v *menuItemView) SetMenuHighlighted(on bool) {
	if v.highlighted != on {
		v.highlighted = on
		v.Invalidate()
	}
}

// ActivateMenuRow picks the row, exactly like a click.
func (v *menuItemView) ActivateMenuRow() { v.activate() }

func (v *menuItemView) labelFont() Font    { return ThemeFont(v.style.labelRole) }
func (v *menuItemView) shortcutFont() Font { return ThemeFont(v.style.shortcutRole) }

func (v *menuItemView) Measure(available Size) Size {
	labelW, _ := TextMetrics(v.item.DisplayLabel(), v.labelFont())
	w := v.style.leadingSpace + v.style.gutterW + v.style.iconW + labelW + v.style.trailingSpace
	if v.style.labelInset > 0 {
		w = v.style.labelInset + labelW + v.style.trailingSpace
	}
	if sc := displayShortcut(v.item.Shortcut); sc != "" {
		sw, _ := TextMetrics(sc, v.shortcutFont())
		w += v.style.shortcutGap + sw
	}
	if v.item.DisplayBadge() != "" {
		w += menuBadgeGap + menuBadgeSize(v.item.DisplayBadge()).W
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
	notifyMenuHover(v.onHover, v.onMove, e)
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
	if st.labelInset > 0 {
		// Live menus mark both a checked row and a radio-style
		// current value (Selected — a <select>'s option) with the ✓ alone.
		selected = false
		showCheckGlyph = (v.item.Checkable && v.item.Checked) || v.item.Selected
	}

	// Selected row: paint secondary-container backdrop. Otherwise the
	// menu's surface-container shows through (the surrounding Container
	// already filled it, so nothing to do for the unselected case).
	if selected {
		canvas.FillRect(b, st.selectedBg)
	}

	// State-layer (hover/pressed). Enabled rows only; pressed beats hover.
	// Disabled rows skip the layer entirely — menus don't focus disabled
	// items, so there is nothing to give feedback for.
	hot := v.Enabled() && st.hoverColor.A > 0 && (v.pressed || v.hovering || v.highlighted || v.subOpen)
	if v.Enabled() {
		switch {
		case hot:
			st.paintHover(canvas, b)
		case v.pressed:
			DrawStateLayer(canvas, b, 0, st.stateColor, CurrentTheme().PressedOpacity)
		case v.hovering || v.highlighted || v.subOpen:
			DrawStateLayer(canvas, b, 0, st.stateColor, CurrentTheme().HoverOpacity)
		}
	}

	// Text colors honor selected + disabled multiplicatively.
	labelColor := st.fg
	if selected {
		labelColor = st.selectedFg
	}
	// A per-row Foreground overrides the palette (but disabled rows still
	// dim it below), so a menu can colour rows individually.
	if v.item.Foreground.A > 0 {
		labelColor = v.item.Foreground
	}
	shortcutColor := st.fgMuted
	if selected {
		shortcutColor = st.selectedFg
	}
	{
		bg := st.bg
		if selected {
			bg = st.selectedBg
		}
		if hot && st.hoverColor.A >= 1 {
			bg = st.hoverColor
		}
		if hot && st.hoverFg.A > 0 && v.item.Foreground.A == 0 {
			labelColor = st.hoverFg
		}
		labelColor = flattenOver(labelColor, bg)
		shortcutColor = flattenOver(shortcutColor, bg)
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

	if st.labelInset > 0 {
		// column layout: the check mark (or else the icon) in the
		// fixed column, the label at labelInset.
		col := Rect{X: b.X + st.iconInset, Y: b.Y + (b.H-st.iconSize)/2, W: st.iconSize, H: st.iconSize}
		if showCheckGlyph {
			drawMenuCheck(canvas, col, st.iconSize, labelColor)
		} else if v.item.Icon != nil {
			canvas.DrawVector(v.item.Icon, col, labelColor)
		}
		cursor = b.X + st.labelInset
	} else if st.gutterW > 0 {
		gutter := Rect{X: cursor, Y: b.Y, W: st.gutterW, H: b.H}
		if showCheckGlyph {
			iconColor := labelColor
			drawMenuCheck(canvas, gutter, 18, iconColor)
		}
		cursor += st.gutterW
	}
	if st.iconW > 0 {
		if v.item.Icon != nil {
			const iconSize = 18
			canvas.DrawVector(v.item.Icon, Rect{X: cursor + (st.iconW-iconSize)/2 - 3, Y: b.Y + (b.H-iconSize)/2, W: iconSize, H: iconSize}, labelColor)
		}
		cursor += st.iconW
	}

	font := v.labelFont()
	label := v.item.DisplayLabel()
	_, textH := TextMetrics(label, font)
	labelRect := Rect{
		X: cursor,
		Y: b.Y + (b.H-textH)/2,
		W: b.W, // overruns are clipped by the popup's outer clip
		H: textH,
	}
	canvas.DrawText(label, labelRect, labelColor, font)

	if v.item.Shortcut != "" || v.item.Submenu != nil || v.item.DisplayBadge() != "" {
		// Right-align trailing content (chevron first, then the shortcut
		// text, then the badge pill).
		rightCursor := b.X + b.W - st.trailingSpace
		if v.item.Submenu != nil {
			if st.filledArrow {
				drawMenuArrow(canvas, b.X+b.W-menuArrowInset, b.Y+b.H/2, labelColor)
			} else {
				chev := Rect{X: rightCursor - st.submenuW, Y: b.Y, W: st.submenuW, H: b.H}
				drawMenuChevron(canvas, chev, 18, labelColor)
			}
			rightCursor -= st.submenuW
		}
		if sc := displayShortcut(v.item.Shortcut); sc != "" {
			sfont := v.shortcutFont()
			sw, sh := TextMetrics(sc, sfont)
			scRect := Rect{
				X: rightCursor - sw,
				Y: b.Y + (b.H-sh)/2,
				W: sw,
				H: sh,
			}
			canvas.DrawText(sc, scRect, shortcutColor, sfont)
			rightCursor -= sw + st.shortcutGap
		} else if v.item.DisplayBadge() != "" {
			rightCursor -= menuBadgeGap
		}
		if v.item.DisplayBadge() != "" {
			sz := menuBadgeSize(v.item.DisplayBadge())
			drawMenuBadge(canvas, Rect{X: rightCursor - sz.W, Y: b.Y + (b.H-sz.H)/2, W: sz.W, H: sz.H}, v.item.DisplayBadge(), v.Enabled())
		}
	}
}

// Badge pill geometry (a "New"-style marker): 11 px medium caption in
// a 16 px pill with 6 px side padding, 8 px clear of what it sits beside.
const (
	menuBadgeHeight float32 = 16
	menuBadgePadX   float32 = 6
	menuBadgeGap    float32 = 8
)

func menuBadgeFont() Font {
	f := ThemeFont(TextLabel)
	f.Size = 11
	return f
}

// menuBadgeSize is the pill's size for caption text.
func menuBadgeSize(text string) Size {
	w, _ := TextMetrics(text, menuBadgeFont())
	return Size{W: w + 2*menuBadgePadX, H: menuBadgeHeight}
}

// drawMenuBadge paints the pill in rect: the theme's badge surface with the
// caption centred in the badge text color. A disabled row's pill dims with
// the rest of the row.
func drawMenuBadge(canvas Canvas, rect Rect, text string, enabled bool) {
	p := CurrentTheme().ResolvedPopup()
	fg, bg := p.MenuBadgeText, p.MenuBadgeSurface
	if !enabled {
		fg = LerpColor(p.MenuSurface, fg, 0.38)
		bg = LerpColor(p.MenuSurface, bg, 0.38)
	}
	canvas.FillRoundedRect(rect, rect.H/2, bg)
	font := menuBadgeFont()
	tw, th := TextMetrics(text, font)
	canvas.DrawText(text, Rect{X: rect.X + (rect.W-tw)/2, Y: rect.Y + (rect.H-th)/2, W: tw, H: th}, fg, font)
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

// menuArrowInset is the gap between a submenu arrow's tip and the row's
// right edge in the column layout.
const menuArrowInset float32 = 10

// drawMenuArrow fills the submenu indicator: a small solid ►
// (5 x 8) whose tip sits at (tipX, midY).
func drawMenuArrow(canvas Canvas, tipX, midY float32, color Color) {
	const w, h = 5, 8
	p := NewPath().MoveTo(tipX-w, midY-h/2).LineTo(tipX, midY).LineTo(tipX-w, midY+h/2).Close()
	canvas.DrawShape(ShapePath{Path: p}, DefaultPaint(color))
}

// newMenuPanelRow wraps MenuItem.Panel content in a menu row: the menu's
// own vertical padding, the label column's leading inset, and nothing else.
// A plain Container is deliberate — it implements neither menuActivatable
// nor any hover painting, so the hosted content is the only thing that
// reacts to the pointer. A nil child yields an empty row rather than a
// hole in the item/widget index alignment.
func newMenuPanelRow(item *MenuItem, child Widget, st menuStyle) Widget {
	var children []Widget
	if child != nil {
		children = append(children, child)
	}
	row := NewContainer(FlexLayout{Direction: Vertical}, children...)
	row.Style().Padding = Insets{
		Top: st.menuPadV, Right: st.leadingSpace,
		Bottom: st.menuPadV, Left: st.leadingSpace,
	}
	if item != nil && item.ID != "" {
		row.SetID(item.ID)
	}
	return row
}

// menuContentRow hosts MenuItem.Content: caller-built content with the
// full row behavior (hover / keyboard highlight, click and Enter pick it).
type menuContentRow struct {
	Container
	item        *MenuItem
	style       menuStyle
	hovering    bool
	highlighted bool
	onSelect    func()
	onHover     func(in bool)
	onMove      func(p Point)
}

func newMenuContentRow(item *MenuItem, st menuStyle, onSelect func()) *menuContentRow {
	r := &menuContentRow{item: item, style: st, onSelect: onSelect}
	r.BaseWidget = NewBaseWidget()
	r.LayoutEngine = FlexLayout{Direction: Horizontal, AlignItems: AlignCenter}
	r.SetSelf(r)
	r.SetEnabled(!item.Disabled)
	r.Style().Padding = Insets{Left: st.leadingSpace + st.gutterW + st.iconW, Right: st.trailingSpace}
	if st.labelInset > 0 {
		r.Style().Padding.Left = st.labelInset
	}
	r.SetMinSize(menuMinWidth, st.itemHeight)
	if item.ID != "" {
		r.SetID(item.ID)
	}
	if c := item.Content(); c != nil {
		r.AddChild(c)
	}
	return r
}

func (r *menuContentRow) SetMenuHighlighted(on bool) {
	if r.highlighted != on {
		r.highlighted = on
		r.Invalidate()
	}
}

func (r *menuContentRow) ActivateMenuRow() {
	if r.Enabled() && r.onSelect != nil {
		r.onSelect()
	}
}

// HitTest keeps the whole row one target so a click on the content picks
// the row, like a label row.
func (r *menuContentRow) HitTest(p Point) Widget {
	if r.Bounds().Contains(p) {
		return r
	}
	return nil
}

func (r *menuContentRow) Handle(event Event) bool {
	e, ok := event.(MouseEvent)
	if !ok {
		return false
	}
	notifyMenuHover(r.onHover, r.onMove, e)
	if !r.Enabled() {
		return false
	}
	switch e.Type() {
	case EventMouseEnter:
		r.hovering = true
		r.Invalidate()
	case EventMouseLeave:
		r.hovering = false
		r.Invalidate()
	case EventMouseDown:
		if e.Button == MouseButtonLeft {
			r.ActivateMenuRow()
			return true
		}
	}
	return false
}

func (r *menuContentRow) Draw(canvas Canvas) {
	r.style.refreshColors()
	if r.item.Selected && r.style.labelInset <= 0 {
		canvas.FillRect(r.Bounds(), r.style.selectedBg)
	}
	if r.Enabled() && (r.hovering || r.highlighted) {
		if r.style.hoverColor.A > 0 {
			r.style.paintHover(canvas, r.Bounds())
		} else {
			DrawStateLayer(canvas, r.Bounds(), 0, r.style.stateColor, CurrentTheme().HoverOpacity)
		}
	}
	r.Container.Draw(canvas)
	if st := r.style; r.item.Selected && st.labelInset > 0 {
		b := r.Bounds()
		col := Rect{X: b.X + st.iconInset, Y: b.Y + (b.H-st.iconSize)/2, W: st.iconSize, H: st.iconSize}
		drawMenuCheck(canvas, col, st.iconSize, flattenOver(st.fg, st.bg))
	}
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
	h := v.style.sepHeight
	if h <= 0 {
		h = 9 // 4 px gap + 1 px line + 4 px gap
	}
	return Size{W: menuMinWidth, H: h}
}

func (v *menuSeparatorView) Draw(canvas Canvas) {
	b := v.Bounds()
	// Refresh divider color from the current theme so a light/dark
	// or seed swap retints the rule line in step with the menu rows.
	v.style.refreshColors()
	line := Rect{
		X: b.X + v.style.dividerInset,
		Y: b.Y + float32(int((b.H-1)/2)),
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
	rows       []Widget // parallel to items; see buildMenuPopupStyled
	focused    int
	closeChain func()
	closeSelf  func()
	isSub      bool
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
// rows are skipped like separators (newMenuListView's caller re-checks
// them once rows are known — see selectable).
func firstSelectable(items []*MenuItem) int {
	for i, it := range items {
		if it.Separator || it.Disabled || it.Panel != nil {
			continue
		}
		return i
	}
	return -1
}

// selectable reports whether keyboard navigation may land on item i: not a
// separator or disabled, and a Panel row only when its content is
// MenuActivatable.
func (lv *menuListView) selectable(i int) bool {
	it := lv.items[i]
	if it.Separator || it.Disabled {
		return false
	}
	if it.Panel != nil {
		if i >= len(lv.rows) {
			return false
		}
		_, ok := lv.rows[i].(MenuActivatable)
		return ok
	}
	return true
}

// setFocused moves the keyboard highlight to i and scrolls it into view.
func (lv *menuListView) setFocused(i int) {
	if i == lv.focused && lv.highlightShown(i) {
		return
	}
	if old := lv.focused; old >= 0 && old < len(lv.rows) {
		if h, ok := lv.rows[old].(MenuHighlighter); ok {
			h.SetMenuHighlighted(false)
		}
	}
	lv.focused = i
	if i < 0 || i >= len(lv.rows) {
		return
	}
	if h, ok := lv.rows[i].(MenuHighlighter); ok {
		h.SetMenuHighlighted(true)
	}
	if lv.scroll != nil {
		lv.scroll.ScrollChildIntoView(lv.rows[i])
	}
}

func (lv *menuListView) highlightShown(i int) bool {
	if i < 0 || i >= len(lv.rows) {
		return false
	}
	if v, ok := lv.rows[i].(*menuItemView); ok {
		return v.highlighted
	}
	return false
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
	if nat.W < lv.style.minWidth {
		nat.W = lv.style.minWidth
	}
	if available.H > 0 && nat.H > available.H {
		if lv.scroll == nil {
			sc := NewScrollView()
			// Adopt (not raw SetParent): the wrapper needs the window so
			// SetContent adopts the content onto the real window instead of
			// detaching it with a nil window.
			if !AdoptWidgetTree(sc, lv, lv.Window()) {
				return Size{W: nat.W, H: available.H}
			}
			lv.scroll = sc
		}
		lv.scroll.SetContent(lv.content, nat)
		return Size{W: nat.W, H: available.H}
	}
	if lv.scroll != nil {
		// The list now fits — move content back under the list and drop the
		// wrapper (detaching it, not leaking it with a stale parent).
		if lv.content != nil {
			AdoptWidgetTree(lv.content, lv, lv.Window())
		}
		DetachWidgetTree(lv.scroll)
		lv.scroll = nil
	}
	return nat
}

func (lv *menuListView) Layout(rect Rect) {
	lv.BaseWidget.Layout(rect)
	lv.applySurface()
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

// applySurface resolves the palette and pushes the panel background onto
// the content/scroll subtree. Runs in Layout, not Draw: changing a child's
// Style during paint invalidates layout from inside the paint pass.
func (lv *menuListView) applySurface() {
	lv.style.refreshColors()
	if lv.content == nil {
		return
	}
	bg := lv.style.bg
	if !lv.style.corners.IsZero() {
		// Per-corner panel: the panel itself paints the shape; content
		// stays transparent over it.
		bg = Color{}
	}
	lv.content.Style().Background = bg
	if lv.scroll != nil {
		lv.scroll.Style().Background = bg
		lv.scroll.Style().Radius = lv.style.radius
	}
}

func (lv *menuListView) Draw(canvas Canvas) {
	// Paint level-2 elevation behind the menu surface. The content
	// Container fills its rounded-rect bg on top.
	if lv.content == nil {
		return
	}
	if lv.style.elevation > 0 {
		DrawElevation(canvas, lv.Bounds(), lv.style.radius, lv.style.elevation)
	} else if lv.style.shadow != (ElevationSpec{}) {
		canvas.DrawShadow(lv.Bounds(), lv.style.radius, lv.style.shadow, CurrentTheme().Shadow)
	}
	if c := lv.style.corners; !c.IsZero() {
		// Per-corner panel (a menu-bar dropdown's square top-left): fill
		// it here; applySurface left the content transparent over it.
		p := NewPath().AddRRectCorners(lv.Bounds(), c.TL, c.TR, c.BR, c.BL)
		canvas.DrawShape(ShapePath{Path: p}, DefaultPaint(lv.style.bg))
	}
	if lv.scroll != nil {
		lv.scroll.Draw(canvas)
		lv.drawOutline(canvas)
		return
	}
	if !boxChildHidden(lv.content) {
		lv.content.Draw(canvas)
	}
	lv.drawOutline(canvas)
}

// drawOutline strokes the panel edge when the style has a border, last so
// row highlights never cover it. The stroke sits half a width inside the
// bounds so it isn't clipped.
func (lv *menuListView) drawOutline(canvas Canvas) {
	st := lv.style
	if st.border.A <= 0 || st.borderW <= 0 {
		return
	}
	h := st.borderW / 2
	b := lv.Bounds()
	r := Rect{X: b.X + h, Y: b.Y + h, W: b.W - st.borderW, H: b.H - st.borderW}
	if c := st.corners; !c.IsZero() {
		p := NewPath().AddRRectCorners(r, c.TL, c.TR, c.BR, c.BL)
		canvas.DrawShape(ShapePath{Path: p}, Paint{Color: st.border, Style: PaintStroke, StrokeWidth: st.borderW, AntiAlias: true})
		return
	}
	canvas.StrokeRoundedRect(r, max(0, st.radius-h), st.border, st.borderW)
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
	// A MenuItem.Panel can host a real control (search field, colour
	// picker). The menu list is its ancestor, so handling navigation keys
	// during capture would steal them before the focused control sees them.
	if ke.Phase() == PhaseCapture {
		return false
	}
	switch ke.Key {
	case KeyUp:
		lv.setFocused(lv.move(-1))
		return true
	case KeyDown:
		lv.setFocused(lv.move(+1))
		return true
	case KeyHome:
		lv.setFocused(lv.moveFrom(-1, +1))
		return true
	case KeyEnd:
		lv.setFocused(lv.moveFrom(len(lv.items), -1))
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
		if lv.style.navigate != nil {
			lv.style.navigate(+1)
			return true
		}
	case KeyLeft:
		// A submenu closes alone and focus returns to the row that
		// opened it; a top-level dropdown in a menu bar moves to the
		// previous menu instead.
		if lv.isSub && lv.closeSelf != nil {
			lv.closeSelf()
			return true
		}
		if lv.style.navigate != nil {
			lv.style.navigate(-1)
			return true
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
	if lv.focused >= len(lv.rows) {
		return
	}
	if row, ok := lv.rows[lv.focused].(MenuActivatable); ok {
		row.ActivateMenuRow()
	}
}

// move steps the focused index by delta, skipping rows that can't be
// selected (see selectable). Wraps around at the ends.
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
		if lv.selectable(cur) {
			return cur
		}
	}
	return lv.focused
}

// moveFrom scans from start (exclusive) in direction step without
// wrapping — Home / End.
func (lv *menuListView) moveFrom(start, step int) int {
	for i := start + step; i >= 0 && i < len(lv.items); i += step {
		if lv.selectable(i) {
			return i
		}
	}
	return lv.focused
}

// ----------------------------------------------------------------------
// MenuBar

// MenuBar is the horizontal strip of top-level menu triggers typically
// rendered at the top of a main window ("File", "Edit", "View", ...).
// Each trigger opens a ContextMenu dropdown when clicked. With a dropdown
// open, Left / Right move to the neighboring menu.
//
// Usage:
//
//	mb := widgets.NewMenuBar(window)
//	mb.AddMenu("File", []*widgets.MenuItem{
//	    {Label: "New",  Shortcut: "CmdOrCtrl+N", OnClick: newFile},
//	    {Label: "Open", Shortcut: "CmdOrCtrl+O", OnClick: openFile},
//	})
//	mb.AddMenu("Edit", ...)
//	mb.BindAccelerators(window.Accelerators())
//
// The window argument may be nil when the bar is built before it is
// mounted: the bar then uses the window it is attached to.
type MenuBar struct {
	Container
	// TriggerStyle, when its Base is set (non-zero Foreground), styles every
	// trigger button; otherwise triggers are flat buttons in the bar's own
	// Background / Foreground. Read at draw time, so changing it (or the
	// bar's Style) restyles existing triggers.
	TriggerStyle StateStyle
	// DropdownColors / DropdownElevation style the dropdowns (zero = theme).
	DropdownColors    MenuColors
	DropdownElevation int

	window  *Window
	menus   []menuEntry
	openIdx int
	open    *Popup

	// accelReg is the registry BindAccelerators feeds; accelRemove removes
	// the bindings it added, so a menu change can re-register them.
	accelReg    *AcceleratorRegistry
	accelRemove []func()
}

type menuEntry struct {
	Label    string
	LabelKey string
	Items    []*MenuItem
	// trigger is the Button that opens this menu's dropdown.
	trigger *Button
}

// NewMenuBar creates an empty menu bar. window is where dropdowns are
// pushed; pass nil to use the window the bar is attached to. Plain surface
// + text colors — apps that want a specific palette set mb.Style() or
// mb.TriggerStyle. The dropdown it opens follows the current theme (see
// ShowContextMenu) unless DropdownColors is set.
func NewMenuBar(window *Window) *MenuBar {
	mb := &MenuBar{window: window, openIdx: -1}
	mb.BaseWidget = NewBaseWidget()
	mb.LayoutEngine = FlexLayout{Direction: Horizontal, Gap: 2}
	mb.SetSelf(mb)
	mb.Style().Background = baselineBarBg
	mb.Style().Foreground = baselineBarText
	mb.Style().Padding = Insets{Top: 4, Right: 4, Bottom: 4, Left: 4}
	return mb
}

// win is the window dropdowns open in: the one the bar is attached to,
// else the constructor's (before mounting).
func (mb *MenuBar) win() *Window {
	if w := mb.Window(); w != nil {
		return w
	}
	return mb.window
}

// SetWindow closes an open dropdown when the bar leaves its window; a
// dropdown anchored to an unmounted bar has nothing left to belong to.
func (mb *MenuBar) SetWindow(w *Window) {
	mb.BaseWidget.SetWindow(w)
	if w == nil {
		mb.window = nil
		mb.CloseMenu()
		return
	}
	mb.window = w
}

// AddMenu appends a top-level menu. The caller-supplied items drive the
// dropdown shown when the menu's trigger is clicked.
func (mb *MenuBar) AddMenu(label string, items []*MenuItem) {
	mb.AddMenuKey("", label, items)
}

// AddMenuKey is AddMenu with a catalog key for the trigger's caption
// (label is the fallback).
func (mb *MenuBar) AddMenuKey(key, label string, items []*MenuItem) {
	entry := menuEntry{Label: label, LabelKey: key, Items: items}
	idx := len(mb.menus)
	entry.trigger = NewButton(label, func() {
		if mb.openIdx == idx && mb.open != nil {
			mb.CloseMenu()
			return
		}
		mb.OpenMenu(idx)
	})
	if key != "" {
		entry.trigger.SetTextKey(key)
	}
	mb.styleTrigger(entry.trigger, false)
	mb.menus = append(mb.menus, entry)
	mb.AddChild(entry.trigger)
	mb.rebindAccelerators()
}

// Menus returns the top-level captions, in order.
func (mb *MenuBar) Menus() []string {
	out := make([]string, len(mb.menus))
	for i, m := range mb.menus {
		out[i] = m.trigger.DisplayText()
	}
	return out
}

// SetTriggerStyle sets the trigger look and re-lays the bar. Prefer this
// over assigning the TriggerStyle field: the field is re-derived in Layout,
// but nothing schedules that pass for a bare assignment.
func (mb *MenuBar) SetTriggerStyle(st StateStyle) {
	mb.TriggerStyle = st
	mb.InvalidateLayout()
}

// Trigger returns the button that opens menu i (nil when out of range).
// Its States are re-derived from TriggerStyle during Layout; set that to
// restyle triggers. Use the button for IDs, tooltips and layout.
func (mb *MenuBar) Trigger(i int) *Button {
	if i < 0 || i >= len(mb.menus) {
		return nil
	}
	return mb.menus[i].trigger
}

// SetMenuItems replaces menu i's items (takes effect the next time it
// opens).
func (mb *MenuBar) SetMenuItems(i int, items []*MenuItem) {
	if i >= 0 && i < len(mb.menus) {
		mb.menus[i].Items = items
		mb.rebindAccelerators()
	}
}

// styleTrigger applies TriggerStyle, or the flat stock menubar look
// derived from the bar's style: 14px captions with 2/7 px padding and a
// 4 px radius, flat on the bar, filled Theme.Popup.MenuBarHover while
// hovered or while its dropdown is open.
func (mb *MenuBar) styleTrigger(b *Button, open bool) {
	if mb.TriggerStyle.Base.Foreground.A > 0 {
		b.States = mb.TriggerStyle
		return
	}
	p := CurrentTheme().ResolvedPopup()
	base := b.States.Base
	base.BorderSize = 0
	base.Radius = p.MenuRadius
	base.Padding = Insets{Top: 2, Right: 7, Bottom: 2, Left: 7}
	base.Font.Size = CurrentTheme().FontBase
	base.Background = themed(mb.Style().Background)
	base.Foreground = themed(mb.Style().Foreground)
	hover := base
	hover.Background = p.MenuBarHover
	if open {
		base = hover
	}
	pressed := hover
	b.States.Base = base
	b.States.Hover = &hover
	b.States.Pressed = &pressed
	b.States.Focused = nil
}

// refreshTriggers derives every trigger's States from TriggerStyle (or the
// stock bar look) and resolves the bar's own baseline colors against the
// active theme. Runs in Layout, never in Draw: mutating widget styles
// during paint breaks per-frame cost and dirty-region assumptions.
func (mb *MenuBar) refreshTriggers() {
	for i, m := range mb.menus {
		mb.styleTrigger(m.trigger, mb.open != nil && mb.openIdx == i)
	}
}

// Measure/Layout re-derive the trigger styles first so an open/close or
// theme switch changes the trigger metrics before the engine measures them.
func (mb *MenuBar) Measure(available Size) Size {
	mb.refreshTriggers()
	return mb.Container.Measure(available)
}

func (mb *MenuBar) Layout(rect Rect) {
	mb.refreshTriggers()
	mb.Container.Layout(rect)
}

func (mb *MenuBar) Draw(canvas Canvas) {
	// Paint the bar surface with the theme-resolved color without writing
	// it back into Style (a draw must not mutate observable state), then
	// paint the triggers.
	bounds := mb.Bounds()
	st := mb.Style()
	bg := themed(st.Background)
	var clip Rect
	if ca, ok := canvas.(ClipAware); ok {
		clip = ca.ClipBoundsLogical()
	}
	if (clip.IsEmpty() || bounds.Intersects(clip)) && bg.A > 0 {
		if st.Radius > 0 {
			canvas.FillRoundedRect(bounds, st.Radius, bg)
		} else {
			canvas.FillRect(bounds, bg)
		}
	}
	for _, child := range ChildrenInPaintOrder(mb.ChildList()) {
		if boxChildHidden(child) {
			continue
		}
		if !clip.IsEmpty() && !PaintBoundsOf(child).Intersects(clip) {
			continue
		}
		child.Draw(canvas)
	}
}

// OpenMenu opens top-level menu idx (closing any other) and focuses it.
func (mb *MenuBar) OpenMenu(idx int) {
	w := mb.win()
	if w == nil || idx < 0 || idx >= len(mb.menus) {
		return
	}
	mb.CloseMenu()
	entry := mb.menus[idx]
	st := topLevelMenuStyle()
	st.ApplyColors(mb.DropdownColors)
	if mb.DropdownElevation > 0 {
		st.elevation = mb.DropdownElevation
	}
	n := len(mb.menus)
	st.navigate = func(dir int) { mb.OpenMenu((idx + dir + n) % n) }
	trigger := entry.trigger
	popup := showMenuBarDropdown(w, func() Rect { return InteractionBoundsOf(trigger) }, entry.Items, st)
	if popup == nil {
		return
	}
	mb.open, mb.openIdx = popup, idx
	mb.InvalidateLayout()
	trigger.Invalidate()
	MenuBarHoverSwitch(popup, func(p Point) func() {
		for j, m := range mb.menus {
			if j != idx && m.trigger.Enabled() && InteractionBoundsOf(m.trigger).Contains(p) {
				return func() { mb.OpenMenu(j) }
			}
		}
		return nil
	})
	prev := popup.OnClose
	popup.OnClose = func() {
		if prev != nil {
			prev()
		}
		if mb.open == popup {
			mb.open, mb.openIdx = nil, -1
			mb.InvalidateLayout()
		}
		trigger.Invalidate()
	}
}

// MenuBarHoverSwitch makes an open menu-bar dropdown follow the pointer
// across the bar, the way desktop and Google menu bars do: while popup is
// shown, a pointer move onto a point for which hit returns a non-nil open
// func closes popup and runs open (which shows the other menu). hit is
// called with window coordinates and should return nil over the trigger
// of the menu that is already open. The watcher goes away with popup.
func MenuBarHoverSwitch(popup *Popup, hit func(p Point) (open func())) {
	if popup == nil || hit == nil || popup.window == nil {
		return
	}
	w := popup.window
	remove := w.AddEventFilter(func(e Event) bool {
		me, ok := e.(MouseEvent)
		if !ok || me.Type() != EventMouseMove || !popup.IsShown() {
			return false
		}
		// Over the dropdown (or one of its submenus) the menu keeps the
		// pointer, even where it overlaps a trigger.
		if top := w.WidgetAt(Point{X: me.X, Y: me.Y}); top != nil && top != Widget(popup) {
			if _, isPopup := top.(*Popup); !isPopup && overlayRootIsPopup(top) {
				return false
			}
		}
		open := hit(Point{X: me.X, Y: me.Y})
		if open == nil {
			return false
		}
		popup.Close()
		open()
		return false
	})
	prev := popup.OnClose
	popup.OnClose = func() {
		remove()
		if prev != nil {
			prev()
		}
	}
}

// overlayRootIsPopup reports whether w sits inside a Popup's content.
func overlayRootIsPopup(w Widget) bool {
	for p := w; p != nil; p = p.Parent() {
		if _, ok := p.(*Popup); ok {
			return true
		}
	}
	return false
}

// CloseMenu closes the open dropdown, if any.
func (mb *MenuBar) CloseMenu() {
	if mb.open != nil {
		p := mb.open
		mb.open, mb.openIdx = nil, -1
		mb.InvalidateLayout()
		p.Close()
	}
}

// OpenIndex reports which top-level menu is open (-1 for none).
func (mb *MenuBar) OpenIndex() int { return mb.openIdx }

// AcceleratorRegistry walks every menu item (including submenus) and
// builds a fresh registry binding each item's Shortcut to its OnClick.
// Attaching it with Window.SetAcceleratorRegistry REPLACES the window's
// registry, so prefer BindAccelerators, which adds the menu's shortcuts to
// a registry that other code also binds into.
//
// Separators and items without a Shortcut or OnClick are skipped.
// Checkable items get wrapped so firing the accelerator flips Checked
// (matching what a click would do); the caller still needs to redraw
// the menu if they want the tick mark to update without a fresh open.
// A Disabled item (or one under a Disabled submenu) doesn't fire: its key
// goes on as if unbound.
func (mb *MenuBar) AcceleratorRegistry() *AcceleratorRegistry {
	reg := NewAcceleratorRegistry()
	for _, m := range mb.menus {
		registerItems(reg, m.Items, nil, nil)
	}
	return reg
}

// BindAccelerators adds every item's shortcut to reg alongside the
// bindings reg already holds (typically window.Accelerators()), with the
// rules AcceleratorRegistry describes, and keeps them current: AddMenu,
// AddMenuKey and SetMenuItems re-register the menu's bindings. The returned
// function removes exactly the menu's bindings and stops the syncing.
// Binding into a second registry moves the menu's bindings there.
func (mb *MenuBar) BindAccelerators(reg *AcceleratorRegistry) (remove func()) {
	mb.unbindAccelerators()
	mb.accelReg = reg
	mb.rebindAccelerators()
	return func() {
		if mb.accelReg == reg {
			mb.unbindAccelerators()
			mb.accelReg = nil
		}
	}
}

func (mb *MenuBar) unbindAccelerators() {
	for _, rm := range mb.accelRemove {
		rm()
	}
	mb.accelRemove = nil
}

// rebindAccelerators re-registers the menu's shortcuts into accelReg, if
// BindAccelerators attached one.
func (mb *MenuBar) rebindAccelerators() {
	if mb.accelReg == nil {
		return
	}
	mb.unbindAccelerators()
	for _, m := range mb.menus {
		registerItems(mb.accelReg, m.Items, nil, &mb.accelRemove)
	}
}

// registerItems binds items' shortcuts into reg. parentEnabled reports
// whether every enclosing submenu row is enabled (nil at the top level);
// removers, when non-nil, collects each binding's remover.
func registerItems(reg *AcceleratorRegistry, items []*MenuItem, parentEnabled func() bool, removers *[]func()) {
	for _, it := range items {
		if it.Separator {
			continue
		}
		item := it // capture
		enabled := func() bool {
			return !item.Disabled && (parentEnabled == nil || parentEnabled())
		}
		if it.Submenu != nil {
			registerItems(reg, it.Submenu, enabled, removers)
			continue
		}
		if it.Shortcut == "" || it.OnClick == nil {
			continue
		}
		fn := func() {
			if item.Checkable {
				item.Checked = !item.Checked
			}
			item.OnClick()
		}
		// Best-effort registration: swallow parse errors so one bad
		// shortcut doesn't break the whole menu.
		rm, err := reg.BindIf(item.Shortcut, enabled, fn)
		if err == nil && removers != nil {
			*removers = append(*removers, rm)
		}
	}
}
