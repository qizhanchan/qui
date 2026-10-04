package qui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"

	"golang.org/x/image/font/opentype"
)

// Font loading — lets apps replace the default font with one that
// covers the scripts they need.
//
// Default: on first Draw, qui probes the per-OS CJK font candidates
// (see cjkFontCandidates) and installs the first that parses, so
// Chinese / Japanese / Korean render out of the box. When no system
// CJK font is present it falls back to bundled goregular (Latin only).
//
// Override explicitly when bundling your own font:
//
//	err := qui.LoadFontFromFile("/System/Library/Fonts/PingFang.ttc")
//	err := qui.LoadFontFromBytes(myEmbeddedTTF)
//	_, _ = qui.LoadSystemCJKFont()  // force the CJK probe early
//
// Call before constructing widgets that draw text. Later calls swap
// the font atomically and invalidate the size-keyed face cache so the
// next draw re-rasterizes with the new font.

// SetAutoLoadSystemCJK toggles lazy system CJK probing on first font use.
//
// Enabled (default): first text measurement/draw tries known per-OS CJK
// system fonts before falling back to bundled goregular.
//
// Disabled: lazy init skips CJK probing and uses bundled goregular directly.
//
// If the current active font came from the default lazy-init path, changing
// this switch resets that default selection and re-initializes on next text
// access. Explicit app-installed fonts are not replaced.
func SetAutoLoadSystemCJK(enabled bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if fontAutoLoadSystemCJK == enabled {
		return
	}
	fontAutoLoadSystemCJK = enabled
	if !fontInitialized || !fontUsingDefaultInit {
		return
	}
	// Re-run default lazy init with the new policy on next text access.
	fontInitialized = false
	fontUsingDefaultInit = false
	fontDefaultIsSystemCJK = false
	fontParsed = nil
	fontErr = nil
	replacePrimaryFontSourceUnlocked(nil)
	clearFontFaceCachesUnlocked()
}

// AutoLoadSystemCJKEnabled reports whether lazy init currently probes
// system CJK fonts before falling back to bundled goregular.
func AutoLoadSystemCJKEnabled() bool {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	return fontAutoLoadSystemCJK
}

// LoadFontFromFile replaces the default font with one loaded from the
// given path. Accepts single-font files (.ttf / .otf) and collections
// (.ttc / .otc — the first face of the collection is used, which for
// most system collections like PingFang.ttc is the primary/SC face).
func LoadFontFromFile(path string) error {
	f, src, err := parseFirstFontFromFile(path)
	if err != nil {
		return fmt.Errorf("qui: load font %q: %w", path, err)
	}
	installFontWithSource(f, src)
	return nil
}

// LoadFontFromBytes replaces the default font with an in-memory font.
// Useful for embedding a font via go:embed so the app doesn't depend
// on system fonts being present.
func LoadFontFromBytes(data []byte) error {
	return loadFontBytes(data)
}

// loadFontBytes tries single-font Parse first (faster path for .ttf),
// falls back to ParseCollection on failure (for .ttc). On success it
// swaps the package-level font and clears the size cache under lock,
// so subsequent Draw calls use the new font even if cached faces
// existed.
func loadFontBytes(data []byte) error {
	f, err := parseFirstFont(data)
	if err != nil {
		return err
	}
	installFontWithSource(f, nil)
	return nil
}

func parseFirstFont(data []byte) (*opentype.Font, error) {
	if f, err := opentype.Parse(data); err == nil {
		// Retain the raw bytes so the PDF backend can embed/subset this
		// font even when sfnt.WriteSourceTo can't recover them.
		rememberRawFontBytes(f, data)
		return f, nil
	}
	coll, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, fmt.Errorf("qui: font data is neither a TTF nor a TTC: %w", err)
	}
	if coll.NumFonts() == 0 {
		return nil, errors.New("qui: font collection is empty")
	}
	f, err := coll.Font(0)
	if err != nil {
		return nil, fmt.Errorf("qui: font collection face 0: %w", err)
	}
	// Retain the whole collection; the PDF subsetter extracts face 0 from it.
	rememberRawFontBytes(f, data)
	return f, nil
}

func parseFirstFontReaderAt(src io.ReaderAt) (*opentype.Font, error) {
	if f, err := opentype.ParseReaderAt(src); err == nil {
		return f, nil
	}
	coll, err := opentype.ParseCollectionReaderAt(src)
	if err != nil {
		return nil, fmt.Errorf("qui: font source is neither a TTF nor a TTC: %w", err)
	}
	if coll.NumFonts() == 0 {
		return nil, errors.New("qui: font collection is empty")
	}
	f, err := coll.Font(0)
	if err != nil {
		return nil, fmt.Errorf("qui: font collection face 0: %w", err)
	}
	return f, nil
}

