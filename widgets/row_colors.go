package widgets

import . "github.com/qizhanchan/qui"

// RowColors is the palette ListView and TableView paint with. Every zero
// field resolves from the current theme at draw time, so SetTheme (or a
// stylesheet setting a few fields) restyles a list that already exists.
// Because zero means "theme", a color field can't switch its decoration
// off; NoStripe / NoHover do that.
type RowColors struct {
	Background   Color
	Text         Color
	Border       Color
	Selected     Color // selected-row fill
	SelectedText Color
	Hover        Color
	Stripe       Color // odd-row zebra fill (TableView)
	Header       Color // TableView header strip
	HeaderText   Color
	Divider      Color // header dividers and separator
	Scrollbar    ScrollbarColors
	// NoStripe paints odd rows like even ones (no zebra); NoHover paints no
	// hover highlight. They beat Stripe / Hover.
	NoStripe bool
	NoHover  bool
}

// defaultRowColors is the theme-derived palette.
func defaultRowColors() RowColors {
	th := CurrentTheme()
	return RowColors{
		Background:   th.Surface,
		Text:         th.Text,
		Border:       th.Border,
		Selected:     th.Accent,
		SelectedText: th.AccentText,
		Hover:        LerpColor(th.Surface, th.Text, th.HoverOpacity),
		Stripe:       LerpColor(th.Surface, th.Text, 0.03),
		Header:       th.SurfaceRaised,
		HeaderText:   th.Text,
		Divider:      th.Border,
	}
}

// resolve overlays c's non-zero fields onto the theme defaults. legacy is
// the widget's Style(): its Background / Foreground / Border, when set,
// beat the theme (but not explicit RowColors).
func (c RowColors) resolve(legacy *Style) RowColors {
	out := defaultRowColors()
	if legacy != nil {
		pick(&out.Background, legacy.Background)
		pick(&out.Text, legacy.Foreground)
		pick(&out.Border, legacy.Border)
	}
	pick(&out.Background, c.Background)
	pick(&out.Text, c.Text)
	pick(&out.Border, c.Border)
	pick(&out.Selected, c.Selected)
	pick(&out.SelectedText, c.SelectedText)
	pick(&out.Hover, c.Hover)
	pick(&out.Stripe, c.Stripe)
	pick(&out.Header, c.Header)
	pick(&out.HeaderText, c.HeaderText)
	pick(&out.Divider, c.Divider)
	out.Scrollbar = c.Scrollbar
	if c.NoStripe {
		out.Stripe = Color{}
	}
	if c.NoHover {
		out.Hover = Color{}
	}
	return out
}

func pick(dst *Color, v Color) {
	if v != (Color{}) {
		*dst = v
	}
}
