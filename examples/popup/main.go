// popup example — non-modal dropdown / context-menu style overlay.
//
// What to try:
//   - Click "Show Menu" to open a popup of menu items anchored below
//     the button. Click any item: it logs and the popup closes.
//   - Click anywhere outside the popup: it auto-dismisses (default
//     DismissOnOutsideClick behavior).
//   - Press Esc: also dismisses.
//   - Unlike Dialog, the main tree stays interactive — click the
//     label or other buttons while the popup is still open (though
//     they'll dismiss it immediately due to the outside-click rule).
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

	window, err := app.NewWindow("Popup Demo", 800, 600)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	selected := widgets.NewLabel("Selected: (none)")
	selected.Style().Foreground = qui.ColorWhite
	selected.Style().Font.Size = 16

	menuBtn := widgets.NewButton("Show Menu", nil)
	menuBtn.Style().Font.Size = 14
	menuBtn.OnClick = func() {
		// Anchor the popup just below the button's layout rect.
		anchor := menuBtn.Bounds()
		showMenu(window, anchor.X, anchor.Y+anchor.H+4, func(item string) {
			// SetText (not `Text =`) so the label re-measures: the
			// preferred width grows from "(none)" to "Save As..."
			// and the layout-dirty flag triggers a fresh Layout pass
			// before the next paint. Mutating Text directly leaves
			// the label at its first-measured width and the new
			// text gets clipped mid-character.
			selected.SetText("Selected: " + item)
			log.Printf("menu item selected: %s", item)
		})
	}

	title := widgets.NewLabel("Popup / Dropdown Demo")
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 20

	hint := widgets.NewLabel("Click outside or press Esc to dismiss.")
	hint.Style().Foreground = qui.Color{R: 0.7, G: 0.7, B: 0.7, A: 1}

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16},
		title,
		hint,
		menuBtn,
		selected,
	)
	root.Style().Background = qui.Color{R: 0.1, G: 0.1, B: 0.12, A: 1}
	root.Style().Padding = qui.Insets{Top: 40, Right: 40, Bottom: 40, Left: 40}

	window.SetRoot(root)
	app.Run()
}

// showMenu builds a Popup whose content is a vertical list of
// clickable items; selecting one fires onSelect and closes the popup.
func showMenu(window *qui.Window, x, y float32, onSelect func(string)) {
	items := []string{"New File", "Open...", "Save", "Save As..."}

	// We need `popup` visible inside each button's OnClick to call
	// Close — build the popup first with a placeholder content, then
	// assign the real content after we can reference the popup.
	var popup *widgets.Popup

	var itemWidgets []qui.Widget
	for _, name := range items {
		item := name // capture loop var
		btn := widgets.NewButton(item, func() {
			onSelect(item)
			if popup != nil {
				popup.Close()
			}
		})
		// Tighter button style for menu items.
		btn.Style().Background = qui.Color{R: 0.22, G: 0.22, B: 0.24, A: 1}
		btn.Style().Foreground = qui.ColorWhite
		btn.Style().Radius = 3
		btn.Style().Padding = qui.Insets{Top: 6, Right: 12, Bottom: 6, Left: 12}
		itemWidgets = append(itemWidgets, btn)
	}

	content := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 2},
		itemWidgets...,
	)
	content.Style().Background = qui.Color{R: 0.17, G: 0.17, B: 0.19, A: 1}
	content.Style().Radius = 6
	content.Style().Padding = qui.Insets{Top: 4, Right: 4, Bottom: 4, Left: 4}

	popup = widgets.NewPopup(content)
	popup.OnClose = func() {
		log.Println("menu dismissed")
	}
	popup.ShowAt(window, x, y)
	log.Printf("menu opened at (%.0f, %.0f)", x, y)
}