func parseFirstFontFromFile(path string) (*opentype.Font, io.Closer, error) {
	// Probe through the file HANDLE first. A ReaderAt-backed parse pulls in
	// the table directory and the tables initialize() validates, and nothing
	// else — so a candidate sfnt cannot parse costs a few KB of reads
	// instead of its whole file. That is what makes the font-candidate scans
	// affordable: on a stock macOS install the CJK probe walks past STHeiti
	// Medium and STHeiti Light — 56 MB each, both rejected on their cmap —
	// before it reaches one that parses.
	probe, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	_, err = parseFirstFontReaderAt(probe)
	closeCloser(probe)
	if err != nil {
		return nil, nil, err
	}

	// It parses, so the file is worth reading in full. Keep the font backed
	// by MEMORY rather than by the handle: a ReaderAt-backed sfnt.Font does
	// a ReadAt per glyph load, and faceForRune / GlyphAdvance sit on the
	// text-measurement path. The bytes double as the raw program the shaper
	// and the PDF embedder ask for later (see rememberRawFontBytes) — sfnt
	// retains them for the font's lifetime either way.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	// Prefer face 0 alone when this is a collection. sfnt keeps the bytes it
	// parsed for the font's lifetime, and qui only ever uses face 0, so a
	// collection otherwise charges the process for every weight it bundles:
	// Hiragino Sans GB is 23.5 MB on disk and 11.6 MB as face 0.
	if slim := extractFace0(data); len(slim) < len(data) {
		if f, err := parseFirstFont(slim); err == nil {
			return f, nil, nil
		}
		// The rewrite did not survive a re-parse. Fall through to the
		// original bytes rather than lose a font that does work.
	}
	f, err := parseFirstFont(data)
	if err != nil {
		return nil, nil, err
	}
	return f, nil, nil
}

// extractFace0 rewrites a .ttc/.otc collection down to a standalone font
// holding face 0's tables, and returns data unchanged for anything else
// (single fonts, collections it cannot rewrite, rewrites that did not come
// out smaller). GIDs are preserved — the tables are copied verbatim — so
// the result measures and rasterizes identically to face 0 of the original.
func extractFace0(data []byte) []byte {
	if len(data) < 4 || string(data[0:4]) != "ttcf" {
		return data
	}
	version, tables, err := parseSFNTTables(data)
	if err != nil {
		return data
	}
	out := writeSFNT(version, tables)
	if len(out) == 0 || len(out) >= len(data) {
		return data
	}
	return out
}

func installFontWithSource(f *opentype.Font, src io.Closer) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	fontParsed = f
	fontErr = nil
	replacePrimaryFontSourceUnlocked(src)
	// Mark the once done so GetFontFace doesn't re-init with goregular.
	// (If LoadFontFromFile is called before any Draw, fontOnce hasn't
	// run yet — we need to short-circuit it.)
	markFontOnceDone()
	clearFontFaceCachesUnlocked()
}

func closeCloser(closer io.Closer) {
	if closer == nil {
		return
	}
	_ = closer.Close()
}

func replacePrimaryFontSourceUnlocked(src io.Closer) {
	if fontPrimarySource == src {
		return
	}
	closeCloser(fontPrimarySource)
	fontPrimarySource = src
}

// fontRegistryGen changes whenever the font registry does. Layout results
// embed face metrics, so anything caching a shaped layout has to notice a
// registry swap — the global memo below is cleared outright, but per-widget
// caches (widgets.Label's rich-text layout, widgets.InlineBox's paragraph)
// live outside this package and can only watch a counter. Include
// FontRegistryGeneration() in such a cache's key.
var fontRegistryGen atomic.Uint64

// FontRegistryGeneration returns a counter that changes every time the font
// registry is mutated (SetDefaultFont, fallback registration, a GL renderer
// reset). Cheap enough to read on every cache lookup.
func FontRegistryGeneration() uint64 { return fontRegistryGen.Load() }

func clearFontFaceCachesUnlocked() {
	fontRegistryGen.Add(1)
	for k := range fontCache {
		delete(fontCache, k)
	}
	for k := range fontChain {
		delete(fontChain, k)
	}
	// Layout results embed face metrics — a registry change invalidates
	// every memoized measurement.
	invalidateTextLayoutCache()
	invalidateShapingFontCache()
}

