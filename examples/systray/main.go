// systray example — macOS NSStatusItem with a popup menu.
//
// What to try:
//   - Look at the top-right of your menu bar — a settings-gear icon
//     appears alongside system icons (Wi-Fi, battery, clock, …).
//   - Click the icon to open the menu.
//   - "Switch Icon" toggles between the settings and add glyphs at
//     runtime (StatusItem.SetIcon).
//   - "Toggle Tooltip" rewrites the hover string (StatusItem.SetTooltip).
//   - Each menu item logs to stdout. Quit closes the window cleanly
//     and the icon disappears from the menu bar.
//   - Switch macOS to dark mode (System Settings → Appearance) — the
//     template icon auto-tints from black to white.
//
// Non-darwin builds: qui.ErrSystrayNotSupported is returned and the
// example logs a warning instead of crashing.
package main

import (
	"errors"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/icons"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	window, err := app.NewWindow("System Tray Demo", 480, 240)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	hint := widgets.NewLabel("Look at the top-right of your menu bar.\nClick the gear icon to open the tray menu.")
	hint.Style().Foreground = qui.ColorWhite

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16, Justify: qui.JustifyCenter, AlignItems: qui.AlignCenter},
		hint,
	)
	root.Style().Background = qui.Color{R: 0.1, G: 0.1, B: 0.12, A: 1}
	root.Style().Padding = qui.Insets{Top: 24, Right: 24, Bottom: 24, Left: 24}
	window.SetRoot(root)

	tooltipState := "qui demo — click for menu"
	var tray *qui.StatusItem
	useSettings := true

	tray, err = app.NewStatusItem(qui.StatusItemOptions{
		Icon:    icons.Settings,
		Tooltip: tooltipState,
		Menu: &qui.NativeMenu{
			Items: []*qui.NativeMenuItem{
				{Label: "Switch Icon", Action: func() {
					useSettings = !useSettings
					if useSettings {
						tray.SetIcon(icons.Settings)
						log.Println("tray: icon → settings")
					} else {
						tray.SetIcon(icons.Add)
						log.Println("tray: icon → add")
					}
				}},
				{Label: "Toggle Tooltip", Action: func() {
					if tooltipState == "qui demo — click for menu" {
						tooltipState = "qui demo — tooltip toggled"
					} else {
						tooltipState = "qui demo — click for menu"
					}
					tray.SetTooltip(tooltipState)
					log.Printf("tray: tooltip → %s", tooltipState)
				}},
				{Separator: true},
				{Label: "Quit", Shortcut: "Cmd+Q", Action: func() {
					log.Println("tray: quit")
					window.Destroy()
				}},
			},
		},
	})
	switch {
	case errors.Is(err, qui.ErrSystrayNotSupported):
		log.Println("systray: not supported on this platform — running window only")
	case err != nil:
		log.Fatal(err)
	default:
		log.Printf("systray: installed (logical 22pt @2x)")
		window.OnClose(func() { tray.Destroy() })
	}

	app.Run()
}
