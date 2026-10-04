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

	window, err := app.NewWindow("TextArea Demo", 400, 520)
	if err != nil {
		log.Fatal(err)
	}
	// Keep the demo usable on resize; below this size controls become
	// too cramped to interact with.
	window.SetMinSize(400, 360)

	window.SetRenderer(qui.NewGLRenderer())

	// Create a title label
	title := widgets.NewLabel("TextArea Component Demo")
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 24

	// Create a TextArea with placeholder
	textarea := widgets.NewTextArea("Enter your text here...")
	// textarea.ShowScrollBarY = false
	// textarea.AutoSizeHeight = true
	textarea.OnChange = func(text string) {
		log.Printf("Text changed: %d characters\n", len(text))
	}
	// Grow=1 makes the textarea absorb remaining vertical space — the
	// other rows (title, instructions, buttons) keep their natural size.
	textarea.SetFlex(1)

	// Create an instruction label
	instructions := widgets.NewLabel("Click to focus, type to edit\nUse arrow keys to navigate\nPress Enter for new line")
	instructions.Style().Foreground = qui.Color{R: 0.7, G: 0.7, B: 0.7, A: 1}
	instructions.Style().Font.Size = 14

	// Create a button to get text
	button := widgets.NewButton("Get Text", func() {
		log.Printf("Current text:\n%s\n", textarea.GetText())
	})

	// Create a button to set text
	setButton := widgets.NewButton("Set Sample Text", func() {
		textarea.SetText("Hello World!\nThis is a multi-line\ntextarea component.\n\nYou can edit this text.")
		log.Println("Sample text set")
	})

	// Create a button to clear text
	clearButton := widgets.NewButton("Clear", func() {
		textarea.SetText("")
		log.Println("Text cleared")
	})

	// Create button container
	buttonContainer := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8}, button, setButton, clearButton)

	// Create content container with all widgets
	content := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 16},
		title,
		textarea,
		instructions,
		buttonContainer,
	)
	content.Style().Background = qui.Color{R: 0.12, G: 0.12, B: 0.12, A: 1}
	content.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}

	// Put the content in a scroll view so short windows stay usable.
	scroll := widgets.NewScrollView()
	scroll.Style().Background = qui.Color{R: 0.12, G: 0.12, B: 0.12, A: 1}
	scroll.SetContent(content, qui.Size{W: 560, H: 560})

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, scroll)
	root.Style().Background = qui.Color{R: 0.12, G: 0.12, B: 0.12, A: 1}

	window.SetRoot(root)

	window.SetCustomRender(func(canvas qui.Canvas) {
		// Custom rendering can be done here if needed
		_ = canvas
	})

	app.Run()
}
