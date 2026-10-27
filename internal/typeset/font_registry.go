package typeset

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"

	"github.com/qizhanchan/qui/internal/gfx"
)

var (
	// RWMutex (not Mutex) because GetFontFacesFor takes only an RLock
	// on the cache-hit fast path. Hits dominate by orders of magnitude
	// once the chain is built — every text draw call goes through here.
	FontCacheMu sync.RWMutex
	FontCache   = map[fontCacheKey]font.Face{}
	fontChain   = map[fontCacheKey][]font.Face{}
	// FontInitialized tracks whether FontParsed has been populated —
	// either by the default lazy goregular init or by a prior
	// LoadFontFromFile / LoadSystemCJKFont call.
	FontInitialized bool
	FontParsed      *opentype.Font
	// FontUsingDefaultInit marks whether the active font came from the
	// lazy default init path in GetFontFacesFor (system CJK probe or
	// bundled goregular), as opposed to explicit app-level font loads.
	FontUsingDefaultInit bool
	// FontDefaultIsSystemCJK narrows FontUsingDefaultInit to the case
	// where the probe actually FOUND a system CJK font (rather than
	// falling through to goregular). populateAutoFallbacksUnlocked reads
	// it to reuse that font instead of loading the same file twice.
	FontDefaultIsSystemCJK bool
	// FontAutoLoadSystemCJK toggles whether lazy init probes system CJK
	// fonts before falling back to bundled goregular.
	FontAutoLoadSystemCJK = true
	// test hook for system CJK probing in lazy init.
	ProbeSystemCJKFontUnlocked = tryLoadCJKUnlocked
	// Keep font file handles alive for ParseReaderAt-backed fonts.
	fontPrimarySource   io.Closer
	FontFallbacks       []*opentype.Font
	FontFallbackSources []io.Closer
	// AutoFallbacks / autoFallbackSources are populated lazily on first
	// font access with a per-OS symbol font (covers chevrons, ⟳, etc.)
	// plus bundled goregular as a last-resort Latin catch-all. They are
	// appended to the per-glyph fallback chain AFTER user-registered
	// FontFallbacks, so explicit fallbacks always win on overlap. The
	// existing per-glyph dispatch in FaceForRune means each fallback is
	// only consulted for glyphs the higher-priority faces lack.
	AutoFallbacks       []*opentype.Font
	autoFallbackSources []io.Closer
	autoFallbacksInit   bool
	// test hook for symbol-font probing in lazy init.
	probeSymbolFontUnlocked = tryLoadSymbolFontUnlocked
	// test hook for Dingbats (✓ ✗ ➜) probing in lazy init.
	probeDingbatFontUnlocked = tryLoadDingbatFontUnlocked
	// test hook for complex-script (Arabic / Hebrew / Thai / Indic)
	// probing in lazy init.
	probeComplexScriptFontUnlocked = tryLoadComplexScriptFontUnlocked
	FontVariants                   = map[fontVariantKey]*opentype.Font{}
	fontVariantSources             = map[fontVariantKey]io.Closer{}
	// fontFamilyNames maps the lowercase family key to the display name
	// given at registration (drives RegisteredFontFamilies).
	fontFamilyNames = map[string]string{}
	// defaultWeightFonts holds bundled gofont weights (medium=500, bold=700)
	// keyed by FontWeight, so a Font with an empty Family but a non-normal
	// Weight resolves to the right glyphs without an explicit
	// LoadFontVariantFromBytes call. Populated lazily inside
	// populateAutoFallbacksUnlocked. The system CJK font (loaded as
	// FontParsed) does not provide weight axes, so this table is what
	// drives qui.ThemeFont(TextLabel)'s weight=500 request.
	defaultWeightFonts = map[gfx.FontWeight]*opentype.Font{}
	FontErr            error
)

type fontCacheKey struct {
	family   string
	size     float32
	weight   gfx.FontWeight
	italic   bool
	features string
	variants string
}

type fontVariantKey struct {
	family string
	weight gfx.FontWeight
	italic bool
}

