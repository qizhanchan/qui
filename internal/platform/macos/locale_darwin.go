//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *quiPreferredLocale(void);
*/
import "C"

import "unsafe"

// PreferredLocale returns the first entry of the user's ordered language
// list, or "".
func PreferredLocale() string {
	c := C.quiPreferredLocale()
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}
