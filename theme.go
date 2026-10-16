package qui

// Theme is a bundle of design tokens (colors / spacing / radius / fonts)
// that widgets reference instead of hard-coding values. Switching the
// global theme via SetTheme triggers re-paint + re-layout on every
// subscribed window, so brand-palette switching is a single call.
//
// The token set is deliberately small and design-system agnostic: roles
// are named for what they DO in a UI (a raised surface, muted text, the
// accent color), not for any vendor's spec. Widgets paint a plain
// raw-HTML look by default (see widgets/native.go); a designed look —
// Material, Fluent, your own — is layered on top through the htmlcss
// layer as plain CSS, not by swapping tokens underneath the widgets.
//
// qui ships ONE scheme: light. A dark UI is an app concern handled in
// CSS (a dark stylesheet, or a class toggled on the root element), NOT
// by a second global theme — see examples/reactive-html, which flips a
// `.dark` class. Nothing in the engine or in widgets branches on
// "is the theme dark".
//
// Why centralize at all: hard-coded colors drift across widgets — a
// "dark gray" in Button ends up different from the one in Dialog
// because authors round inconsistently. Tokens give ONE source of truth
// so every visual change is library-wide and intentional.
type Theme struct {
	// === Surfaces =====================================================
	// The layer ladder, from the page up to floating chrome. On the
	// light scheme each step is slightly darker than the page, except
	// SurfaceOverlay which is pure white so popovers separate from
	// whatever they float over.
	Surface        Color // page / window background
	SurfaceSunken  Color // recessed fill (inputs, wells)
	SurfaceRaised  Color // panels, cards, menus
	SurfaceStrong  Color // emphasized raised layer
	SurfaceOverlay Color // floating chrome: tooltips, popovers

	// === Text =========================================================
	Text      Color // primary foreground
	TextMuted Color // hints, secondary labels, disabled

	// TextSelection is the highlight painted behind selected read-only
	// text (Label / InlineBox / tooltip). Semi-transparent so glyphs stay
	// readable. The zero value falls back to the framework's default
	// selection blue; widgets should resolve it through
	// SelectionHighlight, which also swaps in a light overlay over dark
	// or saturated backgrounds where the default would be invisible.
	TextSelection Color

	// === Accent =======================================================
	// The primary interactive color (selection, focus ring, filled
	// controls) and the foreground painted on top of it. Hover / Pressed
	// are pre-derived shades for widgets that fill with the accent.
	Accent        Color
	AccentText    Color
	AccentHover   Color
	AccentPressed Color

	// === Lines ========================================================
	Border       Color // hairlines, separators, dividers
	BorderStrong Color // control outlines (the visible <input> border)
	BorderFocus  Color // focus ring

	// === Semantic accents =============================================
	Error   Color
	Warning Color
	Success Color

	// === Depth ========================================================
	// Shadow is the color drop shadows are painted in; Elevation is a
	// six-step ladder of shadow specs (index 0 = flat) that DrawElevation
	// resolves. Widgets take an int level, so an app restyles every
	// shadow in the UI by editing this one table.
	Shadow    Color
	Elevation [6]ElevationLevel

	// === Interaction overlays =========================================
	// Opacities for the translucent overlay a widget paints over itself
	// while hovered / focused / pressed / dragged. See DrawStateLayer.
	HoverOpacity   float32
	FocusOpacity   float32
	PressedOpacity float32
	DraggedOpacity float32

	// === Spacing scale ================================================
	Spacing1 float32 // 4
	Spacing2 float32 // 8
	Spacing3 float32 // 12
	Spacing4 float32 // 16
	Spacing5 float32 // 24

	// === Radius scale =================================================
	RadiusSmall  float32 // 4
	RadiusMedium float32 // 8
	RadiusLarge  float32 // 12

	// === Font sizes ===================================================
	// ThemeFont(role) resolves these into a Font; widgets should go
	// through it rather than composing Font literals.
	FontSmall   float32 // 12
	FontBase    float32 // 14
	FontLarge   float32 // 16
	FontHeading float32 // 24

	// === Transition defaults ==========================================
	TransitionShort  Duration // ~200ms — hover / focus fades
	TransitionMedium Duration // ~300ms — enter / exit

	// === Floating chrome ==============================================
	// Popup styles the surfaces that float over the app — menus, tooltips,
	// the modal scrim and dialog box. Kept apart from the surface ladder
	// so an app that retints its page / text tokens still gets crisp,
	// neutral popups; override fields individually to restyle them.
	Popup PopupTokens
}

