package qui

import (
	"image"
	"sync"

	xdraw "golang.org/x/image/draw"
)

// Emoji rendering — Go's standard opentype library has no support for
// color bitmap glyphs (Apple Color Emoji's sbix table, or CBDT/COLR
// variants). When a rune has no outline in the active font, font.Drawer
// silently skips it and the emoji disappears.
//
// The workaround is an EmojiProvider pluggable interface that returns a
// pre-rasterized RGBA bitmap per grapheme cluster. Text shaping inserts the
// bitmap as an atomic glyph in the surrounding bidi and wrapping flow, then
// the renderer blits it at the shaped advance.
//
// Platform backends live in emoji_darwin.go (Core Text via Cgo) and
// emoji_other.go (stub — Latin/CJK still renders; emoji show as
// nothing). Applications can also install a custom provider backed
// by e.g. Twemoji PNG assets via SetEmojiProvider.

// EmojiProvider returns a rasterized RGBA bitmap for the given emoji
// sequence at approximately the given font size. The sequence is a
// UTF-8 string covering one extended grapheme cluster — a single base
// emoji rune, optionally followed by variation selectors, skin-tone
// modifiers, ZWJ-joined components, keycap suffixes, or tag sequences.
// Returns nil when the sequence isn't a supported emoji (caller falls
// through to plain text rendering). The bitmap's origin is the
// top-left; blitting pairs it with text's baseline by centering within
// one line height.
type EmojiProvider interface {
	EmojiImage(seq string, fontSize float32) image.Image
}

var (
	emojiMu       sync.RWMutex
	emojiProvider EmojiProvider
)

// SetEmojiProvider installs the provider used by DrawText for color
// emoji. Pass nil to disable emoji rendering entirely.
func SetEmojiProvider(p EmojiProvider) {
	emojiMu.Lock()
	defer emojiMu.Unlock()
	emojiProvider = p
	// Invalidate the per-size emoji cache — new provider produces
	// different bitmaps.
	emojiCache = map[emojiKey]image.Image{}
}

// currentEmojiProvider returns the active provider under a read lock.
func currentEmojiProvider() EmojiProvider {
	emojiMu.RLock()
	defer emojiMu.RUnlock()
	return emojiProvider
}

// --- Emoji detection via Unicode ranges --------------------------------
//
// Conservative ranges that capture the commonly-used emoji set. Not
// exhaustive — a full implementation would consult Unicode's emoji-data.txt.
// This covers: miscellaneous symbols, dingbats, emoticons, transport,
// symbols & pictographs, supplemental symbols, and variation selector-16.

// isEmojiRune reports whether the rune should be routed through the
// EmojiProvider rather than the text font.
func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // broad emoji plane coverage
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols + dingbats (☀★✓♠ etc.)
		return true
	// BMP code points with default emoji presentation (Unicode
	// Emoji_Presentation=Yes) that fall OUTSIDE 0x2600–0x27BF. These
	// render as color emoji even without a VS16 selector — e.g. ⏳ lives
	// in the Miscellaneous Technical block (U+23F3) which no range above
	// covers, so it was being sent to the text font and dropped. Only
	// the emoji-presentation members are listed; their text-presentation
	// neighbours (→ ▶ ◻ etc.) stay on the text-font path so they keep
	// the theme foreground color.
	case r == 0x231A || r == 0x231B: // ⌚ ⌛
		return true
	case r >= 0x23E9 && r <= 0x23EC: // ⏩ ⏪ ⏫ ⏬
		return true
	case r == 0x23F0 || r == 0x23F3: // ⏰ ⏳
		return true
	case r == 0x25FD || r == 0x25FE: // ◽ ◾
		return true
	case r == 0x2B1B || r == 0x2B1C || r == 0x2B50 || r == 0x2B55: // ⬛ ⬜ ⭐ ⭕
		return true
	case r == 0xFE0F: // variation selector-16 (emoji presentation)
		return true
	case r == 0x200D: // zero-width joiner (for emoji sequences)
		return true
	case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators (flags)
		return true
	}
	return false
}

// isEmojiContinuation reports whether the rune extends an emoji cluster
// started by some preceding base rune. These runes carry zero standalone
// advance — they reshape the previous glyph (skin tone, presentation
// hint, keycap, tag sequence) rather than painting their own pixel
// extent. The renderer always groups them with the base via
// EmojiClusterEnd before calling the EmojiProvider; the per-rune advance
// helpers return 0 for them so cursor math matches the joined glyph's
// real width.
func isEmojiContinuation(r rune) bool {
	switch {
	case r == 0xFE0F, r == 0x200D, r == 0x20E3:
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // Fitzpatrick skin-tone modifiers
		return true
	case r >= 0xE0020 && r <= 0xE007F: // tag sequences (subnational flags)
		return true
	}
	return false
}

