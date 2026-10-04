// dialog example — modal Dialog with form content and custom actions.
//
// What to try:
//   - Click "Open Dialog" to raise a modal. Body has a name field, a
//     CheckBox, a Switch and a Slider — all interactive inside the dialog.
//   - Clear the name and press Enter (or Save): validation keeps the
//     dialog open and shows an error. Enter presses Save from the field.
//   - "Reset" is a custom action that never closes the dialog.
//   - Click outside the box: nothing happens (backdrop absorbed). Esc or
//     Cancel dismisses; OnClose logs why.
//   - Tab / Shift+Tab: focus cycles ONLY within dialog widgets —
//     the main button behind the backdrop is skipped (focus trap).
package main

import (
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
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
	agent.BindEnv(window) // QUI_AGENT=1 → drive it with cmd/qui-agent
	app.Run()
}

// openSettingsDialog assembles a dialog with mixed form controls, a
// validated Save, and a custom action, then shows it modally.
func openSettingsDialog(window *qui.Window) {
	var (
		notifications = true
		darkMode      = false
		volume        = float32(50)
	)

	name := widgets.NewInput("Profile name")
	name.SetText("Default")

	errLabel := widgets.NewLabel("")
	errLabel.Style().Foreground = qui.CurrentTheme().Error

	notify := widgets.NewCheckBox("Enable notifications", func(v bool) {
		notifications = v
	})
	notify.Checked = notifications

	theme := widgets.NewSwitch("Dark mode", func(v bool) {
		darkMode = v
	})
	theme.On = darkMode

	volLabel := widgets.NewLabel("Volume")

	volSlider := widgets.NewSlider(0, 100, volume, func(v float32) {
		volume = v
	})

	body := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		name,
		errLabel,
		notify,
		theme,
		volLabel,
		volSlider,
	)

	dlg := widgets.NewDialog("Settings", body)
	dlg.ContainerElevation = 3

	// A custom action: any widget, and it never closes the dialog.
	reset := widgets.NewButton("Reset", func() {
		name.SetText("Default")
		volSlider.SetValue(50)
		errLabel.SetText("")
	})
	dlg.AddAction(reset)

	dlg.AddButton("Cancel", nil)

	// Validation: Save decides for itself when to close.
	save := widgets.NewButton("Save", nil)
	save.OnClick = func() {
		if name.Text == "" {
			errLabel.SetText("Name is required")
			return
		}
		log.Printf("saved: name=%q notifications=%v darkMode=%v volume=%.0f",
			name.Text, notifications, darkMode, volume)
		dlg.CloseWith(widgets.DialogCloseAction)
	}
	dlg.AddAction(save)
	dlg.DefaultAction = save // Enter presses Save, also from the name field
	dlg.InitialFocus = name

	dlg.OnClose = func(reason widgets.DialogCloseReason) {
		log.Printf("dialog closed (%s)", reason)
	}
	dlg.Show(window)
}
