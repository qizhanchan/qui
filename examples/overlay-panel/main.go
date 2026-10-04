// overlay-panel demonstrates qui.WindowOverlayPanel: a borderless,
// transparent, always-on-top window that does NOT take keyboard focus.
//
// This is the shape an input method's candidate bar needs, and the point of
// the demo is to make the three properties that matter visible/verifiable
// by hand, because none of them can be asserted from inside the process:
//
//  1. Click a chip in the panel while the caret is blinking in ANOTHER app
//     (TextEdit, Terminal). The chip reacts, and the caret keeps blinking —
//     focus never moved. A normal window would steal it.
//  2. Switch to another app, or take it fullscreen. The panel stays on top,
//     on every Space.
//  3. The panel's corners are rounded and the area outside the card is the
//     desktop, not black — the framebuffer's alpha is honored.
//
// Requires the cocoa backend; GLFW cannot make a non-activating window:
//
//	QUI_PLATFORM=cocoa go run ./examples/overlay-panel
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/widgets"
)

const (
	panelW = 420
	panelH = 96
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	main1, err := app.NewWindow("qui — overlay panel demo", 640, 380)
	if err != nil {
		log.Fatal(err)
	}
	main1.SetRenderer(qui.NewGLRenderer())

	panel, err := app.NewOverlayPanel(panelW, panelH)
	if err != nil {
		if errors.Is(err, qui.ErrOverlayPanelUnsupported) {
			log.Fatal("overlay panels need the cocoa backend:\n" +
				"    QUI_PLATFORM=cocoa go run ./examples/overlay-panel")
		}
		log.Fatal(err)
	}
	panel.SetRenderer(qui.NewGLRenderer())

	status := widgets.NewLabel("panel hidden")
	panel.SetRoot(buildCandidateBar(func(word string) {
		// Runs while another app is still the active one — that is the
		// whole point. Reflected in the main window so the effect is
		// visible without switching back.
		status.SetText("picked: " + word + "  (focus never left the other app)")
		main1.Invalidate()
	}))

	main1.SetRoot(buildControls(app, main1, panel, status))

	// Lets the panel be driven and screenshotted from outside, which is the
	// only way to check "did it actually composite transparently" without
	// trusting a pair of eyes. No-op unless QUI_AGENT is set.
	if srv, err := agent.BindEnv(main1); err == nil && srv != nil {
		main1.OnClose(func() { _ = srv.Stop() })
	}

	app.Run()
}

func buildControls(app *qui.App, main1, panel *qui.Window, status *widgets.Label) qui.Widget {
	title := widgets.NewLabel("qui.WindowOverlayPanel")
	title.Style().Font.Size = 20
	title.Style().Foreground = qui.ColorWhite

	help := widgets.NewLabel(
		"1. Show the panel.\n" +
			"2. Click into TextEdit or Terminal so a caret is blinking there.\n" +
			"3. Click a chip in the floating panel.\n" +
			"   The chip reacts and the caret keeps blinking — focus never moved.")
	help.Style().Foreground = qui.Color{R: 0.75, G: 0.78, B: 0.82, A: 1}
	help.Style().Font.Size = 13

	status.Style().Foreground = qui.Color{R: 0.55, G: 0.85, B: 0.65, A: 1}
	status.Style().Font.Size = 14

	show := widgets.NewButton("Show panel below this window", func() {
		origin, ok := main1.PositionOK()
		if !ok {
			// Wayland-style backends have no absolute coordinates; show
			// wherever the compositor puts it rather than doing nothing.
			panel.Show()
		} else {
			mainW := int(main1.WindowSize().W)
			panel.ShowAt(int(origin.X)+(mainW-panelW)/2, int(origin.Y)+420)
		}
		status.SetText("panel visible — now click into another app")
		main1.Invalidate()
	})

	hide := widgets.NewButton("Hide panel", func() {
		panel.Hide()
		status.SetText("panel hidden")
		main1.Invalidate()
	})

	probe := widgets.NewButton("Report panel state", func() {
		status.SetText(fmt.Sprintf("kind=%v visible=%v", panel.Kind(), panel.IsVisible()))
		main1.Invalidate()
	})

	row := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8}, show, hide, probe)

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 14},
		title, help, row, status,
	)
	root.Style().Background = qui.Color{R: 0.11, G: 0.13, B: 0.15, A: 1}
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	return root
}

// buildCandidateBar is a stand-in for an IME candidate bar: a rounded,
// semi-transparent card with clickable chips. Nothing here is IME-specific —
// it exists to prove that a panel can be seen through, drawn round, and
// clicked without activating the process.
func buildCandidateBar(onPick func(string)) qui.Widget {
	preedit := widgets.NewLabel("nihao")
	preedit.Style().Foreground = qui.Color{R: 0.62, G: 0.68, B: 0.76, A: 1}
	preedit.Style().Font.Size = 13

	chips := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 6})
	for i, word := range []string{"你好", "拟好", "泥壕", "尼豪"} {
		word := word
		label := fmt.Sprintf("%d %s", i+1, word)
		chip := widgets.NewButton(label, func() { onPick(word) })
		chips.AddChild(chip)
	}

	card := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 6},
		preedit, chips,
	)
	// Alpha below 1 is the interesting part: on an overlay panel this
	// composites against the desktop, not against a window background.
	card.Style().Background = qui.Color{R: 0.10, G: 0.11, B: 0.13, A: 0.92}
	card.Style().Radius = 12
	card.Style().Border = qui.Color{R: 1, G: 1, B: 1, A: 0.14}
	card.Style().BorderSize = 1
	card.Style().Padding = qui.Insets{Top: 10, Right: 12, Bottom: 10, Left: 12}

	// The root is inset from the window edge so the card's rounded corners
	// and its shadow have transparent pixels to sit in. A card filling the
	// window edge-to-edge would have nothing to show the rounding against.
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, card)
	root.Style().Background = qui.ColorTransparent
	root.Style().Padding = qui.Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}
	return root
}