// markFontOnceDone is called by font.go's installFont after it
// successfully loads a user font so the lazy init inside
// GetFontFace doesn't later overwrite it with goregular.
func markFontOnceDone() {
	FontInitialized = true
	FontUsingDefaultInit = false
	// The primary is now an app-supplied font, so the auto-fallback chain
	// must probe for its own CJK entry rather than aliasing the primary.
	FontDefaultIsSystemCJK = false
}

func GetFontFace(size float32) font.Face {
	return GetFontFaceFor(gfx.Font{Size: size})
}

// GetFontFaceFor resolves a font face for a full Font spec.
func GetFontFaceFor(spec gfx.Font) font.Face {
	faces := GetFontFacesFor(spec)
	if len(faces) == 0 {
		return basicfont.Face7x13
	}
	return faces[0]
}

// GetFontFaces returns primary + fallback faces for the requested size.
// Order is stable: primary first, then fallback registration order.
func GetFontFaces(size float32) []font.Face {
	return GetFontFacesFor(gfx.Font{Size: size})
}

// GetFontFacesFor returns primary + fallback faces for the requested
// font spec. Order is stable: primary first, then fallback registration
// order.
//
// The returned slice aliases the internal cache — callers must not
// mutate or append. Every call site today only iterates with `for _,
// face := range faces`, so handing back the cached slice (instead of
// a defensive copy) saves ~2 MB of allocations per 10s in a busy
// editor without changing observable behavior. If you need to keep a
// mutated copy, do `append([]font.Face(nil), faces...)` at the call
// site.
func GetFontFacesFor(spec gfx.Font) []font.Face {
	if spec.Size <= 0 {
		spec.Size = 14
	}
	key := makeFontCacheKey(spec)

	// Fast path: cache hit under a read lock. The cached chain is
	// set-once + read-many per (font spec, fallback set) — once the
	// font system is initialized and the chain is built, this is the
	// only branch the hot draw loop takes.
	FontCacheMu.RLock()
	if FontInitialized && FontErr == nil {
		if faces, ok := fontChain[key]; ok {
			FontCacheMu.RUnlock()
			return faces
		}
	}
	FontCacheMu.RUnlock()

	FontCacheMu.Lock()
	defer FontCacheMu.Unlock()
	if !FontInitialized {
		// Default: probe system CJK fonts first so non-Latin text
		// renders out of the box. Fall back to goregular when no
		// candidate is installed (Latin-only, but at least something
		// draws). Apps can still override via LoadFontFromFile /
		// LoadFontFromBytes before any Draw.
		FontDefaultIsSystemCJK = false
		if FontAutoLoadSystemCJK {
			if f, src := ProbeSystemCJKFontUnlocked(); f != nil {
				FontParsed = f
				FontErr = nil
				ReplacePrimaryFontSourceUnlocked(src)
				FontDefaultIsSystemCJK = true
			} else {
				FontParsed, FontErr = opentype.Parse(goregular.TTF)
				ReplacePrimaryFontSourceUnlocked(nil)
			}
		} else {
			FontParsed, FontErr = opentype.Parse(goregular.TTF)
			ReplacePrimaryFontSourceUnlocked(nil)
		}
		FontInitialized = true
		FontUsingDefaultInit = true
	}
	populateAutoFallbacksUnlocked()
	if FontErr != nil {
		return []font.Face{basicfont.Face7x13}
	}
	// Re-check after acquiring the write lock — another goroutine may
	// have built the chain between our read-unlock and write-lock.
	if faces, ok := fontChain[key]; ok {
		return faces
	}
	primaryParsed := SelectPrimaryFontUnlocked(spec)
	primary, err := opentype.NewFace(primaryParsed, &opentype.FaceOptions{
		Size: float64(spec.Size),
		DPI:  72,
		// Keep glyph advances scale-proportional across DPI factors so
		// widget-side text metrics (logical coordinates) stay aligned
		// with the HiDPI draw path (scaled font size).
		Hinting: font.HintingNone,
	})
	if err != nil {
		return []font.Face{basicfont.Face7x13}
	}
	chain := []font.Face{primary}
	appendFallback := func(parsed *opentype.Font) {
		if parsed == nil || parsed == primaryParsed {
			return
		}
		fallback, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size:    float64(spec.Size),
			DPI:     72,
			Hinting: font.HintingNone,
		})
		if err != nil {
			return
		}
		if parsed == UISymbolFallback.Load() {
			chain = append(chain, UISymbolFace{fallback})
			return
		}
		chain = append(chain, fallback)
	}
	for _, parsed := range FontFallbacks {
		appendFallback(parsed)
	}
	// Auto fallbacks run last so explicit user fallbacks always win for
	// glyphs both can render. Per-glyph dispatch in FaceForRune skips
	// these entries when the primary or a user fallback already covers
	// the codepoint, so the cost on the hot path is one map lookup per
	// rune until a hit.
	for _, parsed := range AutoFallbacks {
		appendFallback(parsed)
	}
	FontCache[key] = primary
	fontChain[key] = chain
	return chain
}

