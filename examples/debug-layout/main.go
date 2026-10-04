// debug-layout example — demonstrates Qui's layout diagnostics and
// runtime debug overlay on an intentionally broken layout.
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

	window, err := app.NewWindow("Qui Debug Layout Demo", 920, 620)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	root := makeBrokenLayout()
	window.SetRoot(root)

	// Prime layout once so diagnostics can be printed before the first
	// visible frame. Window.Step will lay out again in the normal loop.
	size := window.Size()
	root.Measure(size)
	root.Layout(qui.Rect{W: size.W, H: size.H})
	for _, d := range window.DebugLayoutDiagnostics() {
		log.Printf("[%s] %s %s: %s; hint: %s",
			d.Severity, d.WidgetType, d.Path, d.Message, d.Hint)
	}

	window.EnableDebugOverlay(qui.DebugOverlayOptions{
		ShowBounds:      true,
		ShowOverflow:    true,
		ShowLabels:      true,
		ShowDirtyRegion: true,
	})

	app.Run()
}

func makeBrokenLayout() qui.Widget {
	title := label("Debug Layout Diagnostics", 22, qui.ColorWhite)
	caption := label(
		"This example intentionally leaves the large AbsoluteLayout child with Grow=1 and Shrink=0.",
		13,
		qui.Color{R: 0.72, G: 0.72, B: 0.78, A: 1},
	)

	sidebar := panel("Sidebar\nLeft+Top+Bottom", qui.Color{R: 0.56, G: 0.36, B: 0.88, A: 1})
	sidebar.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop | qui.AnchorBottom,
		Width:  220,
	})

	content := panel("Content\nAll sides anchored", qui.Color{R: 0.22, G: 0.72, B: 0.72, A: 1})
	content.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorTop | qui.AnchorRight | qui.AnchorBottom,
		Left:   232,
		Bottom: 64,
	})

	status := panel("Status anchored Left+Right+Bottom", qui.Color{R: 0.32, G: 0.32, B: 0.38, A: 1})
	status.SetAbsolutePosition(qui.AbsolutePosition{
		Anchor: qui.AnchorLeft | qui.AnchorRight | qui.AnchorBottom,
		Left:   232,
		Height: 52,
	})

	canvas := qui.NewContainer(qui.AbsoluteLayout{}, sidebar, content, status)
	canvas.Style().Background = qui.Color{R: 0.1, G: 0.1, B: 0.13, A: 1}
	canvas.SetFlex(1)
	// Deliberately omitted:
	// canvas.UpdateFlexItem(func(item *qui.FlexItem) { item.Shrink = 1 })
	//
	// The diagnostics should report that this grow child has a full-size
	// measured basis and no shrink, then flag the resulting overflow.

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		title,
		caption,
		canvas,
	)
	root.Style().Background = qui.Color{R: 0.07, G: 0.07, B: 0.09, A: 1}
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	return root
}

func label(text string, size float32, color qui.Color) *widgets.Label {
	l := widgets.NewLabel(text)
	l.Style().Foreground = color
	l.Style().Font = qui.Font{Size: size}
	return l
}

func panel(text string, bg qui.Color) *widgets.Label {
	l := label(text, 15, qui.ColorWhite)
	l.Style().Background = bg
	l.Style().Padding = qui.Insets{Top: 16, Right: 18, Bottom: 16, Left: 18}
	l.Style().Radius = 6
	return l
}
