package typeset

import (
	"sync"

	gtfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/font/opentype"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Synthetic bold for fallback glyphs: a bold run whose glyphs come from a
// fallback face (CJK, symbols — the chain's faces are regular weights)
// would otherwise draw thin next to its bold primary-face neighbors.
// Browsers embolden such glyphs; the renderers draw them a second time one
// logical pixel to the right, as they do for gfx.Font.Bold.

var fallbackFaceMemo sync.Map // *gtfont.Face -> bool

// RunNeedsSyntheticBold reports whether glyphs shaped with face, for a
// run styled spec, should be emboldened: spec asks for semibold or more
// and face is one of the fallback chain's faces.
func RunNeedsSyntheticBold(face *gtfont.Face, spec gfx.Font) bool {
	if face == nil || spec.Bold || effectiveWeight(spec) < gfx.FontWeightSemiBold {
		return false
	}
	if v, ok := fallbackFaceMemo.Load(face); ok {
		return v.(bool)
	}
	src := SourceFontForShapingFace(face)
	fb := src != nil && isFallbackSource(src)
	fallbackFaceMemo.Store(face, fb)
	return fb
}

func effectiveWeight(spec gfx.Font) gfx.FontWeight {
	if spec.Weight != 0 {
		return spec.Weight
	}
	if spec.Bold {
		return gfx.FontWeightBold
	}
	return gfx.FontWeightNormal
}

func isFallbackSource(src *opentype.Font) bool {
	FontCacheMu.RLock()
	defer FontCacheMu.RUnlock()
	for _, f := range FontFallbacks {
		if f == src {
			return true
		}
	}
	for _, f := range AutoFallbacks {
		if f == src {
			return true
		}
	}
	return false
}
