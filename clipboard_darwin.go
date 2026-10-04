//go:build darwin

package qui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void quiSetPasteboardRich(const char *plain, const char *html);
void quiPasteboardRich(char **plain, char **html);
*/
import "C"

import (
	"unsafe"
)

// darwinClipboard adds NSPasteboard HTML publishing on top of the plain
// platform provider. Get/Set still go through the seam (the pasteboard is
// process-wide); SetRich writes both the plain string and a public.html
// flavor so apps like Word / Pages / browsers paste with formatting
// preserved, and GetHTML reads the same flavor back out — including the one
// another app (or another window of this one) put there.
type darwinClipboard struct {
	platformClipboard
}

func (c darwinClipboard) SetRich(plain, html string) {
	cPlain := C.CString(plain)
	cHTML := C.CString(html)
	defer C.free(unsafe.Pointer(cPlain))
	defer C.free(unsafe.Pointer(cHTML))
	C.quiSetPasteboardRich(cPlain, cHTML)
}

// GetRich returns the pasteboard's plain-text and public.html flavors from one
// snapshot; html is "" when it holds none.
//
// A writer declares its types, which invalidates the previous owner's flavors,
// so the two flavors ON THE PASTEBOARD always belong to the same copy. That is
// not enough by itself: two reads are two moments, and a copy landing between
// them would hand back one copy's markup with the next copy's text. The
// pasteboard's changeCount is what closes that, so the pair is read under it —
// see quiPasteboardRich.
func (c darwinClipboard) GetRich() (string, string) {
	var cPlain, cHTML *C.char
	C.quiPasteboardRich(&cPlain, &cHTML)
	plain, html := "", ""
	if cPlain != nil {
		plain = C.GoString(cPlain)
		C.free(unsafe.Pointer(cPlain))
	}
	if cHTML != nil {
		html = C.GoString(cHTML)
		C.free(unsafe.Pointer(cHTML))
	}
	return plain, html
}

func newOSClipboard(win platformWindow) Clipboard {
	return darwinClipboard{platformClipboard{win: win}}
}
