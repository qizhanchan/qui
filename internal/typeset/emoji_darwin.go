//go:build darwin && cgo

package typeset

import (
	"image"
	"runtime"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

// coreTextEmojiProvider uses macOS Core Text with Apple Color Emoji
// to rasterize emoji sequences into RGBA bitmaps. Handles the SBIX
// (PNG-embedded) color font format that Go's opentype can't read.
//
// Core Text shapes the whole input string as one paragraph, so emoji
// clusters that arrive as multi-rune UTF-8 (base + skin-tone modifier,
// regional-indicator flag pairs, ZWJ family/profession sequences,
// keycap suffixes) come back as the single combined glyph rather than
// per-component squares. The caller is responsible for splitting text
// into clusters before calling this — see EmojiClusterEnd in emoji.go.
type coreTextEmojiProvider struct{}

// EmojiImage rasterizes a UTF-8 emoji cluster via Core Text, already
// fitted to round(fontSize) pixels wide so normalizeEmojiBitmap passes
// it through without a second (blurring) resample.
func (coreTextEmojiProvider) EmojiImage(seq string, fontSize float32) image.Image {
	target := int(fontSize + 0.5)
	if target < 1 {
		target = 1
	}
	pix, w, h := macos.RenderEmoji(seq, float64(fontSize), target)
	if pix == nil {
		return nil
	}
	// Core Text produced premultiplied RGBA, which is image.RGBA's format.
	return &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}

// Auto-install the Core Text provider at package init. Callers can
// override with SetEmojiProvider(nil) to disable, or SetEmojiProvider(x)
// to swap in a custom (Twemoji atlas, etc.) implementation.
func init() {
	if runtime.GOOS == "darwin" {
		SetEmojiProvider(coreTextEmojiProvider{})
	}
}