// FontFace bundles one weight/style of a font family for SetDefaultFont.
// Data is a single-font .ttf / .otf (not a collection).
type FontFace struct {
	Weight FontWeight // 0 is treated as FontWeightNormal
	Italic bool
	Data   []byte
}

// SetDefaultFont swaps the entire global default font family in one call.
//
// It installs `regular` as the primary face (same effect as
// LoadFontFromBytes) AND registers every face in `weights` so that the
// theme text roles — which request weight 500 (Medium) and 700 (Bold)
// with an empty Family — resolve to THIS family's own weights instead of
// silently falling back to the bundled Go font's gomedium / gobold. That
// mixed-family fallback is the trap callers hit when they only swap the
// regular face: body text changes but every bold heading stays Go-font.
//
// Pass the Regular bytes as `regular`, and the Medium / Bold (+ italics)
// faces in `weights`. Faces with Italic == true don't feed the default-
// weight table (the empty-Family typescales are all upright); they're
// still registered as named variants so a Style with Italic set picks
// them up. The family is also registered under `family` so callers can
// target it explicitly via Font{Family: family}.
//
// CJK still renders: the system CJK font (PingFang / Noto / YaHei) stays
// in the per-glyph fallback chain, so swapping in a Latin-only family
// like JetBrains Mono does NOT turn Chinese/Japanese/Korean into tofu —
// Latin draws in the new family, CJK falls back automatically. (Apps
// that called SetAutoLoadSystemCJK(false) opt out of that probe and must
// register their own CJK fallback via LoadFallbackFontFromFile.)
//
// Call once at startup before constructing widgets. Example with the
// bundled JetBrains Mono lives in the fonts/jetbrainsmono subpackage:
//
//	fonts.UseJetBrainsMono() // one-liner wrapping SetDefaultFont
func SetDefaultFont(family string, regular []byte, weights ...FontFace) error {
	reg, err := parseFirstFont(regular)
	if err != nil {
		return fmt.Errorf("qui: SetDefaultFont regular face: %w", err)
	}
	family = strings.TrimSpace(family)
	famKey := strings.ToLower(family)

	// Parse every weighted face up front so a bad byte slice fails the
	// whole call atomically rather than half-installing.
	type parsedFace struct {
		key fontVariantKey
		f   *opentype.Font
	}
	parsed := make([]parsedFace, 0, len(weights)+1)
	if famKey != "" {
		parsed = append(parsed, parsedFace{
			key: fontVariantKey{family: famKey, weight: FontWeightNormal, italic: false},
			f:   reg,
		})
	}
	for _, w := range weights {
		if len(w.Data) == 0 {
			continue
		}
		f, err := parseFirstFont(w.Data)
		if err != nil {
			return fmt.Errorf("qui: SetDefaultFont weight %d (italic=%v): %w", w.Weight, w.Italic, err)
		}
		wt := w.Weight
		if wt == 0 {
			wt = FontWeightNormal
		}
		if famKey != "" {
			parsed = append(parsed, parsedFace{
				key: fontVariantKey{family: famKey, weight: wt, italic: w.Italic},
				f:   f,
			})
		}
		// Upright faces drive the empty-Family typescale weight table.
		if !w.Italic {
			parsed = append(parsed, parsedFace{
				key: fontVariantKey{family: "", weight: wt, italic: false},
				f:   f,
			})
		}
	}

	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	// Primary face.
	fontParsed = reg
	fontErr = nil
	replacePrimaryFontSourceUnlocked(nil)
	markFontOnceDone()
	// Make the new regular the empty-Family normal weight too, so a plain
	// Font{} (no weight) and a weight-400 request both land on it.
	setDefaultWeightFontUnlocked(FontWeightNormal, reg)
	if famKey != "" {
		if _, ok := fontFamilyNames[famKey]; !ok {
			fontFamilyNames[famKey] = family
		}
	}
	for _, p := range parsed {
		if p.key.family == "" {
			setDefaultWeightFontUnlocked(p.key.weight, p.f)
		} else {
			if old := fontVariantSources[p.key]; old != nil {
				closeCloser(old)
				delete(fontVariantSources, p.key)
			}
			fontVariants[p.key] = p.f
		}
	}
	clearFontFaceCachesUnlocked()
	return nil
}

