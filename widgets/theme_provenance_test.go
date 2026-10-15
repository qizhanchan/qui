package widgets

import (
	"fmt"
	"testing"

	. "github.com/qizhanchan/qui"
)

// colorCanvas records the color of every paint call, bucketed by what the
// paint is: a fill, a stroke (border), or text.
type colorCanvas struct {
	RecordingCanvas
	fills, strokes, texts []Color
}

func (c *colorCanvas) FillRect(r Rect, col Color) {
	c.fills = append(c.fills, col)
	c.RecordingCanvas.FillRect(r, col)
}

func (c *colorCanvas) FillRoundedRect(r Rect, radius float32, col Color) {
	c.fills = append(c.fills, col)
	c.RecordingCanvas.FillRoundedRect(r, radius, col)
}

func (c *colorCanvas) StrokeRect(r Rect, col Color, w float32) {
	c.strokes = append(c.strokes, col)
	c.RecordingCanvas.StrokeRect(r, col, w)
}

func (c *colorCanvas) StrokeRoundedRect(r Rect, radius float32, col Color, w float32) {
	c.strokes = append(c.strokes, col)
	c.RecordingCanvas.StrokeRoundedRect(r, radius, col, w)
}

func (c *colorCanvas) DrawText(s string, r Rect, col Color, f Font) {
	c.texts = append(c.texts, col)
	c.RecordingCanvas.DrawText(s, r, col, f)
}

func (c *colorCanvas) DrawShape(s Shape, p Paint) {
	if p.Style == PaintFill {
		c.fills = append(c.fills, p.Color)
	} else {
		c.strokes = append(c.strokes, p.Color)
	}
	c.RecordingCanvas.DrawShape(s, p)
}

// darkTestTheme is a dark palette whose every color token differs from
// every native baseline color, so a remap is always visible.
func darkTestTheme() Theme {
	th := LightTheme
	th.Surface = Color{R: 0.07, G: 0.08, B: 0.09, A: 1}
	th.SurfaceSunken = Color{R: 0.05, G: 0.05, B: 0.06, A: 1}
	th.SurfaceRaised = Color{R: 0.11, G: 0.12, B: 0.13, A: 1}
	th.SurfaceStrong = Color{R: 0.15, G: 0.16, B: 0.17, A: 1}
	th.SurfaceOverlay = Color{R: 0.13, G: 0.14, B: 0.15, A: 1}
	th.Text = Color{R: 0.91, G: 0.92, B: 0.93, A: 1}
	th.TextMuted = Color{R: 0.66, G: 0.67, B: 0.68, A: 1}
	th.Accent = Color{R: 0.61, G: 0.42, B: 0.97, A: 1}
	th.Border = Color{R: 0.24, G: 0.25, B: 0.26, A: 1}
	th.BorderStrong = Color{R: 0.36, G: 0.37, B: 0.38, A: 1}
	return th
}

