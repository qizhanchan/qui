package qui

import (
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"unicode"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"

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

// setTextFallback installs (or with nil removes) the platform fallback and
// invalidates every cached measurement, since text it now covers had a
// tofu advance before. Backends reach it through contract.Host.
func setTextFallback(tf contract.TextFallback) {
	if tf == nil {
		textFallback.Store(nil)
	} else {
		textFallback.Store(&textFallbackBox{tf})
	}
	// A platform text engine that draws color glyphs doubles as the emoji
	// provider — the browser has an emoji font even where the engine has
	// none (non-macOS builds ship no provider) — unless the app chose one.
	cur := currentEmojiProvider()
	_, ours := cur.(colorGlyphEmoji)
	if cg, ok := tf.(contract.ColorGlyphs); ok && (cur == nil || ours) {
		SetEmojiProvider(colorGlyphEmoji{cg})
	} else if ours {
		SetEmojiProvider(nil)
	}
	fallbackMasks.Range(func(k, _ any) bool { fallbackMasks.Delete(k); return true })
	fontRegistryGen.Add(1)
	invalidateTextLayoutCache()
}

func currentTextFallback() contract.TextFallback {
	if b := textFallback.Load(); b != nil {
		return b.tf
	}
	return nil
}

// isFallbackCluster reports whether a grapheme cluster should be handed to
// the platform text fallback: one is installed, and the cluster's base
// rune is visible text that no loaded face can draw.
func isFallbackCluster(cluster []rune) bool {
	if len(cluster) == 0 || currentTextFallback() == nil {
		return false
	}
	r := cluster[0]
	if r < 0x20 || unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Mn, r) {
		return false
	}
	return !textFacesHaveGlyph(r)
}

func fontDescFor(spec Font, size float32) contract.FontDesc {
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
func fallbackAdvance(cluster string, fontSpec Font) (float32, bool) {
	tf := currentTextFallback()
	if tf == nil {
		return 0, false
	}
	size := fontSpec.Size
	if size <= 0 {
		size = 14
	}
	return tf.Advance(cluster, fontDescFor(fontSpec, size))
}

type fallbackMaskKey struct {
	cluster string
	desc    contract.FontDesc
}

type fallbackMask struct {
	mask              *image.Alpha
	originX, baseline int
}

// fallbackMasks caches rasterized clusters; masks are color-free, so one
// serves every text color.
var fallbackMasks sync.Map // fallbackMaskKey → *fallbackMask (nil: declined)

func lookupFallbackMask(cluster string, desc contract.FontDesc) *fallbackMask {
	key := fallbackMaskKey{cluster, desc}
	if v, ok := fallbackMasks.Load(key); ok {
		return v.(*fallbackMask)
	}
	var m *fallbackMask
	if tf := currentTextFallback(); tf != nil {
		if a, ox, by, ok := tf.Mask(cluster, desc); ok && a != nil {
			m = &fallbackMask{mask: a, originX: ox, baseline: by}
		}
	}
	fallbackMasks.Store(key, m)
	return m
}

// drawFallbackGlyph draws one fallback cluster at display resolution:
// rasterized at fontSize × canvas scale and tinted with the text color,
// its baseline on the line's baseline.
func drawFallbackGlyph(c imageCanvas, line *shapedLine, glyph shaping.Glyph, penX, baseline float32, fontSpec Font, col Color) {
	start := glyph.ClusterIndex
	end := start + glyph.RuneCount
	if start < 0 || end > len(line.text) || start >= end {
		return
	}
	top := c.topCopy()
	s := top.matrix.avgScale()
	if s <= 0 {
		return
	}
	size := fontSpec.Size
	if size <= 0 {
		size = 14
	}
	m := lookupFallbackMask(string(line.text[start:end]), fontDescFor(fontSpec, size*s))
	if m == nil {
		return
	}
	img := tintMask(m.mask, col)
	b := img.Bounds()
	rect := Rect{
		X: penX - float32(m.originX)/s,
		Y: baseline - float32(m.baseline)/s,
		W: float32(b.Dx()) / s,
		H: float32(b.Dy()) / s,
	}
	mat := top.matrix
	if mat.B != 0 || mat.C != 0 || mat.A <= 0 || mat.D <= 0 || absf(mat.A-mat.D) > 1e-3 {
		c.DrawImage(img, rect)
		return
	}
	// Uniform positive scale: snap to a whole device pixel and blit the
	// raster 1:1, as drawEmojiGlyph does, so the glyph stays crisp.
	dev := mat.TransformRect(rect)
	dev = Rect{X: roundf(dev.X), Y: roundf(dev.Y), W: float32(b.Dx()), H: float32(b.Dy())}
	c.backend.DrawImage(img, dev, top.clip)
}

// tintMask colors a coverage mask with col as premultiplied RGBA.
func tintMask(mask *image.Alpha, col Color) *image.RGBA {
	b := mask.Bounds()
	out := image.NewRGBA(b)
	c := toRGBA(col)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := uint32(mask.AlphaAt(x, y).A) * uint32(c.A) / 255
			if a == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(uint32(c.R) * a / 255),
				G: uint8(uint32(c.G) * a / 255),
				B: uint8(uint32(c.B) * a / 255),
				A: uint8(a),
			})
		}
	}
	return out
}

func roundf(v float32) float32 {
	if v < 0 {
		return float32(int(v - 0.5))
	}
	return float32(int(v + 0.5))
}

// fallbackShapingOutput lays out one fallback cluster as an atomic glyph
// (cf. emojiShapingOutput): EmptyGlyph with the platform's advance.
func fallbackShapingOutput(input shaping.Input, start, end int, fontSpec Font) shaping.Output {
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

// drawAtomicGlyph draws a glyph the shaper did not produce from a font:
// an emoji bitmap, or a platform-fallback cluster.
func drawAtomicGlyph(c imageCanvas, line *shapedLine, glyph shaping.Glyph, penX, baseline, capHeight float32, fontSpec Font, col Color) {
	start, end := glyph.ClusterIndex, glyph.ClusterIndex+glyph.RuneCount
	if start >= 0 && end <= len(line.text) && start < end && !isEmojiCluster(line.text[start:end]) &&
		isFallbackCluster(line.text[start:end]) {
		drawFallbackGlyph(c, line, glyph, penX, baseline, fontSpec, col)
		return
	}
	drawEmojiGlyph(c, line, glyph, penX, baseline, capHeight, fontSpec)
}

// colorGlyphEmoji adapts a platform's color glyph rendering to
// EmojiProvider.
type colorGlyphEmoji struct{ cg contract.ColorGlyphs }

func (e colorGlyphEmoji) EmojiImage(seq string, fontSize float32) image.Image {
	img, ok := e.cg.ColorImage(seq, contract.FontDesc{Size: fontSize, Weight: 400})
	if !ok || img == nil {
		return nil
	}
	return img
}