// PopupTokens is the look of floating chrome: menu panels (menu-bar
// dropdowns, context menus, submenus, toolbar dropdown menus, <select>
// lists), tooltips, and modal dialogs.
//
// A zero field falls back to a value derived from the rest of the theme
// (SurfaceOverlay, Text, Border, Accent, …) — LightTheme leaves them all
// zero — so popups follow whatever palette the theme has; set a field to
// restyle that part alone.
type PopupTokens struct {
	// Menus.
	MenuSurface      Color         // panel fill (SurfaceOverlay)
	MenuText         Color         // row label, icon, check and arrow (Text)
	MenuTextMuted    Color         // shortcut text (TextMuted)
	MenuDivider      Color         // separator line (Border)
	MenuHover        Color         // hovered / keyboard-highlighted row, painted as-is (Text at HoverOpacity)
	MenuSelected     Color         // current value in a radio-style chooser (e.g. a <select> list)
	MenuShadow       ElevationSpec // panel drop shadow (0 2px 6px 2px @ 15%)
	MenuRadius       float32       // panel corner radius (RadiusSmall)
	MenuMinWidth     float32       // floor for top-level menus: menu-bar dropdowns and context menus (none)
	MenuBarHover     Color         // menu-bar trigger hovered / open
	MenuBarText      Color         // menu-bar trigger caption (Text)
	MenuBadgeText    Color         // "New" pill label (MenuItem.Badge; Accent)
	MenuBadgeSurface Color         // "New" pill fill (Accent tint)

	// Tooltips.
	TooltipSurface Color // chip fill (Text)
	TooltipText    Color // chip text (Surface)

	// Modal dialogs.
	Scrim          Color   // backdrop behind a modal (rgba(0,0,0,.32))
	DialogRadius   float32 // dialog box corner radius (RadiusLarge)
	DialogAccent   Color   // default-button fill (Accent)
	DialogOnAccent Color   // default-button label (AccentText)
}

// ElevationSpec is one component of a drop shadow: offset, blur, spread
// and an opacity that blends with Theme.Shadow. Two of them make one
// elevation level — a tight "key" shadow plus a wider, softer "ambient"
// one, which is what makes a raised surface read as lifted rather than
// outlined.
type ElevationSpec struct {
	X, Y, Blur, Spread, Opacity float32
}

// ElevationLevel groups the key+ambient pair for one elevation level.
type ElevationLevel struct {
	Key, Ambient ElevationSpec
}

// Duration is a thin alias over int64 nanoseconds — avoids pulling
// "time" into theme.go which is package-root. Widgets convert to
// time.Duration when using a Transition.
type Duration int64

// TextRole names the handful of text sizes a UI actually needs. Widgets
// resolve a Font through ThemeFont(role) instead of hand-rolling
// Size/Weight pairs, so headings, labels and body text stay consistent
// and follow the theme's font scale.
type TextRole uint8

const (
	// TextBody is default UI text — the size everything else is
	// relative to.
	TextBody TextRole = iota
	// TextBodySmall is secondary / dense text: captions, hints, table
	// footnotes.
	TextBodySmall
	// TextBodyLarge is comfortable reading text: menu rows, dialog body.
	TextBodyLarge
	// TextLabel is interactive-element text: button faces, tabs, menu
	// shortcuts. Same size as TextBody but medium weight, so a control
	// reads as a control.
	TextLabel
	// TextLabelSmall is TextLabel at the small size — dense toolbars,
	// chips.
	TextLabelSmall
	// TextHeading is a section / dialog title.
	TextHeading
	numTextRoles
)

// ThemeFont returns the Font for a text role, sized off the current
// theme's font scale so bumping FontBase moves the whole UI. Falls back
// to TextBody when role is out of range — never panics.
func ThemeFont(role TextRole) Font {
	t := CurrentTheme()
	switch role {
	case TextBodySmall:
		return Font{Size: t.FontSmall, Weight: FontWeightNormal}
	case TextBodyLarge:
		return Font{Size: t.FontLarge, Weight: FontWeightNormal}
	case TextLabel:
		return Font{Size: t.FontBase, Weight: FontWeightMedium}
	case TextLabelSmall:
		return Font{Size: t.FontSmall, Weight: FontWeightMedium}
	case TextHeading:
		return Font{Size: t.FontHeading, Weight: FontWeightNormal}
	default:
		return Font{Size: t.FontBase, Weight: FontWeightNormal}
	}
}

