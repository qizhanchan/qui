package typeset

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/qizhanchan/qui/internal/gfx"
	gtfont "github.com/qizhanchan/qui/third_party/typesetting/font"
	"github.com/qizhanchan/qui/third_party/typesetting/fontscan"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// System font fallback: a rune no loaded font draws is looked up among the
// fonts installed on the machine — the way a browser or a native text view
// finds one — and the font that has it joins the fallback chain.
//
// Without it the chain was a hand-kept list (Apple Symbols, Zapf Dingbats,
// STIX Two Math, a CJK face, …) and every character outside it drew the
// .notdef box: a tool's status glyph, a Thai word, a math symbol. Each
// was fixed by adding one more font to the list; the OS already knows the
// answer for every character.
//
// Where the answer comes from:
//
//   - macOS: Core Text's cascade from the system UI font
//     (CTFontCreateForString) — the font a native app draws the character
//     with. LastResort (Apple's tofu font) and Apple Color Emoji (emoji go
//     through the EmojiProvider) are not text fonts and are skipped.
//   - otherwise, and when Core Text's font is unusable (LastResort, or a
//     font sfnt cannot parse — SF Devanagari, Kokonor): the installed-font
//     index (go-text fontscan, cached on disk), any non-colour font whose
//     cmap has the rune. On macOS the index loads in the background from
//     the first lookup; a lookup waits for it up to indexWait (~100 ms from
//     the disk cache), and past that — the first scan on a machine takes
//     seconds — the rune is retried when it arrives and every window is
//     laid out again (OnLateSystemFallback).
//
// Each rune is looked up once per process (hit or miss). A font that is
// found is appended to AutoFallbacks — after everything the app and the
// lazy init registered, so it only ever supplies glyphs those lack — and
// the face caches are cleared (FontRegistryGeneration moves), exactly as
// when an app registers a fallback.
//
// QUI_SYSTEM_FALLBACK=0 turns it off.

var systemFallbackOn atomic.Bool

func init() { systemFallbackOn.Store(os.Getenv("QUI_SYSTEM_FALLBACK") != "0") }

// EnableSystemFallback turns the per-rune system font fallback on or off.
func EnableSystemFallback(on bool) { systemFallbackOn.Store(on) }

var (
	// sysFallbackDone holds every rune looked at (rune → struct{}).
	sysFallbackDone sync.Map
	// sysFallbackFonts are the loaded fallback fonts by file#postscript, so
	// one font found for many runes loads once. Under sysFallbackMu.
	sysFallbackMu    sync.Mutex
	sysFallbackFonts = map[string]*opentype.Font{}
	// sysIndexReady is set once the fontscan index has been loaded.
	sysIndexReady    atomic.Bool
	sysIndexLoadOnce sync.Once
	sysIndexDone     = make(chan struct{})
	// sysFallbackWaiting: a rune waits for the index (see lateFallback).
	sysFallbackWaiting atomic.Bool
	lateFallbackHook   atomic.Pointer[func()]
)

// OnLateSystemFallback installs fn, called (on the index loader's
// goroutine) when the installed-font index arrives with runes waiting for
// it: the face caches have been cleared, and text laid out meanwhile
// should be laid out again. Root re-lays out every window.
func OnLateSystemFallback(fn func()) { lateFallbackHook.Store(&fn) }

// skipSystemFallback reports runes never looked up: ASCII (the primary or
// the bundled goregular), controls and formatting characters that draw
// nothing, and the emoji plane (the EmojiProvider's).
func skipSystemFallback(r rune) bool {
	switch {
	case r < 0x80:
		return true
	case r >= 0xD800 && r <= 0xDFFF, r > unicode.MaxRune:
		return true
	case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x1F1E6 && r <= 0x1F1FF:
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0000 && r <= 0xE0FFF:
		return true // variation selectors, tags
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Mn, unicode.Me)
}

// EnsureRunesCovered looks up a system font for every rune of text that no
// loaded font draws. It must run with none of the font, shaping or layout
// locks held: a hit clears those caches. Cheap after the first call for a
// rune (one map read).
func EnsureRunesCovered(text []rune) {
	if !systemFallbackOn.Load() {
		return
	}
	for _, r := range text {
		if skipSystemFallback(r) {
			continue
		}
		if _, done := sysFallbackDone.Load(r); done {
			continue
		}
		ensureSystemFallback(r)
	}
}

