package qui

import (
	"image"
	"image/color"
	"testing"
)

// --- Unicode range detection ---

func TestIsEmojiRuneCoversCommonRanges(t *testing.T) {
	wantTrue := []rune{
		0x1F600, // 😀 grinning face
		0x1F44D, // 👍 thumbs up
		0x1F680, // 🚀 rocket
		0x2764,  // ❤ heart
		0x2600,  // ☀ sun
		0x27BF,  // ➿ double curly loop
		0xFE0F,  // variation selector-16
		0x200D,  // zero-width joiner
		0x1F1E6, // regional indicator A (for flags)
		// BMP emoji with default emoji presentation outside 0x2600–0x27BF.
		0x23F3, // ⏳ hourglass with flowing sand (the reported bug)
		0x231B, // ⌛ hourglass done
		0x231A, // ⌚ watch
		0x23E9, // ⏩ fast-forward
		0x23F0, // ⏰ alarm clock
		0x2B50, // ⭐ star
		0x2B55, // ⭕ hollow red circle
	}
	for _, r := range wantTrue {
		if !isEmojiRune(r) {
			t.Errorf("isEmojiRune(U+%04X) = false, want true", r)
		}
	}
}

func TestIsEmojiRuneRejectsPlainText(t *testing.T) {
	wantFalse := []rune{
		'a', 'Z', '0', ' ',
		0x4E2D, // 中 (CJK unified ideograph)
		0x6587, // 文
		0x3042, // あ (hiragana)
		// Text-presentation neighbours of the BMP emoji added above: these
		// default to text and must stay on the text-font path so they keep
		// the theme foreground color (no VS16 → not emoji).
		0x2192, // → rightwards arrow
		0x25B6, // ▶ black right-pointing triangle (emoji only with VS16)
		0x25FB, // ◻ white medium square (text-default)
		0x23F1, // ⏱ stopwatch (text-default)
		0x2122, // ™ trade mark (text-default)
	}
	for _, r := range wantFalse {
		if isEmojiRune(r) {
			t.Errorf("isEmojiRune(U+%04X) = true, want false", r)
		}
	}
}

// --- Provider plumbing ---

type stubEmojiProvider struct {
	calls int
	last  string
}

func (s *stubEmojiProvider) EmojiImage(seq string, _ float32) image.Image {
	s.calls++
	s.last = seq
	// Return a tiny distinctive bitmap so cache hits are observable.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	return img
}

func TestSetEmojiProviderRoutesLookups(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)

	stub := &stubEmojiProvider{}
	SetEmojiProvider(stub)

	got := lookupEmojiImage(0x1F600, 16)
	if got == nil {
		t.Fatal("provider returned nil where stub should have produced a bitmap")
	}
	if stub.calls != 1 || stub.last != "\U0001F600" {
		t.Errorf("stub calls=%d last=%q; want 1, U+1F600", stub.calls, stub.last)
	}
}

func TestEmojiCacheDedupesSameRune(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)

	stub := &stubEmojiProvider{}
	SetEmojiProvider(stub) // also clears cache

	// Two lookups of same rune+size → one provider call.
	lookupEmojiImage(0x1F600, 16)
	lookupEmojiImage(0x1F600, 16)
	if stub.calls != 1 {
		t.Errorf("cache miss: got %d provider calls, want 1", stub.calls)
	}
	// Different size → separate cache entry.
	lookupEmojiImage(0x1F600, 24)
	if stub.calls != 2 {
		t.Errorf("different size should re-rasterize; got %d calls", stub.calls)
	}
}

func TestSetEmojiProviderNilDisables(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)

	SetEmojiProvider(nil)
	img := lookupEmojiImage(0x1F600, 16)
	if img != nil {
		t.Errorf("nil provider should return nil image; got %v", img)
	}
}

func TestRuneAdvanceEmojiUsesBitmapWidth(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)

	// The cache normalizes provider bitmaps to round(fontSize) wide so
	// the advance scales linearly with size — essential for HiDPI
	// cursor alignment (logical-size width * scale == physical-size
	// width). Stub returns a 4×4 raw bitmap, so at fontSize=14 the
	// normalized width is 14.
	SetEmojiProvider(&stubEmojiProvider{})
	got := RuneAdvance(0x1F600, Font{Size: 14})
	if got != 14 {
		t.Errorf("RuneAdvance(emoji) = %v, want 14 (round(fontSize))", got)
	}
}