// argbColor converts a packed ARGB uint32 (alpha in the high byte) into
// a qui.Color — lets the baseline palette below stay readable as hex
// literals.
func argbColor(argb uint32) Color {
	return Color{
		R: float32((argb>>16)&0xff) / 255.0,
		G: float32((argb>>8)&0xff) / 255.0,
		B: float32(argb&0xff) / 255.0,
		A: float32((argb>>24)&0xff) / 255.0,
	}
}

// LightTheme is qui's baseline: a neutral gray scale with a plain blue
// accent, matching the raw-HTML look widgets paint by default. It is the
// sole shipped theme and what the framework loads at startup. Apps that
// want a brand palette build a Theme (usually by copying this one and
// overwriting the color fields) and pass it to SetTheme.
var LightTheme = newLightTheme()

func newLightTheme() Theme {
	t := Theme{
		Surface:        argbColor(0xfffdfdfd),
		SurfaceSunken:  argbColor(0xfff6f6f6),
		SurfaceRaised:  argbColor(0xfff1f1f1),
		SurfaceStrong:  argbColor(0xffebebeb),
		SurfaceOverlay: argbColor(0xffffffff),

		Text:      argbColor(0xff1c1c1c),
		TextMuted: argbColor(0xff474747),

		// Matches widgets/native.go's htmlAccent — the checked/focus
		// color the zero-config controls already paint with.
		Accent:        argbColor(0xff4d8cd8),
		AccentText:    argbColor(0xffffffff),
		AccentHover:   argbColor(0xff3f7bc4),
		AccentPressed: argbColor(0xff3567a8),

		Border:       argbColor(0xffc8c8c8),
		BorderStrong: argbColor(0xff767676),
		BorderFocus:  argbColor(0xff4d8cd8),

		Error:   argbColor(0xffba1a1a),
		Warning: argbColor(0xffd07c00),
		Success: argbColor(0xff216c2a),

		Shadow: argbColor(0xff000000),

		HoverOpacity:   0.08,
		FocusOpacity:   0.12,
		PressedOpacity: 0.12,
		DraggedOpacity: 0.16,

		Spacing1: 4, Spacing2: 8, Spacing3: 12, Spacing4: 16, Spacing5: 24,
		RadiusSmall: 4, RadiusMedium: 8, RadiusLarge: 12,
		FontSmall: 12, FontBase: 14, FontLarge: 16, FontHeading: 24,

		TransitionShort:  Duration(200_000_000),
		TransitionMedium: Duration(300_000_000),

		// Popup is left zero: every popup token derives from the palette
		// above (see ResolvedPopup), so floating chrome follows the theme
		// rather than a particular design system.
	}
	t.Elevation = [6]ElevationLevel{
		0: {},
		1: {Key: ElevationSpec{Y: 1, Blur: 2, Opacity: 0.30}, Ambient: ElevationSpec{Y: 1, Blur: 3, Spread: 1, Opacity: 0.15}},
		2: {Key: ElevationSpec{Y: 1, Blur: 2, Opacity: 0.30}, Ambient: ElevationSpec{Y: 2, Blur: 6, Spread: 2, Opacity: 0.15}},
		3: {Key: ElevationSpec{Y: 1, Blur: 3, Opacity: 0.30}, Ambient: ElevationSpec{Y: 4, Blur: 8, Spread: 3, Opacity: 0.15}},
		4: {Key: ElevationSpec{Y: 2, Blur: 3, Opacity: 0.30}, Ambient: ElevationSpec{Y: 6, Blur: 10, Spread: 4, Opacity: 0.15}},
		5: {Key: ElevationSpec{Y: 4, Blur: 4, Opacity: 0.30}, Ambient: ElevationSpec{Y: 8, Blur: 12, Spread: 6, Opacity: 0.15}},
	}
	return t
}

// Global theme state.
var (
	currentTheme     = LightTheme
	themeSubscribers []themeSubscriber
	themeSubID       int64
	themeGeneration  uint64
)

