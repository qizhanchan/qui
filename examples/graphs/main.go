// Command graphs is the interactive gallery for qui/graphs/chart. It keeps
// the examples close to the real declarative API: every page is built from an
// ordinary chart.Option and hosted in the same window.
package main

import (
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/graphs/chart"
	"github.com/qizhanchan/qui/widgets"
)

type galleryEntry struct {
	category    string
	name        string
	description string
	features    string
	build       func() *chart.Chart
}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — Native Chart Gallery", 1320, 900)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	entries := galleryEntries()
	labels := make([]string, len(entries))
	for i, entry := range entries {
		labels[i] = fmt.Sprintf("%02d  %-12s  %s", i+1, entry.category, entry.name)
	}
	nav := widgets.NewListView(widgets.NewListModel(labels))
	nav.SetFlex(1)
	nav.SetRowHeight(32)
	nav.Style().Width = 306
	nav.Style().Font.Size = 14
	nav.Colors = widgets.RowColors{
		Background:   qui.Color{R: 1, G: 1, B: 1, A: 1},
		Text:         qui.Color{R: .16, G: .20, B: .25, A: 1},
		Border:       qui.Color{R: .86, G: .89, B: .93, A: 1},
		Selected:     qui.Color{R: .90, G: .94, B: 1, A: 1},
		SelectedText: qui.Color{R: .12, G: .32, B: .72, A: 1},
		Hover:        qui.Color{R: .95, G: .97, B: .99, A: 1},
		NoStripe:     true,
	}

	title := widgets.NewLabel("Native Chart Gallery")
	title.Style().Font.Size = 28
	title.Style().Font.Weight = qui.FontWeightBold
	title.Style().Foreground = qui.Color{R: .12, G: .16, B: .22, A: 1}

	subtitle := widgets.NewLabel("20 interactive, parity-backed examples built with chart.Option")
	subtitle.Style().Font.Size = 14
	subtitle.Style().Foreground = qui.Color{R: .38, G: .44, B: .52, A: 1}

	heading := widgets.NewLabel("")
	heading.Style().Font.Size = 24
	heading.Style().Font.Weight = qui.FontWeightBold
	heading.Style().Foreground = qui.Color{R: .12, G: .16, B: .22, A: 1}

	description := widgets.NewLabel("")
	description.Paragraph.Wrap = true
	description.Style().Font.Size = 14
	description.Style().Foreground = qui.Color{R: .32, G: .38, B: .46, A: 1}

	features := widgets.NewLabel("")
	features.Paragraph.Wrap = true
	features.Style().Font.Size = 12
	features.Style().Foreground = qui.Color{R: .08, G: .40, B: .62, A: 1}

	status := widgets.NewLabel("Hover data for tooltips · wheel to zoom · drag to pan · click to select")
	status.Style().Font.Size = 12
	status.Style().Foreground = qui.Color{R: .45, G: .50, B: .58, A: 1}

	chartHost := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	chartHost.SetFlex(1)
	chartHost.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}
	chartHost.Style().Radius = 14
	chartHost.Style().BorderSize = 1
	chartHost.Style().Border = qui.Color{R: .88, G: .91, B: .95, A: 1}
	chartHost.Style().Padding = qui.Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}

	var current *chart.Chart
	selectEntry := func(index int) {
		if index < 0 || index >= len(entries) {
			return
		}
		entry := entries[index]
		heading.SetText(entry.name)
		description.SetText(entry.description)
		features.SetText("Try: " + entry.features)
		current = entry.build()
		current.SetFlex(1)
		chartHost.SetChildren(current)
		window.Invalidate()
	}
	nav.OnSelect = selectEntry

	prev := widgets.NewButton("← Previous", func() {
		index := nav.SelectedIdx - 1
		if index < 0 {
			index = len(entries) - 1
		}
		nav.Select(index)
	})
	next := widgets.NewButton("Next →", func() {
		index := nav.SelectedIdx + 1
		if index >= len(entries) {
			index = 0
		}
		nav.Select(index)
	})
	replay := widgets.NewButton("Replay animation", func() {
		if current != nil {
			current.RestartAnimation()
		}
	})
	reset := widgets.NewButton("Reset axes", func() {
		if current != nil {
			current.ResetAxes()
		}
	})

	actions := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8},
		prev, next, replay, reset,
	)

	navTitle := widgets.NewLabel("DEMOS")
	navTitle.Style().Font.Size = 12
	navTitle.Style().Font.Weight = qui.FontWeightBold
	navTitle.Style().Foreground = qui.Color{R: .42, G: .48, B: .56, A: 1}

	sidebar := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 10}, navTitle, nav)
	sidebar.Style().Width = 306
	sidebar.SetFlex(0)

	right := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 9},
		heading, description, features, chartHost, status, actions,
	)
	right.SetFlex(1)

	content := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 18}, sidebar, right)

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 14}, title, subtitle, content)
	root.SetFlex(1)
	root.Style().Background = qui.Color{R: .955, G: .968, B: .984, A: 1}
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}

	window.SetRoot(root)
	nav.Select(0)
	agent.BindEnv(window)
	app.Run()
}