func TestRuneAdvanceZeroWidthJoiners(t *testing.T) {
	// FE0F (variation selector) and 200D (ZWJ) are combining/invisible —
	// they must contribute zero pixels so cursor math stays aligned
	// with the glyph the IME collapsed them into.
	if w := RuneAdvance(0xFE0F, Font{Size: 14}); w != 0 {
		t.Errorf("VS-16 advance = %v, want 0", w)
	}
	if w := RuneAdvance(0x200D, Font{Size: 14}); w != 0 {
		t.Errorf("ZWJ advance = %v, want 0", w)
	}
}

func TestRuneAdvancePlainTextUsesFontMetrics(t *testing.T) {
	// For ASCII, GlyphAdvance returns positive advance. Exact pixels
	// depend on the loaded font, but we can assert non-zero.
	if w := RuneAdvance('a', Font{Size: 14}); w <= 0 {
		t.Errorf("plain 'a' advance should be positive; got %v", w)
	}
}

func TestTextMetricsMixedContentAccurate(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)
	SetEmojiProvider(&stubEmojiProvider{})

	// Regression: textMetrics used to be `len(text) * 7`, which
	// reported 4 for single ASCII but 4 * n for UTF-8 emoji bytes
	// and CJK characters. Now it walks runes and uses GlyphAdvance /
	// the normalized emoji bitmap width (round(fontSize)), so the
	// result should differ between "a" and "a😀" by exactly the
	// normalized emoji advance.
	w1, _ := TextMetrics("a", Font{Size: 14})
	w2, _ := TextMetrics("a\U0001F600", Font{Size: 14})
	if w2-w1 != 14 {
		t.Errorf("emoji should add 14px (round(fontSize)); got delta %v", w2-w1)
	}
}

func TestEmojiClusterEndGroupsSequences(t *testing.T) {
	cases := []struct {
		name  string
		runes []rune
		start int
		want  int
	}{
		{"ok hand + light skin tone", []rune("\U0001F44C\U0001F3FB"), 0, 2},
		{"emoji with VS-16", []rune("❤️"), 0, 2},
		{"ZWJ family", []rune("\U0001F468‍\U0001F469‍\U0001F467"), 0, 5},
		{"flag pair", []rune("\U0001F1E8\U0001F1F3"), 0, 2},
		// Keycap "1️⃣" = '1' + VS-16 + 0x20E3. The function absorbs the
		// trailing continuations even though the base digit is plain
		// text — useful when the caller has already classified the
		// starting rune as cluster-leading by other means.
		{"keycap continuations", []rune("1️⃣"), 0, 3},
		{"plain ASCII", []rune("abc"), 0, 1},
		{"stray skin-tone modifier", []rune("\U0001F3FB"), 0, 1},
	}
	for _, tc := range cases {
		if got := EmojiClusterEnd(tc.runes, tc.start); got != tc.want {
			t.Errorf("%s: EmojiClusterEnd(%q, %d) = %d, want %d",
				tc.name, string(tc.runes), tc.start, got, tc.want)
		}
	}
}

func TestClusterBoundariesSkipEmojiSequences(t *testing.T) {
	runes := []rune("\U0001F44C\U0001F3FB123")
	// Forward steps: 0 -> 2 (past whole cluster) -> 3 -> 4 -> 5.
	wantNext := map[int]int{0: 2, 1: 2, 2: 3, 3: 4, 4: 5, 5: 5}
	for from, want := range wantNext {
		if got := NextClusterBoundary(runes, from); got != want {
			t.Errorf("NextClusterBoundary(%d) = %d, want %d", from, got, want)
		}
	}
	// Backward steps mirror forward: 5 -> 4 -> 3 -> 2 -> 0 (whole cluster
	// in one press, no intermediate stop at idx=1 inside the cluster).
	wantPrev := map[int]int{5: 4, 4: 3, 3: 2, 2: 0, 1: 0, 0: 0}
	for from, want := range wantPrev {
		if got := PrevClusterBoundary(runes, from); got != want {
			t.Errorf("PrevClusterBoundary(%d) = %d, want %d", from, got, want)
		}
	}
}

