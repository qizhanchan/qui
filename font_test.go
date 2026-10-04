package qui

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func TestLoadFontFromBytesTTF(t *testing.T) {
	err := LoadFontFromBytes(goregular.TTF)
	if err != nil {
		t.Fatalf("load TTF failed: %v", err)
	}
	// Face lookup should now succeed against the newly installed font.
	face := GetFontFace(14)
	if face == nil {
		t.Error("face is nil after install")
	}
}

func TestLoadFontFromBytesRejectsGarbage(t *testing.T) {
	err := LoadFontFromBytes([]byte("not a font at all"))
	if err == nil {
		t.Error("expected error on garbage input")
	}
}

func TestLoadFontFromFileMissingPath(t *testing.T) {
	err := LoadFontFromFile("/definitely/does/not/exist/font.ttf")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFontFromFileTTC(t *testing.T) {
	// Only meaningful on macOS where we know .ttc paths exist; skip
	// elsewhere. PingFang is the canonical CJK TTC.
	if runtime.GOOS != "darwin" {
		t.Skip("ttc test runs on darwin only")
	}
	path := "/System/Library/Fonts/PingFang.ttc"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("PingFang not available: %v", err)
	}
	if err := LoadFontFromFile(path); err != nil {
		t.Fatalf("load ttc: %v", err)
	}
	// Restore goregular so other tests aren't affected by the swap.
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func TestInstallFontClearsCache(t *testing.T) {
	// Warm the cache with the current font.
	_ = GetFontFace(14)
	_ = GetFontFace(16)
	fontCacheMu.Lock()
	if len(fontCache) == 0 {
		fontCacheMu.Unlock()
		t.Fatal("precondition: cache should have warmed entries")
	}
	fontCacheMu.Unlock()

	// Re-install; cache must be cleared.
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if len(fontCache) != 0 {
		t.Errorf("cache not cleared after install; size = %d", len(fontCache))
	}
}

func TestLoadSystemCJKFontAtLeastReturnsErrOrPath(t *testing.T) {
	// Best-effort — the function either returns a path it loaded or
	// an error. Neither is a test failure; we're just making sure it
	// doesn't panic or corrupt state.
	path, err := LoadSystemCJKFont()
	if err != nil && path != "" {
		t.Errorf("inconsistent return: err=%v path=%q", err, path)
	}
	// Restore goregular to keep the test env consistent.
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func TestCJKFontCandidatesNonEmptyOnKnownOS(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skipf("no candidates expected on %s", runtime.GOOS)
	}
	if len(cjkFontCandidates()) == 0 {
		t.Errorf("expected candidates for %s, got none", runtime.GOOS)
	}
}

func TestFontEffectiveWeightCompat(t *testing.T) {
	if got := (Font{Bold: true}).effectiveWeight(); got != FontWeightBold {
		t.Fatalf("Bold compatibility weight = %d, want %d", got, FontWeightBold)
	}
	if got := (Font{Weight: FontWeightLight, Bold: true}).effectiveWeight(); got != FontWeightLight {
		t.Fatalf("explicit weight should win, got %d", got)
	}
}

func TestLoadFontVariantFromBytes(t *testing.T) {
	ClearFontVariants()
	if err := LoadFontVariantFromBytes("demo", FontWeightBold, true, goregular.TTF); err != nil {
		t.Fatalf("register variant: %v", err)
	}
	fontCacheMu.Lock()
	if len(fontVariants) == 0 {
		fontCacheMu.Unlock()
		t.Fatal("expected font variant registration")
	}
	fontCacheMu.Unlock()
	face := GetFontFaceFor(Font{Family: "demo", Size: 14, Weight: FontWeightBold, Italic: true})
	if face == nil {
		t.Fatal("variant face is nil")
	}
	ClearFontVariants()
}

// TestAutoFallbackCoversCommonSymbols pins the invariant that the
// default font chain resolves UI symbol glyphs out of the box, even
// when the primary face (CJK probe result or goregular) is missing
// them. Bundled goregular covers ‹ › « » regardless of OS; the per-OS
// symbol-font probe handles less common chars like ⟳ on platforms
// that ship Apple Symbols / Noto Sans Symbols 2 / Segoe UI Symbol.
func TestAutoFallbackCoversCommonSymbols(t *testing.T) {
	// Reset to lazy-init state so the test runs the real probe path.
	fontCacheMu.Lock()
	fontInitialized = false
	fontUsingDefaultInit = false
	fontParsed = nil
	fontErr = nil
	replacePrimaryFontSourceUnlocked(nil)
	clearAutoFallbacksUnlocked()
	fontCacheMu.Unlock()
	t.Cleanup(func() {
		if err := LoadFontFromBytes(goregular.TTF); err != nil {
			t.Fatalf("restore font: %v", err)
		}
	})

	faces := GetFontFaces(14)
	if len(faces) < 2 {
		t.Fatalf("expected primary + auto-fallback, got %d face(s)", len(faces))
	}

	// goregular is always bundled, so chevrons must resolve on every OS.
	for _, r := range []rune{'‹', '›'} {
		face := faceForRune(r, faces)
		if _, ok := face.GlyphAdvance(r); !ok {
			t.Errorf("rune %q U+%04X did not resolve in auto-fallback chain", r, r)
		}
	}

	// ⟳ depends on the per-OS symbol-font probe finding a candidate.
	// Apple Symbols ships on every macOS; on linux/windows it depends
	// on the distro / install, so we only enforce the invariant on
	// darwin where the candidate path is guaranteed.
	if runtime.GOOS == "darwin" {
		const reload = '⟳'
		face := faceForRune(reload, faces)
		if _, ok := face.GlyphAdvance(reload); !ok {
			t.Errorf("rune %q U+%04X did not resolve via symbol-font fallback", reload, reload)
		}
	}
}

func TestAutoFallbackRespectsUserFallbackPriority(t *testing.T) {
	// User-registered fallbacks must come before auto fallbacks so an
	// app can override coverage decisions.
	fontCacheMu.Lock()
	fontInitialized = false
	fontUsingDefaultInit = false
	fontParsed = nil
	fontErr = nil
	replacePrimaryFontSourceUnlocked(nil)
	clearAutoFallbacksUnlocked()
	for _, src := range fontFallbackSources {
		closeCloser(src)
	}
	fontFallbacks = nil
	fontFallbackSources = nil
	clearFontFaceCachesUnlocked()
	fontCacheMu.Unlock()
	t.Cleanup(func() {
		if err := LoadFontFromBytes(goregular.TTF); err != nil {
			t.Fatalf("restore font: %v", err)
		}
	})

	// First force lazy init so auto-fallbacks populate, then register
	// goregular as an explicit fallback and confirm it lands before
	// the auto-installed copies.
	_ = GetFontFaces(14)
	userFont, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatalf("parse goregular: %v", err)
	}
	fontCacheMu.Lock()
	fontFallbacks = append(fontFallbacks, userFont)
	fontFallbackSources = append(fontFallbackSources, nil)
	clearFontFaceCachesUnlocked()
	fontCacheMu.Unlock()

	faces := GetFontFaces(14)
	if len(faces) < 2 {
		t.Fatalf("expected primary + at least one fallback, got %d", len(faces))
	}
	// faces[0] is the primary; faces[1] must be the user-registered
	// fallback, not the auto-installed one.
	want := opentypeFaceFor(t, userFont, 14)
	if !sameFontFace(faces[1], want) {
		// Diagnostic: dump where user font ends up.
		idx := -1
		for i, f := range faces {
			if sameFontFace(f, want) {
				idx = i
				break
			}
		}
		t.Fatalf("user fallback expected at chain index 1, got index %d (chain len=%d)", idx, len(faces))
	}
}

