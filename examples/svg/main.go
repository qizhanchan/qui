// SVG demo. Two panels:
//
//  1. A row of five Buttons whose IconSize ramps from 16 → 128 logical
//     pixels. Each rasterizes the same SVG fresh at its own size — the
//     ramp makes it obvious that DrawVector stays sharp at every scale
//     (unlike a single bitmap upscaled bilinearly).
//
//  2. A canvas widget that paints a centered SVG, plus a slider beneath
//     it. The slider's value (32 → 512 px) is the live edge length;
//     dragging triggers a repaint that re-rasterizes at the new size.
package main

import (
	"embed"
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
	"github.com/qizhanchan/qui/widgets"
)

//go:embed assets/*.svg
var assets embed.FS

// loadIcon panics on missing/malformed assets — the embed FS is built
// from a fixed file list in the same directory, so any failure here is
// a programming error and we'd rather surface it at startup.
func loadIcon(name string) *svg.Icon {
	data, err := assets.ReadFile("assets/" + name + ".svg")
	if err != nil {
		log.Fatalf("missing svg asset %q: %v", name, err)
	}
	return svg.MustParseBytes(data)
}

// svgViewer is a minimal widget that paints a single SVG centered in
// its bounds at the size dictated by a shared *float32. We can't reuse
// widgets.Button for this because the goal is to show *just* the icon
// scaling cleanly — button chrome would distract.
type svgViewer struct {
	qui.BaseWidget
	icon *svg.Icon
	size *float32
	tint qui.Color
}

func (v *svgViewer) Measure(qui.Size) qui.Size { return qui.Size{W: 560, H: 560} }

func (v *svgViewer) Draw(canvas qui.Canvas) {
	b := v.Bounds()
	canvas.FillRoundedRect(b, 8, qui.Color{R: 0.08, G: 0.08, B: 0.10, A: 1})

	sz := *v.size
	if sz < 1 {
		sz = 1
	}
	rect := qui.Rect{
		X: b.X + (b.W-sz)/2,
		Y: b.Y + (b.H-sz)/2,
		W: sz,
		H: sz,
	}
	qui.DrawVector(canvas, v.icon, rect, v.tint)
}

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("Qui — SVG", 900, 900)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	tint := qui.Color{R: 0.87, G: 0.89, B: 0.94, A: 1}

	// --- Panel 1: five Buttons at increasing icon sizes ----------------
	iconNames := []string{"bold", "italic", "font", "fill-drip", "calculator"}
	sizes := []float32{16, 32, 64, 96, 128}
	buttonRow := qui.NewContainer(qui.FlexLayout{
		Direction:  qui.Horizontal,
		Gap:        16,
		AlignItems: qui.AlignCenter,
		Justify:    qui.JustifyStart,
	})
	for i, name := range iconNames {
		icon := loadIcon(name)
		sz := sizes[i]
		btn := widgets.NewButton("", func() { log.Printf("button %s (%.0fpx) clicked", name, sz) })
		btn.IconVector = icon
		btn.IconTint = tint
		btn.IconSize = sz
		btn.Style().Background = qui.Color{R: 0.16, G: 0.17, B: 0.20, A: 1}
		btn.Style().Border = qui.Color{R: 0.30, G: 0.32, B: 0.36, A: 1}
		btn.Style().BorderSize = 1
		btn.Style().Radius = 6
		btn.Style().Padding = qui.Insets{Left: 10, Right: 10, Top: 10, Bottom: 10}
		buttonRow.AddChild(btn)
	}
	buttonRow.Style().Padding = qui.Insets{Left: 16, Right: 16, Top: 16, Bottom: 16}
	buttonRow.Style().Background = qui.Color{R: 0.10, G: 0.10, B: 0.12, A: 1}

	header := widgets.NewLabel("1) Five buttons with the same SVG icon rendered at 16 / 32 / 64 / 96 / 128 px — each one rasterizes at its own size, so every glyph stays crisp.")
	header.Style().Foreground = tint
	header.Style().Font.Size = 13
	header.Style().Padding = qui.Insets{Left: 16, Right: 16, Top: 0, Bottom: 0}

	// --- Panel 2: slider-controlled SVG canvas ------------------------
	viewerSize := float32(160)
	viewerIcon := loadIcon("fill-drip")
	viewer := &svgViewer{icon: viewerIcon, size: &viewerSize, tint: tint}
	viewer.SetSelf(viewer)
	viewer.SetFlex(1)

	sizeLabel := widgets.NewLabel(fmt.Sprintf("Size: %.0f px", viewerSize))
	sizeLabel.Style().Foreground = tint
	sizeLabel.Style().Font.Size = 13

	slider := widgets.NewSlider(32, 512, viewerSize, func(v float32) {
		viewerSize = v
		sizeLabel.SetText(fmt.Sprintf("Size: %.0f px", v))
	})
	slider.SetFlex(1)

	sliderRow := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Horizontal, Gap: 12, AlignItems: qui.AlignCenter},
		slider, sizeLabel,
	)
	sliderRow.Style().Padding = qui.Insets{Left: 16, Right: 16, Top: 12, Bottom: 12}

	panel2Header := widgets.NewLabel("2) One SVG, live-resized via the slider. Each frame asks svg.Icon to rasterize at the current size — pull the handle and watch the edges stay clean.")
	panel2Header.Style().Foreground = tint
	panel2Header.Style().Font.Size = 13
	panel2Header.Style().Padding = qui.Insets{Left: 16, Right: 16, Top: 0, Bottom: 0}

	viewerPanel := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 0, AlignItems: qui.AlignStretch},
		viewer, sliderRow,
	)
	viewer.SetFlex(1)

	// --- Root --------------------------------------------------------
	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16, AlignItems: qui.AlignStretch},
		header, buttonRow, panel2Header, viewerPanel,
	)
	root.Style().Background = qui.Color{R: 0.07, G: 0.07, B: 0.08, A: 1}
	root.Style().Padding = qui.Insets{Top: 16, Bottom: 16}
	viewerPanel.SetFlex(1)

	window.SetRoot(root)
	app.Run()
}
