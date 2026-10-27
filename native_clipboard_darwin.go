//go:build darwin && cgo

package qui

import "github.com/qizhanchan/qui/internal/platform/macos"

// darwinClipboard adds NSPasteboard HTML publishing on top of the plain
// platform provider. Get/Set still go through the seam (the pasteboard is
// process-wide); SetRich writes both the plain string and a public.html
// flavor so apps like Word / Pages / browsers paste with formatting
// preserved, and GetHTML reads the same flavor back out — including the one
// another app (or another window of this one) put there.
type darwinClipboard struct {
	platformClipboard
}

func (c darwinClipboard) SetRich(plain, html string) { macos.SetPasteboardRich(plain, html) }

// GetRich returns the pasteboard's plain-text and public.html flavors from one
// snapshot; html is "" when it holds none.
//
// A writer declares its types, which invalidates the previous owner's flavors,
// so the two flavors ON THE PASTEBOARD always belong to the same copy. That is
// not enough by itself: two reads are two moments, and a copy landing between
// them would hand back one copy's markup with the next copy's text. The
// pasteboard's changeCount is what closes that, so the pair is read under it —
// see quiPasteboardRich.
func (c darwinClipboard) GetRich() (string, string) { return macos.PasteboardRich() }

// GetImage returns the pasteboard's image (see quiPasteboardImage).
func (c darwinClipboard) GetImage() ([]byte, string) { return macos.PasteboardImage() }

// SetImage publishes PNG bytes with a plain-text flavor.
func (c darwinClipboard) SetImage(png []byte, plain string) { macos.SetPasteboardImage(png, plain) }

// GetFiles returns the paths of the files on the pasteboard.
func (c darwinClipboard) GetFiles() []string { return macos.PasteboardFiles() }

func newOSClipboard(win platformWindow) Clipboard {
	return darwinClipboard{platformClipboard{win: win}}
}