// opentypeFaceFor creates a face from parsed font at the given size
// using the same options as GetFontFacesFor, so equality probes the
// same underlying glyph cache. Test-only.
func opentypeFaceFor(t *testing.T, parsed *opentype.Font, size float32) font.Face {
	t.Helper()
	f, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: 0})
	if err != nil {
		t.Fatalf("NewFace: %v", err)
	}
	return f
}

// sameFontFace returns true when two faces report the same metrics. Two
// faces built from the same *opentype.Font + identical options will be
// distinct *opentype.Face values, but their Metrics are equal — that's
// the cheapest invariant the test can pin without leaking unexported
// internals.
func sameFontFace(a, b font.Face) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Metrics() == b.Metrics()
}

func TestSetAutoLoadSystemCJKControlsLazyProbe(t *testing.T) {
	fontCacheMu.Lock()
	origAuto := fontAutoLoadSystemCJK
	origProbe := probeSystemCJKFontUnlocked
	fontCacheMu.Unlock()
	t.Cleanup(func() {
		fontCacheMu.Lock()
		probeSystemCJKFontUnlocked = origProbe
		fontCacheMu.Unlock()
		SetAutoLoadSystemCJK(origAuto)
		if err := LoadFontFromBytes(goregular.TTF); err != nil {
			t.Fatalf("restore font: %v", err)
		}
	})

	calls := 0
	fontCacheMu.Lock()
	probeSystemCJKFontUnlocked = func() (*opentype.Font, io.Closer) {
		calls++
		return nil, nil
	}
	fontCacheMu.Unlock()

	SetAutoLoadSystemCJK(false)
	fontCacheMu.Lock()
	fontInitialized = false
	fontUsingDefaultInit = false
	fontParsed = nil
	fontErr = nil
	clearFontFaceCachesUnlocked()
	fontCacheMu.Unlock()

	_ = GetFontFace(14)
	if calls != 0 {
		t.Fatalf("expected no CJK probe when disabled, got %d", calls)
	}

	SetAutoLoadSystemCJK(true)
	fontCacheMu.Lock()
	if fontInitialized {
		fontCacheMu.Unlock()
		t.Fatal("expected default font init to be reset after toggling on")
	}
	fontCacheMu.Unlock()

	_ = GetFontFace(14)
	if calls == 0 {
		t.Fatal("expected CJK probe after re-enabling auto load")
	}
}

