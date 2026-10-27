package typeset

import (
	"image"
	"sync"
	"sync/atomic"
	"unicode"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Platform text fallback.
//
// Text is shaped and rasterized from the fonts the engine has loaded. A
// desktop build finds system fonts on disk (CJK included, see font.go); a
// browser build cannot read them and embeds only Latin faces, so Chinese
// would render as tofu. A platform backend that has a text engine of its
// own — the browser's — installs a contract.TextFallback, and any cluster
// no loaded font covers is then measured and drawn by it.
//
// Such a cluster joins the layout as one atomic glyph, the same mechanism
// color emoji use (text_shaping.go): its advance comes from the platform,
// so wrapping, carets, selection and hit testing work unchanged, and it is
// drawn as a coverage mask tinted with the text color. Loading a real font
// that covers those runes (LoadFallbackFontFromBytes) always wins, since
// covered runes never reach the fallback.

var textFallback atomic.Pointer[textFallbackBox]

type textFallbackBox struct{ tf contract.TextFallback }

// SetTextFallback installs (or with nil removes) the platform fallback and
// invalidates every cached measurement, since text it now covers had a
// tofu advance before. Backends reach it through contract.Host.
func SetTextFallback(tf contract.TextFallback) {
	if tf == nil {
		textFallback.Store(nil)
	} else {
		textFallback.Store(&textFallbackBox{tf})
	}
	// A platform text engine that draws color glyphs doubles as the emoji
	// provider — the browser has an emoji font even where the engine has
	// none (non-macOS builds ship no provider) — unless the app chose one.
	cur := CurrentEmojiProvider()
	_, ours := cur.(ColorGlyphEmoji)
	if cg, ok := tf.(contract.ColorGlyphs); ok && (cur == nil || ours) {
		SetEmojiProvider(ColorGlyphEmoji{cg})
	} else if ours {
		SetEmojiProvider(nil)
	}
	fallbackMasks.Range(func(k, _ any) bool { fallbackMasks.Delete(k); return true })
	fontRegistryGen.Add(1)
	InvalidateTextLayoutCache()
}

func currentTextFallback() contract.TextFallback {
	if b := textFallback.Load(); b != nil {
		return b.tf
	}
	return nil
}

// IsFallbackCluster reports whether a grapheme cluster should be handed to
// the platform text fallback: one is installed, and the cluster's base
// rune is visible text that no loaded face can draw.
func IsFallbackCluster(cluster []rune) bool {
	if len(cluster) == 0 || currentTextFallback() == nil {
		return false
	}
	r := cluster[0]
	if r < 0x20 || unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Mn, r) {
		return false
	}
	return !TextFacesHaveGlyph(r)
}

func FontDescFor(spec gfx.Font, size float32) contract.FontDesc {
	weight := int(spec.Weight)
	if weight == 0 {
		weight = 400
		if spec.Bold {
			weight = 700
		}
	}
	return contract.FontDesc{Family: spec.Family, Size: size, Weight: weight, Italic: spec.Italic}
}

// fallbackAdvance is the cluster's advance at fontSpec's logical size.
func fallbackAdvance(cluster string, fontSpec gfx.Font) (float32, bool) {
	tf := currentTextFallback()
	if tf == nil {
		return 0, false
	}
	size := fontSpec.Size
	if size <= 0 {
		size = 14
	}
	return tf.Advance(cluster, FontDescFor(fontSpec, size))
}

type fallbackMaskKey struct {
	cluster string
	desc    contract.FontDesc
}

type fallbackMask struct {
	Mask              *image.Alpha
	OriginX, Baseline int
}

// fallbackMasks caches rasterized clusters; masks are color-free, so one
// serves every text color.
var fallbackMasks sync.Map // fallbackMaskKey → *fallbackMask (nil: declined)

func LookupFallbackMask(cluster string, desc contract.FontDesc) *fallbackMask {
	key := fallbackMaskKey{cluster, desc}
	if v, ok := fallbackMasks.Load(key); ok {
		return v.(*fallbackMask)
	}
	var m *fallbackMask
	if tf := currentTextFallback(); tf != nil {
		if a, ox, by, ok := tf.Mask(cluster, desc); ok && a != nil {
			m = &fallbackMask{Mask: a, OriginX: ox, Baseline: by}
		}
	}
	fallbackMasks.Store(key, m)
	return m
}

func Roundf(v float32) float32 {
	if v < 0 {
		return float32(int(v - 0.5))
	}
	return float32(int(v + 0.5))
}

// fallbackShapingOutput lays out one fallback cluster as an atomic glyph
// (cf. emojiShapingOutput): EmptyGlyph with the platform's advance.
func fallbackShapingOutput(input shaping.Input, start, end int, fontSpec gfx.Font) shaping.Output {
	advance, _ := fallbackAdvance(string(input.Text[start:end]), fontSpec)
	fixedAdvance := pxToFixed(advance)
	return shaping.Output{
		Advance:   fixedAdvance,
		Size:      input.Size,
		Direction: input.Direction,
		Face:      input.Face,
		Runes:     shaping.Range{Offset: start, Count: end - start},
		Glyphs: []shaping.Glyph{{
			GlyphID:      gtfont.EmptyGlyph,
			ClusterIndex: start,
			RuneCount:    end - start,
			GlyphCount:   1,
			Advance:      fixedAdvance,
			XAdvance:     fixedAdvance,
			Width:        fixedAdvance,
			Height:       -input.Size,
		}},
	}
}

// ColorGlyphEmoji adapts a platform's color glyph rendering to
// EmojiProvider.
type ColorGlyphEmoji struct{ cg contract.ColorGlyphs }

func (e ColorGlyphEmoji) EmojiImage(seq string, fontSize float32) image.Image {
	img, ok := e.cg.ColorImage(seq, contract.FontDesc{Size: fontSize, Weight: 400})
	if !ok || img == nil {
		return nil
	}
	return img
}
