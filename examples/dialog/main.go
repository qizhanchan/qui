// dialog example — modal Dialog with form content and action buttons.
//
// What to try:
//   - Click "Open Dialog" to raise a modal. Body has a CheckBox,
//     a Switch, and a Slider — all fully interactive inside the dialog.
//   - Click outside the dialog box: nothing happens (backdrop is
//     absorbed, modal blocks). Click "Save" or "Cancel" or press Esc
//     to dismiss.
//   - Tab / Shift+Tab: focus cycles ONLY within dialog widgets —
//     the main button behind the backdrop is skipped (focus trap).
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

	window, err := app.NewWindow("Dialog Demo", 800, 600)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	title := widgets.NewLabel("Dialog Demo — click the button below")
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 20

	hint := widgets.NewLabel("Tab cycles focus inside the dialog.\nEsc or Cancel dismisses.")
	hint.Style().Foreground = qui.Color{R: 0.7, G: 0.7, B: 0.7, A: 1}

	openBtn := widgets.NewButton("Open Dialog", nil)
	openBtn.Style().Font.Size = 14
	openBtn.OnClick = func() {
		openSettingsDialog(window)
	}

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16},
		title,
		hint,
		openBtn,
	)
	root.Style().Background = qui.Color{R: 0.1, G: 0.1, B: 0.12, A: 1}
	root.Style().Padding = qui.Insets{Top: 40, Right: 40, Bottom: 40, Left: 40}

	window.SetRoot(root)
	app.Run()
}

// openSettingsDialog assembles a dialog with mixed form controls and
// two actions, then shows it modally.
func openSettingsDialog(window *qui.Window) {
	var (
		notifications = true
		darkMode      = false
		volume        = float32(50)
	)

	notify := widgets.NewCheckBox("Enable notifications", func(v bool) {
		notifications = v
		log.Printf("notifications → %v", v)
	})
	notify.Checked = notifications

	theme := widgets.NewSwitch("Dark mode", func(v bool) {
		darkMode = v
		log.Printf("darkMode → %v", v)
	})
	theme.On = darkMode

	volLabel := widgets.NewLabel("Volume")
	volLabel.Style().Foreground = qui.ColorWhite

	volSlider := widgets.NewSlider(0, 100, volume, func(v float32) {
		volume = v
		log.Printf("volume → %.0f", v)
	})

	body := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		notify,
		theme,
		volLabel,
		volSlider,
	)

	dlg := widgets.NewDialog("Settings", body)
	dlg.AddButton("Cancel", func() {
		log.Println("dialog canceled")
	})
	dlg.AddButton("Save", func() {
		log.Printf("saved: notifications=%v darkMode=%v volume=%.0f",
			notifications, darkMode, volume)
	})
	dlg.OnClose = func() {
		log.Println("dialog closed")
	}
	dlg.Show(window)
}
