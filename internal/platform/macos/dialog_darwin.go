//go:build darwin && cgo

package macos

/*
#include <stdlib.h>

// Newline-joined absolute paths, or "" on cancel; caller frees.
char* quiShowOpenPanel(const char* title, const char* message,
                       const char* startDir, const char* allowedExts,
                       int multi, int dirsOnly);
// The chosen save path, or "" on cancel; caller frees.
char* quiShowSavePanel(const char* title, const char* message,
                       const char* startDir, const char* filename,
                       const char* allowedExts);
// 0-based index of the pressed button. style: 0=info, 1=warning, 2=critical.
int quiShowAlert(const char* title, const char* message,
                 int style, const char* buttonsJoined);
*/
import "C"

import (
	"strings"
	"unsafe"
)

// ShowOpenPanel runs an NSOpenPanel and returns the selected paths, nil on
// cancel. exts are extensions without the leading dot; empty allows all.
func ShowOpenPanel(title, message, startDir string, exts []string, multi, dirsOnly bool) []string {
	cTitle, free1 := cStr(title)
	defer free1()
	cMessage, free2 := cStr(message)
	defer free2()
	cStart, free3 := cStr(startDir)
	defer free3()
	cExts, free4 := cStr(strings.Join(exts, "\n"))
	defer free4()

	result := C.quiShowOpenPanel(cTitle, cMessage, cStart, cExts, cBool(multi), cBool(dirsOnly))
	if result == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(result))
	s := C.GoString(result)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// ShowSavePanel runs an NSSavePanel and returns the chosen path, "" on cancel.
func ShowSavePanel(title, message, startDir, filename string, exts []string) string {
	cTitle, free1 := cStr(title)
	defer free1()
	cMessage, free2 := cStr(message)
	defer free2()
	cStart, free3 := cStr(startDir)
	defer free3()
	cName, free4 := cStr(filename)
	defer free4()
	cExts, free5 := cStr(strings.Join(exts, "\n"))
	defer free5()

	result := C.quiShowSavePanel(cTitle, cMessage, cStart, cName, cExts)
	if result == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result)
}

// ShowAlert runs an NSAlert modally and returns the pressed button's index.
func ShowAlert(title, message string, style int, buttons []string) int {
	cTitle, free1 := cStr(title)
	defer free1()
	cMessage, free2 := cStr(message)
	defer free2()
	cButtons, free3 := cStr(strings.Join(buttons, "\n"))
	defer free3()
	return int(C.quiShowAlert(cTitle, cMessage, C.int(style), cButtons))
}