// TestAuthoredColorsSurviveTheme is the provenance matrix: every native
// baseline color, written by an app (or an htmlcss author) into each
// color slot of each native control after construction, must be painted
// as written once a dark theme is active. Only colors the constructors
// seeded themselves follow the theme — the framework must never decide by
// VALUE that an authored color is "its own".
func TestAuthoredColorsSurviveTheme(t *testing.T) {
	baselines := map[string]Color{
		"htmlControlBorder":  htmlControlBorder,
		"htmlControlText":    htmlControlText,
		"htmlControlBg":      htmlControlBg,
		"htmlButtonBg":       htmlButtonBg,
		"htmlAccent":         htmlAccent,
		"htmlDisabledText":   htmlDisabledText,
		"htmlDisabledBorder": htmlDisabledBorder,
		"baselineHoverBg":    baselineHoverBg,
		"baselinePressedBg":  baselinePressedBg,
		"baselineDisabledBg": baselineDisabledBg,
		"baselineBlue":       baselineBlue,
		"baselineTrack":      baselineTrack,
		"baselineOffBorder":  baselineOffBorder,
		"baselineTextGray":   baselineTextGray,
		"baselineBarBg":      baselineBarBg,
		"baselineBarText":    baselineBarText,
	}
	type slot int
	const (
		fg slot = iota
		bg
		border
	)
	slotNames := [...]string{"Foreground", "Background", "Border"}

	// build returns a control with the authored color written into the
	// slot, and the paint bucket that slot's color must show up in.
	type control struct {
		name  string
		build func(s slot, c Color) Widget
	}
	setStyle := func(st *Style, s slot, c Color) {
		switch s {
		case fg:
			st.Foreground = c
		case bg:
			st.Background = c
		case border:
			st.Border = c
		}
	}
	controls := []control{
		{"Input", func(s slot, c Color) Widget {
			in := NewInput("")
			in.SetText("text")
			UpdateStyle(in, func(st *Style) { setStyle(st, s, c) })
			return in
		}},
		{"TextArea", func(s slot, c Color) Widget {
			ta := NewTextArea("")
			ta.SetText("text")
			UpdateStyle(ta, func(st *Style) { setStyle(st, s, c) })
			return ta
		}},
		{"Button", func(s slot, c Color) Widget {
			b := NewButton("text", nil)
			UpdateStyle(b, func(st *Style) { setStyle(st, s, c) })
			return b
		}},
		{"Select", func(s slot, c Color) Widget {
			sel := NewSelect(nil, []string{"text"}, nil)
			sel.SelectedIdx = 0
			UpdateStyle(sel, func(st *Style) { setStyle(st, s, c) })
			return sel
		}},
		// CheckBox has no Style colors; its slots are the label (text),
		// the checked fill (background) and the outline (border).
		{"CheckBox", func(s slot, c Color) Widget {
			cb := NewCheckBox("text", nil)
			switch s {
			case fg:
				cb.LabelColor = c
			case bg:
				cb.CheckedFillColor = c
				cb.Checked = true
			case border:
				cb.OutlineColor = c
			}
			return cb
		}},
	}

	saved := *CurrentTheme()
	defer SetTheme(saved)
	SetTheme(darkTestTheme())

	for _, ctl := range controls {
		for name, base := range baselines {
			// An author's color: the same RGBA, written as a literal.
			authored := Color{R: base.R, G: base.G, B: base.B, A: base.A}
			for s := fg; s <= border; s++ {
				if ctl.name == "Select" && s == bg {
					continue // the outlined native select never paints Background
				}
				t.Run(fmt.Sprintf("%s/%s/%s", ctl.name, slotNames[s], name), func(t *testing.T) {
					w := ctl.build(s, authored)
					w.Layout(Rect{W: 200, H: 60})
					cv := &colorCanvas{}
					w.Draw(cv)
					got := map[slot][]Color{fg: cv.texts, bg: cv.fills, border: cv.strokes}[s]
					for _, c := range got {
						if c == authored {
							return
						}
					}
					t.Errorf("authored %s %v not painted; %s paints: %v", slotNames[s], authored, slotNames[s], got)
				})
			}
		}
	}
}

// FieldSet draws its legend through an internal Label. A style the app set
// on that Label (reached through ChildList) must survive Draw, while an
// unstyled legend keeps following the theme and the FieldSet's font.
func TestFieldSetKeepsAppLegendStyle(t *testing.T) {
	saved := *CurrentTheme()
	defer SetTheme(saved)
	SetTheme(darkTestTheme())

	fs := NewFieldSet("Legend", FlexLayout{Direction: Vertical})
	legend := fs.ChildList()[0].(*Label)
	fs.Layout(Rect{W: 200, H: 100})
	cv := &colorCanvas{}
	fs.Draw(cv)
	if len(cv.texts) == 0 || cv.texts[len(cv.texts)-1] != CurrentTheme().Text {
		t.Fatalf("unstyled legend paints %v, want the theme text color", cv.texts)
	}
	if legend.Style().Font.Size != fs.Style().Font.Size {
		t.Fatal("unstyled legend should take the FieldSet's font")
	}

	red := Color{R: 0.8, G: 0.1, B: 0.1, A: 1}
	appFont := Font{Size: 20, Italic: true}
	UpdateStyle(legend, func(st *Style) {
		st.Foreground = red
		st.Font = appFont
	})
	cv = &colorCanvas{}
	fs.Draw(cv)
	if got := legend.Style(); got.Foreground != red || got.Font.Size != 20 || !got.Font.Italic {
		t.Fatalf("Draw rewrote the app's legend style: fg %v font %+v", got.Foreground, got.Font)
	}
	if cv.texts[len(cv.texts)-1] != red {
		t.Fatalf("legend painted %v, want the app's %v", cv.texts, red)
	}
}

// A Label's unset text color follows the theme; an explicit ColorBlack is
// the app's and stays black.
func TestLabelExplicitBlackSurvivesTheme(t *testing.T) {
	saved := *CurrentTheme()
	defer SetTheme(saved)
	SetTheme(darkTestTheme())

	l := NewLabel("x")
	if got := l.resolveForeground(); got != CurrentTheme().Text {
		t.Fatalf("default label color %v, want the theme text", got)
	}
	l.Style().Foreground = ColorBlack
	if got := l.resolveForeground(); got != ColorBlack {
		t.Fatalf("explicit black label painted %v", got)
	}
}