// populateAutoFallbacksUnlocked seeds the per-glyph fallback chain with
// (a) the OS-best symbol font (Apple Symbols on macOS, Noto Sans Symbols
// 2 / DejaVu on Linux, Segoe UI Symbol on Windows) and (b) bundled
// goregular for Latin / chevron coverage. Idempotent — runs once per
// process. The symbol-font probe is best-effort; goregular is always
// available because it's compiled into the binary.
//
// Caller must hold FontCacheMu.
func populateAutoFallbacksUnlocked() {
	if autoFallbacksInit {
		return
	}
	autoFallbacksInit = true
	// Keyboard symbols (⌘ ⇧ ⌥ ...) from the platform UI face, ahead of the
	// general symbol font whose versions are drawn on a smaller em. Only
	// consulted for those runes — see font_uisymbols.go.
	if f, src := probeUISymbolFontUnlocked(); f != nil {
		AutoFallbacks = append(AutoFallbacks, f)
		autoFallbackSources = append(autoFallbackSources, src)
		UISymbolFallback.Store(f)
	}
	if f, src := probeSymbolFontUnlocked(); f != nil {
		AutoFallbacks = append(AutoFallbacks, f)
		autoFallbackSources = append(autoFallbackSources, src)
	}
	if f, src := probeDingbatFontUnlocked(); f != nil {
		AutoFallbacks = append(AutoFallbacks, f)
		autoFallbackSources = append(autoFallbackSources, src)
	}
	if f, err := opentype.Parse(goregular.TTF); err == nil {
		AutoFallbacks = append(AutoFallbacks, f)
		autoFallbackSources = append(autoFallbackSources, nil)
	}
	// System CJK font as a fallback (not just as the lazy-init primary).
	// In the default setup the CJK font IS the primary and covers Latin +
	// CJK, so this is redundant. But once an app swaps the primary to a
	// Latin-only face — e.g. SetDefaultFont / jetbrainsmono.Use() — the
	// primary no longer covers Chinese/Japanese/Korean, and without a CJK
	// entry in the fallback chain those glyphs render as tofu. Keeping CJK
	// here means a font swap never silently drops CJK coverage. Gated on
	// the same auto-load toggle so apps that opted out don't pay the probe.
	if FontAutoLoadSystemCJK {
		if FontDefaultIsSystemCJK && FontParsed != nil {
			// The lazy init above already probed the system CJK font into
			// the primary slot. Reuse that POINTER rather than calling the
			// probe again: a second call re-reads and re-parses the same
			// file, and a system CJK collection is tens of megabytes (its
			// bytes stay resident for the font's lifetime, so the duplicate
			// was resident too). appendFallback drops this entry while it
			// is still the primary, and it starts counting the moment an
			// app swaps the primary to a Latin-only face — which is the
			// only reason the entry exists.
			//
			// The source stays nil: the handle belongs to
			// fontPrimarySource, and recording it twice would close it
			// twice on reset.
			AutoFallbacks = append(AutoFallbacks, FontParsed)
			autoFallbackSources = append(autoFallbackSources, nil)
		} else if f, src := ProbeSystemCJKFontUnlocked(); f != nil {
			AutoFallbacks = append(AutoFallbacks, f)
			autoFallbackSources = append(autoFallbackSources, src)
		}
	}
	// Complex-script coverage (Arabic / Hebrew / Thai / Indic). Neither
	// the bundled Latin face nor a CJK face carries these, so without
	// this entry an app that switches to Arabic renders a screen of tofu
	// while bidi, shaping and RTL alignment all work correctly on glyphs
	// that do not exist. Gated on the same toggle as the CJK probe: an
	// app that opted out of automatic system-font probing gets neither.
	if FontAutoLoadSystemCJK {
		if f, src := probeComplexScriptFontUnlocked(); f != nil {
			AutoFallbacks = append(AutoFallbacks, f)
			autoFallbackSources = append(autoFallbackSources, src)
		}
	}
	if len(defaultWeightFonts) == 0 {
		if f, err := opentype.Parse(gomedium.TTF); err == nil {
			defaultWeightFonts[gfx.FontWeightMedium] = f
		}
		if f, err := opentype.Parse(gobold.TTF); err == nil {
			defaultWeightFonts[gfx.FontWeightBold] = f
		}
	}
}

