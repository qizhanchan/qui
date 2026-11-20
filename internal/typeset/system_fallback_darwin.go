//go:build darwin && cgo

package typeset

import "github.com/qizhanchan/qui/internal/platform/macos"

func platformHasFontLookup() bool { return true }

func platformFontForRune(r rune) (path, postScript string, ok bool) {
	return macos.FontFileForRune(r)
}

// platformCJKFont is the installed font Core Text draws Han text with for
// the user's preferred languages, by its public name.
func platformCJKFont() (path, postScript string, ok bool) {
	_, ps, ok := macos.FontFileForRune('中')
	if !ok || !usableFallbackName(ps) {
		return "", "", false
	}
	ps = publicFontName(ps)
	path, ok = macos.FontFileForName(ps)
	return path, ps, ok
}

func platformFamilyFaces(family string) []systemFace {
	var out []systemFace
	for _, f := range macos.FamilyFaces(family) {
		out = append(out, systemFace{path: f.Path, index: -1, postScript: f.PostScript,
			weight: cssWeight(f.Weight), italic: f.Italic, stretched: f.Stretched})
	}
	return out
}

func platformFontFamilies() ([]FontFamilyInfo, bool) {
	var out []FontFamilyInfo
	for _, f := range macos.FontFamilies() {
		out = append(out, FontFamilyInfo{Name: f.Name, Monospace: f.Monospace, Han: f.Han})
	}
	return out, true
}
