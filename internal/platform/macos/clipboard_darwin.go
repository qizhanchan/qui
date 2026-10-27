//go:build darwin && cgo

package macos

/*
#include <stdlib.h>
void quiSetPasteboardRich(const char *plain, const char *html);
void quiPasteboardRich(char **plain, char **html);
void *quiPasteboardImage(int *len, char **ext);
void quiSetPasteboardImage(const void *png, int len, const char *plain);
char **quiPasteboardFiles(int *n);
void quiSetPasteboardFiles(char **paths, int n);
*/
import "C"

import "unsafe"

// SetPasteboardRich writes plain text plus a public.html flavor.
func SetPasteboardRich(plain, html string) {
	cPlain, freePlain := cStr(plain)
	defer freePlain()
	cHTML, freeHTML := cStr(html)
	defer freeHTML()
	C.quiSetPasteboardRich(cPlain, cHTML)
}

// PasteboardRich returns the plain-text and public.html flavors read under
// one changeCount, so both belong to the same copy; html is "" when absent.
func PasteboardRich() (plain, html string) {
	var cPlain, cHTML *C.char
	C.quiPasteboardRich(&cPlain, &cHTML)
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

// PasteboardImage returns the pasteboard's image bytes and their extension.
func PasteboardImage() ([]byte, string) {
	var n C.int
	var cExt *C.char
	p := C.quiPasteboardImage(&n, &cExt)
	if p == nil {
		return nil, ""
	}
	defer C.free(p)
	defer C.free(unsafe.Pointer(cExt))
	return C.GoBytes(p, n), C.GoString(cExt)
}

// SetPasteboardImage publishes PNG bytes with a plain-text flavor.
func SetPasteboardImage(png []byte, plain string) {
	if len(png) == 0 {
		return
	}
	cPlain, free := cStr(plain)
	defer free()
	C.quiSetPasteboardImage(unsafe.Pointer(&png[0]), C.int(len(png)), cPlain)
}

// PasteboardFiles returns the paths of the files on the pasteboard.
func PasteboardFiles() []string {
	var n C.int
	arr := C.quiPasteboardFiles(&n)
	if arr == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(arr))
	items := unsafe.Slice(arr, int(n))
	out := make([]string, 0, len(items))
	for _, p := range items {
		out = append(out, C.GoString(p))
		C.free(unsafe.Pointer(p))
	}
	return out
}

// SetPasteboardFiles publishes paths as file URLs (one pasteboard item
// each, as Finder's Copy does) plus the paths as plain text.
func SetPasteboardFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	base := (**C.char)(C.malloc(C.size_t(len(paths)) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
	defer C.free(unsafe.Pointer(base))
	arr := unsafe.Slice(base, len(paths))
	for i, p := range paths {
		arr[i] = C.CString(p)
		defer C.free(unsafe.Pointer(arr[i]))
	}
	C.quiSetPasteboardFiles(base, C.int(len(paths)))
}
