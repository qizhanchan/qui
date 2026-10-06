package widgets

import (
	"image"
	"time"

	. "github.com/qizhanchan/qui"
)

// Text shaping and measurement helpers live in the root package so widgets and
// engine-owned surfaces share glyph-run, cluster and caret semantics.

// Button displays text and handles click interactions.
//
// **Look**: driven entirely by Style + States. The zero-configuration
// NewButton gives a plain, unstyled surface roughly matching a raw
// HTML `<button>` (light-gray background, thin border, black label,
// darker fill on hover / pressed). Callers who want a designed look
// (filled / tonal / outlined / text / elevated variants) build it in
// CSS on the htmlcss layer, or assign States + StateLayerColor +
// Elevation on this widget directly.
//
// **Behavior fields**:
//   - States           layered Style (Base / Hover / Pressed / Focused /
//     Disabled) — resolved every Draw
//   - StateLayerColor  translucent overlay tint on hover/focus (A==0 →
//     no overlay). Use the color the label is drawn in.
//   - Elevation        shadow level at rest (Theme.Elevation index).
//     0 = flat.
//   - HoverElevation   shadow level while hovered (0 = no lift). A
//     flat button leaves both at 0; a lifted one
//     typically pairs 1 with 2.
//
// **Focusable**: Tab cycles focus through buttons; Space or Enter when
// focused triggers OnClick (keyboard activation for accessibility).
type Button struct {
	BaseWidget
	// Text is the literal caption. For a localized app set a message
	// key with SetTextKey instead — a string stored here is captured
	// once and will not follow a language switch. See widgets/i18n.go.
	Text string
	// textKey, when set, supersedes Text and is resolved during
	// Measure/Draw. Read it through DisplayText().
	textKey messageKey
	Icon    image.Image
	// IconVector renders a resolution-independent icon at the
	// destination's physical pixel size. When set it takes precedence
	// over Icon — the bitmap path is for raster assets (PNG/JPG) that
	// have no vector source. Tint applies as a monochrome recolor (see
	// qui.VectorSource); pass an opaque color to use the SVG's own
	// palette if it's multi-color.
	IconVector VectorSource
	IconTint   Color
	// IconSize is the rendered icon edge length in logical px. <=0 uses
	// a sensible default (18 px unscaled).
	IconSize float32
	// IconGap is horizontal spacing between icon and text when both are
	// set. <=0 uses 8 px unscaled.
	IconGap float32
	OnClick func()

	// States carries the layered Base / Hover / Pressed / Focused /
	// Disabled styles. Draw resolves these against currentState() every
	// frame. States.Base doubles as the widget's baseline style.
	// Style() still returns a pointer to States.Base for compatibility.
	// New code should use qui.UpdateStyle or SetStyle so invalidation runs.
	States StateStyle

	// StateLayerColor is the tint of the translucent overlay painted on
	// hover / focused (opacity from Theme.HoverOpacity etc.). Zero A →
	// no overlay; set it to the color the label is drawn in.
	StateLayerColor Color
	// Elevation is the resting shadow level (index into Theme.Elevation).
	// 0 = flat.
	Elevation int
	// HoverElevation is the shadow level while hovered. 0 = no hover
	// lift. Combined with Elevation: rest = Elevation, hover =
	// HoverElevation (falls back to Elevation if 0), pressed = Elevation.
	HoverElevation int

	pressed  bool
	hovering bool
	focused  bool
	disabled bool

	// hoverTrans interpolates the state-layer opacity from 0 → 1 during
	// mouse enter / leave. Duration comes from MotionDurationShort4.
	hoverTrans Transition
}

// buttonShadowMargin bounds how far the elevation shadow can extend
// past the button rect on any side. The biggest button shadow we paint
// is elevation level 2 (elevated-button hover): Y=6, Blur=10, Spread=4
// → ~14 px on the bottom, ~14 px on the sides. We use 12 px as the
// conservative box-blur reach (matching the box-blur radius cap in
// render_shadow.go); level-2 sees about a 1-pixel sub-pixel halo past
// that which is below visual threshold. Buttons union this margin into
// every invalidation / Tick dirty rect so the shadow's outer pixels
// repaint cleanly when hover state flips and the elevation changes —
// otherwise the framework's dirty-region model strands "ghost shadow"
// pixels around the widget.
const buttonShadowMargin = 12