// setDefaultWeightFontUnlocked overrides (or, with f == nil, removes) the
// bundled-weight face used to satisfy empty-Family + non-normal-weight
// lookups. SetDefaultFont calls this so a swapped-in family's Medium /
// Bold typescales resolve to that family's own weights instead of the
// Go font's gomedium / gobold. Caller must hold FontCacheMu.
func setDefaultWeightFontUnlocked(weight gfx.FontWeight, f *opentype.Font) {
	// Ensure the bundled defaults are seeded first so a partial override
	// (e.g. only Medium supplied) doesn't leave Bold pointing at nothing.
	populateAutoFallbacksUnlocked()
	if f == nil {
		delete(defaultWeightFonts, weight)
		return
	}
	defaultWeightFonts[weight] = f
}

// ClearAutoFallbacksUnlocked drops auto-installed fallbacks and closes
// any file-backed sources. Used by tests that swap out the symbol-font
// probe; production code never calls this.
func ClearAutoFallbacksUnlocked() {
	for _, src := range autoFallbackSources {
		CloseCloser(src)
	}
	AutoFallbacks = nil
	autoFallbackSources = nil
	autoFallbacksInit = false
	UISymbolFallback.Store(nil)
	uiSymbolShapingFont.Store(nil)
	ClearFontFaceCachesUnlocked()
}

func SelectPrimaryFontUnlocked(spec gfx.Font) *opentype.Font {
	// No family + non-normal weight: consult the bundled-weight table so
	// MD3 Label/Title typescales (weight 500) actually pick up gomedium
	// instead of falling through to the regular default. goregular's
	// "Weight" axis doesn't exist, so without this we silently lose the
	// visual weight distinction.
	defaultFace := func() *opentype.Font {
		if w := spec.EffectiveWeight(); w != gfx.FontWeightNormal {
			if f, ok := defaultWeightFonts[w]; ok && f != nil {
				return f
			}
		}
		return FontParsed
	}
	family := strings.TrimSpace(spec.Family)
	if family == "" {
		return defaultFace()
	}
	want := fontVariantKey{
		family: strings.ToLower(family),
		weight: spec.EffectiveWeight(),
		italic: spec.Italic,
	}
	if f, ok := FontVariants[want]; ok && f != nil {
		return f
	}
	// Nearest fallback: same family and closest weight, prefer exact italic.
	// A family with no registered faces at all (a CSS generic like
	// sans-serif, a font the app never loaded) is the default family, so
	// it keeps the requested weight rather than collapsing to regular.
	best := defaultFace()
	bestScore := int(^uint(0) >> 1)
	for k, f := range FontVariants {
		if f == nil || k.family != want.family {
			continue
		}
		score := absInt(int(k.weight) - int(want.weight))
		if k.italic != want.italic {
			score += 1000
		}
		if score < bestScore {
			bestScore = score
			best = f
		}
	}
	return best
}