// LoadFontVariantFromBytes registers a variant from in-memory font data.
func LoadFontVariantFromBytes(family string, weight FontWeight, italic bool, data []byte) error {
	display := strings.TrimSpace(family)
	famKey := strings.ToLower(display)
	if famKey == "" {
		return errors.New("qui: variant family must not be empty")
	}
	f, err := parseFirstFont(data)
	if err != nil {
		return err
	}
	if weight == 0 {
		weight = FontWeightNormal
	}
	key := fontVariantKey{family: famKey, weight: weight, italic: italic}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if old := fontVariantSources[key]; old != nil {
		closeCloser(old)
		delete(fontVariantSources, key)
	}
	fontVariants[key] = f
	if _, ok := fontFamilyNames[famKey]; !ok {
		fontFamilyNames[famKey] = display
	}
	clearFontFaceCachesUnlocked()
	return nil
}

// LoadFontVariantFromFile registers a family variant straight from a font
// file on disk (TTF/OTF/TTC — the first font of a collection is used).
// Use it to expose system-installed families to Font{Family: ...} without
// embedding them:
//
//	qui.LoadFontVariantFromFile("Georgia", qui.FontWeightNormal, false,
//	    "/System/Library/Fonts/Supplemental/Georgia.ttf")
func LoadFontVariantFromFile(family string, weight FontWeight, italic bool, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("qui: load font variant %q: %w", path, err)
	}
	return LoadFontVariantFromBytes(family, weight, italic, data)
}