// dirtyRect returns Bounds inflated by buttonShadowMargin on every
// side — the rect that must be repainted to fully refresh the button
// including its elevation shadow.
func (b *Button) dirtyRect() Rect {
	r := b.Bounds()
	return Rect{
		X: r.X - buttonShadowMargin,
		Y: r.Y - buttonShadowMargin,
		W: r.W + 2*buttonShadowMargin,
		H: r.H + 2*buttonShadowMargin,
	}
}

// Invalidate marks the button's full shadow-extent dirty so elevation
// transitions don't leave stale shadow pixels outside Bounds.
func (b *Button) Invalidate() { b.InvalidateRect(b.dirtyRect()) }

// PaintBounds satisfies qui.PaintBounder — Container uses it instead
// of Bounds() during dirty-region paints so a neighbor that
// invalidates inside our shadow halo doesn't blank the halo without
// triggering our Draw to repaint it.
func (b *Button) PaintBounds() Rect { return b.dirtyRect() }

func (b *Button) CancelInteraction() {
	if b.pressed {
		b.pressed = false
		b.Invalidate()
	}
}

func (b *Button) Focusable() bool { return b.Enabled() && !b.disabled }
func (b *Button) SetFocused(f bool) {
	if b.focused == f {
		return
	}
	b.focused = f
	b.Invalidate()
}

// currentState compiles Button's booleans into a State bitmask so
// Draw / state-style lookup can consume it uniformly.
func (b *Button) currentState() State {
	var s State
	if !b.Enabled() || b.disabled {
		s |= StateDisabled
	}
	if b.pressed {
		s |= StatePressed
	}
	if b.focused {
		s |= StateFocused
	}
	if b.hovering {
		s |= StateHover
	}
	return s
}

// NewButton creates a plain, unstyled button — no elevation, no
// ripple, no state-layer overlay; a light-gray background with a thin
// border that darkens on hover / pressed, roughly matching a browser
// default `<button>`. For a designed look, style a <button> on the
// htmlcss layer with CSS; this native widget stays plain and is
// retinted by assigning its States directly.
func NewButton(text string, onClick func()) *Button {
	b := &Button{BaseWidget: NewBaseWidget(), Text: text, OnClick: onClick}
	theme := CurrentTheme()
	b.States = defaultButtonStates()
	b.hoverTrans.Duration = toDuration(theme.TransitionShort)
	return b
}

// defaultButtonStates returns the "raw HTML <button>" StateStyle:
// light-gray container, thin gray border, black label, subtle
// darkening on hover / pressed. Kept as a helper so tests and any
// caller wanting to start from HTML-defaults can compose from it.
func defaultButtonStates() StateStyle {
	base := Style{
		Background: htmlButtonBg,
		Foreground: htmlControlText,
		Border:     htmlControlBorder,
		BorderSize: 1,
		Radius:     htmlControlRadius,
		Padding:    Insets{Top: 2, Right: 8, Bottom: 2, Left: 8},
		Font:       Font{Size: htmlControlFontSize},
	}
	hover := base
	hover.Background = Color{R: 0.90, G: 0.90, B: 0.90, A: 1}
	pressed := base
	pressed.Background = Color{R: 0.82, G: 0.82, B: 0.82, A: 1}
	focused := base
	focused.Border = htmlAccent
	focused.BorderSize = 2
	disabled := base
	disabled.Foreground = htmlDisabledText
	disabled.Background = Color{R: 0.94, G: 0.94, B: 0.94, A: 1}
	return StateStyle{
		Base:     base,
		Hover:    &hover,
		Pressed:  &pressed,
		Focused:  &focused,
		Disabled: &disabled,
	}
}

