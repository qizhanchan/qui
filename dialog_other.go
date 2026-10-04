//go:build !darwin || !cgo

package qui

// Non-darwin / no-cgo fallback. Windows and Linux backends would go
// here — Win32 GetOpenFileName + IFileDialog, or GTK's GtkFileChooser
// (or a portal XDG_DESKTOP_PORTAL bridge for sandboxed Linux).

// OpenFile returns ErrDialogNotSupported on this platform.
func OpenFile(opts OpenFileOptions) (string, error) {
	_ = opts
	return "", ErrDialogNotSupported
}

// OpenFiles returns ErrDialogNotSupported on this platform.
func OpenFiles(opts OpenFileOptions) ([]string, error) {
	_ = opts
	return nil, ErrDialogNotSupported
}

// OpenDirectory returns ErrDialogNotSupported on this platform.
func OpenDirectory(opts OpenDirectoryOptions) (string, error) {
	_ = opts
	return "", ErrDialogNotSupported
}

// SaveFile returns ErrDialogNotSupported on this platform. On
// supported platforms it returns the user-chosen path without
// creating the file — see dialog_darwin.go.
func SaveFile(opts SaveFileOptions) (string, error) {
	_ = opts
	return "", ErrDialogNotSupported
}

// ShowAlert returns ErrDialogNotSupported on this platform.
func ShowAlert(opts AlertOptions) (int, error) {
	_ = opts
	return 0, ErrDialogNotSupported
}
