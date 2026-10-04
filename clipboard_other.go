//go:build !darwin

package qui

// newOSClipboard returns the platform clipboard provider. Non-darwin
// platforms get plain text only; rich (HTML) publishing is darwin-only for
// now, so SetClipboardRich degrades to plain text here and GetClipboardHTML
// returns "" (the provider implements neither rich interface).
func newOSClipboard(win platformWindow) Clipboard {
	return platformClipboard{win: win}
}
