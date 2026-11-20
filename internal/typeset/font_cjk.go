package typeset

import (
	"image"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The system CJK font and the punctuation it owns.
//
// Full-width CJK punctuation (，。：；！？「」（）…) is designed per
// language: Simplified Chinese sits it in the lower-left of the em,
// Traditional Chinese / Japanese centre it. The general symbol fonts ahead
// of the CJK font in the automatic chain (Apple Symbols on macOS) carry
// some of these code points with their own, centred design, and used to
// win them — so Simplified Chinese text read with Traditional-style
// commas and colons, from a different typeface than the characters next
// to them. The automatic symbol / Latin fallbacks now yield those runes to
// the CJK font; a font the app chose (primary or LoadFallbackFontFromFile)
// never yields.

// CJKFallback is the system CJK font of the automatic chain (nil when
// none was found).
var CJKFallback atomic.Pointer[opentype.Font]

// cjkPunctYield holds the automatic fallbacks that yield CJK punctuation
// to CJKFallback.
var cjkPunctYield sync.Map // *opentype.Font → struct{}

// IsCJKPunct reports the full-width punctuation and forms whose design is
// part of a CJK typeface.
func IsCJKPunct(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303F, // CJK Symbols and Punctuation
		r >= 0xFE10 && r <= 0xFE1F, // Vertical Forms
		r >= 0xFE30 && r <= 0xFE4F, // CJK Compatibility Forms
		r >= 0xFF00 && r <= 0xFF60, // Fullwidth ASCII variants
		r >= 0xFFE0 && r <= 0xFFE6: // Fullwidth signs
		return true
	}
	return false
}

// yieldsRune reports whether automatic font f hands r to another
// automatic font: CJK punctuation to the CJK font, and — when the CJK
// font is consulted first, as the default primary — the keyboard symbols
// (⌘ ⇧ ⌥) to the UI-symbol face, which is what macOS draws them with.
func yieldsRune(f *opentype.Font, r rune) bool {
	if f == nil {
		return false
	}
	cjk := CJKFallback.Load()
	if cjk == nil {
		return false
	}
	if f == cjk {
		return IsUIKeyboardSymbol(r) && UISymbolFallback.Load() != nil
	}
	if !IsCJKPunct(r) {
		return false
	}
	_, ok := cjkPunctYield.Load(f)
	return ok
}

// yieldingFace wraps the face of f when f yields any runes (see
// yieldsRune), so FaceForRune moves on to the font that owns them.
func yieldingFace(face font.Face, f *opentype.Font) font.Face {
	if _, ok := cjkPunctYield.Load(f); ok || f == CJKFallback.Load() {
		return cjkPunctYieldFace{face, f}
	}
	return face
}

// cjkPunctYieldFace hides the runes an automatic font yields (see
// yieldsRune) so FaceForRune reaches the font that owns them.
type cjkPunctYieldFace struct {
	font.Face
	src *opentype.Font
}

func (f cjkPunctYieldFace) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	if yieldsRune(f.src, r) {
		return
	}
	return f.Face.Glyph(dot, r)
}

func (f cjkPunctYieldFace) GlyphBounds(r rune) (bounds fixed.Rectangle26_6, advance fixed.Int26_6, ok bool) {
	if yieldsRune(f.src, r) {
		return
	}
	return f.Face.GlyphBounds(r)
}

func (f cjkPunctYieldFace) GlyphAdvance(r rune) (advance fixed.Int26_6, ok bool) {
	if yieldsRune(f.src, r) {
		return
	}
	return f.Face.GlyphAdvance(r)
}

// publicFontName maps the private UI variant Core Text cascades to
// (".PingFangUITextSC-Regular") to the installed font it is cut from
// ("PingFangSC-Regular"); other names are returned unchanged.
func publicFontName(ps string) string {
	if !strings.HasPrefix(ps, ".") {
		return ps
	}
	n := strings.TrimPrefix(ps, ".")
	for _, ui := range []string{"UIText", "UIDisplay", "UI"} {
		if i := strings.Index(n, ui); i > 0 {
			return n[:i] + n[i+len(ui):]
		}
	}
	return n
}

// loadPlatformCJKFont loads the CJK font the OS would use for Han text in
// the user's languages (PingFang SC for Simplified Chinese on macOS), as
// the named face of its collection — face 0 of PingFang.ttc is the Hong
// Kong cut. nil when the platform cannot say or the face does not parse.
func loadPlatformCJKFont() *opentype.Font {
	path, ps, ok := platformCJKFont()
	if !ok {
		return nil
	}
	idx := faceIndexByPostScript(path, ps)
	f := loadSystemFace(path, idx)
	if f == nil {
		return nil
	}
	var buf sfnt.Buffer
	if name, err := f.Name(&buf, sfnt.NameIDPostScript); err != nil || name != ps {
		return nil // the face index lookup missed: not the cut asked for
	}
	if gid, err := f.GlyphIndex(&buf, '中'); err != nil || gid == 0 {
		return nil
	}
	return f
}
