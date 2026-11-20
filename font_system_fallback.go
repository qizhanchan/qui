package qui

import "github.com/qizhanchan/qui/internal/typeset"

// A rune that waited for the installed-font index (see
// internal/typeset/system_fallback.go) found its font after the text was
// laid out with a .notdef in its place: lay every window out again. The
// measure caches already miss — FontRegistryGeneration moved.
func init() { typeset.OnLateSystemFallback(relayoutAllWindows) }

// relayoutAllWindows lays every window out again and repaints it, after
// a font change the measure caches cannot see coming.
func relayoutAllWindows() {
	for _, w := range LiveWindows() {
		w := w
		w.PostJob(func() {
			w.InvalidateLayout()
			w.Invalidate()
		})
	}
}

// EnableSystemFontFallback turns the per-character system font fallback
// on or off (on by default; QUI_SYSTEM_FALLBACK=0 starts with it off).
// With it on, a character no registered font draws is drawn with the
// installed font the OS would use for it, instead of a .notdef box.
func EnableSystemFontFallback(on bool) { typeset.EnableSystemFallback(on) }

// FontFamilyInfo describes an installed font family (InstalledFontFamilies).
type FontFamilyInfo = typeset.FontFamilyInfo

// InstalledFontFamilies lists the font families installed on the machine,
// sorted by name, each marked monospaced and/or Chinese-capable — the
// choices of a font picker. The first call takes a moment (the OS is
// asked once); call it off the UI thread at startup to keep a picker
// instant.
func InstalledFontFamilies() []FontFamilyInfo { return typeset.InstalledFontFamilies() }

// SetFallbackFontFamily makes an installed family the first font consulted
// for characters the requested font lacks, ahead of the automatic symbol
// and system fonts ("" restores the automatic chain) — GoLand's "Fallback
// font". A family that draws Chinese also replaces the CJK font the OS
// would pick. Every window is laid out again. Reports whether the family
// is installed and loads; one that does not leaves the chain automatic.
func SetFallbackFontFamily(family string) bool {
	before := typeset.FallbackFamily()
	ok := typeset.SetFallbackFamily(family)
	if typeset.FallbackFamily() != before {
		relayoutAllWindows()
	}
	return ok
}

// FallbackFontFamily returns the family SetFallbackFontFamily set ("" for
// the automatic chain).
func FallbackFontFamily() string { return typeset.FallbackFamily() }