// EmojiClusterEnd returns the exclusive end index of the emoji cluster
// starting at runes[start]. A cluster is one base emoji rune plus any
// trailing skin-tone modifiers, variation selectors, ZWJ-joined
// components, keycap combiners, or tag-sequence runes. Regional
// indicators consume one partner (two-letter flag glyph). Non-emoji
// starts return start+1.
//
// Why this exists: passing each sub-rune to the EmojiProvider
// separately produces visibly broken output — e.g. 👌🏻 paints as
// 👌 followed by a colored skin-tone square because the modifier is
// rendered as if it stood alone. Joining the cluster into one UTF-8
// string lets the provider's text shaper (Core Text on macOS) produce
// the combined glyph.
func EmojiClusterEnd(runes []rune, start int) int {
	if start < 0 || start >= len(runes) {
		return start
	}
	end := start + 1
	if runes[start] >= 0x1F1E6 && runes[start] <= 0x1F1FF {
		if end < len(runes) && runes[end] >= 0x1F1E6 && runes[end] <= 0x1F1FF {
			end++
		}
		return end
	}
	for end < len(runes) {
		r := runes[end]
		if r == 0x200D {
			if end+1 < len(runes) {
				end += 2
				continue
			}
			return end
		}
		if !isEmojiContinuation(r) {
			return end
		}
		end++
	}
	return end
}

// NextClusterBoundary returns the rune index one cluster forward from
// idx. For an emoji-cluster start it jumps past the whole sequence
// (base + modifiers / ZWJ joins); for any other rune it advances by
// one. Used for caret navigation so a multi-rune emoji moves as a
// single visual unit under the arrow keys, Backspace, and Delete.
func NextClusterBoundary(runes []rune, idx int) int {
	return nextGraphemeBoundary(runes, idx)
}

// PrevClusterBoundary returns the rune index one cluster backward from
// idx. Walks forward from 0 to find the largest cluster boundary
// strictly less than idx — necessary because clusters are defined
// looking forward from their base (skin-tone modifiers, ZWJ chains).
// A naive "skip backwards past continuations" rule would mishandle
// ZWJ-joined sequences where the joining direction matters.
func PrevClusterBoundary(runes []rune, idx int) int {
	return prevGraphemeBoundary(runes, idx)
}

// --- Glyph cache --------------------------------------------------------

// emojiKey caches per-sequence bitmaps keyed by (UTF-8 sequence,
// integer pixel size). Font sizes are quantized to int because
// rasterizing at fractional sizes isn't visually meaningful and
// explodes the cache.
type emojiKey struct {
	seq  string
	size int16
}

var (
	emojiCacheMu sync.RWMutex
	emojiCache   = map[emojiKey]image.Image{}
)

// lookupEmojiSequence returns a cached bitmap for the given UTF-8
// emoji cluster (one rune or a sequence — base + modifiers, ZWJ
// composition, regional-indicator pair, etc.), or asks the provider
// for a fresh one. Nil means the provider declined the sequence.
//
// The cached bitmap's width is forced to round(fontSize) so a layout
// query at logical size and a render query at physical size return
// widths that differ by exactly the HiDPI scale. Apple Color Emoji
// ships sbix at {20,32,40,48,64,96,160}: below the smallest sbix Core
// Text returns a bitmap that's wider than the requested em (19px at
// size 14, 21px at size 16), so the raw bitmap width does NOT scale
// linearly with font size — caching it directly drifts the cursor by
// up to half a digit width on HiDPI displays.
func lookupEmojiSequence(seq string, fontSize float32) image.Image {
	provider := currentEmojiProvider()
	if provider == nil || seq == "" {
		return nil
	}
	if fontSize <= 0 {
		fontSize = 14
	}
	key := emojiKey{seq: seq, size: int16(fontSize + 0.5)}

	emojiCacheMu.RLock()
	img, ok := emojiCache[key]
	emojiCacheMu.RUnlock()
	if ok {
		return img
	}

	raw := provider.EmojiImage(seq, fontSize)
	img = normalizeEmojiBitmap(raw, fontSize)

	emojiCacheMu.Lock()
	emojiCache[key] = img // cache nil too — avoids re-querying unknown sequences
	emojiCacheMu.Unlock()
	return img
}

// normalizeEmojiBitmap rescales the provider's raw bitmap so its width
// equals round(fontSize). Preserves the natural aspect ratio so emoji
// proportions look right at any size — width-only rescaling would
// squash glyphs horizontally.
func normalizeEmojiBitmap(raw image.Image, fontSize float32) image.Image {
	if raw == nil {
		return nil
	}
	target := int(fontSize + 0.5)
	if target < 1 {
		target = 1
	}
	bounds := raw.Bounds()
	if bounds.Dx() == target && bounds.Min.X == 0 && bounds.Min.Y == 0 {
		return raw
	}
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return raw
	}
	aspect := float32(bounds.Dy()) / float32(bounds.Dx())
	h := int(float32(target)*aspect + 0.5)
	if h < 1 {
		h = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, target, h))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), raw, bounds, xdraw.Over, nil)
	return out
}

// lookupEmojiImage is the single-rune convenience wrapper around
// lookupEmojiSequence. Retained for callers that compute per-rune
// advances (where the rune is known to be a cluster base, e.g. plain
// ASCII-or-emoji text without modifiers).
func lookupEmojiImage(r rune, fontSize float32) image.Image {
	return lookupEmojiSequence(string(r), fontSize)
}
