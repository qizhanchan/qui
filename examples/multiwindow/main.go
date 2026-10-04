package main

import (
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	main1, err := app.NewWindow("qui — main window", 640, 400)
	if err != nil {
		log.Fatal(err)
	}
	main1.SetRenderer(qui.NewGLRenderer())

	status := widgets.NewLabel("Main window. Use File → New Window or press Cmd+N.")
	status.Style().Foreground = qui.ColorWhite
	status.Style().Font.Size = 16

	popupBtn := widgets.NewButton("Open inspector", func() {
		inspector, err := app.NewSharedWindow("Inspector", 320, 200, main1)
		if err != nil {
			log.Println(err)
			return
		}
		inspector.SetRenderer(qui.NewGLRenderer())
		lbl := widgets.NewLabel("Shared-context inspector")
		lbl.Style().Foreground = qui.ColorWhite
		inspector.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 8}, lbl))
	})

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 12}, status, popupBtn)
	root.Style().Background = qui.Color{R: 0.11, G: 0.13, B: 0.15, A: 1}
	main1.SetRoot(root)

	// Native macOS menu bar. Cmd+N opens a new window; Cmd+I opens the
	// inspector. On non-darwin platforms SetMenuBar is a no-op.
	windowCount := 1
	sapphire := false // brand-palette toggle (both light — dark is an htmlcss concern)
	app.SetMenuBar(&qui.NativeMenuBar{
		Menus: []*qui.NativeMenu{
			{
				Title: "File",
				Items: []*qui.NativeMenuItem{
					{
						Label:    "New Window",
						Shortcut: "Cmd+N",
						Enabled:  true,
						Action: func() {
							windowCount++
							w, err := app.NewWindow(fmt.Sprintf("qui — window %d", windowCount), 480, 320)
							if err != nil {
								log.Println(err)
								return
							}
							w.SetRenderer(qui.NewGLRenderer())
							lbl := widgets.NewLabel(fmt.Sprintf("Window #%d", windowCount))
							lbl.Style().Foreground = qui.ColorWhite
							w.SetRoot(qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 8}, lbl))
						},
					},
					{
						Label:    "Open Inspector",
						Shortcut: "Cmd+I",
						Enabled:  true,
						Action:   popupBtn.OnClick,
					},
					{Separator: true},
					{
						Label:    "Update Status",
						Shortcut: "Cmd+U",
						Enabled:  true,
						Action: func() {
							status.SetText("Cmd+U fired from the native menu.")
						},
					},
				},
			},
			{
				Title: "View",
				Items: []*qui.NativeMenuItem{
					{
						Label:    "Switch Palette",
						Shortcut: "Cmd+T",
						Enabled:  true,
						Action: func() {
							sapphire = !sapphire
							if sapphire {
								qui.SetTheme(tintedTheme(sapphireAccent))
							} else {
								qui.SetTheme(tintedTheme(baselineAccent))
							}
						},
					},
				},
			},
		},
	})

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