// Style returns the legacy mutable base-state storage.
// Deprecated: use qui.StyleValue and qui.UpdateStyle.
func (b *Button) Style() *Style { return &b.States.Base }

// SetStyle replaces the state base rather than BaseWidget's unused style
// storage. Stateful widgets must override this so generic style updates land
// on the value their Measure and Draw paths actually consume.
func (b *Button) SetStyle(style Style) {
	b.States.Base = style.Clone()
	b.InvalidateLayout()
}

// elevationForState resolves the button's active shadow level given
// the current state bitmask.
//
//	rest / focused → Elevation
//	hover          → HoverElevation (or Elevation if HoverElevation==0)
//	pressed        → Elevation (a press collapses back to rest)
//	disabled       → 0
func (b *Button) elevationForState(state State) int {
	if state&StateDisabled != 0 {
		return 0
	}
	if state&StateHover != 0 && b.HoverElevation != 0 {
		return b.HoverElevation
	}
	return b.Elevation
}

// font returns the button's label font. Measure/Draw read through this
// helper so callers can still override Style().Font.Size (e.g.
// examples/reactive sets 11).
func (b *Button) font() Font {
	return b.Style().Font
}

// toDuration converts a Theme.Duration (int64 ns) to time.Duration
// without needing every caller to know the representation.
func toDuration(d Duration) time.Duration { return time.Duration(d) }

// Button geometry defaults. Used when Style().Padding is zero and the
// derived height falls out of the label font metrics.
const (
	buttonIconSize float32 = 18 // fallback icon edge length
	buttonIconGap  float32 = 8  // fallback icon-to-text gap

	// buttonHeight is the standard 40 px control height. Exposed as a
	// package constant because the other designed controls (Dense Input
	// / Select) match it so a row of controls lines up flush.
	buttonHeight float32 = 40
)

func (b *Button) Measure(available Size) Size {
	base := &b.States.Base
	font := b.font()
	if font.Size <= 0 {
		font = ThemeFont(TextLabel)
	}
	// Measure the RESOLVED caption: a translated string has a different
	// width than the literal, and Measure feeding layout the wrong one
	// is how localized buttons end up clipping their own text.
	text := b.DisplayText()
	var textWidth float32
	if text != "" {
		textWidth, _ = TextMetrics(text, font)
	}
	hasIcon := b.Icon != nil || b.IconVector != nil
	iconSize := b.IconSize
	if iconSize <= 0 {
		iconSize = buttonIconSize
	}
	gap := b.IconGap
	if gap <= 0 {
		gap = buttonIconGap
	}
	contentWidth := textWidth
	if hasIcon {
		contentWidth += iconSize
		if text != "" {
			contentWidth += gap
		}
	}
	pad := base.Padding
	// Font ascent+descent as approximate content height when no explicit
	// height is set. Callers who want a fixed height set MinHeight /
	// SetPreferredSize (see buttonHeight for the designed default).
	height := font.Size*1.3 + pad.Vertical()
	if h := base.Height; h > 0 {
		height = h
	}
	width := contentWidth + pad.Horizontal()
	// Count the border in the box width (border-box), matching a raw HTML
	// <button> whose 1 px border sits inside the layout box.
	if bs := base.BorderSize; bs > 0 {
		width += 2 * bs
	}
	if available.W > 0 && width > available.W {
		width = available.W
	}
	if available.H > 0 && height > available.H {
		height = available.H
	}
	return Size{W: width, H: height}
}

