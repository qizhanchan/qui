package htmlcss

import (
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Native-backed popups (the <select> dropdown, the datalist suggestion
// list, the color palette, `title` tooltips) are overlays outside the
// element tree, so the cascade can't reach them directly. They take their
// colors from the element that opened them instead, in this order:
//
//  1. the inherited custom properties --popup-background, --popup-color,
//     --popup-border and --popup-accent (set them on :root, or on .dark,
//     to restyle every popup at once);
//  2. the element's own authored background-color / color / accent-color;
//  3. the theme.
//
// Tooltips follow the same rule with --tooltip-background, --tooltip-color
// and --tooltip-border (falling back to the --popup-* ones), read from the
// main-tree root. Scrollbars use the standard `scrollbar-color`.

type popupPalette struct {
	Surface, Text, Border, Accent qui.Color
}

func (e *El) popupPalette() popupPalette {
	th := qui.CurrentTheme()
	p := popupPalette{Surface: th.SurfaceOverlay, Text: th.Text, Border: th.BorderStrong, Accent: th.Accent}
	cs := e.lastCS
	if cs == nil {
		return p
	}
	if cs.HasBackground && cs.Background.A > 0 {
		p.Surface = cs.Background
	}
	if cs.HasColor {
		p.Text = cs.Color
	}
	if cs.HasAccentColor {
		p.Accent = cs.AccentColor
	}
	if c, ok := cs.ColorVar("popup-background"); ok {
		p.Surface = c
	}
	if c, ok := cs.ColorVar("popup-color"); ok {
		p.Text = c
	}
	if c, ok := cs.ColorVar("popup-border"); ok {
		p.Border = c
	}
	if c, ok := cs.ColorVar("popup-accent"); ok {
		p.Accent = c
	}
	return p
}

// styled reports whether anything beyond the theme applies — unstyled
// controls keep their exact native look.
func (e *El) popupStyled() bool {
	cs := e.lastCS
	if cs == nil {
		return false
	}
	for _, v := range []string{"popup-background", "popup-color", "popup-border", "popup-accent"} {
		if cs.Var(v) != "" {
			return true
		}
	}
	return cs.HasBackground || cs.HasColor || cs.HasAccentColor
}

// menuColors maps the palette onto a dropdown's MenuColors.
func (p popupPalette) menuColors() widgets.MenuColors {
	return widgets.MenuColors{
		Background:      p.Surface,
		Foreground:      p.Text,
		ForegroundMuted: qui.LerpColor(p.Surface, p.Text, 0.6),
		Divider:         qui.LerpColor(p.Surface, p.Text, 0.2),
		SelectedBg:      qui.LerpColor(p.Surface, p.Accent, 0.25),
		SelectedFg:      p.Text,
		StateLayer:      p.Text,
	}
}

// applyPopupColors pushes the palette onto a <select> backing's dropdown.
func (e *El) applyPopupColors() {
	sel, ok := e.backing.(*widgets.Select)
	if !ok {
		return
	}
	if !e.popupStyled() {
		sel.DropdownColors = widgets.MenuColors{}
		return
	}
	sel.DropdownColors = e.popupPalette().menuColors()
}

// applyScrollbarColors maps `scrollbar-color` onto the element's scroll
// view.
func (e *El) applyScrollbarColors(cs *ComputedStyle) {
	if e.scrollView == nil {
		return
	}
	e.scrollView.BarColors = qui.ScrollbarColors{}
	if cs.HasScrollbarColor {
		e.scrollView.BarColors = qui.ScrollbarColors{
			Thumb: cs.ScrollbarThumb, ThumbActive: cs.ScrollbarThumb, Track: cs.ScrollbarTrack,
		}
	}
}

// applyTooltipStyle derives the window's tooltip style from the main
// root's computed custom properties.
func (s *StyleEngine) applyTooltipStyle() {
	if s.root == nil || s.root.lastCS == nil {
		return
	}
	win := s.root.Window()
	if win == nil {
		return
	}
	cs := s.root.lastCS
	st := win.TooltipStyle()
	any := false
	pick := func(dst *qui.Color, names ...string) {
		*dst = qui.Color{}
		for _, n := range names {
			if c, ok := cs.ColorVar(n); ok {
				*dst, any = c, true
				return
			}
		}
	}
	pick(&st.Background, "tooltip-background", "popup-background")
	pick(&st.Text, "tooltip-color", "popup-color")
	pick(&st.Border, "tooltip-border", "popup-border")
	// Leave an app-set tooltip style alone unless the stylesheet speaks
	// (or spoke before and has stopped: then clear what it set).
	if any || s.tooltipStyled {
		win.SetTooltipStyle(st)
	}
	s.tooltipStyled = any
}

// splitColorList splits a space-separated list of colors, keeping
// functional notations (rgb(1, 2, 3)) whole.
func splitColorList(s string) []string {
	var out []string
	for _, p := range splitTopLevel(strings.TrimSpace(s), ' ') {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
