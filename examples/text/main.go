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

	window, err := app.NewWindow("Qui Text Layout Demo", 980, 680)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	window.SetRoot(buildRoot())
	app.Run()
}

const sample = "Qui text layout now supports wrapped lines, alignment, line-height scaling, and ellipsis. Drag across any card to select text, then press Cmd/Ctrl+C to copy. Selection spans cards, just like a browser."

// buildRoot lays out six cards in a 2-column, 3-row grid. Every body is a
// widgets.RichText, so text selection (drag + Cmd/Ctrl+C, across cards) and
// hyperlink clicks work with no selection code in this file.
func buildRoot() qui.Widget {
	blue := qui.Color{R: 0.18, G: 0.36, B: 0.84, A: 1}
	green := qui.Color{R: 0.10, G: 0.58, B: 0.38, A: 1}
	bold := qui.Font{Size: 18, Bold: true}
	small := qui.Font{Size: 13}

	cards := []qui.Widget{
		card("Wrap + Start Align",
			widgets.NewRichText(qui.TextSpan{Text: sample}).
				Wrap(true).Align(qui.TextAlignStart)),

		card("Center Align",
			widgets.NewRichText(qui.TextSpan{Text: sample}).
				Wrap(true).Align(qui.TextAlignCenter)),

		card("End Align + Tight Height",
			widgets.NewRichText(qui.TextSpan{Text: sample}).
				Wrap(true).Align(qui.TextAlignEnd).LineHeight(0.9)),

		card("MaxLines + Ellipsis",
			widgets.NewRichText(qui.TextSpan{Text: sample}).
				Wrap(true).LineHeight(1.3).MaxLines(3, true)),

		card("Selectable Everywhere",
			widgets.NewRichText(qui.TextSpan{Text: "Each card body is a widgets.RichText. Drag to select; the highlight and clipboard are handled by the framework."}).
				Wrap(true).LineHeight(1.15)),

		card("Rich Text + Link",
			widgets.NewRichText(
				qui.TextSpan{Text: "Rich "},
				qui.TextSpan{Text: "Text", Font: &bold, Color: &blue},
				qui.TextSpan{Text: " mixes styles in one paragraph.\n"},
				qui.TextSpan{Text: "Span-level font + color + paragraph layout.", Font: &small, Color: &green},
				qui.TextSpan{Text: "\nOpen the "},
				qui.TextSpan{Text: "example.com link", Color: &blue, Href: "https://example.com/q"},
				qui.TextSpan{Text: " to launch it in your browser."},
			).Wrap(true).LineHeight(1.1)),
	}

	// Three rows of two cards; rows and cards share space equally.
	var rows []qui.Widget
	for r := 0; r < 3; r++ {
		row := qui.NewContainer(
			qui.FlexLayout{Direction: qui.Horizontal, Gap: 16},
			cards[r*2], cards[r*2+1],
		)
		row.SetFlex(1)
		rows = append(rows, row)
	}

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16},
		rows...,
	)
	root.Style().Background = qui.Color{R: 0.96, G: 0.97, B: 0.99, A: 1}
	root.Style().Padding = qui.Insets{Top: 24, Right: 24, Bottom: 24, Left: 24}
	return root
}

// card wraps a body widget in a titled frame that grows to fill its flex
// slot.
func card(title string, body *widgets.RichText) qui.Widget {
	body.SetFlex(1)
	g := widgets.NewFieldSet(title, qui.FlexLayout{Direction: qui.Vertical}, body)
	g.SetFlex(1)
	return g
}