// TestSystemCJKFontLoadedOnce pins that default init loads the system CJK
// font exactly ONCE. The auto-fallback chain wants a CJK entry too — so a
// later SetDefaultFont can't silently drop CJK coverage — and it used to
// get one by re-running the probe, which re-read and re-parsed the same
// file. A system CJK collection is tens of megabytes and sfnt keeps its
// bytes for the font's lifetime, so the duplicate stayed resident for the
// whole process.
func TestSystemCJKFontLoadedOnce(t *testing.T) {
	stub, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatalf("parse goregular: %v", err)
	}
	restoreProbe := probeSystemCJKFontUnlocked
	calls := 0

	fontCacheMu.Lock()
	probeSystemCJKFontUnlocked = func() (*opentype.Font, io.Closer) {
		calls++
		return stub, nil
	}
	// Back to lazy-init state so the real default path runs.
	fontInitialized = false
	fontUsingDefaultInit = false
	fontDefaultIsSystemCJK = false
	fontParsed = nil
	fontErr = nil
	replacePrimaryFontSourceUnlocked(nil)
	clearAutoFallbacksUnlocked()
	fontCacheMu.Unlock()

	t.Cleanup(func() {
		fontCacheMu.Lock()
		probeSystemCJKFontUnlocked = restoreProbe
		fontCacheMu.Unlock()
		if err := LoadFontFromBytes(goregular.TTF); err != nil {
			t.Fatalf("restore font: %v", err)
		}
	})

	_ = GetFontFaces(14)

	if calls != 1 {
		t.Fatalf("system CJK probe ran %d time(s), want exactly 1", calls)
	}

	fontCacheMu.Lock()
	primary := fontParsed
	fallbacks := append([]*opentype.Font(nil), autoFallbacks...)
	fontCacheMu.Unlock()

	if primary != stub {
		t.Fatalf("primary font = %p, want the probe result %p", primary, stub)
	}
	// The chain must still CARRY the CJK font: appendFallback drops it while
	// it equals the primary, but it has to be there the moment an app swaps
	// the primary to a Latin-only face.
	found := false
	for _, f := range fallbacks {
		if f == stub {
			found = true
			break
		}
	}
	if !found {
		t.Error("auto-fallback chain lost its system CJK entry")
	}
}

