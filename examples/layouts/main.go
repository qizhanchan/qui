// layouts example — showcases the three layout engines behind one
// TabView. Switch tabs to see FlexLayout / GridLayout / AbsoluteLayout
// each arrange the same-shape widgets differently.
//
// What to try:
//   - Resize the window: every tab's layout reacts differently
//     (Flex items grow, Grid fractions redistribute, Absolute anchors
//     keep their edge distances).
//   - Tab key cycles through focusable widgets in the visible tab.
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

	window, err := app.NewWindow("Qui Layouts Demo", 900, 640)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	tabs := widgets.NewTabView(
		widgets.Tab{Title: "Flex", Content: makeFlexDemo()},
		widgets.Tab{Title: "Grid", Content: makeGridDemo()},
		widgets.Tab{Title: "Absolute", Content: makeAbsoluteDemo(window)},
	)
	tabs.SetFlex(1)
	tabs.OnSelect = func(i int) {
		log.Printf("switched to tab %d", i)
	}

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, tabs)
	root.Style().Background = qui.Color{R: 0.08, G: 0.08, B: 0.1, A: 1}

	window.SetRoot(root)
	app.Run()
}

// box builds a labeled colored rectangle — visually distinct "demo
// widget" that has natural size from its text + padding, plus a
// Measure override via padding math so Flex grow/shrink is observable.
func box(text string, bg qui.Color) *widgets.Label {
	l := widgets.NewLabel(text)
	l.Style().Background = bg
	l.Style().Foreground = qui.ColorWhite
	l.Style().Font = qui.Font{Size: 13}
	l.Style().Padding = qui.Insets{Top: 12, Right: 16, Bottom: 12, Left: 16}
	l.Style().Radius = 6
	return l
}

var (
	paletteBlue   = qui.Color{R: 0.25, G: 0.52, B: 1.0, A: 1}
	paletteTeal   = qui.Color{R: 0.25, G: 0.75, B: 0.75, A: 1}
	paletteOrange = qui.Color{R: 0.95, G: 0.55, B: 0.25, A: 1}
	paletteViolet = qui.Color{R: 0.6, G: 0.4, B: 0.9, A: 1}
	paletteGray   = qui.Color{R: 0.3, G: 0.3, B: 0.35, A: 1}
)

// ----------------------------------------------------------------------
// Flex demo — three sections stacked vertically, each illustrating a
// different Flex feature.

func makeFlexDemo() qui.Widget {
	// Section 1: JustifySpaceBetween — one title left, two buttons right.
	title := box("Title", paletteGray)
	cancelBtn := widgets.NewButton("Cancel", func() { log.Println("cancel") })
	saveBtn := widgets.NewButton("Save", func() { log.Println("save") })
	for _, b := range []*widgets.Button{cancelBtn, saveBtn} {
		b.Style().Background = paletteBlue
		b.Style().Foreground = qui.ColorWhite
		b.Style().Radius = 4
	}
	toolbar := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 8, Justify: qui.JustifySpaceBetween, AlignItems: qui.AlignCenter},
		title, qui.NewContainer(
			qui.FlexLayout{Direction: qui.Horizontal, Gap: 8},
			cancelBtn, saveBtn,
		),
	)
	toolbar.Style().Background = qui.Color{R: 0.14, G: 0.14, B: 0.17, A: 1}
	toolbar.Style().Padding = qui.Insets{Top: 10, Right: 12, Bottom: 10, Left: 12}
	toolbar.Style().Radius = 6

	// Section 2: sidebar + body (Grow=1), showing main-axis absorption.
	sidebar := box("Sidebar\n(natural)", paletteViolet)
	body := box("Body — Grow: 1 — resizes with window", paletteTeal)
	body.SetFlex(1)
	splitRow := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 12, AlignItems: qui.AlignStretch},
		sidebar, body,
	)

	// Section 3: AlignItems + proportional Grow demo. 3 items with
	// Grow 1/2/1 ratio, centered vertically.
	a := box("Grow 1", paletteBlue)
	b := box("Grow 2", paletteOrange)
	c := box("Grow 1", paletteBlue)
	a.SetFlex(1)
	b.SetFlex(2)
	c.SetFlex(1)
	proportions := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 8, AlignItems: qui.AlignCenter},
		a, b, c,
	)
	proportions.Style().Background = qui.Color{R: 0.13, G: 0.13, B: 0.16, A: 1}
	proportions.Style().Padding = qui.Insets{Top: 14, Right: 14, Bottom: 14, Left: 14}
	proportions.Style().Radius = 6

	caption1 := widgets.NewLabel("JustifySpaceBetween: first → start, last → end")
	caption2 := widgets.NewLabel("Sidebar natural width, Body Grow=1 fills rest")
	caption3 := widgets.NewLabel("Grow weights 1 : 2 : 1 proportionally split extra space")
	for _, l := range []*widgets.Label{caption1, caption2, caption3} {
		l.Style().Foreground = qui.Color{R: 0.6, G: 0.6, B: 0.65, A: 1}
		l.Style().Font = qui.Font{Size: 12}
	}
	toolbar.SetFlex(0)
	splitRow.SetFlex(1) // this row takes leftover vertical space

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 8},
		caption1, toolbar,
		caption2, splitRow,
		caption3, proportions,
	)
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	return root
}

