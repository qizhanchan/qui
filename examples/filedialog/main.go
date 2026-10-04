// filedialog example — exercise the native OS dialogs.
//
// Run and click each button. On macOS you get real NSOpenPanel /
// NSSavePanel / NSAlert panels driven via Cgo. On other platforms the
// buttons log ErrDialogNotSupported until a Win32 / GTK backend lands.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	window, err := app.NewWindow("Native Dialogs Demo", 640, 360)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	status := widgets.NewLabel("Click a button to show an OS dialog.")
	status.Style().Foreground = qui.CurrentTheme().Text

	setStatus := func(s string) {
		status.SetText(s)
	}

	home, _ := os.UserHomeDir()

	openBtn := widgets.NewButton("Open file…", func() {
		path, err := qui.OpenFile(qui.OpenFileOptions{
			Title:             "Pick an image",
			Message:           "PNG or JPG only",
			StartDirectory:    home,
			AllowedExtensions: []string{"png", "jpg", "jpeg"},
		})
		reportPath(setStatus, "OpenFile", path, err)
	})

	openMultiBtn := widgets.NewButton("Open multiple…", func() {
		paths, err := qui.OpenFiles(qui.OpenFileOptions{
			Title:          "Pick several",
			StartDirectory: home,
		})
		if err != nil {
			log.Printf("OpenFiles error: %v", err)
			setStatus(fmt.Sprintf("OpenFiles error: %v", err))
			return
		}
		log.Printf("OpenFiles: %v", paths)
		switch len(paths) {
		case 0:
			setStatus("OpenFiles: canceled")
		case 1:
			setStatus("OpenFiles: " + paths[0])
		default:
			setStatus(fmt.Sprintf("OpenFiles: %s (+%d more)",
				filepath.Base(paths[0]), len(paths)-1))
		}
	})

	openDirBtn := widgets.NewButton("Open directory…", func() {
		path, err := qui.OpenDirectory(qui.OpenDirectoryOptions{
			Title:          "Pick a folder",
			StartDirectory: home,
		})
		reportPath(setStatus, "OpenDirectory", path, err)
	})

	saveBtn := widgets.NewButton("Save file…", func() {
		path, err := qui.SaveFile(qui.SaveFileOptions{
			Title:             "Save as",
			StartDirectory:    home,
			Filename:          "Untitled.txt",
			AllowedExtensions: []string{"txt"},
		})
		if err != nil {
			log.Printf("SaveFile error: %v", err)
			setStatus(fmt.Sprintf("SaveFile error: %v", err))
			return
		}
		if path == "" {
			setStatus("SaveFile: canceled")
			return
		}
		// NSSavePanel returns the path but doesn't create the file —
		// persist some placeholder bytes so the demo produces a real
		// artifact on disk that the user can inspect.
		payload := []byte("Hello from qui!\n")
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			log.Printf("write %s: %v", path, err)
			setStatus(fmt.Sprintf("SaveFile: wrote to %s failed: %v", path, err))
			return
		}
		log.Printf("SaveFile: wrote %d bytes to %s", len(payload), path)
		setStatus(fmt.Sprintf("SaveFile: wrote %d bytes to %s", len(payload), path))
	})

	alertBtn := widgets.NewButton("Show alert (info)", func() {
		idx, err := qui.ShowAlert(qui.AlertOptions{
			Title:   "Heads up",
			Message: "This is an informational alert fired from Go via Cgo → NSAlert.",
			Style:   qui.AlertInfo,
			Buttons: []string{"OK", "Cancel"},
		})
		if err != nil {
			log.Printf("ShowAlert error: %v", err)
			setStatus(fmt.Sprintf("ShowAlert error: %v", err))
			return
		}
		log.Printf("alert clicked index=%d", idx)
		setStatus(fmt.Sprintf("alert clicked: index=%d", idx))
	})

	warnBtn := widgets.NewButton("Show alert (critical)", func() {
		idx, _ := qui.ShowAlert(qui.AlertOptions{
			Title:   "Delete this?",
			Message: "This can't be undone.",
			Style:   qui.AlertCritical,
			Buttons: []string{"Delete", "Keep"},
		})
		log.Printf("critical alert index=%d", idx)
		setStatus(fmt.Sprintf("critical alert: index=%d", idx))
	})

	row1 := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8},
		openBtn, openMultiBtn, openDirBtn)
	row2 := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8}, saveBtn)
	row3 := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8},
		alertBtn, warnBtn)

	root := qui.NewContainer(
		qui.FlexLayout{Direction: qui.Vertical, Gap: 12},
		widgets.NewLabel("Native OS Dialogs"),
		row1, row2, row3,
		status,
	)
	root.Style().Padding = qui.Insets{Top: 20, Right: 20, Bottom: 20, Left: 20}
	root.Style().Background = qui.CurrentTheme().Surface

	window.SetRoot(root)
	app.Run()
}

func reportPath(setStatus func(string), fn, path string, err error) {
	if err != nil {
		log.Printf("%s error: %v", fn, err)
		setStatus(fmt.Sprintf("%s error: %v", fn, err))
		return
	}
	if path == "" {
		log.Printf("%s canceled", fn)
		setStatus(fn + ": canceled")
		return
	}
	log.Printf("%s: %s", fn, path)
	setStatus(fn + ": " + path)
}