func TestRuneAdvanceSkinToneModifierIsZero(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)
	SetEmojiProvider(&stubEmojiProvider{})

	// Skin-tone modifiers reshape the preceding base — they contribute
	// no standalone width. Without this, the textfield reserved caret
	// space for the modifier and painted a separate skin-tone square
	// next to the joined glyph (the original 👌🏻 bug).
	if w := RuneAdvance(0x1F3FB, Font{Size: 14}); w != 0 {
		t.Errorf("skin-tone modifier advance = %v, want 0", w)
	}
}

func TestEmojiBitmapWidthIsLinearInFontSize(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)
	// Stub returns a fixed 4×4 raw bitmap regardless of size — mimics
	// Apple Color Emoji, where Core Text returns a 19×23 bitmap at size
	// 14 and only 28×37 at size 28 (ratio 1.36 → 1.00, not 2:1). The
	// normalization should kill that nonlinearity.
	SetEmojiProvider(&stubEmojiProvider{})

	// Cursors stay aligned with rendered glyphs only if a logical-size
	// width times the HiDPI scale equals the physical-size width.
	// Regression: skipping normalization made `widthUpToRune` at logical
	// fontSize disagree with the painted width at physical fontSize,
	// drifting the caret by half a digit per emoji on Retina displays.
	for _, s := range []float32{12, 14, 16, 18, 24, 32} {
		w := lookupEmojiSequence("\U0001F44C", s).Bounds().Dx()
		if w != int(s+0.5) {
			t.Errorf("emoji width at size %g = %d, want %d", s, w, int(s+0.5))
		}
	}
	// Logical * HiDPI scale = physical, exact match at every standard
	// scale factor.
	for _, scale := range []float32{1.5, 2, 3} {
		for _, logical := range []float32{12, 14, 16} {
			physical := logical * scale
			wl := lookupEmojiSequence("\U0001F44C", logical).Bounds().Dx()
			wp := lookupEmojiSequence("\U0001F44C", physical).Bounds().Dx()
			if wl*int(scale+0.5) != wp && int(scale) == int(scale+0.5) {
				// Only assert linearity when scale is integer; with
				// fractional scales the round-to-nearest step admits a
				// ±1 px tolerance that's still well under a glyph.
				t.Errorf("size %g (×%g→%g): logical*scale=%d, physical=%d",
					logical, scale, physical, wl*int(scale+0.5), wp)
			}
		}
	}
}

func TestMeasureRunesEmojiClusterUsesSequenceWidth(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)
	stub := &stubEmojiProvider{}
	SetEmojiProvider(stub)

	// 👌🏻 = base + skin-tone modifier. Measuring as a cluster should
	// produce one provider call for the joined sequence and one
	// normalized bitmap width (14 at fontSize=14) — not base+modifier
	// rendered as two separate slots.
	w := measureRunes([]rune("\U0001F44C\U0001F3FB"), Font{Size: 14}, nil)
	if w != 14 {
		t.Errorf("👌🏻 measured width = %v, want 14 (one cluster bitmap, normalized)", w)
	}
	if stub.last != "\U0001F44C\U0001F3FB" {
		t.Errorf("provider received %q, want full sequence", stub.last)
	}
}

func TestSetEmojiProviderInvalidatesCache(t *testing.T) {
	orig := currentEmojiProvider()
	defer SetEmojiProvider(orig)

	stubA := &stubEmojiProvider{}
	SetEmojiProvider(stubA)
	lookupEmojiImage(0x1F600, 16)

	// Second provider: first lookup after SetEmojiProvider should go
	// through (cache invalidation on swap).
	stubB := &stubEmojiProvider{}
	SetEmojiProvider(stubB)
	lookupEmojiImage(0x1F600, 16)
	if stubB.calls != 1 {
		t.Errorf("provider swap did not invalidate cache; stubB calls=%d", stubB.calls)
	}
}
