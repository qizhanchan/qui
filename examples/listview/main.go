// listview example — virtualizing flat list.
//
// What to try:
//   - Click a row to select it; selection highlights in blue.
//   - Focus the list (click inside it), then Up/Down to move the
//     selection — the view auto-scrolls to keep it visible.
//   - Mouse wheel scrolls; drag the scrollbar thumb on the right edge.
//   - Double-click a row, or press Enter on a selected one, to "activate"
//     it (logged).
//   - Use the buttons to mutate the model and observe the list react.
package main

import (
	"fmt"
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

	window, err := app.NewWindow("ListView Demo", 800, 600)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	title := widgets.NewLabel("ListView Demo — 200 virtualized rows")
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 20

	hint := widgets.NewLabel("Click to select · Up/Down to navigate · Enter to activate · wheel to scroll")
	hint.Style().Foreground = qui.Color{R: 0.7, G: 0.7, B: 0.7, A: 1}
	hint.Style().Font.Size = 13

	status := widgets.NewLabel("Selected: (none)")
	status.Style().Foreground = qui.ColorWhite
	status.Style().Font.Size = 14

	// Seed the model with 200 items so virtualization is actually
	// exercised in the default 800x600 window.
	items := make([]string, 0, 200)
	for i := 1; i <= 200; i++ {
		items = append(items, fmt.Sprintf("Item %03d", i))
	}
	model := widgets.NewListModel(items)

	list := widgets.NewListView(model)
	list.OnSelect = func(idx int) {
		status.SetText(fmt.Sprintf("Selected: [%d] %s", idx, model.RowText(idx)))
		log.Printf("selected row %d: %s", idx, model.RowText(idx))
	}
	list.OnActivate = func(idx int) {
		log.Printf("activated row %d: %s", idx, model.RowText(idx))
	}
	// Let the list absorb the remaining vertical space.
	list.SetFlex(1)
	// The list follows the theme by default; this page is dark, so give it
	// a dark palette (unset fields still come from the theme).
	list.Colors = widgets.RowColors{
		Background: qui.Color{R: 0.1, G: 0.1, B: 0.12, A: 1},
		Text:       qui.ColorWhite,
		Border:     qui.Color{R: 0.3, G: 0.3, B: 0.35, A: 1},
		Hover:      qui.Color{R: 0.18, G: 0.18, B: 0.22, A: 1},
		Scrollbar: qui.ScrollbarColors{
			Track: qui.Color{R: 0.06, G: 0.06, B: 0.08, A: 1},
			Thumb: qui.Color{R: 0.4, G: 0.4, B: 0.45, A: 1},
		},
	}

	addBtn := widgets.NewButton("Add Item", func() {
		items = append(items, fmt.Sprintf("Added %03d", len(items)+1))
		model.Items = items
		list.InvalidateLayout()
		log.Printf("row count now %d", len(items))
	})

	removeBtn := widgets.NewButton("Remove Selected", func() {
		idx := list.SelectedIdx
		if idx < 0 || idx >= len(items) {
			log.Println("nothing selected")
			return
		}
		items = append(items[:idx], items[idx+1:]...)
		model.Items = items
		// Selection is now stale — clamp by re-selecting the same idx
		// (Select clamps to the new length).
		list.SelectedIdx = -1
		list.Select(idx)
		list.InvalidateLayout()
	})

	clearBtn := widgets.NewButton("Clear", func() {
		items = items[:0]
		model.Items = items
		list.SelectedIdx = -1
		status.SetText("Selected: (none)")
		list.InvalidateLayout()
	})

	buttons := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 8},
		addBtn, removeBtn, clearBtn,
	)

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		title, hint, list, status, buttons,
	)
	root.Style().Background = qui.Color{R: 0.12, G: 0.12, B: 0.12, A: 1}
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}

	window.SetRoot(root)
	agent.BindEnv(window) // QUI_AGENT=1 → drive it with cmd/qui-agent
	app.Run()
}
