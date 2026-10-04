//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework Quartz

#include <stdlib.h>

// Prints a PDF via PDFKit's NSPrintOperation. Returns 0 on success, or a
// non-zero code (1 = invalid PDF, 2 = print operation failed/cancelled).
int quiPrintPDF(const void* pdfBytes, int length, const char* jobTitle, int showPanel);
*/
import "C"

import (
	"errors"
	"unsafe"
)

func printPDF(pdf []byte, opts PrintOptions) error {
	if len(pdf) == 0 {
		return errors.New("qui: empty PDF")
	}
	title := opts.JobTitle
	if title == "" {
		title = "qui"
	}
	cTitle, free := cStr(title)
	defer free()

	show := C.int(0)
	if opts.ShowPanel {
		show = 1
	}
	rc := C.quiPrintPDF(unsafe.Pointer(&pdf[0]), C.int(len(pdf)), cTitle, show)
	switch rc {
	case 0:
		return nil
	case 1:
		return errors.New("qui: invalid PDF for printing")
	default:
		return errors.New("qui: print operation failed or was cancelled")
	}
}