// RegisteredFontFamilies lists the display names of every font family
// registered via SetDefaultFont / LoadFontVariant*, sorted alphabetically.
// Font pickers build their choices from this; the empty default family is
// not included.
func RegisteredFontFamilies() []string {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	out := make([]string, 0, len(fontFamilyNames))
	for _, name := range fontFamilyNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// FontFamilyRegistered reports whether name resolves to a registered
// family (case-insensitive). Importers use it to decide whether a family
// recorded in a document can be honored or should fall back to the
// default — an unknown name would silently render as the default anyway.
func FontFamilyRegistered(name string) bool {
	if name == "" {
		return false
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	_, ok := fontFamilyNames[strings.ToLower(name)]
	return ok
}

// ClearFontVariants removes all registered style variants.
func ClearFontVariants() {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	for k, src := range fontVariantSources {
		closeCloser(src)
		delete(fontVariantSources, k)
	}
	for k := range fontVariants {
		delete(fontVariants, k)
	}
	for k := range fontFamilyNames {
		delete(fontFamilyNames, k)
	}
	clearFontFaceCachesUnlocked()
}

// LoadFallbackFontFromFile appends a fallback font to the face chain.
// Fallbacks are consulted only when the primary font has no glyph.
func LoadFallbackFontFromFile(path string) error {
	f, src, err := parseFirstFontFromFile(path)
	if err != nil {
		return fmt.Errorf("qui: load fallback font %q: %w", path, err)
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	fontFallbacks = append(fontFallbacks, f)
	fontFallbackSources = append(fontFallbackSources, src)
	clearFontFaceCachesUnlocked()
	return nil
}

// tryLoadCJKUnlocked scans cjkFontCandidates and returns the first
// successfully parsed font, or nil if none are readable / parseable.
// Caller must hold fontCacheMu — used by the lazy init path in
// GetFontFace where we're already under the lock and can't re-enter
// via installFont.
func tryLoadCJKUnlocked() (*opentype.Font, io.Closer) {
	for _, path := range cjkFontCandidates() {
		f, src, err := parseFirstFontFromFile(path)
		if err == nil {
			return f, src
		}
	}
	return nil, nil
}

// tryLoadSymbolFontUnlocked scans symbolFontCandidates for a font that
// covers common UI symbol glyphs (chevrons, arrows, mathematical symbols
// like ⟳ U+27F3). Used by the lazy init to seed an automatic fallback
// chain so widgets can use symbol characters without every app having
// to ship its own fallback font. Caller must hold fontCacheMu.
func tryLoadSymbolFontUnlocked() (*opentype.Font, io.Closer) {
	for _, path := range symbolFontCandidates() {
		f, src, err := parseFirstFontFromFile(path)
		if err == nil {
			return f, src
		}
	}
	return nil, nil
}

// complexScriptFontCandidates returns per-OS fonts covering the scripts
// that neither the bundled Latin face nor a CJK face provides: Arabic,
// Hebrew, Thai and the Indic family.
//
// Why this exists as a separate probe from cjkFontCandidates: a CJK font
// gives you Latin + Han + Kana + Hangul and nothing else. Without an
// entry here, an app whose locale switches to Arabic or Hebrew renders
// an entire screen of tofu boxes — bidi, shaping and RTL alignment all
// working perfectly on glyphs that do not exist. That failure mode is
// invisible until someone actually runs the app in one of those
// languages, which is exactly the sort of bug i18n support is supposed
// to prevent.
//
// Ordered widest-coverage first. Each path is tried until one parses;
// the result joins the per-glyph fallback chain, so it is only consulted
// for runes the primary face lacks.
func complexScriptFontCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/GeezaPro.ttc",              // Arabic (+ Persian, Urdu)
			"/System/Library/Fonts/SFArabic.ttf",              // Arabic, modern UI face
			"/System/Library/Fonts/SFHebrew.ttf",              // Hebrew
			"/System/Library/Fonts/Supplemental/Thonburi.ttc", // Thai
			"/System/Library/Fonts/Supplemental/Kohinoor.ttc", // Devanagari
		}
	case "linux":
		return []string{
			"/usr/share/fonts/truetype/noto/NotoSansArabic-Regular.ttf",
			"/usr/share/fonts/google-noto/NotoSansArabic-Regular.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", // Arabic + Hebrew, partial
			"/usr/share/fonts/truetype/noto/NotoSansHebrew-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansThai-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansDevanagari-Regular.ttf",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\arial.ttf`,   // Arabic + Hebrew coverage
			`C:\Windows\Fonts\tahoma.ttf`,  // Arabic + Hebrew + Thai
			`C:\Windows\Fonts\segoeui.ttf`, // broad UI coverage
			`C:\Windows\Fonts\nirmala.ttf`, // Indic
		}
	}
	return nil
}

// tryLoadComplexScriptFontUnlocked scans complexScriptFontCandidates and
// returns the first font that parses. Caller must hold fontCacheMu.
func tryLoadComplexScriptFontUnlocked() (*opentype.Font, io.Closer) {
	for _, path := range complexScriptFontCandidates() {
		f, src, err := parseFirstFontFromFile(path)
		if err == nil {
			return f, src
		}
	}
	return nil, nil
}

// LoadSystemCJKFont tries a handful of well-known system font paths,
// in priority order per OS, and installs the first one that parses.
// Returns the path used, or an error if nothing worked. Safe to call
// after some widgets have been drawn — the cache is invalidated.
//
// macOS: PingFang → STHeiti → Hiragino Sans GB
// Linux: Noto Sans CJK (various distros)
// Windows: Microsoft YaHei → SimHei
func LoadSystemCJKFont() (string, error) {
	candidates := cjkFontCandidates()
	var lastErr error
	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := LoadFontFromFile(path); err != nil {
			lastErr = err
			continue
		}
		return path, nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("qui: all CJK font candidates failed; last error: %w", lastErr)
	}
	return "", errors.New("qui: no CJK font found at known system paths")
}

// symbolFontCandidates returns per-OS paths to fonts with broad UI
// symbol coverage (chevrons, arrows, geometric shapes, miscellaneous
// mathematical symbols). These are tried in priority order during lazy
// init and the first one that parses is registered as an automatic
// fallback. Missing fonts are silently skipped — the bundled goregular
// always catches Latin/punctuation as a last resort.
func symbolFontCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			// Covers ‹ › « » ⟳ ↻ ↺ ⟲ plus the entire Misc Symbols /
			// Arrows / Geometric Shapes blocks. Ships on every macOS.
			"/System/Library/Fonts/Apple Symbols.ttf",
			// Broad Unicode coverage if Apple Symbols is missing on a
			// stripped install.
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		}
	case "linux":
		return []string{
			"/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf",
			"/usr/share/fonts/opentype/noto/NotoSansSymbols2-Regular.ttf",
			"/usr/share/fonts/google-noto/NotoSansSymbols2-Regular.ttf",
			"/usr/share/fonts/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\seguisym.ttf`, // Segoe UI Symbol
			`C:\Windows\Fonts\arialuni.ttf`, // Arial Unicode MS
		}
	}
	return nil
}

// cjkFontCandidates returns per-OS candidate paths in priority order.
// Paths are tried with os.Stat first, then LoadFontFromFile, so
// ordering captures preference (better-rendering first) and
// availability tolerance (missing file is not an error, next candidate
// tried).
func cjkFontCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/PingFang.ttc", // preferred — modern SC/TC/HK
			"/System/Library/Fonts/STHeiti Medium.ttc",
			"/System/Library/Fonts/STHeiti Light.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/Library/Fonts/Songti.ttc",
		}
	case "linux":
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/wqy-microhei/wqy-microhei.ttc",
			"/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\msyh.ttc`, // Microsoft YaHei
			`C:\Windows\Fonts\msyh.ttf`,
			`C:\Windows\Fonts\simhei.ttf`, // SimHei
			`C:\Windows\Fonts\simsun.ttc`,
		}
	}
	return nil
}
