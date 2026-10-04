// theme example — live brand-palette switching.
//
// qui ships a single light baseline; dark mode is an htmlcss/CSS concern
// (see qui.Theme docs). What stays global is the token set — surfaces,
// text, accent, lines — and any of it can be swapped at runtime. This
// demo retints the accent to show the live-update path.
//
// What to try:
//   - Click the "Switch palette" button. Every widget on screen
//     updates colors immediately. No widget recreation, no manual
//     invalidation — Window subscribes to SetTheme via NewWindow.
//   - Hover over the button itself to see the hover
//     transition (bg cross-fades using theme.TransitionShort).
//   - Tab through the form: focus rings use theme.BorderFocus.
//   - Open the dialog — its backdrop, content surface, title, and
//     action buttons all pick up the theme.
package main

import (
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	window, err := app.NewWindow("Theme Demo", 720, 560)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	// Build a sample form to exercise the themed widgets.
	title := widgets.NewLabel("Qui Theme Demo — Dark / Light live switch")
	title.Style().Font = qui.Font{Size: qui.CurrentTheme().FontHeading, Bold: true}
	// Label currently doesn't read theme for text color — set explicitly
	// to the theme's Text color so it looks right today. (A later
	// migration pass can theme-ify Label too.)
	title.Style().Foreground = qui.CurrentTheme().Text

	cb1 := widgets.NewCheckBox("Enable notifications", nil)
	cb1.Checked = true
	cb2 := widgets.NewCheckBox("Auto-save on close", nil)

	sw := widgets.NewSwitch("Dark mode indicator (see me flip!)", nil)

	// Radio group for theme flavors (demonstration — the actual theme
	// toggle is the button below).
	radios := widgets.NewRadioGroup()
	r1 := widgets.NewRadioButton(radios, "Auto")
	r2 := widgets.NewRadioButton(radios, "Always dark")
	r3 := widgets.NewRadioButton(radios, "Always light")
	radios.Select(r1)
	_ = r2
	_ = r3

	slider := widgets.NewSlider(0, 100, 40, nil)
	slider.SetFlex(1)

	progress := widgets.NewProgress(0, 100)
	progress.Value = 65
	progress.SetFlex(1)

	tf := widgets.NewInput("Your name here…")

	// Dialog trigger.
	dlgBtn := widgets.NewButton("Open themed dialog", func() {
		body := qui.NewContainer(
			qui.FlexLayout{Direction: qui.Vertical, Gap: 8},
			widgets.NewCheckBox("Subscribe to newsletter", nil),
			widgets.NewSlider(0, 10, 5, nil),
		)
		dlg := widgets.NewDialog("Preferences", body)
		dlg.AddButton("Cancel", nil)
		dlg.AddButton("Apply", func() { log.Println("applied") })
		dlg.Show(window)
	})

	// Palette toggle button — the star of the show. Swaps between two
	// accents (the baseline blue ↔ a sapphire blue), both light.
	sapphire := false
	themeToggle := widgets.NewButton("Switch palette", func() {
		sapphire = !sapphire
		if sapphire {
			qui.SetTheme(tintedTheme(sapphireAccent))
			log.Println("→ Sapphire")
		} else {
			qui.SetTheme(tintedTheme(baselineAccent))
			log.Println("→ Baseline")
		}
		// Label text color was set explicitly above — refresh so it
		// tracks the new theme. (Labels don't yet auto-read theme.)
		title.Style().Foreground = qui.CurrentTheme().Text
	})

	// Layout: header with toggle, then a FieldSet full of form widgets.
	header := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 12, AlignItems: qui.AlignCenter, Justify: qui.JustifySpaceBetween},
		title, themeToggle,
	)

	toggles := widgets.NewFieldSet("Toggles", qui.FlexLayout{Direction: qui.Vertical, Gap: 8}, cb1, cb2, sw)
	toggles.SetFlex(1)

	radiosGroup := widgets.NewFieldSet("Mode", qui.FlexLayout{Direction: qui.Vertical, Gap: 6}, r1, r2, r3)
	radiosGroup.SetFlex(1)

	togglesRow := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 12, AlignItems: qui.AlignStretch},
		toggles, radiosGroup,
	)

	sliderLabel := widgets.NewLabel("Slider")
	sliderLabel.Style().Foreground = qui.CurrentTheme().Text
	sliderRow := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 12, AlignItems: qui.AlignCenter},
		sliderLabel, slider,
	)

	progressLabel := widgets.NewLabel("Progress")
	progressLabel.Style().Foreground = qui.CurrentTheme().Text
	progressRow := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 12, AlignItems: qui.AlignCenter},
		progressLabel, progress,
	)

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16},
		header,
		togglesRow,
		sliderRow,
		progressRow,
		tf,
		dlgBtn,
	)
	root.Style().Padding = qui.Insets{Top: 20, Right: 24, Bottom: 20, Left: 24}

	// Root background also needs to track theme. Cheapest approach:
	// subscribe to theme changes and update root.Style().Background.
	refreshRoot := func() {
		root.Style().Background = qui.CurrentTheme().Surface
		// Label.Style.Foreground captured at construction — refresh
		// the few labels we pre-set.
		title.Style().Foreground = qui.CurrentTheme().Text
		sliderLabel.Style().Foreground = qui.CurrentTheme().Text
		progressLabel.Style().Foreground = qui.CurrentTheme().Text
	}
	refreshRoot()
	qui.SubscribeTheme(refreshRoot)

	window.SetRoot(root)
	app.Run()
}

// tintedTheme returns the baseline theme with a different accent color.
// Now that qui's tokens carry no design-system baggage, "swap the brand
// palette" is exactly this: copy the baseline, move the accent, and hand
// it to SetTheme. Anything more opinionated belongs in CSS on the
// htmlcss layer.
func tintedTheme(accent qui.Color) qui.Theme {
	t := qui.LightTheme
	t.Accent = accent
	t.AccentHover = qui.LerpColor(accent, qui.ColorBlack, 0.12)
	t.AccentPressed = qui.LerpColor(accent, qui.ColorBlack, 0.24)
	t.BorderFocus = accent
	return t
}

// Two accents to toggle between: the neutral baseline blue and a
// sapphire that is clearly a different brand.
var (
	baselineAccent = qui.LightTheme.Accent
	sapphireAccent = qui.Color{R: 0x0b / 255.0, G: 0x57 / 255.0, B: 0xd0 / 255.0, A: 1}
)
