//go:build darwin

package qui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *quiPreferredLocale(void);
*/
import "C"

import "unsafe"

// platformSystemLocale reads the user's preferred language from
// NSLocale. macOS returns the FIRST entry of the user's ordered
// language list (System Settings > General > Language & Region), which
// is what an app should follow — [NSLocale currentLocale] reports the
// region formatting locale and can differ (an English UI in Germany).
func platformSystemLocale() Locale {
	c := C.quiPreferredLocale()
	if c == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(c))
	return Locale(C.GoString(c))
}
