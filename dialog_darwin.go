//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>

// Returns a newline-joined list of absolute file paths the user
// selected, or "" on cancel. Caller frees the returned char* with
// free(). `multi` and `dirsOnly` are bool-int toggles; `allowedExts`
// is newline-joined extensions (no leading dot) or "" for all.
char* quiShowOpenPanel(const char* title, const char* message,
                       const char* startDir, const char* allowedExts,
                       int multi, int dirsOnly);

// Returns the chosen save path, or "" on cancel. Caller frees.
char* quiShowSavePanel(const char* title, const char* message,
                       const char* startDir, const char* filename,
                       const char* allowedExts);

// Shows NSAlert modally. Returns the 0-based index of the pressed
// button. style: 0=info, 1=warning, 2=critical.
int quiShowAlert(const char* title, const char* message,
                 int style, const char* buttonsJoined);
*/
import "C"

import (
	"strings"
	"unsafe"
)

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
	cTitle, free1 := cStr(opts.Title)
	defer free1()
	cMessage, free2 := cStr(opts.Message)
	defer free2()
	cStart, free3 := cStr(opts.StartDirectory)
	defer free3()
	cName, free4 := cStr(opts.Filename)
	defer free4()
	cExts, free5 := cStr(strings.Join(opts.AllowedExtensions, "\n"))
	defer free5()

	result := C.quiShowSavePanel(cTitle, cMessage, cStart, cName, cExts)
	if result == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result), nil
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
	cTitle, free1 := cStr(opts.Title)
	defer free1()
	cMessage, free2 := cStr(opts.Message)
	defer free2()
	cButtons, free3 := cStr(strings.Join(buttons, "\n"))
	defer free3()

	idx := C.quiShowAlert(cTitle, cMessage, C.int(opts.Style), cButtons)
	return int(idx), nil
}

// openPanelImpl is the shared Cgo glue for the three Open variants.
// Splits the newline-joined return string back into a slice.
func openPanelImpl(title, message, startDir string, exts []string, multi, dirsOnly bool) ([]string, error) {
	cTitle, free1 := cStr(title)
	defer free1()
	cMessage, free2 := cStr(message)
	defer free2()
	cStart, free3 := cStr(startDir)
	defer free3()
	cExts, free4 := cStr(strings.Join(exts, "\n"))
	defer free4()

	var multiI, dirsI C.int
	if multi {
		multiI = 1
	}
	if dirsOnly {
		dirsI = 1
	}

	result := C.quiShowOpenPanel(cTitle, cMessage, cStart, cExts, multiI, dirsI)
	if result == nil {
		return nil, nil
	}
	defer C.free(unsafe.Pointer(result))
	s := C.GoString(result)
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// cStr returns a C string + a free function. Always safe to call the
// free — even for empty Go strings C.CString returns a valid pointer.
func cStr(s string) (*C.char, func()) {
	c := C.CString(s)
	return c, func() { C.free(unsafe.Pointer(c)) }
}