func makeFontCacheKey(spec gfx.Font) fontCacheKey {
	return fontCacheKey{
		family:   strings.ToLower(strings.TrimSpace(spec.Family)),
		size:     spec.Size,
		weight:   spec.EffectiveWeight(),
		italic:   spec.Italic,
		features: canonicalFeatureKey(spec.Features),
		variants: canonicalVariationKey(spec.Variations),
	}
}

func canonicalFeatureKey(features []string) string {
	if len(features) == 0 {
		return ""
	}
	cp := append([]string(nil), features...)
	sort.Strings(cp)
	return strings.Join(cp, ",")
}

func canonicalVariationKey(vars map[string]float32) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(fmt.Sprintf("%g", vars[k]))
	}
	return b.String()
}

// RawFontBytesFor returns the retained bytes for f, or falls back to
// sfnt.WriteSourceTo (single-font sources), or nil.
func RawFontBytesFor(f *opentype.Font) []byte {
	if b := lookupRawFontBytes(f); b != nil {
		return b
	}
	if b, err := rawFontBytes(f); err == nil {
		return b
	}
	return nil
}

// FontChainForSpec returns the parallel *opentype.Font chain (primary +
// user fallbacks + auto fallbacks) matching GetFontFacesFor's face chain.
// This is the authoritative rune->font mapping the PDF text path uses.
func FontChainForSpec(spec gfx.Font) []*opentype.Font {
	// Ensure the font system is initialized and auto-fallbacks populated.
	GetFontFacesFor(spec)

	FontCacheMu.Lock()
	defer FontCacheMu.Unlock()
	primary := SelectPrimaryFontUnlocked(spec)
	var chain []*opentype.Font
	seen := map[*opentype.Font]bool{}
	add := func(f *opentype.Font) {
		if f == nil || seen[f] {
			return
		}
		seen[f] = true
		chain = append(chain, f)
	}
	add(primary)
	for _, f := range FontFallbacks {
		add(f)
	}
	for _, f := range AutoFallbacks {
		add(f)
	}
	return chain
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// rawFontBytes recovers the original font program bytes. For single-font
// sources this uses sfnt.WriteSourceTo; collection-backed fonts (.ttc) are
// handled by the raw-bytes registry populated at install time.
func rawFontBytes(f *opentype.Font) ([]byte, error) {
	if b := lookupRawFontBytes(f); b != nil {
		return b, nil
	}
	var buf bytes.Buffer
	if _, err := f.WriteSourceTo(nil, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ResolveRune picks the first font in the chain that has a glyph for r,
// returning that font, its GID, and the advance in font design units. If no
// font covers r, it returns the primary font with GID 0 (notdef).
func ResolveRune(chain []*opentype.Font, r rune, buf *sfnt.Buffer) (*opentype.Font, sfnt.GlyphIndex, int) {
	for _, f := range chain {
		if FallbackSkipsRune(f, r) {
			continue
		}
		gid, err := f.GlyphIndex(buf, r)
		if err != nil || gid == 0 {
			continue
		}
		return f, gid, GlyphAdvanceUnits(f, gid, buf)
	}
	if len(chain) > 0 {
		f := chain[0]
		return f, 0, GlyphAdvanceUnits(f, 0, buf)
	}
	return nil, 0, 0
}

// GlyphAdvanceUnits returns a glyph's advance in font design units. Using
// ppem == unitsPerEm makes the returned pixel advance equal the design-unit
// advance.
func GlyphAdvanceUnits(f *opentype.Font, gid sfnt.GlyphIndex, buf *sfnt.Buffer) int {
	upem := int(f.UnitsPerEm())
	if upem <= 0 {
		upem = 1000
	}
	adv, err := f.GlyphAdvance(buf, gid, fixed.I(upem), font.HintingNone)
	if err != nil {
		return 0
	}
	return adv.Round()
}
