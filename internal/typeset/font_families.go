package typeset

import (
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

	"github.com/qizhanchan/qui/internal/gfx"
)

// FontFamilyInfo describes an installed font family, for font pickers.
type FontFamilyInfo struct {
	Name      string
	Monospace bool // fixed-pitch (a code / terminal font)
	Han       bool // draws Chinese characters
}

var (
	familyListOnce sync.Once
	familyList     []FontFamilyInfo
)

// InstalledFontFamilies lists the font families installed on the machine,
// sorted by name. The first call asks the OS (or scans the font
// directories) and takes a moment; later calls return the same list.
func InstalledFontFamilies() []FontFamilyInfo {
	familyListOnce.Do(func() {
		list, ok := platformFontFamilies()
		if !ok {
			list = indexFontFamilies()
		}
		sort.SliceStable(list, func(i, j int) bool {
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
		familyList = list
	})
	return append([]FontFamilyInfo(nil), familyList...)
}

// indexFontFamilies builds the list from the font directory index where
// the OS has no font list of its own. The index keeps only normalized
// family names, and no pitch flag: names that say "mono" or "code" count
// as monospaced.
func indexFontFamilies() []FontFamilyInfo {
	var out []FontFamilyInfo
	for fam, fps := range loadSystemFontIndex() {
		if fam == "" || len(fps) == 0 {
			continue
		}
		info := FontFamilyInfo{Name: fam}
		for _, k := range []string{"mono", "code", "courier", "consol", "fixed"} {
			if strings.Contains(fam, k) {
				info.Monospace = true
			}
		}
		for _, fp := range fps {
			if fp.Runes.Contains('中') {
				info.Han = true
				break
			}
		}
		out = append(out, info)
	}
	return out
}

// The user's fallback family: the first font consulted for a character
// the primary font lacks (GoLand's "Fallback font"). Under FontCacheMu.
var (
	fallbackFamily string
	// retiredFallbackSources are the file handles of an automatic chain
	// SetFallbackFamily replaced. They stay open: a layout built before
	// the swap may still draw with those faces.
	retiredFallbackSources []interface{ Close() error }
)

// SetFallbackFamily makes family ("" = automatic) the first fallback font,
// ahead of the automatic symbol and system fonts; when it draws Chinese it
// is also the CJK font (it replaces the one the OS would pick). Reports
// whether family is installed and loads — an unknown family leaves the
// automatic chain alone.
func SetFallbackFamily(family string) bool {
	family = strings.TrimSpace(family)
	FontCacheMu.Lock()
	defer FontCacheMu.Unlock()
	ok := family == "" || registerSystemFamilyUnlocked(family)
	if !ok {
		family = ""
	}
	if strings.EqualFold(family, fallbackFamily) {
		return ok
	}
	fallbackFamily = family
	rebuildAutoFallbacksUnlocked()
	return ok
}

// FallbackFamily returns the family SetFallbackFamily set ("" for none).
func FallbackFamily() string {
	FontCacheMu.RLock()
	defer FontCacheMu.RUnlock()
	return fallbackFamily
}

// rebuildAutoFallbacksUnlocked drops the automatic fallback chain so the
// next lookup builds it again, keeping the old fonts' sources open (see
// retiredFallbackSources). Fonts the per-character system fallback added
// come back as their characters are shaped again.
func rebuildAutoFallbacksUnlocked() {
	if !autoFallbacksInit {
		return
	}
	for _, src := range autoFallbackSources {
		if src != nil {
			retiredFallbackSources = append(retiredFallbackSources, src)
		}
	}
	AutoFallbacks = nil
	autoFallbackSources = nil
	autoFallbacksInit = false
	fallbackFaceMemo.Clear()
	UISymbolFallback.Store(nil)
	uiSymbolShapingFont.Store(nil)
	CJKFallback.Store(nil)
	cjkPunctYield.Clear()
	sysFallbackDone.Clear()
	ClearFontFaceCachesUnlocked()
}

// userFallbackFontUnlocked is the regular face of the user's fallback
// family (nil when none is set or it does not load). Caller holds
// FontCacheMu.
func userFallbackFontUnlocked() *opentype.Font {
	if fallbackFamily == "" || !registerSystemFamilyUnlocked(fallbackFamily) {
		return nil
	}
	key := strings.ToLower(fallbackFamily)
	if f := FontVariants[fontVariantKey{family: key, weight: gfx.FontWeightNormal}]; f != nil {
		return f
	}
	var best *opentype.Font
	bestScore := int(^uint(0) >> 1)
	for k, f := range FontVariants {
		if k.family != key || f == nil {
			continue
		}
		score := absInt(int(k.weight) - int(gfx.FontWeightNormal))
		if k.italic {
			score += 1000
		}
		if score < bestScore {
			best, bestScore = f, score
		}
	}
	return best
}

// fontDrawsHan reports whether f has a glyph for 中.
func fontDrawsHan(f *opentype.Font) bool {
	var buf sfnt.Buffer
	gid, err := f.GlyphIndex(&buf, '中')
	return err == nil && gid != 0
}
