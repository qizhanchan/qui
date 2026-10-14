package qui

import (
	"image"
	"io"
	"runtime"
	"sync/atomic"

	gtfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// UI keyboard symbols (⌘ ⇧ ⌥ ⌃ ⌫ ...) as drawn in menu shortcuts.
//
// The default faces (a system CJK face, goregular) do not carry them, so
// they used to land on the general symbol fallback — Apple Symbols on
// macOS, whose glyphs sit on a much smaller em (its ⌘ is ~57% of the em
// against ~72% in the platform UI face), so a shortcut like "⌘B" drew a
// visibly shrunken ⌘ next to a full-size B. The platform UI face (SF on
// macOS, Lucida Grande on older systems) has them designed at cap height,
// which is how native menus draw them.
//
// The UI-symbol face joins the automatic fallback chain AHEAD of the
// general symbol font, but it is consulted only for the runes in
// isUIKeyboardSymbol: every other glyph keeps resolving exactly as before,
// so no other text changes face.

// uiSymbolFallback is the parsed UI-symbol face, or nil when none was
// found. Stored atomically because the shaping and PDF lookups read it
// without fontCacheMu.
var uiSymbolFallback atomic.Pointer[opentype.Font]

// uiSymbolShapingFont is the go-text font parsed from uiSymbolFallback
// (set the first time the shaping path builds a face for it).
var uiSymbolShapingFont atomic.Pointer[gtfont.Font]

// probeUISymbolFontUnlocked is the test hook for the UI-symbol probe.
var probeUISymbolFontUnlocked = tryLoadUISymbolFontUnlocked

func uiSymbolFontCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/SFNS.ttf",
			"/System/Library/Fonts/LucidaGrande.ttc",
		}
	}
	return nil
}

func tryLoadUISymbolFontUnlocked() (*opentype.Font, io.Closer) {
	for _, path := range uiSymbolFontCandidates() {
		f, src, err := parseFirstFontFromFile(path, true)
		if err == nil {
			return f, src
		}
	}
	return nil, nil
}

// isUIKeyboardSymbol reports the keyboard / modifier symbols routed to
// the UI-symbol face.
func isUIKeyboardSymbol(r rune) bool {
	switch r {
	case 0x2318, // ⌘ command
		0x21E7, // ⇧ shift
		0x2325, // ⌥ option
		0x2303, // ⌃ control
		0x21EA, // ⇪ caps lock
		0x232B, // ⌫ delete
		0x2326, // ⌦ forward delete
		0x238B, // ⎋ escape
		0x21E5, // ⇥ tab
		0x21E4, // ⇤ back-tab
		0x2324, // ⌤ enter
		0x23CE, // ⏎ return
		0x23CF, // ⏏ eject
		0x2304, // ⌄
		0x2305, // ⌅
		0x2327, // ⌧ clear
		0x21DE, // ⇞ page up
		0x21DF, // ⇟ page down
		0x2196, // ↖ home
		0x2198: // ↘ end
		return true
	}
	return false
}

// fallbackSkipsRune reports whether chain member f must be skipped for r:
// the UI-symbol face only answers for keyboard symbols.
func fallbackSkipsRune(f *opentype.Font, r rune) bool {
	return f != nil && f == uiSymbolFallback.Load() && !isUIKeyboardSymbol(r)
}

// shapingFallbackSkipsRune is fallbackSkipsRune for the go-text chain.
func shapingFallbackSkipsRune(f *gtfont.Font, r rune) bool {
	return f != nil && f == uiSymbolShapingFont.Load() && !isUIKeyboardSymbol(r)
}

// uiSymbolFace restricts a face to the keyboard symbols: for any other
// rune it reports no glyph, so faceForRune moves on down the chain.
type uiSymbolFace struct {
	font.Face
}

func (f uiSymbolFace) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	if !isUIKeyboardSymbol(r) {
		return
	}
	return f.Face.Glyph(dot, r)
}

func (f uiSymbolFace) GlyphBounds(r rune) (bounds fixed.Rectangle26_6, advance fixed.Int26_6, ok bool) {
	if !isUIKeyboardSymbol(r) {
		return
	}
	return f.Face.GlyphBounds(r)
}

func (f uiSymbolFace) GlyphAdvance(r rune) (advance fixed.Int26_6, ok bool) {
	if !isUIKeyboardSymbol(r) {
		return
	}
	return f.Face.GlyphAdvance(r)
}
