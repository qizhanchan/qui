//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework Quartz
#include <stdlib.h>
// 0 on success, 1 = invalid PDF, 2 = print operation failed/cancelled.
int quiPrintPDF(const void* pdfBytes, int length, const char* jobTitle, int showPanel);
*/
import "C"

import "unsafe"

// PrintPDF prints a PDF via PDFKit's NSPrintOperation. It returns 0 on
// success, 1 for an invalid PDF, 2 when printing failed or was cancelled.
func PrintPDF(pdf []byte, jobTitle string, showPanel bool) int {
	if len(pdf) == 0 {
		return 1
	}
	cTitle, free := cStr(jobTitle)
	defer free()
	return int(C.quiPrintPDF(unsafe.Pointer(&pdf[0]), C.int(len(pdf)), cTitle, cBool(showPanel)))
}
