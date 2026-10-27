package qui

// DrawStateLayer overlays a translucent interaction layer above a
// rounded rect: the way a widget shows it is hovered / focused /
// pressed without swapping its palette. The layer is a tint of the
// widget's own foreground painted at one of the Theme's interaction
// opacities, so one background color plus one overlay covers every
// state. Widgets call this once per Draw (or twice — once for hover,
// once for focus when they coexist) after painting their background.
//
// layerColor is normally the color the widget draws its content in
// (Theme.Text over a neutral surface, Theme.AccentText over an accent
// fill). opacity comes from Theme.HoverOpacity etc.; multiply by a
// transition value (0..1) so the layer fades in and out smoothly.
func DrawStateLayer(canvas Canvas, rect Rect, radius float32, layerColor Color, opacity float32) {
	if canvas == nil || opacity <= 0 {
		return
	}
	c := layerColor
	c.A = c.A * opacity
	if c.A <= 0 {
		return
	}
	canvas.FillRoundedRect(rect, radius, c)
}

// DrawElevation paints both shadow components (key + ambient) for the
// given level. Reads Theme.Elevation[level] and Theme.Shadow internally.
// Pass level 0 to no-op cleanly.
func DrawElevation(canvas Canvas, rect Rect, radius float32, level int) {
	if canvas == nil || level <= 0 {
		return
	}
	theme := CurrentTheme()
	if level >= len(theme.Elevation) {
		level = len(theme.Elevation) - 1
	}
	lev := theme.Elevation[level]
	canvas.DrawShadow(rect, radius, lev.Ambient, theme.Shadow)
	canvas.DrawShadow(rect, radius, lev.Key, theme.Shadow)
}

// StateLayerColor picks the conventional layer color + opacity for the
// state combination, given the surface a widget sits over. Most widgets
// only care about hover vs pressed vs focused vs none — this collapses
// the cases into one call.
//
// foreground is the color the widget paints its content in over the
// surface in question — Theme.Text for a neutral surface,
// Theme.AccentText for an accent-filled control.
func StateLayerColor(foreground Color, state State, theme *Theme) (Color, float32) {
	switch {
	case state&StatePressed != 0:
		return foreground, theme.PressedOpacity
	case state&StateFocused != 0:
		return foreground, theme.FocusOpacity
	case state&StateHover != 0:
		return foreground, theme.HoverOpacity
	}
	return foreground, 0
}