func (b *Button) Draw(canvas Canvas) {
	rect := b.Bounds()
	theme := CurrentTheme()
	state := b.currentState()
	style := themedStyle(b.States.Resolve(state))
	bg := style.Background
	fg := style.Foreground
	border := style.Border
	borderSize := style.BorderSize
	if borderSize <= 0 && border.A > 0 {
		borderSize = 1
	}

	// Radius clamps to the pill limit (min(w,h)/2). Style.Radius may
	// be a large "full-corner" sentinel; FillRoundedRect clamps anyway,
	// but we clamp here for the state-layer radius too.
	radius := style.Radius
	if lim := rect.H / 2; lim > 0 && (radius <= 0 || radius > lim) {
		radius = lim
	}

	// Elevation (opt-in via b.Elevation / HoverElevation; 0 = flat).
	if lvl := b.elevationForState(state); lvl > 0 {
		DrawElevation(canvas, rect, radius, lvl)
	}

	if bg.A > 0 {
		canvas.FillRoundedRect(rect, radius, bg)
	}

	if border.A > 0 {
		canvas.StrokeRoundedRect(rect, radius, border, borderSize)
	}

	// State-layer overlay (opt-in via b.StateLayerColor). Painted on
	// top of bg so it tints without replacing.
	if b.StateLayerColor.A > 0 && state&StateDisabled == 0 {
		switch {
		case state&StateHover != 0:
			t := b.hoverTrans.Value(time.Now())
			DrawStateLayer(canvas, rect, radius, b.StateLayerColor, theme.HoverOpacity*t)
		case state&StateFocused != 0:
			DrawStateLayer(canvas, rect, radius, b.StateLayerColor, theme.FocusOpacity)
		}
	}

	pad := style.Padding
	leftPad, rightPad := pad.Left, pad.Right
	content := Rect{X: rect.X + leftPad, Y: rect.Y, W: rect.W - leftPad - rightPad, H: rect.H}
	text := b.DisplayText()
	hasIcon := b.Icon != nil || b.IconVector != nil
	if hasIcon && text == "" {
		content = rect
	}

	iconSize := b.IconSize
	if iconSize <= 0 {
		iconSize = buttonIconSize
	}
	gap := b.IconGap
	if gap <= 0 {
		gap = buttonIconGap
	}

	if b.IconVector != nil || b.Icon != nil {
		iconRect := Rect{W: iconSize, H: iconSize}
		iconRect.Y = content.Y + (content.H-iconRect.H)/2
		if text == "" {
			iconRect.X = content.X + (content.W-iconRect.W)/2
			b.drawIcon(canvas, iconRect)
			return
		}
		// Icon + label: anchor icon at the left of the centered icon+gap+text
		// run so the pair stays visually centered as a unit.
		textW, _ := TextMetrics(text, b.font())
		runW := iconRect.W + gap + textW
		offset := (content.W - runW) / 2
		if offset < 0 {
			offset = 0
		}
		iconRect.X = content.X + offset
		b.drawIcon(canvas, iconRect)
		content.X = iconRect.X + iconRect.W + gap
		content.W = textW
	}
	if text != "" {
		drawCentered(canvas, text, content, fg, b.font())
	}
}

// drawCentered renders text horizontally and vertically centered within
// rect. Buttons (and other compact controls) want their label in the
// middle, not stuck against the top-left corner of the content box.
// Vertical centering is performed by DrawText itself (it inspects the
// supplied rect.H and centers the glyph ink span when there's slack);
// we only need to handle horizontal positioning here.
func drawCentered(canvas Canvas, text string, rect Rect, color Color, font Font) {
	if text == "" {
		return
	}
	tw, _ := TextMetrics(text, font)
	r := Rect{
		X: rect.X + (rect.W-tw)/2,
		Y: rect.Y,
		W: tw,
		H: rect.H,
	}
	if r.W < 0 {
		r.W = 0
	}
	canvas.DrawText(text, r, color, font)
}

// drawIcon prefers the vector source over the bitmap. Vector goes
// through qui.DrawVector so HiDPI canvases rasterize at physical pixel
// size — no bilinear upscale, no fuzz. Bitmap path is unchanged.
func (b *Button) drawIcon(canvas Canvas, rect Rect) {
	if b.IconVector != nil {
		DrawVector(canvas, b.IconVector, rect, b.IconTint)
		return
	}
	canvas.DrawImage(b.Icon, rect)
}