// EnsureStringCovered is EnsureRunesCovered for a string.
func EnsureStringCovered(s string) {
	if !systemFallbackOn.Load() {
		return
	}
	for _, r := range s {
		if r >= 0x80 {
			EnsureRunesCovered([]rune(s))
			return
		}
	}
}

// indexWait bounds how long a lookup waits for the installed-font index
// when Core Text's font cannot be used. Loading it from its disk cache
// takes ~100 ms; the first scan on a machine takes seconds, and then the
// rune waits for the background load instead of the frame.
const indexWait = 200 * time.Millisecond

// startSystemFontIndex loads the index in the background (once) and
// re-lays out the text whose runes waited for it.
func startSystemFontIndex() {
	sysIndexLoadOnce.Do(func() {
		go func() {
			loadSystemFontIndex()
			close(sysIndexDone)
			if sysFallbackWaiting.Swap(false) {
				FontCacheMu.Lock()
				ClearFontFaceCachesUnlocked()
				FontCacheMu.Unlock()
				if fn := lateFallbackHook.Load(); fn != nil {
					(*fn)()
				}
			}
		}()
	})
}

// waitSystemFontIndex waits up to d for the index; reports whether it is in.
func waitSystemFontIndex(d time.Duration) bool {
	if sysIndexReady.Load() {
		return true
	}
	startSystemFontIndex()
	select {
	case <-sysIndexDone:
		return true
	case <-time.After(d):
		return false
	}
}

func ensureSystemFallback(r rune) {
	if _, loaded := sysFallbackDone.LoadOrStore(r, struct{}{}); loaded {
		return
	}
	if platformHasFontLookup() {
		startSystemFontIndex() // likely needed soon; load it meanwhile
	}
	GetFontFacesFor(gfx.Font{Size: 14}) // the lazy init and auto fallbacks first
	if loadedFontsHaveGlyph(r) {
		return
	}
	f := lookupSystemFallback(r)
	if f == nil {
		return
	}
	FontCacheMu.Lock()
	for _, have := range AutoFallbacks {
		if have == f {
			FontCacheMu.Unlock()
			return
		}
	}
	AutoFallbacks = append(AutoFallbacks, f)
	autoFallbackSources = append(autoFallbackSources, nil)
	ClearFontFaceCachesUnlocked()
	FontCacheMu.Unlock()
}

// loadedFontsHaveGlyph reports whether any registered font — primary,
// fallbacks, family variants — has a glyph for r. A font that only a
// family's own chain uses still counts: appending a system font then
// would never draw anything.
func loadedFontsHaveGlyph(r rune) bool {
	var buf sfnt.Buffer
	has := func(f *opentype.Font) bool {
		if f == nil {
			return false
		}
		gid, err := f.GlyphIndex(&buf, r)
		return err == nil && gid != 0
	}
	FontCacheMu.RLock()
	defer FontCacheMu.RUnlock()
	if has(FontParsed) {
		return true
	}
	for _, f := range FontFallbacks {
		if has(f) {
			return true
		}
	}
	for _, f := range AutoFallbacks {
		if has(f) {
			return true
		}
	}
	for _, f := range FontVariants {
		if has(f) {
			return true
		}
	}
	return false
}

// lookupSystemFallback finds and loads an installed font with a glyph for
// r (nil for none).
func lookupSystemFallback(r rune) *opentype.Font {
	if path, ps, ok := platformFontForRune(r); ok && usableFallbackName(ps) {
		if f := loadFallbackFont(path, ps, r); f != nil {
			return f
		}
	}
	if !platformHasFontLookup() {
		loadSystemFontIndex() // nothing else to ask: wait for it
	} else if !waitSystemFontIndex(indexWait) {
		// Asked again once the index is in: the text is laid out again
		// then (OnLateSystemFallback).
		sysFallbackDone.Delete(r)
		sysFallbackWaiting.Store(true)
		return nil
	}
	return indexFontForRune(r)
}