// TestParseFontFromFileSkipsUnparseableBytes pins that a candidate sfnt
// cannot parse does NOT cost its file size in allocations. The font
// candidate scans walk past system collections that sfnt rejects (STHeiti
// Medium and STHeiti Light, 56 MB each, on a stock macOS install), and
// reading each one in full before finding out dominated startup allocation.
func TestParseFontFromFileSkipsUnparseableBytes(t *testing.T) {
	const size = 16 << 20
	path := filepath.Join(t.TempDir(), "junk.ttc")
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatalf("write junk font: %v", err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, _, err := parseFirstFontFromFile(path); err == nil {
		t.Fatal("expected a junk file to fail parsing")
	}
	runtime.ReadMemStats(&after)

	if grew := after.TotalAlloc - before.TotalAlloc; grew > size/4 {
		t.Errorf("a failed parse allocated %d bytes for a %d-byte file; "+
			"the file should not be read in full before it parses", grew, size)
	}
}

// TestExtractFace0MatchesCollectionFace0 pins that slimming a .ttc down to
// face 0 is behaviour-preserving. qui only ever uses face 0, but sfnt keeps
// the bytes it parsed for the font's lifetime — so loading the collection
// whole charged the process for every weight it bundles. The rewrite is
// only safe if glyph identity and metrics survive it exactly.
func TestExtractFace0MatchesCollectionFace0(t *testing.T) {
	// Pick the first candidate that actually PARSES — the same one the
	// probe would install. Several macOS collections (STHeiti Medium and
	// Light) are rejected by sfnt, and skipping on those would leave this
	// test silently unrun on the machines it matters most for.
	var (
		path string
		data []byte
		full *opentype.Font
	)
	for _, candidate := range cjkFontCandidates() {
		raw, err := os.ReadFile(candidate)
		if err != nil || len(raw) < 4 || string(raw[0:4]) != "ttcf" {
			continue
		}
		coll, err := opentype.ParseCollection(raw)
		if err != nil {
			continue
		}
		face0, err := coll.Font(0)
		if err != nil {
			continue
		}
		path, data, full = candidate, raw, face0
		break
	}
	if full == nil {
		t.Skip("no parseable system font collection installed to compare against")
	}

	slim := extractFace0(data)
	if len(slim) >= len(data) {
		t.Fatalf("extractFace0(%s) did not shrink: %d -> %d bytes", path, len(data), len(slim))
	}
	thin, err := opentype.Parse(slim)
	if err != nil {
		t.Fatalf("extracted face 0 of %s does not parse: %v", path, err)
	}

	if got, want := thin.NumGlyphs(), full.NumGlyphs(); got != want {
		t.Fatalf("NumGlyphs = %d, want %d", got, want)
	}

	const ppem = 12
	var bufFull, bufThin sfnt.Buffer
	for _, r := range []rune{'A', 'z', '0', ' ', '中', '文', '日', '本', '，', '。', 'É', '€'} {
		gFull, err := full.GlyphIndex(&bufFull, r)
		if err != nil {
			t.Fatalf("collection GlyphIndex(%q): %v", r, err)
		}
		gThin, err := thin.GlyphIndex(&bufThin, r)
		if err != nil {
			t.Fatalf("extracted GlyphIndex(%q): %v", r, err)
		}
		if gFull != gThin {
			t.Errorf("rune %q: glyph index %d in collection, %d after extraction", r, gFull, gThin)
			continue
		}
		if gFull == 0 {
			continue // not covered by this font; nothing to compare
		}
		advFull, err := full.GlyphAdvance(&bufFull, gFull, fixed.I(ppem), font.HintingNone)
		if err != nil {
			t.Fatalf("collection GlyphAdvance(%q): %v", r, err)
		}
		advThin, err := thin.GlyphAdvance(&bufThin, gThin, fixed.I(ppem), font.HintingNone)
		if err != nil {
			t.Fatalf("extracted GlyphAdvance(%q): %v", r, err)
		}
		if advFull != advThin {
			t.Errorf("rune %q: advance %v in collection, %v after extraction", r, advFull, advThin)
		}
	}
}

// FontRegistryGeneration backs the per-widget layout caches in the widgets
// package (Label's rich text, InlineBox's paragraph): they hold shaped
// layouts whose face metrics a registry swap invalidates, and unlike the
// global memo in this package they cannot be cleared from here — they compare
// the counter instead. So the counter MUST move on every registry mutation.
func TestFontRegistryGenerationChanges(t *testing.T) {
	before := FontRegistryGeneration()
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("load font: %v", err)
	}
	after := FontRegistryGeneration()
	if after == before {
		t.Fatalf("generation did not change across a registry swap (%d)", before)
	}
	// Restore so later tests see the same font this one started with.
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if FontRegistryGeneration() == after {
		t.Error("generation did not change on the second swap either")
	}
}
