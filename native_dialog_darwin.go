//go:build darwin && cgo

package qui

import "github.com/qizhanchan/qui/internal/platform/macos"

// OpenFile shows an NSOpenPanel for selecting a single file. Returns
// "" if the user canceled.
func OpenFile(opts OpenFileOptions) (string, error) {
	paths, err := openPanelImpl(opts.Title, opts.Message, opts.StartDirectory,
		opts.AllowedExtensions, false, false)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// OpenFiles shows an NSOpenPanel with multi-selection enabled. Returns
// a nil slice on cancel.
func OpenFiles(opts OpenFileOptions) ([]string, error) {
	return openPanelImpl(opts.Title, opts.Message, opts.StartDirectory,
		opts.AllowedExtensions, true, false)
}

// OpenDirectory shows an NSOpenPanel restricted to directories.
// Returns "" on cancel.
func OpenDirectory(opts OpenDirectoryOptions) (string, error) {
	paths, err := openPanelImpl(opts.Title, opts.Message, opts.StartDirectory,
		nil, false, true)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// SaveFile shows an NSSavePanel and returns the user-chosen path,
// or "" on cancel. The panel only collects the destination — it does
// NOT create the file. The caller must os.WriteFile / os.Create the
// returned path to actually persist data. This matches every native
// save API on macOS, Windows, and Linux.
func SaveFile(opts SaveFileOptions) (string, error) {
	return macos.ShowSavePanel(opts.Title, opts.Message, opts.StartDirectory,
		opts.Filename, opts.AllowedExtensions), nil
}

// ShowAlert shows an NSAlert modally. Returns the 0-based index of the
// button the user clicked (0 = first/default).
func ShowAlert(opts AlertOptions) (int, error) {
	buttons := opts.Buttons
	if len(buttons) == 0 {
		// Translated, not hard-coded: this is a system-modal button the
		// user reads in whatever language the app is running in.
		buttons = []string{TOr("qui.ok", "OK")}
	}
	return macos.ShowAlert(opts.Title, opts.Message, int(opts.Style), buttons), nil
}

// openPanelImpl is the shared glue for the three Open variants.
func openPanelImpl(title, message, startDir string, exts []string, multi, dirsOnly bool) ([]string, error) {
	return macos.ShowOpenPanel(title, message, startDir, exts, multi, dirsOnly), nil
}