// Tick keeps the button repainting while its hover transition or
// ripple animation is still running. The dirty rect includes the
// shadow margin so elevation animation refreshes the shadow halo
// cleanly (without leaking ghost pixels at the previous elevation).
func (b *Button) Tick(now time.Time) Rect {
	if b.hoverTrans.Active(now) {
		return RectInWindow(b, b.dirtyRect())
	}
	return Rect{}
}

// mixAlpha scales c's alpha channel.
func mixAlpha(c Color, alpha float32) Color {
	return Color{R: c.R, G: c.G, B: c.B, A: c.A * alpha}
}

func (b *Button) Handle(event Event) bool {
	if !b.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			// Begin transition from wherever we currently are (handles
			// rapid re-enter mid-fade gracefully) toward fully-hovered.
			now := time.Now()
			b.hoverTrans.Begin(b.hoverTrans.Value(now), 1, now)
			b.hovering = true
			b.Invalidate()
		case EventMouseLeave:
			now := time.Now()
			b.hoverTrans.Begin(b.hoverTrans.Value(now), 0, now)
			b.hovering = false
			b.Invalidate()
		case EventMouseDown:
			if b.Bounds().Contains(Point{X: e.X, Y: e.Y}) && e.Button == MouseButtonLeft {
				b.pressed = true
				b.Invalidate()
				return true
			}
		case EventMouseUp:
			if b.pressed && e.Button == MouseButtonLeft {
				b.pressed = false
				b.Invalidate()
				if b.Bounds().Contains(Point{X: e.X, Y: e.Y}) && b.OnClick != nil {
					b.OnClick()
					return true
				}
			}
		}
	case KeyEvent:
		if b.focused && e.Type() == EventKeyDown && (e.Key == KeySpace || e.Key == KeyEnter) {
			if b.OnClick != nil {
				b.OnClick()
			}
			return true
		}
	}
	return false
}

func (b *Button) SetEnabled(enabled bool) {
	b.BaseWidget.SetEnabled(enabled)
	b.disabled = !enabled
	b.Invalidate()
}

func (b *Button) HitTest(p Point) Widget {
	if b.Bounds().Contains(p) {
		return b
	}
	return nil
}

// Image draws an image inside the widget bounds.
type Image struct {
	BaseWidget
	Image image.Image
	// Fit selects how the image maps into its bounds (CSS object-fit).
	// Default ImageStretch (fill) matches the historical behavior.
	Fit ImageFit
}

func NewImage(img image.Image) *Image {
	i := &Image{BaseWidget: NewBaseWidget(), Image: img}
	return i
}

func (i *Image) Measure(available Size) Size {
	if i.Image == nil {
		return Size{W: 0, H: 0}
	}
	bounds := i.Image.Bounds()
	return Size{W: float32(bounds.Dx()), H: float32(bounds.Dy())}
}

func (i *Image) Draw(canvas Canvas) {
	if i.Image == nil {
		return
	}
	b := i.Bounds()
	if i.Fit == ImageStretch {
		canvas.DrawImage(i.Image, b)
		return
	}
	ib := i.Image.Bounds()
	iw, ih := float32(ib.Dx()), float32(ib.Dy())
	if iw <= 0 || ih <= 0 {
		canvas.DrawImage(i.Image, b)
		return
	}
	sc := b.W / iw
	if h := b.H / ih; (i.Fit == ImageContain && h < sc) || (i.Fit == ImageCover && h > sc) {
		sc = h
	}
	dw, dh := iw*sc, ih*sc
	dst := Rect{X: b.X + (b.W-dw)/2, Y: b.Y + (b.H-dh)/2, W: dw, H: dh}
	if i.Fit == ImageCover {
		// Crop overflow to the box.
		depth := canvas.Save()
		canvas.ClipRect(b)
		canvas.DrawImage(i.Image, dst)
		canvas.RestoreTo(depth)
		return
	}
	canvas.DrawImage(i.Image, dst) // contain: letterboxed, no clip needed
}

func (i *Image) HitTest(p Point) Widget {
	if i.Bounds().Contains(p) {
		return i
	}
	return nil
}

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
