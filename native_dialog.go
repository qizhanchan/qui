package qui

import "errors"

// Native OS dialogs — file open / save / directory pick, and a simple
// alert box. These spin the OS's own modal panels (NSOpenPanel /
// NSSavePanel / NSAlert on macOS), not the in-window Dialog widget.
//
// Call from any goroutine; on macOS the Cgo bridge dispatches to the
// main thread internally because the OS requires panel.runModal there.
// These functions BLOCK until the user dismisses the panel.
//
// Non-darwin platforms return ErrDialogNotSupported until a Win32 /
// GTK backend lands.

// ErrDialogNotSupported is returned by the native dialog functions on
// platforms where no backend is implemented yet (everything except
// darwin/cgo today).
var ErrDialogNotSupported = errors.New("qui: native dialogs not supported on this platform")

// OpenFileOptions configures OpenFile / OpenFiles.
type OpenFileOptions struct {
	// Title is the panel's titlebar text. Empty uses the OS default.
	Title string
	// Message is secondary text shown above the file list. Empty hides it.
	Message string
	// StartDirectory is the absolute path the panel opens at. Empty uses
	// the OS default (usually ~/Documents or last-used).
	StartDirectory string
	// AllowedExtensions filters the file list to these extensions (no
	// leading dot, e.g. ["png", "jpg"]). Empty allows all files.
	AllowedExtensions []string
}

// SaveFileOptions configures SaveFile.
type SaveFileOptions struct {
	Title             string
	Message           string
	StartDirectory    string
	Filename          string // suggested filename shown in the name field
	AllowedExtensions []string
}

// OpenDirectoryOptions configures OpenDirectory.
type OpenDirectoryOptions struct {
	Title          string
	Message        string
	StartDirectory string
}

// AlertStyle selects the icon / severity of ShowAlert.
type AlertStyle int

const (
	AlertInfo AlertStyle = iota
	AlertWarning
	AlertCritical
)

// AlertOptions configures ShowAlert.
type AlertOptions struct {
	// Title is the bold main text (messageText on macOS).
	Title string
	// Message is the smaller body text (informativeText on macOS).
	Message string
	// Style drives the icon shown next to the text.
	Style AlertStyle
	// Buttons labels, listed right-to-left on macOS. Empty defaults to
	// a single "OK" button. The first button is the default (Return).
	Buttons []string
}
