package typeset

import (
	"io"
	"log"
	"strings"
	"sync"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"golang.org/x/image/font/opentype"

	"github.com/qizhanchan/qui/internal/gfx"
)

// System font lookup: with it enabled, a family the app never registered
// ("Verdana", "Times New Roman", "PingFang SC") is looked up among the
// fonts installed on the machine and registered on first use, every
// installed weight and style of it. A browser needs this — pages name
// fonts by family and expect the OS to have them; an app that bundles its
// fonts leaves it off and keeps rendering exactly what it registered.
//
// The index of installed fonts comes from go-text's fontscan, which scans
// the platform font directories once and caches the result on disk (the
// first scan on a new machine takes a few seconds).

var (
	sysFontsOn   bool
	sysFontsOnce sync.Once
	sysFontIndex map[string][]fontscan.Footprint // normalized family → faces
	sysFontTried = map[string]bool{}             // under FontCacheMu
)

// EnableSystemFonts turns system font lookup on (or off) for family names
// that were not registered explicitly.
func EnableSystemFonts(on bool) {
	FontCacheMu.Lock()
	sysFontsOn = on
	FontCacheMu.Unlock()
}

// SystemFontsEnabled reports whether system font lookup is on.
func SystemFontsEnabled() bool {
	FontCacheMu.Lock()
	defer FontCacheMu.Unlock()
	return sysFontsOn
}

// PreloadSystemFontIndex builds (or loads the cached) index of installed
// fonts now, so the first page that names a font does not pay for it.
func PreloadSystemFontIndex() { loadSystemFontIndex() }

func loadSystemFontIndex() map[string][]fontscan.Footprint {
	sysFontsOnce.Do(func() {
		fps, err := fontscan.SystemFonts(log.New(io.Discard, "", 0), "")
		idx := map[string][]fontscan.Footprint{}
		if err == nil {
			for _, fp := range fps {
				idx[fp.Family] = append(idx[fp.Family], fp)
			}
		}
		sysFontIndex = idx
	})
	return sysFontIndex
}

// SystemFontFamilyInstalled reports whether family is installed on the
// machine (whether or not it has been registered yet).
func SystemFontFamilyInstalled(family string) bool {
	return len(loadSystemFontIndex()[gtfont.NormalizeFamily(family)]) > 0
}

// ensureSystemFamilyUnlocked registers every installed face of family the
// first time it is asked for. Caller holds FontCacheMu. Reports whether
// the family is now registered.
func ensureSystemFamilyUnlocked(family string) bool {
	key := strings.ToLower(strings.TrimSpace(family))
	if key == "" {
		return false
	}
	if _, ok := fontFamilyNames[key]; ok {
		return true
	}
	if !sysFontsOn || sysFontTried[key] {
		return false
	}
	sysFontTried[key] = true
	// The index normally is loaded already (FontFamilyRegistered and
	// PreloadSystemFontIndex load it outside the lock); a first lookup
	// straight from font selection loads it here, once.
	faces := loadSystemFontIndex()[gtfont.NormalizeFamily(family)]
	type loaded struct {
		k fontVariantKey
		f *opentype.Font
	}
	var got []loaded
	for _, fp := range faces {
		f := loadSystemFace(fp.Location.File, int(fp.Location.Index))
		if f == nil {
			continue
		}
		w := gfx.FontWeight(int(fp.Aspect.Weight))
		if w <= 0 {
			w = gfx.FontWeightNormal
		}
		italic := fp.Aspect.Style == gtfont.StyleItalic
		// Condensed / expanded cuts are separate faces of the same family;
		// the registry has no stretch axis, so they must not shadow the
		// normal-width face of the same weight.
		if fp.Aspect.Stretch != 0 && fp.Aspect.Stretch != gtfont.StretchNormal {
			continue
		}
		got = append(got, loaded{fontVariantKey{family: key, weight: w, italic: italic}, f})
	}
	if len(got) == 0 {
		return false
	}
	for _, l := range got {
		if _, exists := FontVariants[l.k]; !exists {
			FontVariants[l.k] = l.f
		}
	}
	fontFamilyNames[key] = strings.TrimSpace(family)
	ClearFontFaceCachesUnlocked()
	return true
}

// loadSystemFace parses face index of a system font file (mapped, like the
// automatic fallback probes).
func loadSystemFace(path string, index int) *opentype.Font {
	if data, ok := mapFontFile(path); ok {
		if f := parseFace(data, index); f != nil {
			return f
		}
		unmapFontFile(data)
	}
	return nil
}

func parseFace(data []byte, index int) *opentype.Font {
	if index == 0 {
		if f, err := opentype.Parse(data); err == nil {
			return f
		}
	}
	coll, err := opentype.ParseCollection(data)
	if err != nil {
		return nil
	}
	if index >= coll.NumFonts() {
		return nil
	}
	f, err := coll.Font(index)
	if err != nil {
		return nil
	}
	return f
}