// usableFallbackName rejects the fonts that are not text fonts for this
// purpose: Apple's LastResort draws a tofu of its own, and colour emoji
// belong to the EmojiProvider.
func usableFallbackName(name string) bool {
	n := strings.ToLower(name)
	return n != "" && !strings.Contains(n, "lastresort") && !strings.Contains(n, "emoji")
}

// indexFontForRune picks, from the installed-font index, a regular face of
// a non-colour font whose cmap has r. Families are tried in name order so
// the pick is stable from run to run.
func indexFontForRune(r rune) *opentype.Font {
	idx := sysFontIndex
	if idx == nil {
		return nil
	}
	families := make([]string, 0, len(idx))
	for fam := range idx {
		if usableFallbackName(fam) {
			families = append(families, fam)
		}
	}
	sort.Strings(families)
	var best *fontscan.Footprint
	score := func(fp *fontscan.Footprint) int {
		s := 0
		if fp.Aspect.Style != gtfont.StyleItalic {
			s += 4
		}
		if fp.Aspect.Weight == 0 || fp.Aspect.Weight == gtfont.WeightNormal {
			s += 2
		}
		if fp.Aspect.Stretch == 0 || fp.Aspect.Stretch == gtfont.StretchNormal {
			s++
		}
		return s
	}
	for _, fam := range families {
		for i := range idx[fam] {
			fp := &idx[fam][i]
			if !fp.Runes.Contains(r) {
				continue
			}
			if best == nil || score(fp) > score(best) {
				best = fp
			}
		}
		if best != nil && score(best) == 7 {
			break
		}
	}
	if best == nil {
		return nil
	}
	return loadFallbackFace(best.Location.File, int(best.Location.Index), r)
}

// loadFallbackFont loads the face named postScript (any face of a
// collection) from path, if it has a glyph for r.
func loadFallbackFont(path, postScript string, r rune) *opentype.Font {
	key := path + "#" + postScript
	sysFallbackMu.Lock()
	f, seen := sysFallbackFonts[key]
	sysFallbackMu.Unlock()
	if !seen {
		f = loadSystemFace(path, faceIndexByPostScript(path, postScript))
		if f != nil {
			if name, err := f.Name(&sfnt.Buffer{}, sfnt.NameIDPostScript); err == nil {
				systemTypefaceNames.Store(f, name)
			}
		}
		sysFallbackMu.Lock()
		sysFallbackFonts[key] = f
		sysFallbackMu.Unlock()
	}
	return withGlyph(f, r)
}

// loadFallbackFace is loadFallbackFont by face index.
func loadFallbackFace(path string, index int, r rune) *opentype.Font {
	key := path + "#" + strconv.Itoa(index)
	sysFallbackMu.Lock()
	f, seen := sysFallbackFonts[key]
	sysFallbackMu.Unlock()
	if !seen {
		f = loadSystemFace(path, index)
		sysFallbackMu.Lock()
		sysFallbackFonts[key] = f
		sysFallbackMu.Unlock()
	}
	return withGlyph(f, r)
}

func withGlyph(f *opentype.Font, r rune) *opentype.Font {
	if f == nil {
		return nil
	}
	if gid, err := f.GlyphIndex(&sfnt.Buffer{}, r); err != nil || gid == 0 {
		return nil
	}
	return f
}

// faceIndexByPostScript is the index of the face named postScript in the
// collection at path (0 for a single font, or when no face matches).
func faceIndexByPostScript(path, postScript string) int {
	data, ok := mapFontFile(path)
	if !ok {
		return 0
	}
	defer unmapFontFile(data)
	if len(data) < 4 || string(data[0:4]) != "ttcf" {
		return 0
	}
	coll, err := opentype.ParseCollection(data)
	if err != nil {
		return 0
	}
	var buf sfnt.Buffer
	for i := 0; i < coll.NumFonts(); i++ {
		f, err := coll.Font(i)
		if err != nil {
			continue
		}
		if name, err := f.Name(&buf, sfnt.NameIDPostScript); err == nil && name == postScript {
			return i
		}
	}
	return 0
}
