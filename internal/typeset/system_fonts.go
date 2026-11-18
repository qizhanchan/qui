package typeset

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

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

var systemTypefaceNames sync.Map // *opentype.Font -> PostScript name

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
		cacheDir, cleanup := systemFontCacheDir()
		defer cleanup()
		fps, err := fontscan.SystemFonts(log.New(io.Discard, "", 0), cacheDir)
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

// fontscan treats an unwritable index cache as a failed font scan. Keep
// installed fonts available in sandboxed/read-only profiles by selecting a
// writable cache before its process-wide, one-shot initialization.
func systemFontCacheDir() (string, func()) {
	dir, _ := os.UserCacheDir()
	return writableSystemFontCacheDir(dir)
}

func writableSystemFontCacheDir(dir string) (string, func()) {
	if dir != "" {
		if f, err := os.CreateTemp(dir, "qui-font-cache-"); err == nil {
			name := f.Name()
			f.Close()
			os.Remove(name)
			return dir, func() {}
		}
	}
	dir, err := os.MkdirTemp("", "qui-font-index-")
	if err != nil {
		return "", func() {}
	}
	return dir, func() { os.RemoveAll(dir) }
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
		if name, err := f.Name(&sfnt.Buffer{}, sfnt.NameIDPostScript); err == nil {
			systemTypefaceNames.Store(f, name)
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

// loadSystemFace loads face index of a system font file. A face of a
// collection is rewritten into a standalone font first: the shaper parses
// a font's raw bytes as one font, so it needs the face's own program.
func loadSystemFace(path string, index int) *opentype.Font {
	data, ok := mapFontFile(path)
	if !ok {
		return nil
	}
	if len(data) >= 4 && string(data[0:4]) == "ttcf" {
		face := ExtractFace(data, index)
		unmapFontFile(data)
		if len(face) == 0 {
			return nil
		}
		f, usable := parseSystemFontBytes(face)
		if f == nil {
			return nil
		}
		rememberRawFontBytes(f, usable)
		return f
	}
	if f := parseFace(data, index); f != nil {
		rememberRawFontBytes(f, data)
		return f
	}
	unmapFontFile(data)
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

// parseSystemFontBytes accepts legacy Unicode cmap identifiers used by system
// fonts. x/image recognizes Unicode BMP identifier 3 but rejects the older
// identifiers even when they contain the same supported format-4 mapping.
func parseSystemFontBytes(data []byte) (*opentype.Font, []byte) {
	if f, err := opentype.Parse(data); err == nil {
		return f, data
	}
	version, tables, err := ParseSFNTTables(data)
	if err != nil {
		return nil, nil
	}
	cmap := tables["cmap"]
	if len(cmap) < 4 {
		return nil, nil
	}
	count := int(be16(cmap, 2))
	if count > (len(cmap)-4)/8 {
		return nil, nil
	}
	changed := false
	for i := 0; i < count; i++ {
		rec := 4 + 8*i
		if be16(cmap, rec) != 0 || be16(cmap, rec+2) > 2 {
			continue
		}
		off := uint64(be32(cmap, rec+4))
		if off+2 > uint64(len(cmap)) {
			continue
		}
		format := be16(cmap, int(off))
		if format == 4 || format == 6 {
			putBE16(cmap, rec+2, 3)
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	usable := writeSFNT(version, tables)
	f, err := opentype.Parse(usable)
	if err != nil {
		return nil, nil
	}
	return f, usable
}