type themeSubscriber struct {
	id int64
	fn func()
}

// ThemeGeneration increments on every SetTheme. Anything that caches values
// derived from the theme (resolved colors, rasterized chrome) includes it in
// its cache key, exactly like FontRegistryGeneration and LocaleGeneration.
func ThemeGeneration() uint64 { return themeGeneration }

// CurrentTheme returns a pointer to the active Theme. Widgets use this
// at Draw time to pick colors. The pointer is stable across SetTheme
// calls (SetTheme mutates the underlying struct, not the address).
func CurrentTheme() *Theme { return &currentTheme }

// SetTheme swaps the global theme. Every subscribed window has its
// callback fired so they can Invalidate + InvalidateLayout. Widgets
// don't need to do anything — Draw reads CurrentTheme() afresh.
func SetTheme(t Theme) {
	currentTheme = t
	themeGeneration++
	for _, s := range append([]themeSubscriber(nil), themeSubscribers...) {
		s.fn()
	}
}

// SubscribeTheme registers a callback fired whenever SetTheme runs and
// returns a function that removes that subscription. Window.NewWindow
// hooks itself here so theme changes flow through without widget-level
// plumbing, then unsubscribes during Destroy.
//
// A widget that subscribes should do so while attached and unsubscribe when
// detached (TreeLifecycle's OnAttach / OnDetach), or the subscription keeps
// it reachable after it leaves the tree.
func SubscribeTheme(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	themeSubID++
	id := themeSubID
	themeSubscribers = append(themeSubscribers, themeSubscriber{id: id, fn: fn})
	return func() {
		for i, s := range themeSubscribers {
			if s.id == id {
				themeSubscribers = append(themeSubscribers[:i:i], themeSubscribers[i+1:]...)
				return
			}
		}
	}
}

// ResolvedPopup returns t.Popup with every zero field filled from the rest
// of the theme, so popup painters never see an unset token: a Theme built
// from scratch (rather than copied from LightTheme) still gets a white-ish
// menu in its own text color, a hairline divider and a shadow.
func (t *Theme) ResolvedPopup() PopupTokens {
	p := t.Popup
	if p.MenuSurface.A == 0 {
		p.MenuSurface = t.SurfaceOverlay
	}
	if p.MenuText.A == 0 {
		p.MenuText = t.Text
	}
	if p.MenuTextMuted.A == 0 {
		p.MenuTextMuted = t.TextMuted
	}
	if p.MenuDivider.A == 0 {
		p.MenuDivider = t.Border
	}
	if p.MenuHover.A == 0 {
		c := p.MenuText
		c.A *= t.HoverOpacity
		p.MenuHover = c
	}
	if p.MenuSelected.A == 0 {
		p.MenuSelected = LerpColor(p.MenuSurface, p.MenuText, 0.12)
	}
	if p.MenuShadow == (ElevationSpec{}) {
		p.MenuShadow = ElevationSpec{Y: 2, Blur: 6, Spread: 2, Opacity: 0.15}
	}
	if p.MenuRadius == 0 {
		p.MenuRadius = t.RadiusSmall
	}
	if p.MenuBarHover.A == 0 {
		p.MenuBarHover = LerpColor(t.Surface, t.Text, t.HoverOpacity)
	}
	if p.MenuBarText.A == 0 {
		p.MenuBarText = t.Text
	}
	if p.MenuBadgeText.A == 0 {
		p.MenuBadgeText = t.Accent
	}
	if p.MenuBadgeSurface.A == 0 {
		p.MenuBadgeSurface = LerpColor(p.MenuSurface, p.MenuBadgeText, 0.16)
	}
	if p.TooltipSurface.A == 0 {
		p.TooltipSurface = t.Text
	}
	if p.TooltipText.A == 0 {
		p.TooltipText = t.Surface
	}
	if p.Scrim.A == 0 {
		p.Scrim = Color{A: 0.32}
	}
	if p.DialogRadius == 0 {
		p.DialogRadius = t.RadiusLarge
	}
	if p.DialogAccent.A == 0 {
		p.DialogAccent = t.Accent
	}
	if p.DialogOnAccent.A == 0 {
		p.DialogOnAccent = t.AccentText
	}
	return p
}