// ----------------------------------------------------------------------
// Grid demo — a dashboard-style layout with fixed / fractional / auto
// tracks, plus a header that spans all columns.

func makeGridDemo() qui.Widget {
	header := box("Header — col span 3 (Fixed 40)", paletteBlue)
	header.SetGridItem(qui.GridItem{Col: 0, Row: 0, ColSpan: 3, Explicit: true})

	sidebar := box("Sidebar\n(Fixed 180, row span 2)", paletteViolet)
	sidebar.SetGridItem(qui.GridItem{Col: 0, Row: 1, RowSpan: 2, Explicit: true})

	cellA := box("1fr", paletteTeal)
	cellA.SetGridCell(1, 1)
	cellB := box("2fr", paletteOrange)
	cellB.SetGridCell(2, 1)

	footer := box("Content footer (col 1-2)", paletteGray)
	footer.SetGridItem(qui.GridItem{Col: 1, Row: 2, ColSpan: 2, Explicit: true})

	grid := qui.NewContainer(
		qui.GridLayout{
			Columns: []qui.GridTrack{
				qui.GridFixedTrack(180),  // sidebar fixed
				qui.GridFractionTrack(1), // content narrow
				qui.GridFractionTrack(2), // content wide
			},
			Rows: []qui.GridTrack{
				qui.GridFixedTrack(56),   // header
				qui.GridFractionTrack(3), // main content row
				qui.GridFixedTrack(50),   // footer row
			},
			ColGap: 10,
			RowGap: 10,
		},
		header, sidebar, cellA, cellB, footer,
	)
	grid.Style().Background = qui.Color{R: 0.11, G: 0.11, B: 0.14, A: 1}
	grid.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	grid.SetFlex(1) // fill remaining vertical space

	caption := widgets.NewLabel(
		"Columns: [Fixed 180px | 1fr | 2fr]   Rows: [Fixed 56 | 3fr | Fixed 50]\n" +
			"Header spans all 3 cols; sidebar spans 2 rows. Resize the window to see fractions redistribute.",
	)
	caption.Style().Foreground = qui.Color{R: 0.6, G: 0.6, B: 0.65, A: 1}
	caption.Style().Font = qui.Font{Size: 12}

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		caption, grid,
	)
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	return root
}

// ----------------------------------------------------------------------
// Absolute demo — anchor-based positioning. Needs the Window so we can
// wire a button that shows a Popup anchored near one of the widgets.

func makeAbsoluteDemo(window *qui.Window) qui.Widget {
	// Sidebar pinned left, full height, fixed width.
	sidebar := box("Sidebar\nLeft+Top+Bottom\nWidth 220", paletteViolet)
	sidebar.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop | qui.AnchorBottom,
		Width:  220,
	})

	// Content area: anchored all four sides, offset by sidebar width.
	content := box("Content\nAll four edges anchored\nStretches with window", paletteTeal)
	content.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop | qui.AnchorRight | qui.AnchorBottom,
		Left:   230,
		Bottom: 60,
	})

	// Badge anchored top-right, absolute 40px tall, 80px wide.
	badge := box("Top-right\nbadge", paletteOrange)
	badge.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorTop | qui.AnchorRight,
		Top:    12,
		Right:  12,
		Width:  100,
		Height: 50,
	})

	// Floating action button bottom-right, kept above the status bar.
	fab := widgets.NewButton("+", func() {
		popup := widgets.NewPopup(box("Anchored popup!\nClick outside to dismiss", paletteBlue))
		popup.ShowAt(window, 600, 400)
	})
	fab.Style().Background = paletteBlue
	fab.Style().Foreground = qui.ColorWhite
	fab.Style().Font = qui.Font{Size: 22, Bold: true}
	fab.Style().Padding = qui.Insets{}
	fab.Style().Radius = 22
	fab.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorRight | qui.AnchorBottom,
		Right:  20,
		Bottom: 70,
		Width:  44,
		Height: 44,
	})

	// Bottom status bar pinned to bottom, full width minus sidebar.
	statusBar := box("Status — anchored Left+Right+Bottom", paletteGray)
	statusBar.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorRight | qui.AnchorBottom,
		Left:   230,
		Height: 48,
	})

	canvas := qui.NewContainer(qui.AbsoluteLayout{},
		sidebar, content, statusBar, badge, fab,
	)
	canvas.Style().Background = qui.Color{R: 0.11, G: 0.11, B: 0.14, A: 1}
	canvas.SetFlex(1) // fill remaining vertical space

	caption := widgets.NewLabel(
		"Sidebar: Left+Top+Bottom, Width=220.   Content: all 4 sides, Left=230, Bottom=60.\n" +
			"Badge: Top+Right, fixed size.   +: Right+Bottom corner.   StatusBar: Left+Right+Bottom.",
	)
	caption.Style().Foreground = qui.Color{R: 0.6, G: 0.6, B: 0.65, A: 1}
	caption.Style().Font = qui.Font{Size: 12}

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		caption, canvas,
	)
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	return root
}
